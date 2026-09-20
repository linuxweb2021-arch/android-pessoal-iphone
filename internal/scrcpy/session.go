package scrcpy

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const Version = "3.3.4"

var ErrStaleControl = errors.New("stale control message")

type Config struct {
	ADBPath        string
	Serial         string
	ServerPath     string
	LocalPort      int
	MaxSize        int
	MaxFPS         int
	VideoBitrate   int
	AudioBitrate   int
	StartupTimeout time.Duration
}

type Packet struct {
	Data     []byte
	PTS      time.Duration
	Config   bool
	KeyFrame bool
}

type Session struct {
	cfg Config
	log *slog.Logger

	video   net.Conn
	audio   net.Conn
	control net.Conn
	process *exec.Cmd

	Width  uint32
	Height uint32

	mu             sync.Mutex
	activePointers map[uint32]struct{}
	lastHeartbeat  time.Time
	lastSequence   uint64
	screenRevision int64
}

func Start(ctx context.Context, cfg Config, logger *slog.Logger) (*Session, error) {
	if cfg.ADBPath == "" || cfg.ServerPath == "" {
		return nil, errors.New("adb and scrcpy server paths are required")
	}
	if cfg.LocalPort == 0 {
		cfg.LocalPort = 27183
	}
	if cfg.MaxSize == 0 {
		cfg.MaxSize = 1280
	}
	if cfg.MaxFPS == 0 {
		cfg.MaxFPS = 60
	}
	if cfg.VideoBitrate == 0 {
		cfg.VideoBitrate = 6_000_000
	}
	if cfg.AudioBitrate == 0 {
		cfg.AudioBitrate = 192_000
	}
	if cfg.StartupTimeout == 0 {
		cfg.StartupTimeout = 15 * time.Second
	}

	adb := func(args ...string) *exec.Cmd {
		base := make([]string, 0, len(args)+2)
		if cfg.Serial != "" {
			base = append(base, "-s", cfg.Serial)
		}
		return exec.CommandContext(ctx, cfg.ADBPath, append(base, args...)...)
	}
	if output, err := adb("push", cfg.ServerPath, "/data/local/tmp/android-pessoal-scrcpy-server.jar").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("push scrcpy server: %w: %s", err, output)
	}
	scid := uint32(time.Now().UnixNano()) & 0x7fffffff
	socketName := fmt.Sprintf("localabstract:scrcpy_%08x", scid)
	port := "tcp:" + strconv.Itoa(cfg.LocalPort)
	_ = adb("forward", "--remove", port).Run()
	if output, err := adb("forward", port, socketName).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("create adb forward: %w: %s", err, output)
	}

	args := []string{
		"shell", "CLASSPATH=/data/local/tmp/android-pessoal-scrcpy-server.jar", "app_process", "/",
		"com.genymobile.scrcpy.Server", Version,
		fmt.Sprintf("scid=%08x", scid), "log_level=info", "tunnel_forward=true",
		"send_dummy_byte=false", "send_device_meta=false", "video_codec=h264", "audio_codec=opus",
		"video_codec_options=i-frame-interval=1",
		"max_size=" + strconv.Itoa(cfg.MaxSize), "max_fps=" + strconv.Itoa(cfg.MaxFPS),
		"video_bit_rate=" + strconv.Itoa(cfg.VideoBitrate),
		"audio_bit_rate=" + strconv.Itoa(cfg.AudioBitrate),
	}
	cmd := adb(args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start scrcpy server: %w", err)
	}
	logStream := func(reader io.Reader) {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			logger.Info("scrcpy", "message", scanner.Text())
		}
	}
	go logStream(stderr)
	go logStream(stdout)

	s := &Session{cfg: cfg, log: logger, process: cmd, activePointers: make(map[uint32]struct{})}
	cleanup := func(err error) (*Session, error) {
		s.Close()
		return nil, err
	}
	deadline := time.Now().Add(cfg.StartupTimeout)
	var videoMeta [12]byte
	var audioMeta [4]byte
	for time.Now().Before(deadline) {
		s.video, s.audio, s.control, err = dialSocketGroup(ctx, cfg.LocalPort)
		if err == nil {
			_ = s.video.SetReadDeadline(time.Now().Add(time.Second))
			_ = s.audio.SetReadDeadline(time.Now().Add(time.Second))
			_, videoErr := io.ReadFull(s.video, videoMeta[:])
			_, audioErr := io.ReadFull(s.audio, audioMeta[:])
			if videoErr == nil && audioErr == nil {
				_ = s.video.SetReadDeadline(time.Time{})
				_ = s.audio.SetReadDeadline(time.Time{})
				break
			}
			err = fmt.Errorf("video metadata: %v; audio metadata: %v", videoErr, audioErr)
		}
		for _, conn := range []net.Conn{s.video, s.audio, s.control} {
			if conn != nil {
				_ = conn.Close()
			}
		}
		s.video, s.audio, s.control = nil, nil, nil
		select {
		case <-ctx.Done():
			return cleanup(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if s.video == nil {
		return cleanup(fmt.Errorf("connect scrcpy sockets: %w", err))
	}
	s.Width = binary.BigEndian.Uint32(videoMeta[4:8])
	s.Height = binary.BigEndian.Uint32(videoMeta[8:12])
	go s.watchTouches(ctx)
	return s, nil
}

func dialSocketGroup(ctx context.Context, port int) (net.Conn, net.Conn, net.Conn, error) {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
	video, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, nil, err
	}
	audio, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		video.Close()
		return nil, nil, nil, err
	}
	control, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		video.Close()
		audio.Close()
		return nil, nil, nil, err
	}
	return video, audio, control, nil
}

func (s *Session) ReadVideo() (Packet, error) { return readPacket(s.video) }
func (s *Session) ReadAudio() (Packet, error) { return readPacket(s.audio) }

func readPacket(reader io.Reader) (Packet, error) {
	var header [12]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return Packet{}, err
	}
	value := binary.BigEndian.Uint64(header[:8])
	size := binary.BigEndian.Uint32(header[8:])
	if size > 8<<20 {
		return Packet{}, fmt.Errorf("scrcpy packet too large: %d", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return Packet{}, err
	}
	return Packet{
		Data: data, PTS: time.Duration(value&((uint64(1)<<62)-1)) * time.Microsecond,
		Config: value&(uint64(1)<<63) != 0, KeyFrame: value&(uint64(1)<<62) != 0,
	}, nil
}

type ControlEnvelope struct {
	Version        string  `json:"version"`
	SessionID      string  `json:"sessionId"`
	Generation     int64   `json:"generation"`
	ScreenRevision int64   `json:"screenRevision"`
	ScreenWidth    uint32  `json:"screenWidth"`
	ScreenHeight   uint32  `json:"screenHeight"`
	Sequence       uint64  `json:"sequence"`
	GestureID      uint64  `json:"gestureId"`
	PointerID      uint32  `json:"pointerId"`
	Action         string  `json:"action"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	Pressure       float64 `json:"pressure"`
	Key            string  `json:"key"`
	Text           string  `json:"text"`
}

func (s *Session) HandleControl(data []byte, sessionID string, generation int64) error {
	var event ControlEnvelope
	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}
	if event.Version != "control.v1" || event.SessionID != sessionID || event.Generation != generation {
		return errors.New("control authorization mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastHeartbeat = time.Now()
	switch event.Action {
	case "heartbeat":
		return nil
	case "cancelAll":
		return s.cancelAllLocked()
	case "key":
		return s.writeKeyLocked(event.Key)
	case "text":
		return s.writeTextLocked(event.Text)
	case "down", "move", "up", "cancel":
		if event.ScreenRevision <= 0 {
			return ErrStaleControl
		}
		if s.screenRevision != 0 && event.ScreenRevision != s.screenRevision {
			if err := s.cancelAllLocked(); err != nil {
				return err
			}
		}
		_, active := s.activePointers[event.PointerID]
		if event.Action == "down" {
			if active {
				return ErrStaleControl
			}
		} else if event.Sequence <= s.lastSequence || !active {
			return ErrStaleControl
		}
		s.screenRevision = event.ScreenRevision
		if event.Sequence > s.lastSequence {
			s.lastSequence = event.Sequence
		}
		if event.X < 0 || event.X > 1 || event.Y < 0 || event.Y > 1 || math.IsNaN(event.X) || math.IsNaN(event.Y) ||
			event.ScreenWidth == 0 || event.ScreenHeight == 0 || event.ScreenWidth > 8192 || event.ScreenHeight > 8192 {
			return errors.New("invalid touch coordinates")
		}
		return s.writeTouchLocked(event)
	default:
		return errors.New("unsupported control action")
	}
}

func (s *Session) writeTextLocked(text string) error {
	data := []byte(text)
	if len(data) == 0 || len(data) > (256<<10)-14 {
		return errors.New("invalid text length")
	}
	message := make([]byte, 14+len(data))
	message[0] = 9
	binary.BigEndian.PutUint64(message[1:9], uint64(time.Now().UnixNano()))
	message[9] = 1
	binary.BigEndian.PutUint32(message[10:14], uint32(len(data)))
	copy(message[14:], data)
	_, err := s.control.Write(message)
	return err
}

func (s *Session) writeTouchLocked(event ControlEnvelope) error {
	width, height := event.ScreenWidth, event.ScreenHeight
	action := byte(2)
	switch event.Action {
	case "down":
		action = 0
		s.activePointers[event.PointerID] = struct{}{}
	case "up":
		action = 1
	case "cancel":
		action = 3
	}
	var message [32]byte
	message[0] = 2
	message[1] = action
	binary.BigEndian.PutUint64(message[2:10], uint64(event.PointerID))
	binary.BigEndian.PutUint32(message[10:14], uint32(math.Round(event.X*float64(width-1))))
	binary.BigEndian.PutUint32(message[14:18], uint32(math.Round(event.Y*float64(height-1))))
	binary.BigEndian.PutUint16(message[18:20], uint16(width))
	binary.BigEndian.PutUint16(message[20:22], uint16(height))
	pressure := uint16(math.Round(math.Max(0, math.Min(1, event.Pressure)) * 65535))
	if pressure == 0 && (event.Action == "down" || event.Action == "move") {
		pressure = 65535
	}
	binary.BigEndian.PutUint16(message[22:24], pressure)
	if _, err := s.control.Write(message[:]); err != nil {
		return err
	}
	if event.Action == "up" || event.Action == "cancel" {
		delete(s.activePointers, event.PointerID)
	}
	return nil
}

func (s *Session) writeKeyLocked(key string) error {
	keycode := uint32(0)
	switch key {
	case "back":
		keycode = 4
	case "home":
		keycode = 3
	case "recents":
		keycode = 187
	default:
		return errors.New("unsupported Android key")
	}
	var message [14]byte
	message[0] = 0
	binary.BigEndian.PutUint32(message[2:6], keycode)
	if _, err := s.control.Write(message[:]); err != nil {
		return err
	}
	message[1] = 1
	_, err := s.control.Write(message[:])
	return err
}

func (s *Session) ResetVideo() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.control.Write([]byte{17})
	return err
}

func (s *Session) cancelAllLocked() error {
	for pointerID := range s.activePointers {
		event := ControlEnvelope{PointerID: pointerID, Action: "cancel", X: 0, Y: 0, ScreenWidth: s.Width, ScreenHeight: s.Height}
		if err := s.writeTouchLocked(event); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) watchTouches(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if len(s.activePointers) > 0 && time.Since(s.lastHeartbeat) > 1500*time.Millisecond {
				_ = s.cancelAllLocked()
			}
			s.mu.Unlock()
		}
	}
}

func (s *Session) Close() error {
	s.mu.Lock()
	if s.control != nil {
		_ = s.cancelAllLocked()
	}
	s.mu.Unlock()
	for _, conn := range []net.Conn{s.video, s.audio, s.control} {
		if conn != nil {
			_ = conn.Close()
		}
	}
	if s.process != nil && s.process.Process != nil {
		_ = s.process.Process.Kill()
		_, _ = s.process.Process.Wait()
	}
	args := []string{}
	if s.cfg.Serial != "" {
		args = append(args, "-s", s.cfg.Serial)
	}
	args = append(args, "forward", "--remove", "tcp:"+strconv.Itoa(s.cfg.LocalPort))
	return exec.Command(s.cfg.ADBPath, args...).Run()
}

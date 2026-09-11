package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/vitorfulll/android-pessoal/internal/scrcpy"
	"github.com/vitorfulll/android-pessoal/internal/store"
)

type Config struct {
	APIBaseURL    string
	ExecutorToken string
	ICEServers    []webrtc.ICEServer
	Scrcpy        scrcpy.Config
	ICEUDPPort    int
}

type Gateway struct {
	cfg  Config
	log  *slog.Logger
	http *http.Client
}

func New(cfg Config, logger *slog.Logger) *Gateway {
	return &Gateway{cfg: cfg, log: logger, http: &http.Client{Timeout: 10 * time.Second}}
}

func (g *Gateway) Run(ctx context.Context) error {
	for {
		session, found, err := g.activeSession(ctx)
		if err != nil {
			g.log.Error("poll active session", "error", err)
		} else if found {
			if err := g.serveSession(ctx, session); err != nil && !errors.Is(err, context.Canceled) {
				g.log.Error("remote session ended", "session", session.ID, "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (g *Gateway) activeSession(ctx context.Context) (store.Session, bool, error) {
	endpoint := strings.TrimRight(g.cfg.APIBaseURL, "/") + "/v1/executor/session"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return store.Session{}, false, err
	}
	request.Header.Set("Authorization", "Bearer "+g.cfg.ExecutorToken)
	response, err := g.http.Do(request)
	if err != nil {
		return store.Session{}, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return store.Session{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return store.Session{}, false, fmt.Errorf("executor session endpoint returned %s", response.Status)
	}
	var session store.Session
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		return store.Session{}, false, err
	}
	return session, true, nil
}

type signalMessage struct {
	Type          string  `json:"type"`
	SDP           string  `json:"sdp,omitempty"`
	Candidate     string  `json:"candidate,omitempty"`
	SDPMid        *string `json:"sdpMid,omitempty"`
	SDPMLineIndex *uint16 `json:"sdpMLineIndex,omitempty"`
}

func (g *Gateway) serveSession(parent context.Context, remote store.Session) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	scrcpyConfig := g.cfg.Scrcpy
	switch remote.Quality {
	case "economy":
		scrcpyConfig.MaxSize, scrcpyConfig.MaxFPS, scrcpyConfig.VideoBitrate = 960, 24, 2_000_000
	case "quality":
		scrcpyConfig.MaxSize, scrcpyConfig.MaxFPS, scrcpyConfig.VideoBitrate = 1280, 45, 7_000_000
	default:
		scrcpyConfig.MaxSize, scrcpyConfig.MaxFPS, scrcpyConfig.VideoBitrate = 1280, 45, 5_000_000
	}
	android, err := scrcpy.Start(ctx, scrcpyConfig, g.log)
	if err != nil {
		return err
	}
	defer android.Close()

	signalURL, err := url.Parse(strings.TrimRight(g.cfg.APIBaseURL, "/"))
	if err != nil {
		return err
	}
	if signalURL.Scheme == "https" {
		signalURL.Scheme = "wss"
	} else {
		signalURL.Scheme = "ws"
	}
	signalURL.Path = "/v1/sessions/" + remote.ID + "/signal/gateway"
	header := http.Header{"Authorization": []string{"Bearer " + g.cfg.ExecutorToken}}
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, signalURL.String(), header)
	if err != nil {
		return fmt.Errorf("connect signaling: %w", err)
	}
	defer connection.Close()
	go func() {
		<-ctx.Done()
		_ = connection.Close()
	}()

	var api *webrtc.API
	if g.cfg.ICEUDPPort > 0 {
		udpConnection, err := net.ListenUDP("udp4", &net.UDPAddr{Port: g.cfg.ICEUDPPort})
		if err != nil {
			return fmt.Errorf("listen ICE UDP: %w", err)
		}
		defer udpConnection.Close()
		settings := webrtc.SettingEngine{}
		settings.SetICEUDPMux(webrtc.NewICEUDPMux(nil, udpConnection))
		api = webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	} else {
		api = webrtc.NewAPI()
	}
	peer, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: g.cfg.ICEServers})
	if err != nil {
		return err
	}
	defer peer.Close()

	video, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeH264, ClockRate: 90_000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
	}, "video", "android")
	if err != nil {
		return err
	}
	videoSender, err := peer.AddTrack(video)
	if err != nil {
		return err
	}
	audio, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: 48_000, Channels: 2,
		SDPFmtpLine: "minptime=10;useinbandfec=1",
	}, "audio", "android")
	if err != nil {
		return err
	}
	audioSender, err := peer.AddTrack(audio)
	if err != nil {
		return err
	}

	var writeMu sync.Mutex
	writeSignal := func(message signalMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return connection.WriteJSON(message)
	}
	peer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		g.log.Info("WebRTC state", "session", remote.ID, "state", state.String())
		if state == webrtc.PeerConnectionStateConnected {
			if err := android.ResetVideo(); err != nil {
				g.log.Warn("request initial keyframe", "error", err)
			}
		}
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			cancel()
		}
	})
	peer.OnDataChannel(func(channel *webrtc.DataChannel) {
		if channel.Label() != "control.v1" && channel.Label() != "touch.v1" {
			channel.Close()
			return
		}
		channel.OnMessage(func(message webrtc.DataChannelMessage) {
			if err := android.HandleControl(message.Data, remote.ID, remote.Generation); err != nil {
				if !errors.Is(err, scrcpy.ErrStaleControl) {
					g.log.Warn("rejected control message", "error", err)
				}
			}
		})
		channel.OnClose(func() {
			_ = android.HandleControl(mustJSON(scrcpy.ControlEnvelope{Version: "control.v1", SessionID: remote.ID, Generation: remote.Generation, Action: "cancelAll"}), remote.ID, remote.Generation)
		})
	})

	// An IDR is already emitted every second. Restarting the capture for every
	// PLI creates a feedback loop with the receiver and stalls screen updates.
	go readRTCP(ctx, videoSender, nil)
	go readRTCP(ctx, audioSender, nil)
	go streamVideo(ctx, android, video, g.log)
	go streamAudio(ctx, android, audio, g.log)

	for {
		_, raw, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		var message signalMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			continue
		}
		switch message.Type {
		case "offer":
			if err := peer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: message.SDP}); err != nil {
				return err
			}
			answer, err := peer.CreateAnswer(nil)
			if err != nil {
				return err
			}
			gatheringComplete := webrtc.GatheringCompletePromise(peer)
			if err := peer.SetLocalDescription(answer); err != nil {
				return err
			}
			select {
			case <-gatheringComplete:
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(12 * time.Second):
				return errors.New("ICE gathering timed out")
			}
			local := peer.LocalDescription()
			if local == nil {
				return errors.New("local SDP unavailable after ICE gathering")
			}
			if err := writeSignal(signalMessage{Type: "answer", SDP: local.SDP}); err != nil {
				return err
			}
		case "ice":
			if err := peer.AddICECandidate(webrtc.ICECandidateInit{Candidate: message.Candidate, SDPMid: message.SDPMid, SDPMLineIndex: message.SDPMLineIndex}); err != nil {
				return err
			}
		}
	}
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func readRTCP(ctx context.Context, sender *webrtc.RTPSender, keyframe func() error) {
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		if keyframe == nil {
			continue
		}
		for _, packet := range packets {
			switch packet.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				_ = keyframe()
			}
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func streamVideo(ctx context.Context, source *scrcpy.Session, track *webrtc.TrackLocalStaticSample, logger *slog.Logger) {
	var config []byte
	var previous time.Duration
	firstFrame := true
	for {
		packet, err := source.ReadVideo()
		if err != nil {
			logger.Error("video source stopped", "error", err)
			return
		}
		if packet.Config {
			config = append(config[:0], packet.Data...)
			continue
		}
		data := packet.Data
		if packet.KeyFrame && len(config) > 0 {
			data = append(append(make([]byte, 0, len(config)+len(data)), config...), data...)
		}
		if firstFrame {
			logger.Info("first video frame", "keyframe", packet.KeyFrame, "bytes", len(data), "width", source.Width, "height", source.Height)
			firstFrame = false
		}
		duration := 16 * time.Millisecond
		if previous > 0 && packet.PTS > previous && packet.PTS-previous < time.Second {
			duration = packet.PTS - previous
		}
		previous = packet.PTS
		if err := track.WriteSample(media.Sample{Data: data, Duration: duration}); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func streamAudio(ctx context.Context, source *scrcpy.Session, track *webrtc.TrackLocalStaticSample, logger *slog.Logger) {
	var previous time.Duration
	for {
		packet, err := source.ReadAudio()
		if err != nil {
			logger.Error("audio source stopped", "error", err)
			return
		}
		if packet.Config {
			continue
		}
		duration := 20 * time.Millisecond
		if previous > 0 && packet.PTS > previous && packet.PTS-previous < time.Second {
			duration = packet.PTS - previous
		}
		previous = packet.PTS
		if err := track.WriteSample(media.Sample{Data: packet.Data, Duration: duration}); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

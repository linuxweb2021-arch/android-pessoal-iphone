package scrcpy

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
)

func TestTouchMessageUsesScrcpy334WireFormat(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	session := &Session{control: client, Width: 720, Height: 1280, activePointers: make(map[uint32]struct{})}
	event := ControlEnvelope{
		Version: "control.v1", SessionID: "s", Generation: 7, ScreenRevision: 1,
		ScreenWidth: 720, ScreenHeight: 1280, Sequence: 1, PointerID: 42, Action: "down", X: 0.5, Y: 1,
	}
	done := make(chan error, 1)
	go func() { done <- session.HandleControl(mustControlJSON(t, event), "s", 7) }()
	message := make([]byte, 32)
	if _, err := server.Read(message); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if message[0] != 2 || message[1] != 0 {
		t.Fatalf("unexpected type/action: %d/%d", message[0], message[1])
	}
	if got := binary.BigEndian.Uint64(message[2:10]); got != 42 {
		t.Fatalf("pointer id = %d", got)
	}
	if x, y := binary.BigEndian.Uint32(message[10:14]), binary.BigEndian.Uint32(message[14:18]); x != 360 || y != 1279 {
		t.Fatalf("point = %d,%d", x, y)
	}
	if pressure := binary.BigEndian.Uint16(message[22:24]); pressure != 65535 {
		t.Fatalf("default pressure = %d", pressure)
	}
}

func mustControlJSON(t *testing.T, value ControlEnvelope) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

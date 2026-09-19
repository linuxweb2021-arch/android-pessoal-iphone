package gateway

import (
	"testing"
	"time"
)

func TestOpusPacketDuration(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want time.Duration
		ok   bool
	}{
		// Observed live format: CELT fullband stereo, one 20 ms frame.
		{name: "celt-fb-stereo-20ms", data: []byte{0xFC, 0x11, 0x22, 0x33}, want: 20 * time.Millisecond, ok: true},
		// SILK narrowband mono, one 20 ms frame (config 1, code 0).
		{name: "silk-nb-mono-20ms", data: []byte{0x08, 0xAA}, want: 20 * time.Millisecond, ok: true},
		// CELT narrowband, 2.5 ms frame (config 16, code 0).
		{name: "celt-nb-2.5ms", data: []byte{0x80, 0xAA}, want: 2500 * time.Microsecond, ok: true},
		// Code 1: two CBR 20 ms frames.
		{name: "cbr-pair-40ms", data: []byte{0xF9, 0xAA, 0xBB}, want: 40 * time.Millisecond, ok: true},
		// Code 2: two VBR 20 ms frames with first-frame length octet.
		{name: "vbr-pair-40ms", data: []byte{0xFA, 0x10, 0xAA, 0xBB}, want: 40 * time.Millisecond, ok: true},
		{name: "vbr-pair-truncated", data: []byte{0xFA}, want: 0, ok: false},
		// Code 3: three CBR frames, then VBR with per-frame lengths.
		{name: "count3-cbr-60ms", data: []byte{0xFB, 0x03, 0xAA}, want: 60 * time.Millisecond, ok: true},
		{name: "count3-vbr-60ms", data: []byte{0xFB, 0x83, 0x05, 0x06, 0xAA}, want: 60 * time.Millisecond, ok: true},
		{name: "count0-invalid", data: []byte{0xFB, 0x00, 0xAA}, want: 0, ok: false},
		{name: "count-truncated", data: []byte{0xFB}, want: 0, ok: false},
		// Absurd totals are rejected instead of moving the RTP clock.
		{name: "huge-multiframe", data: []byte{0xFB, 0x30, 0xAA}, want: 0, ok: false},
		{name: "empty", data: nil, want: 0, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := opusPacketDuration(tc.data)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("opusPacketDuration(% X) = %v, %v; want %v, %v", tc.data, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestOpusDurationIgnoresSchedulingJitter is the regression test for the
// choppy-audio defect: PTS arrival gaps must not change the RTP clock when
// every packet carries continuous 20 ms frames.
func TestOpusDurationIgnoresSchedulingJitter(t *testing.T) {
	payload := []byte{0xFC, 0x11, 0x22, 0x33, 0x44}
	for i := 0; i < 3; i++ {
		got, ok := opusPacketDuration(payload)
		if !ok || got != 20*time.Millisecond {
			t.Fatalf("packet %d decoded as %v, %v; want 20ms, true", i, got, ok)
		}
	}
}

package gateway

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestStereoOpusNegotiation(t *testing.T) {
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatal(err)
	}
	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	offer.SDP = requestStereoOpus(offer.SDP)

	server, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	track, err := webrtc.NewTrackLocalStaticSample(androidAudioCodec, "audio", "android")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.AddTrack(track); err != nil {
		t.Fatal(err)
	}
	if err := server.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := server.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer.SDP, "stereo=1") || !strings.Contains(answer.SDP, "sprop-stereo=1") {
		t.Fatalf("answer did not preserve stereo Opus negotiation:\n%s", answer.SDP)
	}
}

func requestStereoOpus(sdp string) string {
	lines := strings.Split(sdp, "\r\n")
	opusPayloads := map[string]bool{}
	for _, line := range lines {
		if !strings.HasPrefix(strings.ToLower(line), "a=rtpmap:") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 && strings.HasPrefix(strings.ToLower(parts[1]), "opus/48000/") {
			opusPayloads[strings.TrimPrefix(parts[0], "a=rtpmap:")] = true
		}
	}
	for index, line := range lines {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "a=fmtp:") {
			continue
		}
		if opusPayloads[strings.TrimPrefix(parts[0], "a=fmtp:")] {
			lines[index] += ";stereo=1;sprop-stereo=1;maxplaybackrate=48000;maxaveragebitrate=192000"
		}
	}
	return strings.Join(lines, "\r\n")
}

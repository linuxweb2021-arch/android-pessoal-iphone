package gateway

import (
	"time"
)

// Opus frame sizes in microseconds indexed by TOC config (RFC 6716,
// Section 3.1). SILK-only modes carry 10/20/40/60 ms, Hybrid modes
// 10/20 ms and CELT modes 2.5/5/10/20 ms.
func opusFrameDurationUs(config int) (int64, bool) {
	switch {
	case config >= 0 && config < 12:
		return [...]int64{10000, 20000, 40000, 60000}[config%4], true
	case config >= 12 && config < 16:
		return [...]int64{10000, 20000}[(config-12)%2], true
	case config >= 16 && config < 32:
		return [...]int64{2500, 5000, 10000, 20000}[(config-16)%4], true
	}
	return 0, false
}

// maxOpusPacketDuration caps a single packet at 120 ms of audio. Anything
// above is treated as malformed input, never as RTP clock movement.
const maxOpusPacketDuration = 120 * time.Millisecond

// opusPacketDuration decodes the total audio duration carried by one Opus
// packet from its table of contents (RFC 6716, Section 3.2), so the RTP
// clock advances by encoded audio instead of scheduling jitter. ok is
// false for malformed packets; callers must fall back to a nominal
// duration that preserves stream continuity.
func opusPacketDuration(data []byte) (time.Duration, bool) {
	if len(data) < 1 {
		return 0, false
	}
	toc := data[0]
	frameUs, ok := opusFrameDurationUs(int(toc >> 3))
	if !ok {
		return 0, false
	}
	frames := 0
	switch toc & 0x03 {
	case 0x00: // Single frame.
		if len(data) < 1 {
			return 0, false
		}
		frames = 1
	case 0x01: // Two CBR frames.
		if len(data) < 1 {
			return 0, false
		}
		frames = 2
	case 0x02: // Two VBR frames, first-frame length follows the TOC.
		if len(data) < 2 {
			return 0, false
		}
		frames = 2
	case 0x03: // Count octet: VBR flag, padding flag, frame count.
		if len(data) < 2 {
			return 0, false
		}
		count := data[1]
		frames = int(count & 0x3F)
		if frames < 1 || frames > 48 {
			return 0, false
		}
		need := 2
		if count&0x80 != 0 {
			need += frames - 1 // One length octet per frame but the last.
		}
		if len(data) < need {
			return 0, false
		}
	}
	duration := time.Duration(int64(frames) * frameUs * int64(time.Microsecond))
	if duration <= 0 || duration > maxOpusPacketDuration {
		return 0, false
	}
	return duration, true
}

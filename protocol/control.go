package protocol

import (
	"errors"
	"math"
)

const ControlVersion = "control.v1"

type Action string

const (
	ActionDown      Action = "down"
	ActionMove      Action = "move"
	ActionUp        Action = "up"
	ActionCancel    Action = "cancel"
	ActionHeartbeat Action = "heartbeat"
)

type ControlMessage struct {
	Version        string  `json:"version"`
	SessionID      string  `json:"sessionId"`
	Generation     int64   `json:"generation"`
	ScreenRevision int64   `json:"screenRevision"`
	Sequence       uint64  `json:"sequence"`
	GestureID      uint64  `json:"gestureId"`
	PointerID      uint32  `json:"pointerId"`
	Action         Action  `json:"action"`
	X              float64 `json:"x,omitempty"`
	Y              float64 `json:"y,omitempty"`
	Pressure       float64 `json:"pressure,omitempty"`
	MonotonicNanos uint64  `json:"monotonicNanos"`
}

func (m ControlMessage) Validate() error {
	if m.Version != ControlVersion || m.SessionID == "" || m.Generation <= 0 || m.ScreenRevision <= 0 {
		return errors.New("invalid control envelope")
	}
	switch m.Action {
	case ActionHeartbeat:
		return nil
	case ActionDown, ActionMove, ActionUp, ActionCancel:
	default:
		return errors.New("invalid control action")
	}
	if math.IsNaN(m.X) || math.IsNaN(m.Y) || m.X < 0 || m.X > 1 || m.Y < 0 || m.Y > 1 {
		return errors.New("coordinates must be normalized")
	}
	if math.IsNaN(m.Pressure) || m.Pressure < 0 || m.Pressure > 1 {
		return errors.New("pressure must be normalized")
	}
	return nil
}

type Screen struct {
	Width    int
	Height   int
	Rotation int
}

func (s Screen) PixelPoint(x, y float64) (int, int, error) {
	if s.Width <= 0 || s.Height <= 0 || x < 0 || x > 1 || y < 0 || y > 1 {
		return 0, 0, errors.New("invalid screen transform")
	}
	var rx, ry float64
	switch ((s.Rotation % 360) + 360) % 360 {
	case 0:
		rx, ry = x, y
	case 90:
		rx, ry = y, 1-x
	case 180:
		rx, ry = 1-x, 1-y
	case 270:
		rx, ry = 1-y, x
	default:
		return 0, 0, errors.New("rotation must be a right angle")
	}
	return int(math.Round(rx * float64(s.Width-1))), int(math.Round(ry * float64(s.Height-1))), nil
}

package protocol

import "testing"

func TestPixelPointRotations(t *testing.T) {
	tests := []struct {
		rotation int
		x, y     float64
		wantX    int
		wantY    int
	}{
		{0, 0, 0, 0, 0},
		{0, 1, 1, 1079, 1919},
		{90, 0.25, 0.75, 809, 1439},
		{180, 0.25, 0.75, 809, 480},
		{270, 0.25, 0.75, 270, 480},
	}
	for _, test := range tests {
		gotX, gotY, err := (Screen{Width: 1080, Height: 1920, Rotation: test.rotation}).PixelPoint(test.x, test.y)
		if err != nil || gotX != test.wantX || gotY != test.wantY {
			t.Fatalf("rotation %d: got (%d,%d,%v), want (%d,%d,nil)", test.rotation, gotX, gotY, err, test.wantX, test.wantY)
		}
	}
}

func TestControlValidation(t *testing.T) {
	message := ControlMessage{Version: ControlVersion, SessionID: "session", Generation: 1, ScreenRevision: 1, Sequence: 1, GestureID: 1, PointerID: 1, Action: ActionDown, X: .5, Y: .5}
	if err := message.Validate(); err != nil {
		t.Fatal(err)
	}
	message.X = 1.1
	if err := message.Validate(); err == nil {
		t.Fatal("expected invalid coordinate")
	}
}

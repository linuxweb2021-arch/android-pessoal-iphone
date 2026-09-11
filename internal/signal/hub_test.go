package signal

import "testing"

func TestHubPairsOneClientAndOneGateway(t *testing.T) {
	hub := NewHub()
	client, err := hub.Join("session", Client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Join("session", Client); err == nil {
		t.Fatal("expected a second client to be refused")
	}
	gateway, err := hub.Join("session", Gateway)
	if err != nil {
		t.Fatal(err)
	}
	want := Message{Type: 1, Data: []byte("offer")}
	if err := hub.Forward("session", client, want); err != nil {
		t.Fatal(err)
	}
	got := <-gateway.Send
	if string(got.Data) != string(want.Data) {
		t.Fatalf("got %q, want %q", got.Data, want.Data)
	}
	hub.Leave("session", client)
	hub.Leave("session", gateway)
}

func TestHubQueuesOfferUntilGatewayConnects(t *testing.T) {
	hub := NewHub()
	client, err := hub.Join("session", Client)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Forward("session", client, Message{Type: 1, Data: []byte("offer")}); err != nil {
		t.Fatal(err)
	}
	gateway, err := hub.Join("session", Gateway)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-gateway.Send:
		if string(message.Data) != "offer" {
			t.Fatalf("got %q", message.Data)
		}
	default:
		t.Fatal("queued offer was not delivered")
	}
}

package signal

import (
	"errors"
	"sync"
)

type Role string

const (
	Client  Role = "client"
	Gateway Role = "gateway"
)

type Message struct {
	Type int
	Data []byte
}

type Peer struct {
	Role Role
	Send chan Message
	done chan struct{}
	once sync.Once
}

func (p *Peer) Done() <-chan struct{} { return p.done }
func (p *Peer) close()                { p.once.Do(func() { close(p.done) }) }

type room struct {
	client         *Peer
	gateway        *Peer
	pendingClient  []Message
	pendingGateway []Message
}

type Hub struct {
	mu    sync.Mutex
	rooms map[string]*room
}

func NewHub() *Hub { return &Hub{rooms: make(map[string]*room)} }

func (h *Hub) Join(sessionID string, role Role) (*Peer, error) {
	if role != Client && role != Gateway {
		return nil, errors.New("invalid signal role")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	value := h.rooms[sessionID]
	if value == nil {
		value = &room{}
		h.rooms[sessionID] = value
	}
	peer := &Peer{Role: role, Send: make(chan Message, 32), done: make(chan struct{})}
	if role == Client {
		if value.client != nil {
			return nil, errors.New("client already connected")
		}
		value.client = peer
		for _, message := range value.pendingGateway {
			peer.Send <- message
		}
		value.pendingGateway = nil
	} else {
		if value.gateway != nil {
			return nil, errors.New("gateway already connected")
		}
		value.gateway = peer
		for _, message := range value.pendingClient {
			peer.Send <- message
		}
		value.pendingClient = nil
	}
	return peer, nil
}

func (h *Hub) Forward(sessionID string, sender *Peer, message Message) error {
	h.mu.Lock()
	value := h.rooms[sessionID]
	var receiver *Peer
	if value != nil && sender.Role == Client {
		receiver = value.gateway
	} else if value != nil {
		receiver = value.client
	}
	if receiver == nil {
		if value == nil {
			h.mu.Unlock()
			return errors.New("room unavailable")
		}
		if sender.Role == Client && len(value.pendingClient) < 32 {
			value.pendingClient = append(value.pendingClient, message)
			h.mu.Unlock()
			return nil
		}
		if sender.Role == Gateway && len(value.pendingGateway) < 32 {
			value.pendingGateway = append(value.pendingGateway, message)
			h.mu.Unlock()
			return nil
		}
		h.mu.Unlock()
		return errors.New("signaling queue full")
	}
	select {
	case receiver.Send <- message:
		h.mu.Unlock()
		return nil
	case <-receiver.done:
		h.mu.Unlock()
		return errors.New("peer disconnected")
	default:
		h.mu.Unlock()
		return errors.New("peer is not consuming signaling messages")
	}
}

func (h *Hub) Leave(sessionID string, peer *Peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	value := h.rooms[sessionID]
	if value == nil {
		return
	}
	if value.client == peer {
		value.client = nil
	}
	if value.gateway == peer {
		value.gateway = nil
	}
	peer.close()
	if value.client == nil && value.gateway == nil {
		delete(h.rooms, sessionID)
	}
}

func (h *Hub) CloseSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	value := h.rooms[sessionID]
	if value == nil {
		return
	}
	if value.client != nil {
		value.client.close()
	}
	if value.gateway != nil {
		value.gateway.close()
	}
	delete(h.rooms, sessionID)
}

func (h *Hub) HasGateway(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	value := h.rooms[sessionID]
	return value != nil && value.gateway != nil
}

package ws

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
)

// Hub coordinates multiple active WebSocket client sessions and room events distribution.
type Hub struct {
	// Registered active connection instances
	clients map[*Client]bool

	// Registration requests from new connecting clients
	register chan *Client

	// Unregistration requests from disconnecting clients
	unregister chan *Client

	// Mutex to protect concurrent access to room lists
	mu sync.RWMutex

	// Mapping of room ID strings to active client pointers joined in those channels
	rooms map[string]map[*Client]bool

	// Mapping of active NATS subscriptions to empty rooms once all local clients leave
	subscriptions map[string]*nats.Subscription

	// NATS JetStream interface for distributed pub/sub sync
	js nats.JetStreamContext

	// NATS connection handler
	nc *nats.Conn
}

// NewHub constructs a new centralized coordinate Hub instance.
func NewHub(js nats.JetStreamContext, nc *nats.Conn) *Hub {
	return &Hub{
		clients:       make(map[*Client]bool),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		rooms:         make(map[string]map[*Client]bool),
		subscriptions: make(map[string]*nats.Subscription),
		js:            js,
		nc:            nc,
	}
}

// Run acts as the primary orchestrator loop handling registration and unregistration lifecycles.
func (h *Hub) Run() {
	h.subscribeToPresenceNATS()
	h.subscribeToSystemNATS()

	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("[Hub] Client connection registered for user [%s]", client.userID)

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				h.leaveAllRooms(client)
				log.Printf("[Hub] Client connection unregistered for user [%s]", client.userID)
			}
		}
	}
}

// BroadcastToTenant routes a payload frame directly to all local clients within tenantID.
func (h *Hub) BroadcastToTenant(tenantID string, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client.tenantID == tenantID {
			select {
			case client.send <- payload:
			default:
				// Handshake block or closed client
			}
		}
	}
}

func (h *Hub) subscribeToPresenceNATS() {
	subject := "tenant.*.presence"

	_, err := h.nc.Subscribe(subject, func(msg *nats.Msg) {
		// Parse tenant ID from subject "tenant.<tenantID>.presence"
		parts := strings.Split(msg.Subject, ".")
		if len(parts) >= 3 {
			tenantID := parts[1]
			h.BroadcastToTenant(tenantID, msg.Data)
		}
	})
	if err != nil {
		log.Printf("[Hub] Failed to subscribe to presence subject: %v", err)
	}
}

// subscribeToSystemNATS listens for system-wide events (plugin toggles, config changes)
// and broadcasts them to ALL connected clients regardless of tenant.
func (h *Hub) subscribeToSystemNATS() {
	subject := "tenant.*.system"

	_, err := h.nc.Subscribe(subject, func(msg *nats.Msg) {
		log.Printf("[Hub] System event broadcast to all clients: %s", string(msg.Data))
		h.BroadcastToAll(msg.Data)
	})
	if err != nil {
		log.Printf("[Hub] Failed to subscribe to system subject: %v", err)
	}
}

// BroadcastToAll sends a payload to every connected client.
func (h *Hub) BroadcastToAll(payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		select {
		case client.send <- payload:
		default:
			// Client buffer full, skip
		}
	}
}



// JoinRoom binds a local client connection into the room subscription map.
func (h *Hub) JoinRoom(roomID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.rooms[roomID]; !ok {
		h.rooms[roomID] = make(map[*Client]bool)

		// Dynamically spawn a cluster-wide NATS topic subscription for the new room channel
		go h.subscribeToRoomNATS(roomID)
	}
	h.rooms[roomID][client] = true
	log.Printf("[Hub] Client [%s] joined room channel [%s] locally", client.userID, roomID)
}

// LeaveRoom unbinds a client connection from a specific room channel.
func (h *Hub) LeaveRoom(roomID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.leaveRoomUnsafe(roomID, client)
}

func (h *Hub) leaveRoomUnsafe(roomID string, client *Client) {
	if clients, ok := h.rooms[roomID]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.rooms, roomID)

			// Clean up idle NATS subscriptions to prevent memory leaks in gateway node
			if sub, ok2 := h.subscriptions[roomID]; ok2 {
				_ = sub.Unsubscribe()
				delete(h.subscriptions, roomID)
				log.Printf("[Hub] Unsubscribed empty NATS subject stream for room [%s]", roomID)
			}
		}
	}
	log.Printf("[Hub] Client [%s] left room channel [%s] locally", client.userID, roomID)
}

func (h *Hub) leaveAllRooms(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for roomID := range h.rooms {
		h.leaveRoomUnsafe(roomID, client)
	}
}

// BroadcastToRoom routes a payload frame directly to all local subscribers within roomID.
func (h *Hub) BroadcastToRoom(roomID string, payload []byte) {
	var deadClients []*Client

	h.mu.RLock()
	clients, ok := h.rooms[roomID]
	if !ok {
		h.mu.RUnlock()
		return
	}

	for client := range clients {
		select {
		case client.send <- payload:
		default:
			// Client buffer full; collect for cleanup AFTER releasing RLock
			deadClients = append(deadClients, client)
		}
	}
	h.mu.RUnlock()

	// Cleanup dead clients outside of RLock to avoid DEADLOCK
	for _, client := range deadClients {
		// Use the hub's unregister channel for proper lifecycle management
		// This ensures close(client.send) + leaveAllRooms happen in the Run() goroutine
		h.unregister <- client
	}
}

func (h *Hub) subscribeToRoomNATS(roomID string) {
	subject := fmt.Sprintf("tenant.*.room.%s", roomID)

	sub, err := h.nc.Subscribe(subject, func(msg *nats.Msg) {
		h.BroadcastToRoom(roomID, msg.Data)
	})
	if err != nil {
		log.Printf("[Hub] Failed to subscribe to NATS topic for room %s: %v", roomID, err)
		return
	}

	h.mu.Lock()
	h.subscriptions[roomID] = sub
	h.mu.Unlock()
	log.Printf("[Hub] Registered NATS pub/sub subscription for topic [%s]", subject)
}

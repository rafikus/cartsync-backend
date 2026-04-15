// internal/ws/hub.go

package ws

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// ── Message ───────────────────────────────────────────────────────────────────

type Message struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// ── Client ────────────────────────────────────────────────────────────────────

type Client struct {
	ListID string
	UserID string
	conn   *websocket.Conn
	send   chan []byte
}

func (c *Client) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		// Handle ping
		if msg.Type == "ping" {
			out, _ := json.Marshal(Message{Type: "pong"})
			c.send <- out
			continue
		}
		// Forward item:checking and other client-originated events to the list
		hub.broadcast <- broadcastMsg{listID: c.ListID, senderID: c.UserID, msg: raw}
	}
}

// ── Hub ───────────────────────────────────────────────────────────────────────

type broadcastMsg struct {
	listID   string
	senderID string // excluded from broadcast (don't echo back to sender)
	msg      []byte
}

type Hub struct {
	mu         sync.RWMutex
	clients    map[string]map[*Client]struct{} // listID -> set of clients
	register   chan *Client
	unregister chan *Client
	broadcast  chan broadcastMsg
}

var Default = &Hub{
	clients:    make(map[string]map[*Client]struct{}),
	register:   make(chan *Client, 32),
	unregister: make(chan *Client, 32),
	broadcast:  make(chan broadcastMsg, 256),
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			if h.clients[c.ListID] == nil {
				h.clients[c.ListID] = make(map[*Client]struct{})
			}
			h.clients[c.ListID][c] = struct{}{}
			h.mu.Unlock()
			log.Printf("ws: client %s joined list %s", c.UserID, c.ListID)

		case c := <-h.unregister:
			h.mu.Lock()
			delete(h.clients[c.ListID], c)
			close(c.send)
			h.mu.Unlock()
			log.Printf("ws: client %s left list %s", c.UserID, c.ListID)

		case bm := <-h.broadcast:
			h.mu.RLock()
			for c := range h.clients[bm.listID] {
				if c.UserID == bm.senderID {
					continue // don't echo to sender
				}
				select {
				case c.send <- bm.msg:
				default:
					// Slow client — drop message
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a server-originated message to all members of a list.
func (h *Hub) Broadcast(listID string, senderID string, msg Message) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.broadcast <- broadcastMsg{listID: listID, senderID: senderID, msg: raw}
}

// BroadcastAll sends to everyone including the sender (used for server events).
func (h *Hub) BroadcastAll(listID string, msg Message) {
	h.Broadcast(listID, "", msg)
}

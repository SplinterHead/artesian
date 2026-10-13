package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for local control UI
	},
}

// Event represents a WebSocket broadcast payload
type Event struct {
	Type      string      `json:"type"` // e.g. "proxy:traffic", "steam:status", "prefill:progress", "stats:update"
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
}

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	clients   map[*Client]bool
	broadcast chan Event
	register  chan *Client
	unregister chan *Client
	mu        sync.RWMutex
}

// Client represents a single connected UI websocket client
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

var (
	globalHub  *Hub
	globalOnce sync.Once
)

// GetHub returns the singleton WebSocket hub instance
func GetHub() *Hub {
	globalOnce.Do(func() {
		globalHub = &Hub{
			clients:    make(map[*Client]bool),
			broadcast:  make(chan Event, 256),
			register:   make(chan *Client),
			unregister: make(chan *Client),
		}
		go globalHub.run()
	})
	return globalHub
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case event := <-h.broadcast:
			if event.Timestamp == 0 {
				event.Timestamp = time.Now().UnixMilli()
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}

			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast dispatches an event to all connected UI clients
func (h *Hub) Broadcast(eventType string, data interface{}) {
	select {
	case h.broadcast <- Event{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
	}:
	default:
		// Queue full, drop to prevent blocking real-time proxy
	}
}

// ServeHTTP handles incoming websocket upgrade requests
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebSocket] Upgrade error: %v", err)
		return
	}

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 256),
	}

	h.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Drain queued messages
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

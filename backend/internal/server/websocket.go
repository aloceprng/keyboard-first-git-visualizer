package server

import (
	"encoding/json"
	"net/http"
	"time"
	"github.com/gorilla/websocket"
)

const (
	writeWait = 10 * time.Second
	pongWait = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxMessageSize = 512
)

// WatchEvent is the payload pushed to all connected frontends when .git changes
type WatchEvent struct {
	Type        string     `json:"type"`
	AddedSHAs   []string   `json:"added_shas"`
	RemovedSHAs []string   `json:"removed_shas"`
	MovedRefs   []movedRef `json:"moved_refs"`
	InProgress  string     `json:"in_progress"`
}

type movedRef struct {
	Ref  string `json:"ref"`
	From string `json:"from"`
	To   string `json:"to"`
}

// wsHub owns the full set of live WebSocket connections and fans out
// WatchEvents to every connected client
type wsHub struct {
	clients    map[*wsClient]bool
	broadcast  chan WatchEvent
	register   chan *wsClient
	unregister chan *wsClient
}

func newHub() *wsHub {
	return &wsHub{
		clients: make(map[*wsClient]bool),
	
		broadcast: make(chan WatchEvent, 256),

		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
	}
}

// hub event loop
func (h *wsHub) run() {
	for {
		select {

		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}

		case event := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- event:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// wsClient represents one connected browser tab.
type wsClient struct {
	hub  *wsHub
	conn *websocket.Conn

	send chan WatchEvent
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool { return true },
}

// upgrades the HTTP connection to WebSocket, registers the client
// with the hub, and starts its read and write pumps in separate goroutines
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {	return }

	client := &wsClient{
		hub:  s.hub,
		conn: conn,
		send: make(chan WatchEvent, 64),
	}

	s.hub.register <- client

	go client.writePump()
	go client.readPump()
}

// sends queued WatchEvents to the client and keeps the connection alive with periodic pings
func (c *wsClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))

			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil { return }

			if err := json.NewEncoder(w).Encode(event); err != nil {
				w.Close()
				return
			}

			pending := len(c.send)
			for i := 0; i < pending; i++ {
				if err := json.NewEncoder(w).Encode(<-c.send); err != nil {
					break
				}
			}

			if err := w.Close(); err != nil { return }

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// reads from the connection solely to handle pong frames and detect clean disconnects
func (c *wsClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
			) {
				// log.Printf("ws unexpected close: %v", err)
			}
			return
		}
	}
}
// internal/ws/handler.go

package ws

import (
	"context"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/rafikus/cartsync/internal/db"
	"github.com/rafikus/cartsync/internal/middleware"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins in development. Restrict in production.
		return true
	},
}

// Handler upgrades the connection and registers the client with the hub.
// Route: GET /ws/lists/{listId}?token=<jwt>
func Handler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	// Extract listId from path /ws/lists/{listId}
	listID := strings.TrimPrefix(r.URL.Path, "/ws/lists/")
	listID = strings.TrimSuffix(listID, "/")
	if listID == "" {
		http.Error(w, "missing listId", http.StatusBadRequest)
		return
	}

	// Verify the user is a member of this list
	var count int
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM list_members WHERE list_id = $1 AND user_id = $2`,
		listID, userID,
	).Scan(&count)
	if err != nil || count == 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &Client{
		ListID: listID,
		UserID: userID,
		conn:   conn,
		send:   make(chan []byte, 64),
	}

	Default.register <- client

	go client.writePump()
	go client.readPump(Default)
}

// internal/lists/items.go

package lists

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/cartsync/internal/db"
	"github.com/yourname/cartsync/internal/middleware"
	"github.com/yourname/cartsync/internal/ws"
)

// POST /lists/{listId}/items
func AddItem(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())

	var req struct {
		Name     string  `json:"name"`
		Quantity float64 `json:"quantity"`
		Unit     string  `json:"unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	if req.Unit == "" {
		req.Unit = "×"
	}

	itemID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(),
		`INSERT INTO items (id, list_id, name, quantity, unit, added_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		itemID, listID, req.Name, req.Quantity, req.Unit, userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not add item")
		return
	}

	item := db.Item{
		ID: itemID, ListID: listID, Name: req.Name,
		Quantity: req.Quantity, Unit: req.Unit, AddedBy: userID,
	}
	ws.Default.Broadcast(listID, userID, ws.Message{Type: "item:added", Payload: item})
	writeJSON(w, http.StatusCreated, item)
}

// PATCH /lists/{listId}/items/{itemId}
func UpdateItem(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	itemID := extractTrailingID(r.URL.Path)

	var req struct {
		Name     *string  `json:"name"`
		Quantity *float64 `json:"quantity"`
		Unit     *string  `json:"unit"`
		Checked  *bool    `json:"checked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	type setEntry struct {
		col string
		val any
	}
	var entries []setEntry

	if req.Name != nil {
		entries = append(entries, setEntry{"name", *req.Name})
	}
	if req.Quantity != nil {
		entries = append(entries, setEntry{"quantity", *req.Quantity})
	}
	if req.Unit != nil {
		entries = append(entries, setEntry{"unit", *req.Unit})
	}
	if req.Checked != nil {
		entries = append(entries, setEntry{"checked", *req.Checked})
		if *req.Checked {
			entries = append(entries, setEntry{"checked_by", userID})
			entries = append(entries, setEntry{"checked_at", time.Now()})
		} else {
			entries = append(entries, setEntry{"checked_by", nil})
			entries = append(entries, setEntry{"checked_at", nil})
		}
	}

	if len(entries) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}

	setClauses := make([]string, len(entries))
	args := make([]any, len(entries))
	for i, e := range entries {
		setClauses[i] = fmt.Sprintf("%s = $%d", e.col, i+1)
		args[i] = e.val
	}
	args = append(args, itemID, listID)
	query := fmt.Sprintf(
		"UPDATE items SET %s WHERE id = $%d AND list_id = $%d",
		strings.Join(setClauses, ", "),
		len(entries)+1,
		len(entries)+2,
	)

	if _, err := db.Pool.Exec(context.Background(), query, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update item")
		return
	}

	var item db.Item
	db.Pool.QueryRow(context.Background(),
		`SELECT id, list_id, name, quantity, unit, checked, checked_by, checked_at, added_by, created_at
		 FROM items WHERE id = $1`, itemID,
	).Scan(
		&item.ID, &item.ListID, &item.Name, &item.Quantity, &item.Unit,
		&item.Checked, &item.CheckedBy, &item.CheckedAt, &item.AddedBy, &item.CreatedAt,
	)

	ws.Default.Broadcast(listID, userID, ws.Message{Type: "item:updated", Payload: item})
	writeJSON(w, http.StatusOK, item)
}

// DELETE /lists/{listId}/items/{itemId}
func DeleteItem(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	itemID := extractTrailingID(r.URL.Path)

	db.Pool.Exec(context.Background(),
		`DELETE FROM items WHERE id = $1 AND list_id = $2`, itemID, listID)

	ws.Default.Broadcast(listID, userID, ws.Message{
		Type:    "item:removed",
		Payload: map[string]string{"id": itemID},
	})
	w.WriteHeader(http.StatusNoContent)
}

// extractTrailingID returns the last non-empty path segment.
func extractTrailingID(path string) string {
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	return parts[len(parts)-1]
}

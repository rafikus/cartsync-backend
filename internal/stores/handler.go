// internal/stores/handler.go

package stores

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/rafikus/cartsync/internal/db"
	"github.com/rafikus/cartsync/internal/middleware"
	"github.com/rafikus/cartsync/internal/ws"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}

func requireMember(w http.ResponseWriter, r *http.Request) (listID string, ok bool) {
	userID := middleware.UserIDFromCtx(r.Context())
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/lists/"), "/")
	listID = parts[0]

	var count int
	db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM list_members WHERE list_id = $1 AND user_id = $2`,
		listID, userID,
	).Scan(&count)

	if count == 0 {
		writeError(w, http.StatusForbidden, "not a member of this list")
		return "", false
	}
	return listID, true
}

func fetchStore(ctx context.Context, storeID string) (db.Store, error) {
	var s db.Store
	err := db.Pool.QueryRow(ctx,
		`SELECT id, list_id, name, trip_count, created_at FROM stores WHERE id = $1`, storeID,
	).Scan(&s.ID, &s.ListID, &s.Name, &s.TripCount, &s.CreatedAt)
	if err != nil {
		return s, err
	}
	s.LearnedOrder = make(map[string]float64)
	rows, _ := db.Pool.Query(ctx,
		`SELECT item_name, avg_pos FROM store_order_entries WHERE store_id = $1`, storeID)
	defer rows.Close()
	for rows.Next() {
		var name string
		var pos float64
		rows.Scan(&name, &pos)
		s.LearnedOrder[name] = pos
	}
	return s, nil
}

// GET /lists/{listId}/stores
func List(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}

	rows, err := db.Pool.Query(context.Background(),
		`SELECT id FROM stores WHERE list_id = $1 ORDER BY created_at ASC`, listID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var storeList []db.Store
	for rows.Next() {
		var id string
		rows.Scan(&id)
		if s, err := fetchStore(context.Background(), id); err == nil {
			storeList = append(storeList, s)
		}
	}
	if storeList == nil {
		storeList = []db.Store{}
	}
	writeJSON(w, http.StatusOK, storeList)
}

// POST /lists/{listId}/stores
func Create(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	storeID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(),
		`INSERT INTO stores (id, list_id, name) VALUES ($1, $2, $3)
		 ON CONFLICT (list_id, name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`,
		storeID, listID, req.Name,
	)
	if err != nil {
		// If conflict, fetch existing
		db.Pool.QueryRow(context.Background(),
			`SELECT id FROM stores WHERE list_id = $1 AND name = $2`, listID, req.Name,
		).Scan(&storeID)
	}

	store, _ := fetchStore(context.Background(), storeID)
	writeJSON(w, http.StatusCreated, store)
}

// POST /lists/{listId}/stores/{storeId}/order
func RecordOrder(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}

	// Extract storeId from path .../stores/{storeId}/order
	parts := strings.Split(r.URL.Path, "/")
	storeID := ""
	for i, p := range parts {
		if p == "stores" && i+1 < len(parts) {
			storeID = parts[i+1]
			break
		}
	}
	if storeID == "" {
		writeError(w, http.StatusBadRequest, "missing storeId")
		return
	}

	var req struct {
		Order []string `json:"order"` // item names in check order
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	// Upsert each item position using a rolling average:
	// new_avg = (old_avg * samples + new_pos) / (samples + 1)
	for pos, itemName := range req.Order {
		db.Pool.Exec(context.Background(), `
			INSERT INTO store_order_entries (store_id, item_name, avg_pos, samples)
			VALUES ($1, $2, $3, 1)
			ON CONFLICT (store_id, item_name) DO UPDATE
			SET avg_pos  = (store_order_entries.avg_pos * store_order_entries.samples + EXCLUDED.avg_pos)
			              / (store_order_entries.samples + 1),
			    samples  = store_order_entries.samples + 1
		`, storeID, itemName, pos+1)
	}

	// Bump trip count
	db.Pool.Exec(context.Background(),
		`UPDATE stores SET trip_count = trip_count + 1 WHERE id = $1`, storeID)

	store, _ := fetchStore(context.Background(), storeID)
	ws.Default.BroadcastAll(listID, ws.Message{Type: "store:updated", Payload: store})
	writeJSON(w, http.StatusOK, store)
}

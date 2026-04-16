// internal/trips/handler.go

package trips

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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

func requireMember(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := middleware.UserIDFromCtx(r.Context())
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/lists/"), "/")
	listID := parts[0]

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

func fetchTrip(ctx context.Context, tripID string) (db.Trip, error) {
	var t db.Trip
	err := db.Pool.QueryRow(ctx,
		`SELECT t.id, t.list_id, t.store_id, s.name, t.completed_by, t.completed_at, t.item_count, t.receipt_url
		 FROM trips t LEFT JOIN stores s ON s.id = t.store_id
		 WHERE t.id = $1`, tripID,
	).Scan(&t.ID, &t.ListID, &t.StoreID, &t.StoreName, &t.CompletedBy, &t.CompletedAt, &t.ItemCount, &t.ReceiptURL)
	if err != nil {
		return t, err
	}

	rows, _ := db.Pool.Query(ctx, `SELECT name, quantity, unit FROM trip_items WHERE trip_id = $1`, tripID)
	defer rows.Close()
	for rows.Next() {
		var ti db.TripItem
		rows.Scan(&ti.Name, &ti.Quantity, &ti.Unit)
		t.Items = append(t.Items, ti)
	}
	return t, nil
}

// GET /lists/{listId}/trips
func List(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}

	rows, err := db.Pool.Query(context.Background(),
		`SELECT id FROM trips WHERE list_id = $1 ORDER BY completed_at DESC LIMIT 50`, listID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var tripList []db.Trip
	for rows.Next() {
		var id string
		rows.Scan(&id)
		if t, err := fetchTrip(context.Background(), id); err == nil {
			tripList = append(tripList, t)
		}
	}
	if tripList == nil {
		tripList = []db.Trip{}
	}
	writeJSON(w, http.StatusOK, tripList)
}

// POST /lists/{listId}/trips
func Complete(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())

	var req struct {
		StoreID *string `json:"storeId"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Snapshot checked items
	rows, err := db.Pool.Query(context.Background(),
		`SELECT id, name, quantity, unit FROM items WHERE list_id = $1 AND checked = TRUE`, listID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	type snap struct {
		id       string
		name     string
		quantity float64
		unit     string
	}
	var snaps []snap
	for rows.Next() {
		var s snap
		rows.Scan(&s.id, &s.name, &s.quantity, &s.unit)
		snaps = append(snaps, s)
	}

	tripID := uuid.NewString()
	now := time.Now()

	tx, err := db.Pool.Begin(context.Background())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(context.Background())

	tx.Exec(context.Background(),
		`INSERT INTO trips (id, list_id, store_id, completed_by, completed_at, item_count)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		tripID, listID, req.StoreID, userID, now, len(snaps),
	)

	for _, s := range snaps {
		tx.Exec(context.Background(),
			`INSERT INTO trip_items (trip_id, name, quantity, unit) VALUES ($1, $2, $3, $4)`,
			tripID, s.name, s.quantity, s.unit,
		)
	}

	// Delete ALL items from the list (reset for next shop)
	tx.Exec(context.Background(), `DELETE FROM items WHERE list_id = $1`, listID)

	tx.Commit(context.Background())

	trip, _ := fetchTrip(context.Background(), tripID)

	// Notify partner — they need to reload the (now empty) list
	ws.Default.BroadcastAll(listID, ws.Message{Type: "trip:completed", Payload: trip})
	writeJSON(w, http.StatusCreated, trip)
}

// POST /lists/{listId}/trips/{tripId}/receipt
func AttachReceipt(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	tripID := ""
	for i, p := range parts {
		if p == "trips" && i+1 < len(parts) {
			tripID = parts[i+1]
			break
		}
	}

	// Verify trip belongs to this list
	var count int
	db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM trips WHERE id = $1 AND list_id = $2`, tripID, listID,
	).Scan(&count)
	if count == 0 {
		writeError(w, http.StatusNotFound, "trip not found")
		return
	}

	r.ParseMultipartForm(10 << 20) // 10MB
	file, header, err := r.FormFile("receipt")
	if err != nil {
		writeError(w, http.StatusBadRequest, "receipt file required")
		return
	}
	defer file.Close()

	// Save to ./uploads/ — swap for S3 or similar in production
	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./uploads"
	}
	os.MkdirAll(uploadDir, 0755)

	ext := filepath.Ext(header.Filename)
	filename := fmt.Sprintf("%s%s", uuid.NewString(), ext)
	dst, err := os.Create(filepath.Join(uploadDir, filename))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save file")
		return
	}
	defer dst.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			dst.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	receiptURL := fmt.Sprintf("/uploads/%s", filename)
	db.Pool.Exec(context.Background(),
		`UPDATE trips SET receipt_url = $1 WHERE id = $2`, receiptURL, tripID)

	trip, _ := fetchTrip(context.Background(), tripID)
	writeJSON(w, http.StatusOK, trip)
}

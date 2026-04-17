// internal/lists/handler.go

package lists

import (
	"context"
	"encoding/json"
	"math/rand"
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

func CreateList(userID string, name string) (string, error) {
	listID := uuid.NewString()
	code := generateInviteCode()

	tx, err := db.Pool.Begin(context.Background())
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())

	tx.Exec(context.Background(),
		`INSERT INTO lists (id, name, invite_code) VALUES ($1, $2, $3)`, listID, name, code)
	tx.Exec(context.Background(),
		`INSERT INTO list_members (list_id, user_id, is_admin) VALUES ($1, $2, $3)`, listID, userID, true)
	tx.Commit(context.Background())
	return listID, nil
}

// ── invite code generator ─────────────────────────────────────────────────────

const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func generateInviteCode() string {
	b := make([]byte, 7)
	for i := range b {
		if i == 3 {
			b[i] = '-'
		} else {
			b[i] = codeChars[rand.Intn(len(codeChars))]
		}
	}
	return string(b)
}

// ── getList fetches a list with its members ───────────────────────────────────

func getList(ctx context.Context, listID string) (map[string]any, error) {
	var list db.List
	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, invite_code, created_at FROM lists WHERE id = $1`, listID,
	).Scan(&list.ID, &list.Name, &list.InviteCode, &list.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Members
	rows, err := db.Pool.Query(ctx,
		`SELECT u.id, u.email, u.name FROM users u
		 JOIN list_members lm ON lm.user_id = u.id
		 WHERE lm.list_id = $1`, listID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []map[string]string{}
	for rows.Next() {
		var id, email, name string
		rows.Scan(&id, &email, &name)
		members = append(members, map[string]string{"id": id, "email": email, "name": name})
	}

	// Items
	irows, err := db.Pool.Query(ctx,
		`SELECT id, list_id, name, quantity, unit, checked, checked_by, checked_at, added_by, created_at
		 FROM items WHERE list_id = $1 ORDER BY created_at ASC`, listID,
	)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	items := []db.Item{}
	for irows.Next() {
		var item db.Item
		irows.Scan(&item.ID, &item.ListID, &item.Name, &item.Quantity, &item.Unit,
			&item.Checked, &item.CheckedBy, &item.CheckedAt, &item.AddedBy, &item.CreatedAt)
		items = append(items, item)
	}

	return map[string]any{
		"id":         list.ID,
		"name":       list.Name,
		"inviteCode": list.InviteCode,
		"createdAt":  list.CreatedAt,
		"members":    members,
		"items":      items,
	}, nil
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// POST /lists
func Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	listID, err := CreateList(userID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create list")
		return
	}

	result, err := getList(context.Background(), listID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not fetch list")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// POST /lists/join
func Join(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	var req struct {
		InviteCode string `json:"inviteCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InviteCode == "" {
		writeError(w, http.StatusBadRequest, "inviteCode is required")
		return
	}

	var listID string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id FROM lists WHERE invite_code = $1`, req.InviteCode,
	).Scan(&listID)
	if err != nil {
		writeError(w, http.StatusNotFound, "invite code not found")
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO list_members (list_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		listID, userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not join list")
		return
	}

	result, _ := getList(context.Background(), listID)
	// Notify other members
	ws.Default.Broadcast(listID, userID, ws.Message{Type: "list:updated", Payload: result})
	writeJSON(w, http.StatusOK, result)
}

// GET /lists/{listId}
func Get(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}
	result, err := getList(context.Background(), listID)
	if err != nil {
		writeError(w, http.StatusNotFound, "list not found")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// PATCH /lists/{listId}
func Rename(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireMember(w, r)
	if !ok {
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	db.Pool.Exec(context.Background(), `UPDATE lists SET name = $1 WHERE id = $2`, req.Name, listID)
	ws.Default.Broadcast(listID, userID, ws.Message{Type: "list:updated", Payload: map[string]string{"name": req.Name}})
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name})
}

// ── requireMember extracts listId and verifies membership ─────────────────────

func requireMember(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := middleware.UserIDFromCtx(r.Context())
	// Path pattern: /lists/{listId}/...
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

// internal/auth/handler.go

package auth

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/yourname/cartsync/internal/db"
	"github.com/yourname/cartsync/internal/middleware"
	"github.com/yourname/cartsync/internal/token"
	"golang.org/x/crypto/bcrypt"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}

// ── Register ──────────────────────────────────────────────────────────────────

type registerReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func Register(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Email == "" || req.Password == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "email, password and name are required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	userID := uuid.NewString()
	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO users (id, email, name, password_hash) VALUES ($1, $2, $3, $4)`,
		userID, req.Email, req.Name, string(hash),
	)
	if err != nil {
		writeError(w, http.StatusConflict, "email already in use")
		return
	}

	token, err := token.IssueToken(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"token": token,
		"user":  map[string]string{"id": userID, "email": req.Email, "name": req.Name},
	})
}

// ── Login ─────────────────────────────────────────────────────────────────────

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	var user db.User
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, email, name, password_hash FROM users WHERE email = $1`, req.Email,
	).Scan(&user.ID, &user.Email, &user.Name, &user.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := token.IssueToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  map[string]string{"id": user.ID, "email": user.Email, "name": user.Name},
	})
}

// ── Me ────────────────────────────────────────────────────────────────────────

func Me(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	var user db.User
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, email, name FROM users WHERE id = $1`, userID,
	).Scan(&user.ID, &user.Email, &user.Name)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// Also return the user's list IDs so the client knows which list to load
	rows, _ := db.Pool.Query(context.Background(),
		`SELECT list_id FROM list_members WHERE user_id = $1 ORDER BY joined_at ASC`, userID,
	)
	defer rows.Close()
	var listIDs []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		listIDs = append(listIDs, id)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      user.ID,
		"email":   user.Email,
		"name":    user.Name,
		"listIds": listIDs,
	})
}

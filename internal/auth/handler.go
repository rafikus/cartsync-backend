// internal/auth/handler.go

package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rafikus/cartsync/internal/db"
	"github.com/rafikus/cartsync/internal/email"
	"github.com/rafikus/cartsync/internal/middleware"
	"github.com/rafikus/cartsync/internal/token"
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

	writeJSON(w, http.StatusCreated, map[string]any{
		"token":   token,
		"user":    map[string]string{"id": userID, "email": req.Email, "name": req.Name},
		"listIds": listIDs,
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

	// Also return the user's list IDs so the client knows which list to load
	rows, _ := db.Pool.Query(context.Background(),
		`SELECT list_id FROM list_members WHERE user_id = $1 ORDER BY joined_at ASC`, user.ID,
	)

	defer rows.Close()
	var listIDs []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		listIDs = append(listIDs, id)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":   token,
		"user":    map[string]string{"id": user.ID, "email": user.Email, "name": user.Name},
		"listIds": listIDs,
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

// ── Password Reset ────────────────────────────────────────────────────────────

// generateResetToken generates a 6-digit numeric code
func generateResetToken() (string, error) {
	b := make([]byte, 3) // 3 bytes = 6 hex digits
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", int(b[0])<<16|int(b[1])<<8|int(b[2])%1000000), nil
}

type requestPasswordResetReq struct {
	Email string `json:"email"`
}

func RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req requestPasswordResetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	// Check if user exists
	var userID string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE email = $1`, req.Email,
	).Scan(&userID)
	
	// Always return success even if email doesn't exist (security best practice)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "If the email exists, a reset code has been sent",
		})
		return
	}

	// Generate reset token (6-digit code)
	resetToken, err := generateResetToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate reset token")
		return
	}

	// Store token (expires in 1 hour)
	expiresAt := time.Now().Add(1 * time.Hour)
	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO password_reset_tokens (token, user_id, expires_at) VALUES ($1, $2, $3)`,
		resetToken, userID, expiresAt,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save reset token")
		return
	}

	// Send email with reset token
	if err := email.SendPasswordReset(req.Email, resetToken); err != nil {
		writeError(w, http.StatusInternalServerError, "could not send reset email")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "If the email exists, a reset code has been sent",
	})
}

type resetPasswordReq struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

func ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Token == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "token and newPassword are required")
		return
	}

	// Validate token
	var userID string
	var expiresAt time.Time
	var usedAt *time.Time
	err := db.Pool.QueryRow(context.Background(),
		`SELECT user_id, expires_at, used_at FROM password_reset_tokens WHERE token = $1`,
		req.Token,
	).Scan(&userID, &expiresAt, &usedAt)
	
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired reset code")
		return
	}

	// Check if token has been used
	if usedAt != nil {
		writeError(w, http.StatusUnauthorized, "reset code has already been used")
		return
	}

	// Check if token has expired
	if time.Now().After(expiresAt) {
		writeError(w, http.StatusUnauthorized, "reset code has expired")
		return
	}

	// Hash new password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	// Update password
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE users SET password_hash = $1 WHERE id = $2`,
		string(hash), userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update password")
		return
	}

	// Mark token as used
	now := time.Now()
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE password_reset_tokens SET used_at = $1 WHERE token = $2`,
		now, req.Token,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not mark token as used")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Password reset successfully",
	})
}

// internal/suggestions/handler.go

package suggestions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rafikus/cartsync/internal/db"
	"github.com/rafikus/cartsync/internal/middleware"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}

type Suggestion struct {
	Name           string `json:"name"`
	Unit           string `json:"unit"`
	FrequencyLabel string `json:"frequencyLabel"`
}

// GET /lists/{listId}/suggestions
// Returns items from trip history sorted by how recently / frequently they appear,
// excluding items already on the current list.
func Get(w http.ResponseWriter, r *http.Request) {
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
		return
	}

	// Aggregate item appearances across all trips for this list
	// ordered by: most recent trip first, then frequency
	rows, err := db.Pool.Query(context.Background(), `
		SELECT
			ti.name,
			ti.unit,
			COUNT(*) AS appearances,
			MAX(t.completed_at) AS last_seen
		FROM trip_items ti
		JOIN trips t ON t.id = ti.trip_id
		WHERE t.list_id = $1
		  AND ti.name NOT IN (
		      SELECT name FROM items WHERE list_id = $1
		  )
		GROUP BY ti.name, ti.unit
		ORDER BY last_seen DESC, appearances DESC
		LIMIT 20
	`, listID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var result []Suggestion
	now := time.Now()

	for rows.Next() {
		var name, unit string
		var appearances int
		var lastSeen time.Time
		rows.Scan(&name, &unit, &appearances, &lastSeen)

		label := frequencyLabel(appearances, now.Sub(lastSeen))
		result = append(result, Suggestion{Name: name, Unit: unit, FrequencyLabel: label})
	}

	if result == nil {
		result = []Suggestion{}
	}
	writeJSON(w, http.StatusOK, result)
}

// frequencyLabel returns a human-readable frequency based on trip count and recency.
func frequencyLabel(appearances int, age time.Duration) string {
	weeks := age.Hours() / (24 * 7)
	if weeks < 0.5 {
		weeks = 0.5 // avoid division by zero
	}
	perWeek := float64(appearances) / weeks

	switch {
	case perWeek >= 0.9:
		return "weekly"
	case perWeek >= 0.45:
		return "biweekly"
	case perWeek >= 0.2:
		return fmt.Sprintf("%dx/mo", appearances)
	default:
		return "monthly"
	}
}

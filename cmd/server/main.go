// cmd/server/main.go

package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/rafikus/cartsync/internal/auth"
	"github.com/rafikus/cartsync/internal/db"
	"github.com/rafikus/cartsync/internal/health"
	"github.com/rafikus/cartsync/internal/lists"
	"github.com/rafikus/cartsync/internal/middleware"
	"github.com/rafikus/cartsync/internal/stores"
	"github.com/rafikus/cartsync/internal/suggestions"
	"github.com/rafikus/cartsync/internal/trips"
	"github.com/rafikus/cartsync/internal/ws"
)

func main() {
	// Load .env if present
	_ = godotenv.Load()

	// Database
	ctx := context.Background()
	if err := db.Connect(ctx); err != nil {
		log.Fatalf("db: %v", err)
	}
	log.Println("db: connected")

	// Run migrations
	if err := runMigrations(ctx); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	// Start WebSocket hub
	go ws.Default.Run()

	// Router
	mux := http.NewServeMux()

	// Static uploads
	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./uploads"
	}
	os.MkdirAll(uploadDir, 0755)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))

	// ── Public routes ─────────────────────────────────────────────────────────
	mux.HandleFunc("/health", method(http.MethodGet, health.Check))
	mux.HandleFunc("/auth/register", method(http.MethodPost, auth.Register))
	mux.HandleFunc("/auth/login", method(http.MethodPost, auth.Login))

	// ── Protected routes ──────────────────────────────────────────────────────
	protected := http.NewServeMux()

	// Auth
	protected.HandleFunc("/auth/me", method(http.MethodGet, auth.Me))

	// Lists
	protected.HandleFunc("/lists", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lists" && r.URL.Path != "/lists/" {
			http.NotFound(w, r)
			return
		}
		method(http.MethodPost, lists.Create)(w, r)
	})
	protected.HandleFunc("/lists/join", method(http.MethodPost, lists.Join))

	protected.HandleFunc("/lists/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		// ── Items ─────────────────────────────────────────────────────────────
		case matchSuffix(path, "/items") && r.Method == http.MethodPost:
			lists.AddItem(w, r)
		case matchSegment(path, "/items/") && r.Method == http.MethodPatch:
			lists.UpdateItem(w, r)
		case matchSegment(path, "/items/") && r.Method == http.MethodDelete:
			lists.DeleteItem(w, r)

		// ── Stores ────────────────────────────────────────────────────────────
		case matchSuffix(path, "/stores") && r.Method == http.MethodGet:
			stores.List(w, r)
		case matchSuffix(path, "/stores") && r.Method == http.MethodPost:
			stores.Create(w, r)
		case matchSuffix(path, "/order") && r.Method == http.MethodPost:
			stores.RecordOrder(w, r)

		// ── Trips ─────────────────────────────────────────────────────────────
		case matchSuffix(path, "/trips") && r.Method == http.MethodGet:
			trips.List(w, r)
		case matchSuffix(path, "/trips") && r.Method == http.MethodPost:
			trips.Complete(w, r)
		case matchSuffix(path, "/receipt") && r.Method == http.MethodPost:
			trips.AttachReceipt(w, r)

		// ── Suggestions ───────────────────────────────────────────────────────
		case matchSuffix(path, "/suggestions") && r.Method == http.MethodGet:
			suggestions.Get(w, r)

		// ── List CRUD ─────────────────────────────────────────────────────────
		case r.Method == http.MethodGet:
			lists.Get(w, r)
		case r.Method == http.MethodPatch:
			lists.Rename(w, r)

		default:
			http.NotFound(w, r)
		}
	})

	// WebSocket — auth middleware reads token from query string
	mux.Handle("/ws/", middleware.Authenticate(http.HandlerFunc(ws.Handler)))

	// Apply auth + CORS middleware to protected routes
	mux.Handle("/", middleware.CORS(middleware.Authenticate(protected)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	srv := &http.Server{
		Addr:         "0.0.0.0:" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("server: listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// ── Routing helpers ───────────────────────────────────────────────────────────

func method(m string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != m {
			http.Error(w, `{"message":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

func matchSuffix(path, suffix string) bool {
	return strings.HasSuffix(strings.TrimSuffix(path, "/"), strings.TrimSuffix(suffix, "/"))
}

func matchSegment(path, segment string) bool {
	return strings.Contains(path, segment)
}

// ── Migration runner ──────────────────────────────────────────────────────────

func runMigrations(ctx context.Context) error {
	sql, err := os.ReadFile("migrations/001_init.sql")
	if err != nil {
		return err
	}
	_, err = db.Pool.Exec(ctx, string(sql))
	return err
}

// internal/health/handler.go

package health

import (
	"context"
	"net/http"
	"time"

	"github.com/rafikus/cartsync/internal/db"
)

// Check performs a healthcheck and returns an SVG image showing healthy status
func Check(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// Check database connectivity
	if err := db.Pool.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}

	// Generate SVG badge
	svg := generateHealthyBadge()

	// Set response headers
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write([]byte(svg))
}

// generateHealthyBadge creates an SVG badge showing healthy status
func generateHealthyBadge() string {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="140" height="20">
  <defs>
    <linearGradient id="grad" x1="0%" y1="0%" x2="0%" y2="100%">
      <stop offset="0%" style="stop-color:rgb(255,255,255);stop-opacity:0.1" />
      <stop offset="100%" style="stop-color:rgb(0,0,0);stop-opacity:0.1" />
    </linearGradient> 
  </defs>
  <rect width="140" height="20" fill="#4caf50" rx="3"/>
  <rect width="140" height="20" fill="url(#grad)" rx="3"/>
  <g fill="#fff" font-family="DejaVu Sans,Verdana,Geneva,sans-serif" font-size="11">
    <text x="10" y="14" fill="#000" fill-opacity="0.3">Health:</text>
    <text x="9" y="13">Health:</text>
    <text x="60" y="14" fill="#000" fill-opacity="0.3">Healthy</text>
    <text x="59" y="13">Healthy</text>
  </g>
</svg>`

	return svg
}

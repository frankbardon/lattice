package realtime

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/centrifugal/centrifuge"
)

//go:embed dashboard.html
var assets embed.FS

// TokenResponse is the JSON body returned by the token-mint endpoint.
type TokenResponse struct {
	Channel string `json:"channel"`
	Token   string `json:"token"`
	Expires string `json:"expires"`
}

// Handler returns an http.Handler exposing the realtime surface:
//
//   - GET  /connection/websocket       — Parsec/centrifuge websocket transport
//   - GET  /dashboards/{id}            — thin HTML client (Alpine + centrifuge-js)
//   - GET  /dashboards/{id}/doc        — initial dashboard document snapshot (JSON)
//   - POST /dashboards/{id}/token      — mint a scoped anonymous subscribe JWT
//
// The handler is mounted by the server; it owns only realtime routes.
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/connection/websocket", centrifuge.NewWebsocketHandler(h.Node(), centrifuge.WebsocketConfig{}))
	mux.HandleFunc("POST /dashboards/{id}/token", h.handleMintToken)
	mux.HandleFunc("GET /dashboards/{id}/doc", h.handleDashboardDoc)
	mux.HandleFunc("GET /dashboards/{id}", h.handleDashboardPage)
	return mux
}

// handleDashboardDoc returns the current authoritative dashboard document as
// JSON for the thin client's initial snapshot. The client then converges by
// replaying the live patch stream. Requires a DocProvider; without one the
// endpoint is not available.
func (h *Hub) handleDashboardDoc(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "dashboard id required", http.StatusBadRequest)
		return
	}
	if h.docOf == nil {
		http.Error(w, "document reads not configured", http.StatusNotImplemented)
		return
	}
	doc, err := h.docOf(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc)
}

// handleMintToken mints a short-lived anonymous JWT scoped to the dashboard's
// channel and returns it with the channel name and expiry.
func (h *Hub) handleMintToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tok, exp, err := h.MintToken(id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	ch, _ := ChannelName(id)
	writeJSON(w, http.StatusOK, TokenResponse{
		Channel: ch.String(),
		Token:   tok,
		Expires: exp.UTC().Format(time.RFC3339),
	})
}

// handleDashboardPage serves the thin centrifuge-js client for a dashboard,
// with the dashboard id injected into the static asset.
func (h *Hub) handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "dashboard id required", http.StatusBadRequest)
		return
	}
	body, err := assets.ReadFile("dashboard.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := strings.ReplaceAll(string(body), "{{DASHBOARD_ID}}", id)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(out))
}

func (h *Hub) writeError(w http.ResponseWriter, err error) {
	code := CodeOf(err)
	status := http.StatusInternalServerError
	switch {
	case code == InvalidArgument:
		status = http.StatusBadRequest
	case strings.HasSuffix(string(code), "_NOT_FOUND"):
		// A DocProvider may surface a store NotFound; map it to 404 without
		// taking a dependency on the store package's concrete error type.
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]any{
		"code":  string(code),
		"error": err.Error(),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

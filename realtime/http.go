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
//   - GET  /dashboards/{id}            — thin HTML client (centrifuge-js)
//   - POST /dashboards/{id}/token      — mint a scoped anonymous subscribe JWT
//
// The handler is mounted by the server; it owns only realtime routes.
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/connection/websocket", centrifuge.NewWebsocketHandler(h.Node(), centrifuge.WebsocketConfig{}))
	mux.HandleFunc("POST /dashboards/{id}/token", h.handleMintToken)
	mux.HandleFunc("GET /dashboards/{id}", h.handleDashboardPage)
	return mux
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
	status := http.StatusInternalServerError
	if CodeOf(err) == InvalidArgument {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{
		"code":  string(CodeOf(err)),
		"error": err.Error(),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

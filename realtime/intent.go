package realtime

import (
	"context"
	"encoding/json"

	"github.com/centrifugal/centrifuge"
)

// intentMethod is the RPC method clients call to submit an intent. Clients send
// intents request/response over RPC rather than publishing to the channel: the
// server is authoritative, so only the server publishes to the channel, and a
// client learns the outcome (applied patch or rejection) from the RPC reply.
const intentMethod = "intent"

// IntentRequest is the RPC payload a client sends to mutate a dashboard. The
// dashboard id is explicit so the server can route the intent to the right
// authoritative document; Intent is the opaque intent body (decoded by the
// scene engine).
type IntentRequest struct {
	DashboardID string          `json:"dashboard_id"`
	Intent      json.RawMessage `json:"intent"`
}

// handleRPC dispatches an inbound client RPC. Only the intent method is
// supported; anything else is rejected so the surface stays small and the
// server remains the sole authority over board state.
func (h *Hub) handleRPC(ctx context.Context, _ string, event centrifuge.RPCEvent) ([]byte, error) {
	if event.Method != intentMethod {
		return nil, newError(InvalidArgument, "unknown rpc method")
	}
	if h.onIntent == nil {
		return nil, newError(Internal, "intent handling not configured")
	}
	var req IntentRequest
	if err := json.Unmarshal(event.Data, &req); err != nil {
		return nil, newError(InvalidArgument, "malformed intent request")
	}
	if req.DashboardID == "" {
		return nil, newError(InvalidArgument, "intent requires a dashboard_id")
	}
	res, err := h.onIntent(ctx, req.DashboardID, req.Intent)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(res)
	if err != nil {
		return nil, wrapError(Internal, "marshal intent result", err)
	}
	return body, nil
}

// BroadcastPatch publishes an applied RFC6902 patch on the dashboard's patches
// topic. It is the realtime side of the server-authoritative loop: the scene
// engine calls this after persisting each applied patch so every subscriber
// converges on the same document. It satisfies scene.Broadcaster.
func (h *Hub) BroadcastPatch(ctx context.Context, dashboardID string, patch json.RawMessage) error {
	return h.Publish(ctx, dashboardID, TopicPatches, patch)
}

// RenderedData is the body of a TopicRendered envelope: the outcome of rendering
// one brick. The shape is the client contract — the thin client
// (realtime/dashboard.html) slots Data.HTML by BrickID on success — so the
// success-path field names must not drift from {brick_id, html}.
//
// A render can also FAIL (e.g. a hand-edited pulse_prism template with bad JSON
// or an unknown column): rendering is decoupled from the edit_template intent
// (the intent only stores the template string and succeeds), so a render failure
// has no other path back to the client. When Error is set, Code carries the
// server's typed render error code and HTML is empty; subscribers surface the
// error against the brick (and, for the editor, inline in the edit panel) instead
// of slotting a fragment, so the board never crashes on a bad template. Both
// fields are omitempty so the success envelope stays exactly {brick_id, html}.
type RenderedData struct {
	// BrickID is the brick the outcome belongs to.
	BrickID string `json:"brick_id"`
	// HTML is the finished, server-rendered fragment to slot into that brick.
	HTML string `json:"html"`
	// Error is the human-readable render error message, set only on failure.
	Error string `json:"error,omitempty"`
	// Code is the server's typed render error code (render.Code), set on failure.
	Code string `json:"code,omitempty"`
}

// BroadcastRendered publishes a server-rendered brick fragment on the dashboard's
// rendered topic. The render pipeline calls this after rendering a brick (e.g.
// following a template edit) so every subscriber slots the same fragment.
func (h *Hub) BroadcastRendered(ctx context.Context, dashboardID, brickID, html string) error {
	return h.Publish(ctx, dashboardID, TopicRendered, RenderedData{BrickID: brickID, HTML: html})
}

// BroadcastRenderError publishes a render FAILURE on the dashboard's rendered
// topic so every subscriber learns the brick's template did not render. It
// carries the server's typed code and message; the thin client shows it against
// the brick (and inline in the editor) rather than treating the silence as a
// blank chart. This is the only path a render error reaches the client, since
// rendering is decoupled from the (already-acked) edit_template intent.
func (h *Hub) BroadcastRenderError(ctx context.Context, dashboardID, brickID, code, msg string) error {
	return h.Publish(ctx, dashboardID, TopicRendered, RenderedData{BrickID: brickID, Code: code, Error: msg})
}

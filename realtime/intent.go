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

// RenderedData is the body of a TopicRendered envelope: a server-rendered
// HTML/SVG fragment for one brick. The shape is the client contract — the thin
// client (realtime/dashboard.html) slots Data.HTML by BrickID — so the field
// names must not drift from {brick_id, html}.
type RenderedData struct {
	// BrickID is the brick the fragment belongs to.
	BrickID string `json:"brick_id"`
	// HTML is the finished, server-rendered fragment to slot into that brick.
	HTML string `json:"html"`
}

// BroadcastRendered publishes a server-rendered brick fragment on the dashboard's
// rendered topic. The render pipeline calls this after rendering a brick (e.g.
// following a template edit) so every subscriber slots the same fragment.
func (h *Hub) BroadcastRendered(ctx context.Context, dashboardID, brickID, html string) error {
	return h.Publish(ctx, dashboardID, TopicRendered, RenderedData{BrickID: brickID, HTML: html})
}

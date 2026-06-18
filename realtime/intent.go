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

// brickChatMethod is the RPC method clients call to drive a brick's AI builder
// agent with a chat message (E4-S2). The server routes it to the brick build
// loop, which drives the agent, validates the emitted parameterized template,
// and applies it as an edit_template intent through the scene engine — so the
// rendered fragment reaches every viewer via the normal rendered topic, and the
// RPC reply is the applied patch (an ack for the requesting client). E4-S3 (the
// chat UI) calls this method.
const brickChatMethod = "brick_chat"

// boardChatMethod is the RPC method clients call to drive the board's LAYOUT
// coordinator agent with a chat message (E5-S1). The server routes it to the
// layout build loop, which drives the layout agent, validates the emitted plan,
// applies each layout action (add/move/resize/delete) as a server-authoritative
// scene intent, and delegates the content of each created brick to that brick's
// own agent. Every patch + rendered fragment reaches viewers over the normal
// topics; the RPC reply summarizes the applied patches + created bricks (an ack
// for the requesting client). It mirrors brick_chat's shape but targets the
// whole board rather than one brick. E5-S2 (the board chat UI) calls this.
const boardChatMethod = "board_chat"

// IntentRequest is the RPC payload a client sends to mutate a dashboard. The
// dashboard id is explicit so the server can route the intent to the right
// authoritative document; Intent is the opaque intent body (decoded by the
// scene engine).
type IntentRequest struct {
	DashboardID string          `json:"dashboard_id"`
	Intent      json.RawMessage `json:"intent"`
}

// BrickChatRequest is the RPC payload a client sends to drive a brick's AI
// builder agent. DashboardID + BrickID target the brick (the server resolves
// its agent_id authoritatively); Message is the user's chat input. The brick's
// agent_id is intentionally NOT on the wire — the client never picks which
// engine runs; the server reads it from the brick.
type BrickChatRequest struct {
	DashboardID string `json:"dashboard_id"`
	BrickID     string `json:"brick_id"`
	Message     string `json:"message"`
}

// BrickChatResult is the RPC reply for a brick_chat call: the applied RFC6902
// patch (the edit_template the agent's template produced) and the canonical
// template that was applied. The rendered SVG fragment reaches every viewer over
// the rendered topic (via the scene RenderHook), not in this reply.
type BrickChatResult struct {
	Patch    json.RawMessage `json:"patch,omitempty"`
	Template string          `json:"template,omitempty"`
}

// BrickChatHandler runs the brick build loop for one chat message: it drives the
// brick's agent, validates the emitted parameterized template, and applies it as
// an edit_template intent (server-authoritative). It is satisfied by the
// brickagent Builder (bound in cmd/server). dashboardID + brickID target the
// brick; message is the chat input; it returns the applied patch + canonical
// template, or a coded error on rejection.
type BrickChatHandler func(ctx context.Context, dashboardID, brickID, message string) (BrickChatResult, error)

// BoardChatRequest is the RPC payload a client sends to drive the board's LAYOUT
// coordinator agent. DashboardID targets the board; Message is the user's
// chat input. There is no agent id on the wire — the server keys the layout
// engine off the dashboard id (mirroring brick_chat keeping the brick's
// agent_id off the wire).
type BoardChatRequest struct {
	DashboardID string `json:"dashboard_id"`
	Message     string `json:"message"`
}

// BoardChatResult is the RPC reply for a board_chat call: the applied RFC6902
// patches (one per layout action) and the bricks created + delegated this turn.
// The rendered SVG fragments reach every viewer over the rendered topic (via the
// scene RenderHook), not in this reply.
type BoardChatResult struct {
	Patches []json.RawMessage `json:"patches,omitempty"`
	Created []BoardChatBrick  `json:"created,omitempty"`
}

// BoardChatBrick reports one brick the coordinator created and delegated.
type BoardChatBrick struct {
	BrickID       string `json:"brick_id"`
	AgentID       string `json:"agent_id"`
	Prompt        string `json:"prompt,omitempty"`
	DelegateError string `json:"delegate_error,omitempty"`
}

// BoardChatHandler runs the layout build loop for one board-level chat message:
// it drives the layout coordinator agent, validates the emitted plan, applies
// each layout action as a server-authoritative scene intent, and delegates the
// content of each created brick to that brick's agent. It is satisfied by the
// layoutagent Coordinator (bound in cmd/server). dashboardID targets the board;
// message is the chat input; it returns the applied patches + created bricks, or
// a coded error on rejection.
type BoardChatHandler func(ctx context.Context, dashboardID, message string) (BoardChatResult, error)

// handleRPC dispatches an inbound client RPC. Two methods are supported — intent
// (board mutations) and brick_chat (drive a brick's AI builder) — and anything
// else is rejected so the surface stays small and the server remains the sole
// authority over board state.
func (h *Hub) handleRPC(ctx context.Context, _ string, event centrifuge.RPCEvent) ([]byte, error) {
	switch event.Method {
	case intentMethod:
		return h.handleIntentRPC(ctx, event)
	case brickChatMethod:
		return h.handleBrickChatRPC(ctx, event)
	case boardChatMethod:
		return h.handleBoardChatRPC(ctx, event)
	default:
		return nil, newError(InvalidArgument, "unknown rpc method")
	}
}

// handleIntentRPC applies a board-mutation intent.
func (h *Hub) handleIntentRPC(ctx context.Context, event centrifuge.RPCEvent) ([]byte, error) {
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

// handleBrickChatRPC routes a chat message to the brick build loop.
func (h *Hub) handleBrickChatRPC(ctx context.Context, event centrifuge.RPCEvent) ([]byte, error) {
	if h.onBrickChat == nil {
		return nil, newError(Internal, "brick chat handling not configured")
	}
	var req BrickChatRequest
	if err := json.Unmarshal(event.Data, &req); err != nil {
		return nil, newError(InvalidArgument, "malformed brick_chat request")
	}
	if req.DashboardID == "" {
		return nil, newError(InvalidArgument, "brick_chat requires a dashboard_id")
	}
	if req.BrickID == "" {
		return nil, newError(InvalidArgument, "brick_chat requires a brick_id")
	}
	if req.Message == "" {
		return nil, newError(InvalidArgument, "brick_chat requires a message")
	}
	res, err := h.onBrickChat(ctx, req.DashboardID, req.BrickID, req.Message)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(res)
	if err != nil {
		return nil, wrapError(Internal, "marshal brick_chat result", err)
	}
	return body, nil
}

// handleBoardChatRPC routes a board-level chat message to the layout build loop.
func (h *Hub) handleBoardChatRPC(ctx context.Context, event centrifuge.RPCEvent) ([]byte, error) {
	if h.onBoardChat == nil {
		return nil, newError(Internal, "board chat handling not configured")
	}
	var req BoardChatRequest
	if err := json.Unmarshal(event.Data, &req); err != nil {
		return nil, newError(InvalidArgument, "malformed board_chat request")
	}
	if req.DashboardID == "" {
		return nil, newError(InvalidArgument, "board_chat requires a dashboard_id")
	}
	if req.Message == "" {
		return nil, newError(InvalidArgument, "board_chat requires a message")
	}
	res, err := h.onBoardChat(ctx, req.DashboardID, req.Message)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(res)
	if err != nil {
		return nil, wrapError(Internal, "marshal board_chat result", err)
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

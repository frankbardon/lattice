package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/centrifugal/centrifuge"
)

func newIntentHub(t *testing.T, handler IntentHandler) *Hub {
	t.Helper()
	h, err := NewHub([]byte(testSecret), Options{Logger: slog.New(slog.DiscardHandler), IntentHandler: handler})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	return h
}

func TestHandleRPCDispatchesIntent(t *testing.T) {
	var gotID string
	var gotIntent json.RawMessage
	h := newIntentHub(t, func(_ context.Context, dashboardID string, raw json.RawMessage) (IntentResult, error) {
		gotID = dashboardID
		gotIntent = raw
		return IntentResult{Patch: json.RawMessage(`[{"op":"replace","path":"/x","value":1}]`)}, nil
	})

	req, _ := json.Marshal(IntentRequest{
		DashboardID: "d1",
		Intent:      json.RawMessage(`{"type":"move_brick","brick_id":"b1"}`),
	})
	out, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: intentMethod, Data: req})
	if err != nil {
		t.Fatalf("handleRPC: %v", err)
	}
	if gotID != "d1" {
		t.Fatalf("dashboard id = %q, want d1", gotID)
	}
	if string(gotIntent) != `{"type":"move_brick","brick_id":"b1"}` {
		t.Fatalf("intent body not forwarded: %s", gotIntent)
	}
	var res IntentResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(res.Patch) == 0 {
		t.Fatal("reply missing applied patch")
	}
}

func TestHandleRPCErrors(t *testing.T) {
	h := newIntentHub(t, func(context.Context, string, json.RawMessage) (IntentResult, error) {
		return IntentResult{}, newError(InvalidArgument, "bad intent")
	})

	// Unknown method.
	if _, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: "other"}); err == nil {
		t.Fatal("unknown method must error")
	}
	// Malformed payload.
	if _, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: intentMethod, Data: []byte("nope")}); err == nil {
		t.Fatal("malformed payload must error")
	}
	// Missing dashboard id.
	missing, _ := json.Marshal(IntentRequest{Intent: json.RawMessage(`{}`)})
	if _, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: intentMethod, Data: missing}); err == nil {
		t.Fatal("missing dashboard id must error")
	}
	// Handler error propagates.
	req, _ := json.Marshal(IntentRequest{DashboardID: "d1", Intent: json.RawMessage(`{}`)})
	_, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: intentMethod, Data: req})
	if err == nil || CodeOf(err) != InvalidArgument {
		t.Fatalf("handler error not propagated: %v", err)
	}
}

func newBrickChatHub(t *testing.T, handler BrickChatHandler) *Hub {
	t.Helper()
	h, err := NewHub([]byte(testSecret), Options{Logger: slog.New(slog.DiscardHandler), BrickChatHandler: handler})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	return h
}

func TestHandleRPCDispatchesBrickChat(t *testing.T) {
	var gotDash, gotBrick, gotMsg string
	h := newBrickChatHub(t, func(_ context.Context, dashboardID, brickID, message string) (BrickChatResult, error) {
		gotDash, gotBrick, gotMsg = dashboardID, brickID, message
		return BrickChatResult{
			Patch:    json.RawMessage(`[{"op":"replace","path":"/bricks/0/template","value":"t"}]`),
			Template: `{"pulse_request":{},"prism_spec":{"mark":"bar"}}`,
		}, nil
	})

	req, _ := json.Marshal(BrickChatRequest{DashboardID: "d1", BrickID: "b1", Message: "revenue by month"})
	out, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: brickChatMethod, Data: req})
	if err != nil {
		t.Fatalf("handleRPC: %v", err)
	}
	if gotDash != "d1" || gotBrick != "b1" || gotMsg != "revenue by month" {
		t.Fatalf("forwarded args = (%q,%q,%q)", gotDash, gotBrick, gotMsg)
	}
	var res BrickChatResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(res.Patch) == 0 || res.Template == "" {
		t.Fatalf("reply missing patch/template: %+v", res)
	}
}

func TestHandleRPCBrickChatErrors(t *testing.T) {
	h := newBrickChatHub(t, func(context.Context, string, string, string) (BrickChatResult, error) {
		return BrickChatResult{}, newError(InvalidArgument, "nope")
	})
	// Missing fields are rejected before the handler runs.
	for _, body := range []BrickChatRequest{
		{BrickID: "b1", Message: "m"},
		{DashboardID: "d1", Message: "m"},
		{DashboardID: "d1", BrickID: "b1"},
	} {
		raw, _ := json.Marshal(body)
		if _, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: brickChatMethod, Data: raw}); err == nil {
			t.Fatalf("expected rejection for %+v", body)
		}
	}
	// Malformed payload.
	if _, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: brickChatMethod, Data: []byte("nope")}); err == nil {
		t.Fatal("malformed brick_chat must error")
	}
	// Handler error propagates with its code.
	ok, _ := json.Marshal(BrickChatRequest{DashboardID: "d1", BrickID: "b1", Message: "m"})
	if _, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: brickChatMethod, Data: ok}); err == nil || CodeOf(err) != InvalidArgument {
		t.Fatalf("handler error not propagated: %v", err)
	}
}

// TestHandleRPCBrickChatNotConfigured: a brick_chat RPC with no handler wired is
// rejected rather than panicking.
func TestHandleRPCBrickChatNotConfigured(t *testing.T) {
	h := newIntentHub(t, func(context.Context, string, json.RawMessage) (IntentResult, error) {
		return IntentResult{}, nil
	})
	req, _ := json.Marshal(BrickChatRequest{DashboardID: "d1", BrickID: "b1", Message: "m"})
	if _, err := h.handleRPC(context.Background(), "", centrifuge.RPCEvent{Method: brickChatMethod, Data: req}); err == nil {
		t.Fatal("brick_chat with no handler must error")
	}
}

func TestBroadcastPatchPublishesOnPatchesTopic(t *testing.T) {
	h := newIntentHub(t, func(context.Context, string, json.RawMessage) (IntentResult, error) {
		return IntentResult{}, nil
	})
	// Drive the broker so Publish can land.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()
	for !h.Started() {
		time.Sleep(2 * time.Millisecond)
	}
	t.Cleanup(func() { cancel(); <-done })

	if err := h.BroadcastPatch(context.Background(), "d1", json.RawMessage(`[{"op":"add","path":"/bricks/-","value":{}}]`)); err != nil {
		t.Fatalf("BroadcastPatch: %v", err)
	}
}

// TestRenderedDataWireShape pins the rendered-topic envelope body to the
// client contract: realtime/dashboard.html slots data.html by data.brick_id, so
// the JSON must be exactly {brick_id, html}.
func TestRenderedDataWireShape(t *testing.T) {
	raw, err := json.Marshal(RenderedData{BrickID: "b1", HTML: "<h1>hi</h1>"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// encoding/json HTML-escapes <,>,& by default; the client unescapes on the
	// way in. What matters for the contract is the exact field names brick_id
	// and html, so assert on the decoded map.
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["brick_id"] != "b1" {
		t.Fatalf("brick_id = %v, want b1", got["brick_id"])
	}
	if got["html"] != "<h1>hi</h1>" {
		t.Fatalf("html = %v, want <h1>hi</h1>", got["html"])
	}
	if len(got) != 2 {
		t.Fatalf("envelope body has unexpected fields: %v", got)
	}
}

// TestRenderedDataErrorWireShape pins the render-FAILURE envelope body: on a bad
// template the client receives {brick_id, code, error} (html omitted) and shows
// the typed error against the brick. The success path stays {brick_id, html}
// (asserted by TestRenderedDataWireShape) since code/error are omitempty.
func TestRenderedDataErrorWireShape(t *testing.T) {
	raw, err := json.Marshal(RenderedData{BrickID: "b1", Code: "RENDER_INVALID_TEMPLATE", Error: "bad json"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["brick_id"] != "b1" {
		t.Fatalf("brick_id = %v, want b1", got["brick_id"])
	}
	if got["code"] != "RENDER_INVALID_TEMPLATE" {
		t.Fatalf("code = %v, want RENDER_INVALID_TEMPLATE", got["code"])
	}
	if got["error"] != "bad json" {
		t.Fatalf("error = %v, want bad json", got["error"])
	}
	if got["html"] != "" {
		t.Fatalf("html must be empty on error, got %v", got["html"])
	}
}

// TestBroadcastRenderedPublishesOnRenderedTopic drives the broker and confirms a
// rendered fragment publishes without error.
func TestBroadcastRenderedPublishesOnRenderedTopic(t *testing.T) {
	h := newIntentHub(t, func(context.Context, string, json.RawMessage) (IntentResult, error) {
		return IntentResult{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()
	for !h.Started() {
		time.Sleep(2 * time.Millisecond)
	}
	t.Cleanup(func() { cancel(); <-done })

	if err := h.BroadcastRendered(context.Background(), "d1", "b1", "<h1>hi</h1>"); err != nil {
		t.Fatalf("BroadcastRendered: %v", err)
	}
}

// guard: IntentHandler errors should remain *Error-typed through the stack.
var _ = errors.Is

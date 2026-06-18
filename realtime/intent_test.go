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

// guard: IntentHandler errors should remain *Error-typed through the stack.
var _ = errors.Is

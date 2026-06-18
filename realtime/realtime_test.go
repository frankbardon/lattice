package realtime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/centrifugal/centrifuge"

	"github.com/frankbardon/parsec/auth"
)

const testSecret = "test-hmac-secret-do-not-use-in-prod"

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	h, err := NewHub([]byte(testSecret), Options{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()
	for !h.Started() {
		time.Sleep(2 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return h
}

func TestChannelName(t *testing.T) {
	n, err := ChannelName("board-42")
	if err != nil {
		t.Fatalf("ChannelName: %v", err)
	}
	if got := n.String(); got != "public:dashboards.board.board-42" {
		t.Fatalf("channel = %q, want public:dashboards.board.board-42", got)
	}
	if _, err := ChannelName(""); err == nil {
		t.Fatal("empty id must error")
	}
}

func TestMintTokenScopesToOneChannel(t *testing.T) {
	h := newTestHub(t)

	tok, exp, err := h.MintToken("abc")
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	if !exp.After(time.Now()) {
		t.Fatalf("token already expired: %v", exp)
	}

	claims, err := h.verifier.Verify(tok, auth.TypeAccess)
	if err != nil {
		t.Fatalf("verify minted token: %v", err)
	}
	mine, _ := ChannelName("abc")
	other, _ := ChannelName("xyz")
	if !claims.Authorizes(mine.String(), auth.VerbSubscribe) {
		t.Fatal("token must authorize subscribe on its own channel")
	}
	if claims.Authorizes(other.String(), auth.VerbSubscribe) {
		t.Fatal("token must NOT authorize a different dashboard channel")
	}
}

func TestSubscribeAuthorizer(t *testing.T) {
	h := newTestHub(t)
	authz := newSubscribeAuthorizer(h)

	tok, _, err := h.MintToken("abc")
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	mine, _ := ChannelName("abc")
	other, _ := ChannelName("xyz")
	ctx := context.Background()

	// Valid token on its own channel: allowed.
	if err := authz(ctx, "", mine, centrifuge.SubscribeEvent{Token: tok}); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	// Same token on a different dashboard channel: denied.
	if err := authz(ctx, "", other, centrifuge.SubscribeEvent{Token: tok}); err == nil {
		t.Fatal("token must be rejected on a different dashboard channel")
	}
	// No token: denied.
	if err := authz(ctx, "", mine, centrifuge.SubscribeEvent{}); err == nil {
		t.Fatal("missing token must be rejected")
	}
}

func TestPresenceCount(t *testing.T) {
	h := newTestHub(t)
	ch, _ := ChannelName("count")

	if v, _ := h.Viewers("count"); v != 0 {
		t.Fatalf("initial viewers = %d, want 0", v)
	}

	h.onSubscriberChange(ch, +1)
	h.onSubscriberChange(ch, +1)
	if v, _ := h.Viewers("count"); v != 2 {
		t.Fatalf("after two joins viewers = %d, want 2", v)
	}

	h.onSubscriberChange(ch, -1)
	if v, _ := h.Viewers("count"); v != 1 {
		t.Fatalf("after one leave viewers = %d, want 1", v)
	}

	// Floor at zero even on an over-decrement.
	h.onSubscriberChange(ch, -5)
	if v, _ := h.Viewers("count"); v != 0 {
		t.Fatalf("over-decrement viewers = %d, want 0", v)
	}
}

func TestPublishEnvelopeRoundTrip(t *testing.T) {
	h := newTestHub(t)
	// Publish should open the channel and succeed without a subscriber.
	if err := h.Publish(context.Background(), "pub", TopicRendered, map[string]string{"html": "<div/>"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

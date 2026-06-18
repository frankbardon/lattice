// Package realtime wires the embedded Parsec broker into lattice. It owns the
// one-channel-per-dashboard model, mints short-lived anonymous HMAC JWTs that
// scope a browser to a single dashboard channel, and broadcasts a live viewer
// (presence) count over that channel.
//
// v1 is anonymous: the dashboard link is the access grant. There are no
// accounts and no ownership. Presence is a count only — named cursors are out
// of scope.
//
// # Channel model
//
// Each dashboard gets exactly one Parsec channel,
// public:dashboards.board.<id>. All server→client traffic rides that single
// channel; the message kind (patches, rendered, presence) is carried in the
// Envelope.Topic field rather than as separate Parsec sub-channels. Keeping it
// to one channel name lets the minted JWT scope a client to precisely that
// dashboard and nothing else.
package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/centrifugal/centrifuge"

	"github.com/frankbardon/parsec"
	"github.com/frankbardon/parsec/auth"
	"github.com/frankbardon/parsec/channels"

	"log/slog"
)

// Channel topics. The topic is carried inside the Envelope, not as a Parsec
// sub-channel, so one JWT scopes a client to the whole dashboard stream.
const (
	// TopicPatches carries RFC6902 patches to the scene document.
	TopicPatches = "patches"
	// TopicRendered carries server-rendered HTML/SVG fragments.
	TopicRendered = "rendered"
	// TopicPresence carries the live viewer count.
	TopicPresence = "presence"
)

const (
	// channelApp is the app segment of every dashboard channel name.
	channelApp = "dashboards"
	// channelDomain is the domain segment of every dashboard channel name.
	channelDomain = "board"
	// channelTTL keeps the public channel open well past a token's lifetime;
	// the hub re-opens on demand so a swept channel never wedges a board.
	channelTTL = 24 * time.Hour
	// tokenTTL bounds a minted subscribe token. Short-lived: the link is the
	// grant, the token is just the handshake. Parsec clamps this to [1m, 1h].
	tokenTTL = 15 * time.Minute
)

// Envelope is the lattice wire payload published on a dashboard channel. The
// Topic discriminates the message kind; Data is the topic-specific body.
type Envelope struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}

// PresenceData is the body of a TopicPresence envelope.
type PresenceData struct {
	// Viewers is the number of clients currently subscribed to the dashboard.
	Viewers int `json:"viewers"`
}

// Options configures a Hub.
type Options struct {
	// Logger receives hub events. Defaults to slog.Default().
	Logger *slog.Logger
}

// Hub is the realtime façade for lattice. It embeds a Parsec instance, tracks
// per-dashboard viewer counts, and mints scoped subscribe tokens. Construct
// with NewHub and start the underlying broker with Run.
type Hub struct {
	parsec   *parsec.Parsec
	verifier *auth.Verifier
	logger   *slog.Logger

	mu      sync.Mutex
	viewers map[string]int // channel name -> live subscriber count
}

// NewHub constructs a Hub backed by an embedded Parsec instance signing with
// the supplied HMAC secret. The secret seeds the keyring so minted tokens
// verify against the same key without a state directory; it must come from
// config/env and never be hardcoded.
func NewHub(secret []byte, opts Options) (*Hub, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if len(secret) == 0 {
		return nil, newError(InvalidArgument, "hmac secret required")
	}

	ring, err := auth.NewKeyRingFromSecret(secret)
	if err != nil {
		return nil, wrapError(Internal, "build keyring", err)
	}
	// Build a verifier over the same ring so the subscribe authorizer can
	// validate tokens before *parsec.Parsec exists (parsec.New consumes the
	// authorizer at construction time).
	verifier, err := auth.NewVerifier(ring)
	if err != nil {
		return nil, wrapError(Internal, "build verifier", err)
	}

	h := &Hub{
		verifier: verifier,
		logger:   logger,
		viewers:  make(map[string]int),
	}

	p, err := parsec.New(parsec.Options{
		KeyRing:       ring,
		Logger:        logger,
		BrokerOptions: brokerOptions(h),
	})
	if err != nil {
		return nil, wrapError(Internal, "build parsec", err)
	}
	h.parsec = p
	return h, nil
}

// Run starts the embedded broker and blocks until ctx is canceled. Callers
// typically run it in a goroutine and gate first use on Started.
func (h *Hub) Run(ctx context.Context) error {
	if err := h.parsec.Run(ctx); err != nil {
		return wrapError(Internal, "run parsec", err)
	}
	return nil
}

// Started reports whether the broker has finished booting.
func (h *Hub) Started() bool { return h.parsec.Broker().Started() }

// Node exposes the underlying centrifuge node so the HTTP layer can mount the
// websocket transport.
func (h *Hub) Node() *centrifuge.Node { return h.parsec.Broker().Node() }

// ChannelName returns the canonical channel name for a dashboard id.
func ChannelName(dashboardID string) (channels.Name, error) {
	if dashboardID == "" {
		return channels.Name{}, newError(InvalidArgument, "dashboard id required")
	}
	n, err := channels.BuildName(channels.VisibilityPublic, channelApp, channelDomain, dashboardID, "")
	if err != nil {
		return channels.Name{}, wrapError(InvalidArgument, "invalid dashboard id", err)
	}
	return n, nil
}

// EnsureChannel opens (or re-opens) the dashboard's channel so clients can
// subscribe and the server can publish. Idempotent.
func (h *Hub) EnsureChannel(dashboardID string) (channels.Name, error) {
	n, err := ChannelName(dashboardID)
	if err != nil {
		return channels.Name{}, err
	}
	if _, err := h.parsec.OpenPublic(n.String(), channelTTL); err != nil {
		return channels.Name{}, wrapError(Internal, "open channel", err)
	}
	return n, nil
}

// MintToken issues a short-lived anonymous HMAC JWT that grants subscribe to
// exactly one dashboard channel. The channel is opened as a side effect so the
// client can subscribe immediately. The returned expiry is the token's
// absolute expiration.
func (h *Hub) MintToken(dashboardID string) (token string, expires time.Time, err error) {
	n, err := h.EnsureChannel(dashboardID)
	if err != nil {
		return "", time.Time{}, err
	}
	// Anonymous subject; the channel listed in Chs is authorized for subscribe
	// (and nothing else is, since it is the only channel in the token).
	exp := time.Now().Add(tokenTTL)
	tok, tokExp, err := h.parsec.Issuer().IssueAccess("anonymous", n.String(), exp, nil)
	if err != nil {
		return "", time.Time{}, wrapError(Internal, "issue access token", err)
	}
	return tok, tokExp, nil
}

// Publish sends a topic envelope to a dashboard's channel. The channel is
// re-opened on demand so a swept channel does not silently drop a broadcast.
func (h *Hub) Publish(ctx context.Context, dashboardID, topic string, data any) error {
	n, err := h.EnsureChannel(dashboardID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(data)
	if err != nil {
		return wrapError(Internal, "marshal payload", err)
	}
	env, err := json.Marshal(Envelope{Topic: topic, Data: body})
	if err != nil {
		return wrapError(Internal, "marshal envelope", err)
	}
	if _, err := h.parsec.Publish(ctx, n.String(), env); err != nil {
		return wrapError(Internal, "publish", err)
	}
	return nil
}

// Viewers returns the current viewer count for a dashboard.
func (h *Hub) Viewers(dashboardID string) (int, error) {
	n, err := ChannelName(dashboardID)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.viewers[n.String()], nil
}

// onSubscriberChange records a +1/-1 subscriber delta and broadcasts the new
// viewer count on the dashboard channel. It is invoked by the broker on every
// subscribe and unsubscribe.
func (h *Hub) onSubscriberChange(ch channels.Name, delta int) {
	if ch.App != channelApp || ch.Domain != channelDomain {
		return
	}
	key := ch.String()
	h.mu.Lock()
	count := h.viewers[key] + delta
	if count < 0 {
		count = 0
	}
	if count == 0 {
		delete(h.viewers, key)
	} else {
		h.viewers[key] = count
	}
	h.mu.Unlock()

	if ch.ID == "" {
		return
	}
	// Fire-and-forget the presence broadcast; subscriber-change callbacks run
	// on the broker hot path so we must not block.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.Publish(ctx, ch.ID, TopicPresence, PresenceData{Viewers: count}); err != nil {
			h.logger.Warn("realtime: presence broadcast failed", "channel", key, "error", err)
		}
	}()
}

// brokerOptions returns the broker wiring for the hub: a subscribe authorizer
// that gates dashboard channels on a token scoping them, plus the
// subscriber-change hook that drives presence.
func brokerOptions(h *Hub) brokerOpts {
	return brokerOpts{
		SubscribeAuthorizer: newSubscribeAuthorizer(h),
		OnSubscriberChange:  h.onSubscriberChange,
	}
}

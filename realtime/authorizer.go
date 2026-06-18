package realtime

import (
	"context"

	"github.com/centrifugal/centrifuge"

	"github.com/frankbardon/parsec/auth"
	"github.com/frankbardon/parsec/broker"
	"github.com/frankbardon/parsec/channels"
)

// brokerOpts aliases broker.Options so realtime.go can reference the broker
// wiring without importing the broker package itself.
type brokerOpts = broker.Options

// newSubscribeAuthorizer returns a broker subscribe authorizer that gates
// dashboard channels behind a token scoping the exact channel, while leaving
// any other (non-dashboard) public channel open.
//
// Parsec's default authorizer would let a client subscribe to ANY public
// channel with no token. Because lattice puts dashboards on public channels
// (so the channel never wedges on a private-TTL expiry), the authorizer must
// re-impose the "this dashboard only" rule itself: the client must present an
// anonymous HMAC JWT whose claims authorize subscribe on that precise channel.
func newSubscribeAuthorizer(h *Hub) func(ctx context.Context, userID string, ch channels.Name, event centrifuge.SubscribeEvent) error {
	return func(_ context.Context, _ string, ch channels.Name, event centrifuge.SubscribeEvent) error {
		if ch.App != channelApp || ch.Domain != channelDomain {
			// Not a dashboard channel; defer to Parsec's public/private rules
			// by allowing well-formed public and rejecting private here.
			if ch.IsPrivate() {
				return auth.MapErr(auth.ErrChannelMismatch)
			}
			return nil
		}
		if event.Token == "" {
			return newError(InvalidArgument, "dashboard subscribe requires an access token")
		}
		claims, err := h.verifier.Verify(event.Token, auth.TypeAccess)
		if err != nil {
			return auth.MapErr(err)
		}
		if !claims.Authorizes(ch.String(), auth.VerbSubscribe) {
			return newError(InvalidArgument, "token does not authorize this dashboard")
		}
		return nil
	}
}

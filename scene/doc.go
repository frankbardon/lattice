// Package scene is the server-authoritative convergence core for lattice.
//
// Parsec gives raw fan-out with no conflict resolution, so the server is the
// single source of truth. A Doc holds one dashboard as an in-memory document,
// accepts typed client Intents, converts each into an RFC6902 patch, applies
// it atomically (rejecting invalid patches), snapshots the full document to the
// Store, and broadcasts the applied patch to all subscribers on the patches
// topic.
//
// # Authority and concurrency
//
// Multiple clients hit one server. A Doc serialises every mutation behind a
// mutex so applies are a single ordered stream: applying the same intent
// stream to two Docs (or rehydrating from the snapshot) yields the same
// document, which is what makes two clients converge and state survive a
// restart.
//
// # Patch model
//
// Intents compile to RFC6902 (JSON Patch) operations applied via
// github.com/evanphx/json-patch/v5. Prism ships an equivalent RFC6902 engine,
// but its ApplyPatch is welded to *spec.Spec (it decodes the patched tree back
// into a Prism chart spec), so it cannot apply to a dashboard document; the
// brief's named fallback library is used instead.
//
// Render is intentionally out of scope here — this package broadcasts patches
// only; the render pipeline (and the rendered topic) is owned by a later story.
package scene

import (
	"context"
	"encoding/json"
	"sync"

	jsonpatch "github.com/evanphx/json-patch/v5"

	"github.com/frankbardon/lattice/dashboard"

	"log/slog"
)

// Store is the persistence seam the engine snapshots through. It is satisfied
// by store.Store; declared here so scene does not depend on a concrete backend.
type Store interface {
	Load(ctx context.Context, id string) (*dashboard.Dashboard, error)
	Save(ctx context.Context, doc *dashboard.Dashboard) error
}

// Broadcaster publishes an applied patch to a dashboard's subscribers. It is
// satisfied by the realtime Hub's Publish bound to the patches topic.
type Broadcaster interface {
	BroadcastPatch(ctx context.Context, dashboardID string, patch json.RawMessage) error
}

// RenderHook is invoked after an applied intent changes what a brick should
// display, so the server can render the brick's fragment and broadcast it on the
// rendered topic. It fires on two paths: an edit_template intent (the brick's
// own template changed) and a variable change (define/set/remove_variable) for
// EACH brick whose template references the affected variable. It is the
// decoupling seam: scene owns patch application and knows *what* changed, but it
// does not know *how* to render — the hook (wired in cmd/server) bridges to the
// render package and the realtime hub, so render stays out of the core patch
// path and scene never imports render.
//
// brick is the post-apply state of the brick to render; vars is the dashboard's
// full set of current variable definitions, so the hook can resolve the brick's
// ${var} placeholders before rendering (the server-side DataResolver step). No
// agent is invoked on this path — it is pure resolve+render.
type RenderHook func(ctx context.Context, dashboardID string, brick dashboard.Brick, vars []dashboard.Variable)

// Options configures a Doc.
type Options struct {
	// Logger receives engine events. Defaults to slog.Default().
	Logger *slog.Logger
	// RenderHook, when set, fires after an applied edit_template intent with the
	// changed brick so the server can render and broadcast its fragment. Leave
	// nil to disable render-on-change (patches still broadcast normally).
	RenderHook RenderHook
}

// Doc is the in-memory, server-authoritative dashboard document. Construct with
// Open (which rehydrates from the Store) and drive with Apply. Safe for
// concurrent use.
type Doc struct {
	store       Store
	broadcaster Broadcaster
	onRender    RenderHook
	logger      *slog.Logger

	mu  sync.Mutex
	doc *dashboard.Dashboard
}

// Open rehydrates the dashboard with id from the store into a new in-memory
// Doc. This is the load-on-open path: reopening a board (including after a
// server restart) restores the last snapshotted state. The store and
// broadcaster must be non-nil.
func Open(ctx context.Context, id string, st Store, bc Broadcaster, opts Options) (*Doc, error) {
	if st == nil {
		return nil, newError(Internal, "store required")
	}
	if bc == nil {
		return nil, newError(Internal, "broadcaster required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	doc, err := st.Load(ctx, id)
	if err != nil {
		return nil, wrapError(Internal, "load dashboard", err)
	}
	return &Doc{
		store:       st,
		broadcaster: bc,
		onRender:    opts.RenderHook,
		logger:      logger,
		doc:         doc,
	}, nil
}

// Snapshot returns a deep copy of the current document. Callers may read or
// mutate the copy freely without affecting the in-memory authority.
func (d *Doc) Snapshot() *dashboard.Dashboard {
	d.mu.Lock()
	defer d.mu.Unlock()
	return cloneDoc(d.doc)
}

// ID returns the dashboard id this Doc owns.
func (d *Doc) ID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.doc.ID
}

// Apply realizes an intent against the authoritative document. It is the whole
// convergence step, performed atomically under the lock:
//
//  1. resolve the intent to an RFC6902 patch against the current document;
//  2. apply the patch (rejecting an invalid one — InvalidPatch, no state change);
//  3. snapshot the full resulting document to the store;
//  4. broadcast the applied patch on the patches topic.
//
// On any failure the in-memory document is left unchanged. The applied patch
// (RFC6902 JSON) is returned so callers can ack the originating client.
func (d *Doc) Apply(ctx context.Context, in Intent) (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	patch, err := patchFor(d.doc, in)
	if err != nil {
		return nil, err
	}

	next, raw, err := applyPatch(d.doc, patch)
	if err != nil {
		return nil, err
	}

	// Persist before broadcasting: a snapshot the clients never see is
	// recoverable; a broadcast we never persisted is a divergence on restart.
	if err := d.store.Save(ctx, next); err != nil {
		return nil, wrapError(Internal, "snapshot to store", err)
	}

	d.doc = next

	if err := d.broadcaster.BroadcastPatch(ctx, next.ID, raw); err != nil {
		// State is already persisted and advanced; a dropped broadcast is a
		// transport hiccup, not a divergence — log and report it but keep the
		// authoritative document advanced (a re-open rehydrates the truth).
		d.logger.Warn("scene: patch broadcast failed", "dashboard", next.ID, "error", err)
		return raw, wrapError(Internal, "broadcast patch", err)
	}

	// Render-on-change: certain applied intents change what a brick should
	// display, so hand the renderer the new brick state to render and broadcast
	// its fragment. This is decoupled from patch application via the hook — render
	// never blocks or fails the (already persisted, already broadcast)
	// convergence step. Two cases:
	//
	//   - edit_template: the edited brick's own template changed; render it.
	//   - define/set/remove_variable: a variable changed; render EXACTLY the
	//     bricks whose templates reference that variable, leaving the rest
	//     untouched. No agent is re-invoked — this is pure server-side resolution.
	if d.onRender != nil {
		d.fireRenderHooks(ctx, next, in)
	}
	return raw, nil
}

// fireRenderHooks invokes the render hook for every brick affected by an applied
// intent. It runs after the patch is persisted and broadcast, so it never blocks
// convergence. The dashboard's current variables are passed through so the hook
// can resolve ${var} placeholders before rendering.
func (d *Doc) fireRenderHooks(ctx context.Context, next *dashboard.Dashboard, in Intent) {
	switch in.Type {
	case IntentEditTemplate:
		if i := indexOfBrick(next, in.BrickID); i >= 0 {
			d.onRender(ctx, next.ID, next.Bricks[i], next.Variables)
		}
	case IntentDefineVariable, IntentSetVariable, IntentRemoveVariable:
		// Re-render only the bricks that reference the changed variable. A removed
		// variable still triggers a re-render of its referencing bricks so their
		// ${var} placeholders fall back / surface as undefined consistently.
		for i := range next.Bricks {
			if brickReferences(next.Bricks[i].Template, in.Name) {
				d.onRender(ctx, next.ID, next.Bricks[i], next.Variables)
			}
		}
	}
}

// applyPatch applies an RFC6902 patch to a copy of doc, returning the new
// document and the canonical JSON form of the patch that was applied. The input
// document is not mutated. A patch that fails to apply yields InvalidPatch.
func applyPatch(doc *dashboard.Dashboard, patch jsonpatch.Patch) (*dashboard.Dashboard, json.RawMessage, error) {
	cur, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, wrapError(Internal, "encode document", err)
	}
	out, err := patch.ApplyWithOptions(cur, applyOptions())
	if err != nil {
		return nil, nil, wrapError(InvalidPatch, "apply patch", err)
	}
	var nextDoc dashboard.Dashboard
	if err := json.Unmarshal(out, &nextDoc); err != nil {
		return nil, nil, wrapError(InvalidPatch, "patched document failed to decode", err)
	}
	rawPatch, err := json.Marshal(patch)
	if err != nil {
		return nil, nil, wrapError(Internal, "encode applied patch", err)
	}
	return &nextDoc, rawPatch, nil
}

// applyOptions pins deterministic, standards-conforming apply behaviour: no
// non-standard negative array indices, and strict "remove"/"add" semantics so
// an out-of-range op is rejected rather than silently absorbed.
func applyOptions() *jsonpatch.ApplyOptions {
	o := jsonpatch.NewApplyOptions()
	o.SupportNegativeIndices = false
	o.AllowMissingPathOnRemove = false
	o.EnsurePathExistsOnAdd = false
	return o
}

// cloneDoc deep-copies a dashboard document via its JSON form.
func cloneDoc(doc *dashboard.Dashboard) *dashboard.Dashboard {
	raw, err := json.Marshal(doc)
	if err != nil {
		// Document is always JSON-serialisable by construction; a failure here
		// is a programmer error, so surface an empty doc rather than panic.
		return &dashboard.Dashboard{}
	}
	var out dashboard.Dashboard
	_ = json.Unmarshal(raw, &out)
	return &out
}

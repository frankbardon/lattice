// Package layoutagent is the board LAYOUT coordinator build loop (E5-S1): it
// turns a board-level chat message into an arranged board whose bricks are
// filled in by per-brick agents.
//
// The coordinator sits ABOVE the agenthub lifecycle (which stays
// lifecycle-only: boot/drive/teardown) and the scene patch engine (which stays
// the authoritative mutation path). It is the board-level peer of the
// per-brick brickagent.Builder. One call to Coordinate:
//
//  1. drives the dashboard's LAYOUT Nexus engine — its OWN engine in the hub,
//     keyed layout:<dashboard_id>, distinct from the per-brick builders — with
//     the chat message, capturing the agent's structured plan;
//  2. parses the reply into a {actions:[...]} plan and validates it against
//     PlanSchema — the coordinator emits STRUCTURE (create/move/resize/delete
//     bricks), never a Pulse/Prism template;
//  3. applies each action as the SAME authoritative scene intent a hand edit
//     takes (add_brick / move_brick / resize_brick / delete_brick through
//     HandleIntent), so every change snapshots, broadcasts its patch, and fires
//     the RenderHook like any other;
//  4. for each brick it CREATES, assigns a server-side agent_id and DELEGATES
//     that brick's content to that brick's own agent via the brick builder
//     (cross-engine delegation through the hub) — the coordinator decides what
//     bricks exist and where; the brick agent decides what each shows.
//
// The coordinator never authors brick templates and never renders or broadcasts
// directly: structure goes through scene; per-brick content goes through the
// brick builder (which itself goes through scene).
package layoutagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/frankbardon/lattice/agenthub"
	"github.com/frankbardon/lattice/dashboard"
	"github.com/frankbardon/lattice/scene"
)

// defaultBrickKind is the kind assigned to bricks the coordinator creates. New
// data bricks are pulse_prism (the brick agent fills in the Pulse/Prism
// template); it is a plain string here so layoutagent does not depend on render.
const defaultBrickKind = "pulse_prism"

// AgentDriver drives the layout coordinator's engine: it sends content to the
// agent identified by agentID and returns the agent's final assistant reply. It
// is satisfied by *agenthub.Hub (its Drive method) — the SAME interface the
// brick build loop uses — so the coordinator is unit-testable with a
// canned-reply stub (no LLM).
type AgentDriver interface {
	Drive(ctx context.Context, agentID, content string) (string, error)
}

// SceneStore is the slice of the scene layer the coordinator needs: read a
// snapshot of the authoritative document (to report current board state to the
// agent and to pick non-colliding brick ids) and apply layout actions as
// intents (the authoritative mutation path). It is satisfied by *scene.Manager
// via brickagent.SceneManagerStore (reused in cmd/server) or any equivalent.
type SceneStore interface {
	Snapshot(ctx context.Context, dashboardID string) (*dashboard.Dashboard, error)
	HandleIntent(ctx context.Context, dashboardID string, raw json.RawMessage) (json.RawMessage, error)
}

// BrickBuilder constructs a created brick's content by driving that brick's own
// agent: given a brick id and a per-brick prompt, it drives the brick agent and
// applies the emitted parameterized template through scene. The coordinator
// only needs to know whether the delegation succeeded, so this returns just an
// error. A thin adapter in cmd/server bridges *brickagent.Builder (whose Build
// returns brickagent.Result, error) to this interface; declaring it narrowly
// keeps layoutagent free of a brickagent import and makes delegation mockable
// in tests.
type BrickBuilder interface {
	BuildBrick(ctx context.Context, dashboardID, brickID, prompt string) error
}

// Options configures a Coordinator.
type Options struct {
	// Logger receives build-loop events. Defaults to slog.Default().
	Logger *slog.Logger
}

// Coordinator runs the layout build loop. Construct with NewCoordinator; call
// Coordinate for each board-level chat message. Safe for concurrent use (it
// holds no per-request state; its collaborators are concurrency-safe).
type Coordinator struct {
	driver AgentDriver
	scenes SceneStore
	bricks BrickBuilder
	logger *slog.Logger
	newID  func() string
}

// NewCoordinator constructs a Coordinator over a layout-agent driver, a scene
// store, and a brick builder (for delegation). All three are required.
func NewCoordinator(driver AgentDriver, scenes SceneStore, bricks BrickBuilder, opts Options) (*Coordinator, error) {
	if driver == nil {
		return nil, newError(Internal, "agent driver required")
	}
	if scenes == nil {
		return nil, newError(Internal, "scene store required")
	}
	if bricks == nil {
		return nil, newError(Internal, "brick builder required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Coordinator{
		driver: driver,
		scenes: scenes,
		bricks: bricks,
		logger: logger,
		newID:  randomToken,
	}, nil
}

// CreatedBrick records one brick the coordinator created and delegated.
type CreatedBrick struct {
	// BrickID is the server-assigned brick id.
	BrickID string `json:"brick_id"`
	// AgentID is the server-assigned agent_id bound to the brick.
	AgentID string `json:"agent_id"`
	// Prompt is the per-brick prompt that was delegated to the brick agent.
	Prompt string `json:"prompt"`
	// DelegateError, when non-empty, is the coded error from delegating the
	// brick's content build. The brick still exists (its add_brick applied); only
	// the content build failed, so the board is left with an empty brick the user
	// can re-drive via brick_chat.
	DelegateError string `json:"delegate_error,omitempty"`
}

// Result is the outcome of a successful layout build: the applied structural
// patches (one per applied action) and the bricks created + delegated.
type Result struct {
	// Patches is the applied RFC6902 patches, in action order (the add/move/
	// resize/delete the scene engine produced). Echoed to the requesting client.
	Patches []json.RawMessage `json:"patches,omitempty"`
	// Created lists the bricks created this turn, with their server-assigned ids
	// and the delegation outcome for each.
	Created []CreatedBrick `json:"created,omitempty"`
}

// AgentID returns the layout coordinator agentID for a dashboard. It is keyed
// distinctly from the per-brick builders (agenthub.LayoutAgentPrefix) so the
// two agent types share one Hub but run as separate engines.
func AgentID(dashboardID string) string {
	return agenthub.LayoutAgentPrefix + dashboardID
}

// Coordinate runs the loop for one board-level chat message: drive the layout
// agent, validate its plan, apply each action through scene, and delegate the
// content of each created brick to that brick's agent. dashboardID targets the
// board; message is the user's chat input.
//
// Any failure to drive or parse the agent is returned as a coded *Error and
// leaves the board unchanged. A delegation failure for an individual brick is
// NOT fatal — the brick exists (its add_brick applied) and the failure is
// reported on that brick's CreatedBrick.DelegateError — so a single brick
// agent's miss does not unwind the whole arranged board.
func (c *Coordinator) Coordinate(ctx context.Context, dashboardID, message string) (Result, error) {
	if dashboardID == "" {
		return Result{}, newError(InvalidRequest, "dashboard_id required")
	}
	if strings.TrimSpace(message) == "" {
		return Result{}, newError(InvalidRequest, "message required")
	}

	agentID := AgentID(dashboardID)

	prompt, err := c.buildPrompt(ctx, dashboardID, message)
	if err != nil {
		return Result{}, err
	}

	reply, err := c.driver.Drive(ctx, agentID, prompt)
	if err != nil {
		return Result{}, wrapError(AgentFailed, "drive layout agent", err)
	}

	plan, err := parsePlan(reply)
	if err != nil {
		return Result{}, err
	}

	return c.applyPlan(ctx, dashboardID, plan)
}

// buildPrompt augments the user's message with the current board state (brick
// ids + their layout) so the coordinator can reference existing bricks by id
// for move/resize/delete. A read failure is non-fatal — the agent can still
// build a fresh board — so an open error only drops the context, it does not
// abort the turn.
func (c *Coordinator) buildPrompt(ctx context.Context, dashboardID, message string) (string, error) {
	snap, err := c.scenes.Snapshot(ctx, dashboardID)
	if err != nil {
		return "", wrapError(Internal, "open dashboard document", err)
	}
	var b strings.Builder
	b.WriteString(message)
	if len(snap.Bricks) > 0 {
		b.WriteString("\n\nCurrent board bricks (reference these by brick_id):\n")
		for _, br := range snap.Bricks {
			fmt.Fprintf(&b, "- brick_id=%s pos=(%d,%d) size=(%dx%d)\n",
				br.ID, br.Layout.Pos.X, br.Layout.Pos.Y, br.Layout.Size.Width, br.Layout.Size.Height)
		}
	} else {
		b.WriteString("\n\nThe board is currently empty.")
	}
	return b.String(), nil
}

// applyPlan applies each action in order through scene and delegates each
// created brick's content to its agent.
func (c *Coordinator) applyPlan(ctx context.Context, dashboardID string, plan Plan) (Result, error) {
	var res Result
	for _, a := range plan.Actions {
		switch a.Type {
		case ActionCreateBrick:
			created, patch, err := c.createBrick(ctx, dashboardID, a)
			if err != nil {
				return res, err
			}
			res.Patches = append(res.Patches, patch)
			res.Created = append(res.Created, created)
		default:
			patch, err := c.applyMutation(ctx, dashboardID, a)
			if err != nil {
				return res, err
			}
			res.Patches = append(res.Patches, patch)
		}
	}
	c.logger.Info("layoutagent: coordinated board",
		"dashboard", dashboardID, "actions", len(plan.Actions), "created", len(res.Created))
	return res, nil
}

// createBrick assigns a server-side brick id + agent_id, applies an add_brick
// intent through scene (server-authoritative), then DELEGATES the brick's
// content to that brick's agent via the brick builder. A delegation failure is
// recorded on the CreatedBrick but does not abort the plan.
func (c *Coordinator) createBrick(ctx context.Context, dashboardID string, a Action) (CreatedBrick, json.RawMessage, error) {
	snap, err := c.scenes.Snapshot(ctx, dashboardID)
	if err != nil {
		return CreatedBrick{}, nil, wrapError(Internal, "open dashboard document", err)
	}

	brickID := c.freshBrickID(snap)
	// Server-assigned agent_id (the NOTE from E4-S3): the coordinator owns brick
	// identity rather than the client seeding it. Bound to the brick id so the
	// brick's own engine in the hub is keyed deterministically per brick.
	agentID := "brick:" + brickID

	brick := dashboard.Brick{
		ID:      brickID,
		Kind:    defaultBrickKind,
		Layout:  dashboard.Layout{Pos: *a.Position, Size: *a.Size},
		AgentID: agentID,
	}
	intent := scene.Intent{Type: scene.IntentAddBrick, BrickID: brickID, Brick: &brick}
	patch, err := c.apply(ctx, dashboardID, intent)
	if err != nil {
		return CreatedBrick{}, nil, err
	}

	out := CreatedBrick{BrickID: brickID, AgentID: agentID, Prompt: a.Prompt}

	// Delegate the brick's content to its own agent (cross-engine, via the hub).
	if derr := c.bricks.BuildBrick(ctx, dashboardID, brickID, a.Prompt); derr != nil {
		out.DelegateError = string(deriveCode(derr))
		c.logger.Warn("layoutagent: brick delegation failed",
			"dashboard", dashboardID, "brick", brickID, "agent", agentID, "error", derr)
	}
	return out, patch, nil
}

// applyMutation maps a non-create action to its scene intent and applies it.
func (c *Coordinator) applyMutation(ctx context.Context, dashboardID string, a Action) (json.RawMessage, error) {
	var intent scene.Intent
	switch a.Type {
	case ActionMoveBrick:
		intent = scene.Intent{Type: scene.IntentMoveBrick, BrickID: a.BrickID, Pos: a.Position}
	case ActionResizeBrick:
		intent = scene.Intent{Type: scene.IntentResizeBrick, BrickID: a.BrickID, Size: a.Size}
	case ActionDeleteBrick:
		intent = scene.Intent{Type: scene.IntentDeleteBrick, BrickID: a.BrickID}
	default:
		return nil, newError(InvalidOutput, "unknown layout action type: "+a.Type)
	}
	return c.apply(ctx, dashboardID, intent)
}

// apply marshals an intent and routes it through the authoritative scene path.
func (c *Coordinator) apply(ctx context.Context, dashboardID string, intent scene.Intent) (json.RawMessage, error) {
	raw, err := json.Marshal(intent)
	if err != nil {
		return nil, wrapError(Internal, "marshal "+string(intent.Type)+" intent", err)
	}
	patch, err := c.scenes.HandleIntent(ctx, dashboardID, raw)
	if err != nil {
		return nil, wrapError(ApplyFailed, "apply "+string(intent.Type), err)
	}
	return patch, nil
}

// freshBrickID returns a brick id not already present on the board.
func (c *Coordinator) freshBrickID(snap *dashboard.Dashboard) string {
	existing := make(map[string]bool, len(snap.Bricks))
	for _, b := range snap.Bricks {
		existing[b.ID] = true
	}
	for {
		id := "brk_" + c.newID()
		if !existing[id] {
			return id
		}
	}
}

// deriveCode extracts a stable string code from a delegation error for the
// CreatedBrick report. A *layoutagent.Error carries its own code; anything else
// is reported as DelegateFailed.
func deriveCode(err error) Code {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return DelegateFailed
}

// randomToken returns a short random hex token for brick ids.
func randomToken() string {
	var buf [6]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

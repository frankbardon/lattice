// Package brickagent is the AI brick build loop (E4-S2): it turns a user's chat
// message into a live brick.
//
// The loop is the orchestration layer that sits ABOVE the agenthub lifecycle
// (which stays lifecycle-only: boot/drive/teardown) and the scene patch engine
// (which stays the authoritative mutation path). One call to Build:
//
//  1. resolves the targeted brick's agent_id from the dashboard's authoritative
//     scene document;
//  2. drives that brick's Nexus engine with the chat message via the agenthub
//     Hub (io.input → io.output), capturing the agent's structured reply;
//  3. parses the reply into the {pulse_request, prism_spec} brick template and
//     validates it against TemplateSchema (the json_schema contract) — the agent
//     emits a PARAMETERIZED template (Pulse request + Prism spec carrying ${var}
//     placeholders), never a concrete spec;
//  4. applies that template as an edit_template intent through the SAME scene
//     path a hand edit takes, so it snapshots, broadcasts the patch, and fires
//     the RenderHook (DataResolver substitutes ${var}, the renderer produces the
//     SVG, the realtime layer broadcasts it on the rendered topic).
//
// Because the emitted template is parameterized, a later variable change
// re-renders the brick through the E3-S2 resolve+render path WITHOUT re-invoking
// the agent. brickagent never renders or broadcasts directly — it only drives
// the agent and feeds scene; render+broadcast remain scene's RenderHook job.
package brickagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/frankbardon/lattice/dashboard"
	"github.com/frankbardon/lattice/scene"
)

// AgentDriver drives a brick's builder engine: it sends content to the agent
// identified by agentID and returns the agent's final assistant reply. It is
// satisfied by *agenthub.Hub (its Drive method). Declared as a narrow interface
// so the build loop is unit-testable with a canned-reply stub (no LLM, no Pulse
// child process).
type AgentDriver interface {
	Drive(ctx context.Context, agentID, content string) (string, error)
}

// SceneStore is the slice of the scene layer the build loop needs: read a
// snapshot of the authoritative dashboard document (to resolve the target
// brick's agent_id) and apply the agent's template as an intent (the
// authoritative mutation path). It is satisfied by *scene.Manager via the
// thin SceneManagerStore adapter below — the adapter exists so the build loop
// depends on a snapshot read (easy to stub in tests) rather than on the
// concrete *scene.Doc type.
type SceneStore interface {
	// Snapshot returns a read-only copy of the dashboard document.
	Snapshot(ctx context.Context, dashboardID string) (*dashboard.Dashboard, error)
	// HandleIntent applies a raw client intent against the dashboard's document
	// and returns the applied RFC6902 patch.
	HandleIntent(ctx context.Context, dashboardID string, raw json.RawMessage) (json.RawMessage, error)
}

// SceneManagerStore adapts *scene.Manager to SceneStore: Snapshot opens the
// dashboard's authoritative Doc and copies it. This is the production wiring
// (bound in cmd/server); tests use a lightweight stub instead.
type SceneManagerStore struct{ Manager *scene.Manager }

// Snapshot returns a copy of the dashboard's authoritative document.
func (s SceneManagerStore) Snapshot(ctx context.Context, dashboardID string) (*dashboard.Dashboard, error) {
	doc, err := s.Manager.Doc(ctx, dashboardID)
	if err != nil {
		return nil, err
	}
	return doc.Snapshot(), nil
}

// HandleIntent applies an intent through the scene Manager.
func (s SceneManagerStore) HandleIntent(ctx context.Context, dashboardID string, raw json.RawMessage) (json.RawMessage, error) {
	return s.Manager.HandleIntent(ctx, dashboardID, raw)
}

// Options configures a Builder.
type Options struct {
	// Logger receives build-loop events. Defaults to slog.Default().
	Logger *slog.Logger
}

// Builder runs the brick build loop. Construct with NewBuilder; call Build for
// each chat message. Safe for concurrent use (it holds no per-request state; the
// driver and scene store it depends on are concurrency-safe).
type Builder struct {
	driver AgentDriver
	scenes SceneStore
	logger *slog.Logger
}

// NewBuilder constructs a Builder over an agent driver and a scene store. Both
// are required.
func NewBuilder(driver AgentDriver, scenes SceneStore, opts Options) (*Builder, error) {
	if driver == nil {
		return nil, newError(Internal, "agent driver required")
	}
	if scenes == nil {
		return nil, newError(Internal, "scene store required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Builder{driver: driver, scenes: scenes, logger: logger}, nil
}

// Result is the outcome of a successful build: the applied RFC6902 patch (the
// edit_template patch the scene engine produced) and the canonical template the
// agent emitted. The realtime layer echoes Patch back to the requesting client;
// the rendered fragment reaches every viewer via the scene RenderHook, not here.
type Result struct {
	// Patch is the applied RFC6902 patch (edit_template) — echoed to the client.
	Patch json.RawMessage `json:"patch,omitempty"`
	// Template is the canonical {pulse_request, prism_spec} template that was
	// applied. Useful for the chat UI to confirm what was built / for debugging.
	Template string `json:"template,omitempty"`
}

// Build runs the loop for one chat message: drive the brick's agent, validate
// its template, and apply it as an edit_template intent. dashboardID + brickID
// target the brick; message is the user's chat input.
//
// On the happy path the agent emits a parameterized {pulse_request, prism_spec}
// template, it is applied through scene (which broadcasts the patch and fires
// the render hook), and Build returns the applied patch + canonical template.
// Any failure is returned as a coded *Error and leaves the board unchanged.
func (b *Builder) Build(ctx context.Context, dashboardID, brickID, message string) (Result, error) {
	if dashboardID == "" {
		return Result{}, newError(InvalidRequest, "dashboard_id required")
	}
	if brickID == "" {
		return Result{}, newError(InvalidRequest, "brick_id required")
	}
	if strings.TrimSpace(message) == "" {
		return Result{}, newError(InvalidRequest, "message required")
	}

	agentID, err := b.resolveAgentID(ctx, dashboardID, brickID)
	if err != nil {
		return Result{}, err
	}

	reply, err := b.driver.Drive(ctx, agentID, message)
	if err != nil {
		return Result{}, wrapError(AgentFailed, "drive brick agent", err)
	}

	template, err := b.parseTemplate(reply)
	if err != nil {
		return Result{}, err
	}

	if !isParameterized(template) {
		// Not fatal — some bricks have nothing to parameterize — but the whole
		// point of the loop is a parameterized template, so surface the miss.
		b.logger.Warn("brickagent: agent template has no ${var} placeholders",
			"dashboard", dashboardID, "brick", brickID, "agent", agentID)
	}

	patch, err := b.applyTemplate(ctx, dashboardID, brickID, string(template))
	if err != nil {
		return Result{}, err
	}

	b.logger.Info("brickagent: built brick from chat",
		"dashboard", dashboardID, "brick", brickID, "agent", agentID,
		"parameterized", isParameterized(template))
	return Result{Patch: patch, Template: string(template)}, nil
}

// resolveAgentID reads the dashboard's authoritative document and returns the
// agent_id of the targeted brick. A missing brick or one with no agent_id is a
// BrickNotFound error.
func (b *Builder) resolveAgentID(ctx context.Context, dashboardID, brickID string) (string, error) {
	snap, err := b.scenes.Snapshot(ctx, dashboardID)
	if err != nil {
		return "", wrapError(Internal, "open dashboard document", err)
	}
	brick, ok := findBrick(snap, brickID)
	if !ok {
		return "", newError(BrickNotFound, "brick not found: "+brickID)
	}
	if brick.AgentID == "" {
		return "", newError(BrickNotFound, "brick has no agent_id: "+brickID)
	}
	return brick.AgentID, nil
}

// parseTemplate extracts the JSON template object from the agent's reply,
// validates it against TemplateSchema, and returns it in canonical compact form.
func (b *Builder) parseTemplate(reply string) ([]byte, error) {
	raw, err := extractTemplate(reply)
	if err != nil {
		return nil, err
	}
	if err := validateTemplate(raw); err != nil {
		return nil, err
	}
	return canonicalize(raw)
}

// applyTemplate applies the template as an edit_template intent through the
// scene engine — the SAME authoritative path a hand edit takes, so the patch is
// snapshotted, broadcast, and the RenderHook fires (resolve + render + rendered
// broadcast). It returns the applied RFC6902 patch.
func (b *Builder) applyTemplate(ctx context.Context, dashboardID, brickID, template string) (json.RawMessage, error) {
	intent := scene.Intent{
		Type:     scene.IntentEditTemplate,
		BrickID:  brickID,
		Template: template,
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return nil, wrapError(Internal, "marshal edit_template intent", err)
	}
	patch, err := b.scenes.HandleIntent(ctx, dashboardID, raw)
	if err != nil {
		return nil, wrapError(ApplyFailed, "apply edit_template", err)
	}
	return patch, nil
}

// findBrick returns the brick with id from a dashboard snapshot.
func findBrick(doc *dashboard.Dashboard, id string) (dashboard.Brick, bool) {
	for i := range doc.Bricks {
		if doc.Bricks[i].ID == id {
			return doc.Bricks[i], true
		}
	}
	return dashboard.Brick{}, false
}

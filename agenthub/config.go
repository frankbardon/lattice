package agenthub

import (
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config configures a Hub. Construct it directly or via DefaultConfig and set
// the binary/data paths. The hub never reads LLM credentials itself — those
// flow into the agent engine through the provider plugin's api_key_env (see
// AnthropicKeyEnv), so no secret is ever held in this struct or in code.
type Config struct {
	// PulseBinaryPath is the absolute path to the `pulse` binary the brick
	// agent reaches through nexus.mcp.client (stdio transport). This MUST be
	// the SAME binary the E2-S1 pulsemcp.Manager spawns — the agent's MCP tool
	// surface and the renderer's data calls share one Pulse install. Required.
	PulseBinaryPath string

	// PulseDataDir is the Pulse cohort directory, passed as `--data-dir` to the
	// agent's pulse MCP server. Should match the renderer's PULSE_DATA_DIR so
	// the agent reasons over the same cohorts the board renders. Required.
	PulseDataDir string

	// PrismBinaryPath is the absolute path to the `prism` binary the brick
	// agent reaches through nexus.mcp.client for plotting/validation tools.
	// Optional: when empty the Prism MCP server is omitted from the config and
	// the agent gets Pulse tools only.
	PrismBinaryPath string

	// PrismExamplesRoot is the directory `prism mcp` walks for example specs
	// (its --examples-root flag). Optional; Prism applies its own default.
	PrismExamplesRoot string

	// SessionsRoot is where booted engines write their per-session workspaces
	// (core.sessions.root in the agent YAML). Defaults to "~/.nexus/lattice/sessions".
	SessionsRoot string

	// AnthropicKeyEnv names the environment variable the Anthropic provider
	// plugin reads its API key from (api_key_env). Defaults to
	// "ANTHROPIC_API_KEY". The hub never reads this value; it only wires the
	// name into the agent config so the provider resolves it at boot.
	AnthropicKeyEnv string

	// Model is the Anthropic model id the brick agent uses. Defaults to a
	// recent Sonnet (see defaultModel).
	Model string

	// MaxTokens caps a single LLM response. Defaults to 4096.
	MaxTokens int

	// MaxConcurrent bounds how many brick-agent engines may be booted at once
	// per dashboard (one Hub == one dashboard). Bounds concurrent LLM sessions.
	// Must be > 0; DefaultConfig sets 4.
	MaxConcurrent int

	// IdleTimeout is how long an engine may sit without being driven before the
	// hub tears it down to reclaim its LLM session slot. Zero disables the idle
	// reaper (engines then live until Close or an explicit Stop). DefaultConfig
	// sets 5 minutes.
	IdleTimeout time.Duration

	// DriveTimeout bounds a single Drive round-trip (io.input → io.output).
	// Defaults to 2 minutes when zero.
	DriveTimeout time.Duration

	// OutputSchema is the JSON Schema the brick agent's final output must
	// satisfy (the build loop's structured-output contract). When non-empty the
	// nexus.gate.json_schema gate is activated and the schema is injected so the
	// LLM is forced to emit a conforming object (the {pulse_request, prism_spec}
	// brick template) instead of prose. Empty leaves the agent free-form (the
	// E4-S1 lifecycle behaviour). Set by the brickagent build loop (E4-S2) to
	// brickagent.TemplateSchema; the schema is also re-validated server-side
	// after Drive, so this is force + defence-in-depth.
	OutputSchema string

	// OutputRetries is how many times the json_schema gate re-prompts the LLM
	// when its output fails OutputSchema validation. Defaults to 2 when zero and
	// OutputSchema is set; ignored when OutputSchema is empty.
	OutputRetries int

	// SystemPrompt overrides the brick-agent system prompt. Empty uses the
	// built-in prompt (which, when OutputSchema is set, instructs the agent to
	// emit a parameterized {pulse_request, prism_spec} template with ${var}
	// placeholders). The build loop leaves this empty to use the built-in.
	SystemPrompt string

	// LayoutOutputSchema is the JSON Schema the LAYOUT coordinator agent's final
	// output must satisfy (the layout-actions plan envelope, distinct from the
	// brick template schema above). When non-empty, an engine keyed with the
	// LayoutAgentPrefix renders a DIFFERENT config: the layout system prompt and
	// this schema (via the json_schema gate), and NO Pulse/Prism MCP tools (the
	// coordinator decides structure — what bricks exist + where — and delegates
	// the data detail to per-brick agents, so it needs no data tools of its own).
	// Set by the layout coordinator (E5-S1) to layoutagent.PlanSchema. Empty
	// leaves a layout-keyed engine free-form.
	LayoutOutputSchema string

	// LayoutSystemPrompt overrides the layout-coordinator system prompt. Empty
	// uses the built-in defaultLayoutPrompt (which, when LayoutOutputSchema is
	// set, instructs the agent to emit a layout-actions plan). The coordinator
	// leaves this empty to use the built-in.
	LayoutSystemPrompt string
}

// LayoutAgentPrefix is the agentID prefix that marks an engine as the board
// LAYOUT coordinator (one per dashboard, e.g. "layout:<dashboard_id>") rather
// than a per-brick builder. The hub renders a DIFFERENT config for these
// engines — the layout system prompt + LayoutOutputSchema, no Pulse/Prism MCP
// tools — so the layout coordinator and brick builders share one Hub but run
// distinct agent types keyed apart. IsLayoutAgent reports the match.
const LayoutAgentPrefix = "layout:"

// IsLayoutAgent reports whether agentID names the board layout coordinator (it
// carries LayoutAgentPrefix) rather than a per-brick builder.
func IsLayoutAgent(agentID string) bool {
	return strings.HasPrefix(agentID, LayoutAgentPrefix)
}

const (
	defaultModel         = "claude-sonnet-4-20250514"
	defaultMaxTokens     = 4096
	defaultMaxConcurr    = 4
	defaultIdleTimeout   = 5 * time.Minute
	defaultDriveTimeout  = 2 * time.Minute
	defaultSessionsRoot  = "~/.nexus/lattice/sessions"
	defaultKeyEnv        = "ANTHROPIC_API_KEY"
	defaultOutputRetries = 2
)

// defaultLifecyclePrompt is the free-form brick-agent prompt (no structured
// output). Used when Config.OutputSchema is empty (the E4-S1 lifecycle path).
const defaultLifecyclePrompt = `You are the AI builder for a single dashboard brick. You reach Pulse (a
tabular-data engine) and Prism (a plotting engine) through MCP tools
prefixed with mcp__pulse__ and mcp__prism__. Call the relevant tool
rather than inventing answers.`

// defaultBuildPrompt is the brick-agent prompt for the E4-S2 build loop: it
// instructs the agent to inspect the data via Pulse/Prism MCP tools and emit a
// PARAMETERIZED {pulse_request, prism_spec} brick template as its FINAL message,
// with ${var} placeholders rather than concrete values so a later variable
// change re-renders the brick without re-invoking the agent. The json_schema
// gate (wired when OutputSchema is set) enforces the envelope shape.
const defaultBuildPrompt = `You are the AI builder for a single dashboard brick on a collaborative
dashboard. You reach Pulse (a tabular-data engine) and Prism (a plotting
engine) through MCP tools prefixed with mcp__pulse__ and mcp__prism__. Use
those tools to discover the available cohorts, fields, and chart shapes —
never invent field names or data.

Your job: turn the user's request into a brick TEMPLATE and emit it as your
final message. The template is a single JSON object with exactly two keys:

  {
    "pulse_request": { ... a Pulse query (declarative JSON) ... },
    "prism_spec":    { ... a Prism chart spec (Vega-Lite-style) ... }
  }

Rules:
  - Emit ONLY that JSON object as your final message. No prose, no code fences.
  - The template must be PARAMETERIZED: wherever a value could reasonably be
    controlled by a dashboard variable (a filter value, a row limit, a region,
    a date range), use a "${var_name}" placeholder STRING instead of a concrete
    value. The server substitutes these from dashboard variables at render time,
    so a variable change re-renders without asking you again. Always include at
    least one ${var} placeholder.
  - Name Pulse aggregation output columns as AGG_<TYPE>_<field> (e.g.
    AGG_SUM_amount) unless you set an explicit "label" on the aggregation, in
    which case the column is that label.
  - The prism_spec must have a "mark" and an "encoding" whose field names match
    the columns the pulse_request produces.`

// defaultLayoutPrompt is the layout coordinator's prompt for the E5-S1 board
// build loop: it instructs the agent to translate a board-level request into a
// PLAN of layout actions (create / move / resize / delete / rearrange bricks)
// and emit it as its FINAL message. The coordinator decides WHAT bricks exist
// and WHERE on the masonry grid; for each brick it creates it supplies a
// per-brick prompt that the server hands to that brick's own agent to fill in
// the data detail — the coordinator never authors Pulse/Prism templates itself.
// The json_schema gate (wired when LayoutOutputSchema is set) enforces the
// envelope shape.
const defaultLayoutPrompt = `You are the LAYOUT COORDINATOR for a collaborative masonry dashboard. You
arrange the board: you decide WHAT bricks exist and WHERE they sit on a grid,
and you DELEGATE the contents of each brick to that brick's own AI builder
agent. You do NOT write chart specs or data queries yourself.

The board is a grid: position {x,y} is the top-left cell (0-based) and size
{width,height} is the span in grid cells. A typical board is 12 columns wide.

Your job: turn the user's board-level request into a PLAN and emit it as your
final message. The plan is a single JSON object with one key:

  {
    "actions": [ { "type": "create_brick", ... }, ... ]
  }

Each action is one of:
  - {"type":"create_brick","position":{"x":0,"y":0},"size":{"width":6,"height":4},"prompt":"a description of what this brick should show, for the brick agent"}
  - {"type":"move_brick","brick_id":"<id>","position":{"x":..,"y":..}}
  - {"type":"resize_brick","brick_id":"<id>","size":{"width":..,"height":..}}
  - {"type":"delete_brick","brick_id":"<id>"}

Rules:
  - Emit ONLY that JSON object as your final message. No prose, no code fences.
  - For a NEW board, emit create_brick actions that lay out a sensible,
    non-overlapping grid. Give each a clear "prompt" describing the chart or
    panel it should contain (e.g. "a bar chart of total sales by region") — the
    server routes that prompt to a dedicated brick agent that builds the actual
    Pulse query + Prism chart. Do not invent field names or data here.
  - When the user asks to rearrange or resize existing bricks, reference them by
    their brick_id (the server tells you the current board state).`

// DefaultConfig returns a Config with sensible defaults. PulseBinaryPath and
// PulseDataDir are left empty and MUST be set by the caller before NewHub.
func DefaultConfig() Config {
	return Config{
		SessionsRoot:    defaultSessionsRoot,
		AnthropicKeyEnv: defaultKeyEnv,
		Model:           defaultModel,
		MaxTokens:       defaultMaxTokens,
		MaxConcurrent:   defaultMaxConcurr,
		IdleTimeout:     defaultIdleTimeout,
		DriveTimeout:    defaultDriveTimeout,
	}
}

// validate checks the config is usable and applies zero-value defaults for the
// optional fields. It mutates the receiver's defaults but not required fields.
func (c *Config) validate() error {
	if c.PulseBinaryPath == "" {
		return newError(InvalidConfig, "pulse_binary_path is required")
	}
	if c.PulseDataDir == "" {
		return newError(InvalidConfig, "pulse_data_dir is required")
	}
	if c.MaxConcurrent < 0 {
		return newError(InvalidConfig, "max_concurrent must not be negative")
	}
	if c.MaxConcurrent == 0 {
		c.MaxConcurrent = defaultMaxConcurr
	}
	if c.SessionsRoot == "" {
		c.SessionsRoot = defaultSessionsRoot
	}
	if c.AnthropicKeyEnv == "" {
		c.AnthropicKeyEnv = defaultKeyEnv
	}
	if c.Model == "" {
		c.Model = defaultModel
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = defaultMaxTokens
	}
	if c.DriveTimeout == 0 {
		c.DriveTimeout = defaultDriveTimeout
	}
	return nil
}

// brickAgentTemplate is the Nexus engine config for one brick builder agent. It
// activates a ReAct agent with the Anthropic provider and an MCP client that
// connects the SAME pulse binary E2-S1 spawns (and optionally prism) over
// stdio, projecting their tools (mcp__pulse__*, mcp__prism__*) into the agent's
// catalog. core.agent_id partitions per-agent storage, mirroring the desktop
// shell. When Config.OutputSchema is set (the E4-S2 build loop) the
// nexus.gate.json_schema gate is added so the agent is FORCED to emit a
// conforming {pulse_request, prism_spec} template; absent a schema the agent is
// free-form (the E4-S1 lifecycle behaviour).
//
// renderConfig builds the config programmatically (a Go map → yaml) rather than
// a text template so arbitrary multi-line prompt / JSON-schema content is
// encoded safely (no manual YAML indentation of an embedded JSON schema).
func (c *Config) renderConfig(agentID string) ([]byte, error) {
	if IsLayoutAgent(agentID) {
		return c.renderLayoutConfig(agentID)
	}

	systemPrompt := c.SystemPrompt
	if systemPrompt == "" {
		if c.OutputSchema != "" {
			systemPrompt = defaultBuildPrompt
		} else {
			systemPrompt = defaultLifecyclePrompt
		}
	}

	active := []string{
		"nexus.io.test",
		"nexus.llm.anthropic",
		"nexus.agent.react",
		"nexus.gate.endless_loop",
		"nexus.memory.capped",
		"nexus.mcp.client",
	}

	pulseServer := map[string]any{
		"name":      "pulse",
		"transport": "stdio",
		"command":   c.PulseBinaryPath,
		"args":      []any{"mcp", "--data-dir", c.PulseDataDir},
		"lifecycle": "engine",
		"timeout":   "30s",
	}
	servers := []any{pulseServer}
	if c.PrismBinaryPath != "" {
		prismArgs := []any{"mcp"}
		if c.PrismExamplesRoot != "" {
			prismArgs = append(prismArgs, "--examples-root", c.PrismExamplesRoot)
		}
		servers = append(servers, map[string]any{
			"name":      "prism",
			"transport": "stdio",
			"command":   c.PrismBinaryPath,
			"args":      prismArgs,
			"lifecycle": "engine",
			"timeout":   "30s",
		})
	}

	plugins := map[string]any{
		"active": active,
		"nexus.llm.anthropic": map[string]any{
			"api_key_env": c.AnthropicKeyEnv,
		},
		"nexus.agent.react": map[string]any{
			"system_prompt": systemPrompt,
		},
		"nexus.gate.endless_loop": map[string]any{
			"max_iterations": 8,
		},
		"nexus.memory.capped": map[string]any{
			"max_messages": 50,
			"persist":      false,
		},
		"nexus.mcp.client": map[string]any{
			"servers": servers,
		},
	}

	// When a structured-output schema is supplied, activate the json_schema gate
	// and feed it the schema so the agent's final output is forced to conform and
	// re-prompted on a miss. The schema is passed as a JSON string (the gate
	// accepts a string or object form).
	if c.OutputSchema != "" {
		retries := c.OutputRetries
		if retries == 0 {
			retries = defaultOutputRetries
		}
		plugins["active"] = append(active, "nexus.gate.json_schema")
		plugins["nexus.gate.json_schema"] = map[string]any{
			"schema":      c.OutputSchema,
			"max_retries": retries,
		}
	}

	cfg := map[string]any{
		"core":    c.coreConfig(agentID),
		"plugins": plugins,
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, wrapError(InvalidConfig, "render agent config", err)
	}
	return out, nil
}

// coreConfig builds the shared core.* block (models, sessions, agent_id) used
// by both the brick-builder and layout-coordinator configs.
func (c *Config) coreConfig(agentID string) map[string]any {
	return map[string]any{
		"log_level":             "warn",
		"tick_interval":         "5s",
		"max_concurrent_events": 100,
		"agent_id":              agentID,
		"models": map[string]any{
			"default": "balanced",
			"balanced": map[string]any{
				"provider":   "nexus.llm.anthropic",
				"model":      c.Model,
				"max_tokens": c.MaxTokens,
			},
		},
		"sessions": map[string]any{
			"root":      c.SessionsRoot,
			"retention": "7d",
			"id_format": "datetime_short",
		},
	}
}

// renderLayoutConfig is the Nexus engine config for the board LAYOUT
// coordinator (one engine per dashboard, keyed layout:<dashboard_id>). It
// differs from the brick-builder config in two deliberate ways:
//
//   - NO nexus.mcp.client: the coordinator decides board STRUCTURE (what bricks
//     exist + where) and delegates the per-brick data detail to brick agents,
//     so it needs no Pulse/Prism data tools of its own (and we avoid spawning an
//     extra pulse child per layout engine);
//   - it uses the layout system prompt + LayoutOutputSchema (the layout-actions
//     plan envelope) rather than the brick template prompt/schema.
//
// When LayoutOutputSchema is set the nexus.gate.json_schema gate is activated so
// the coordinator is FORCED to emit a conforming plan; absent a schema it is
// free-form.
func (c *Config) renderLayoutConfig(agentID string) ([]byte, error) {
	systemPrompt := c.LayoutSystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultLayoutPrompt
	}

	active := []string{
		"nexus.io.test",
		"nexus.llm.anthropic",
		"nexus.agent.react",
		"nexus.gate.endless_loop",
		"nexus.memory.capped",
	}

	plugins := map[string]any{
		"active": active,
		"nexus.llm.anthropic": map[string]any{
			"api_key_env": c.AnthropicKeyEnv,
		},
		"nexus.agent.react": map[string]any{
			"system_prompt": systemPrompt,
		},
		"nexus.gate.endless_loop": map[string]any{
			"max_iterations": 8,
		},
		"nexus.memory.capped": map[string]any{
			"max_messages": 50,
			"persist":      false,
		},
	}

	if c.LayoutOutputSchema != "" {
		retries := c.OutputRetries
		if retries == 0 {
			retries = defaultOutputRetries
		}
		plugins["active"] = append(active, "nexus.gate.json_schema")
		plugins["nexus.gate.json_schema"] = map[string]any{
			"schema":      c.LayoutOutputSchema,
			"max_retries": retries,
		}
	}

	cfg := map[string]any{
		"core":    c.coreConfig(agentID),
		"plugins": plugins,
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, wrapError(InvalidConfig, "render layout agent config", err)
	}
	return out, nil
}

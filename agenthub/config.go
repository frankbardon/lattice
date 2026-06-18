package agenthub

import (
	"strings"
	"text/template"
	"time"
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
}

const (
	defaultModel        = "claude-sonnet-4-20250514"
	defaultMaxTokens    = 4096
	defaultMaxConcurr   = 4
	defaultIdleTimeout  = 5 * time.Minute
	defaultDriveTimeout = 2 * time.Minute
	defaultSessionsRoot = "~/.nexus/lattice/sessions"
	defaultKeyEnv       = "ANTHROPIC_API_KEY"
)

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
// shell. The build loop / structured-output gate is E4-S2; this config just
// proves the engine boots and drives.
const brickAgentTemplate = `core:
  log_level: warn
  tick_interval: 5s
  max_concurrent_events: 100
  agent_id: {{.AgentID}}
  models:
    default: balanced
    balanced:
      provider: nexus.llm.anthropic
      model: {{.Model}}
      max_tokens: {{.MaxTokens}}
  sessions:
    root: {{.SessionsRoot}}
    retention: 7d
    id_format: datetime_short

plugins:
  active:
    - nexus.io.test
    - nexus.llm.anthropic
    - nexus.agent.react
    - nexus.gate.endless_loop
    - nexus.memory.capped
    - nexus.mcp.client

  nexus.llm.anthropic:
    api_key_env: {{.AnthropicKeyEnv}}

  nexus.agent.react:
    system_prompt: |
      You are the AI builder for a single dashboard brick. You reach Pulse (a
      tabular-data engine) and Prism (a plotting engine) through MCP tools
      prefixed with mcp__pulse__ and mcp__prism__. Call the relevant tool
      rather than inventing answers.

  nexus.gate.endless_loop:
    max_iterations: 8

  nexus.memory.capped:
    max_messages: 50
    persist: false

  nexus.mcp.client:
    servers:
      - name: pulse
        transport: stdio
        command: {{.PulseBinaryPath}}
        args: ["mcp", "--data-dir", "{{.PulseDataDir}}"]
        lifecycle: engine
        timeout: 30s
{{- if .PrismBinaryPath}}
      - name: prism
        transport: stdio
        command: {{.PrismBinaryPath}}
        args: ["mcp"{{if .PrismExamplesRoot}}, "--examples-root", "{{.PrismExamplesRoot}}"{{end}}]
        lifecycle: engine
        timeout: 30s
{{- end}}
`

var brickAgentTmpl = template.Must(template.New("brickAgent").Parse(brickAgentTemplate))

// renderConfig produces the engine YAML for a given agentID from the hub
// config. The agentID partitions the engine's storage and sessions.
func (c *Config) renderConfig(agentID string) ([]byte, error) {
	data := struct {
		AgentID           string
		Model             string
		MaxTokens         int
		SessionsRoot      string
		AnthropicKeyEnv   string
		PulseBinaryPath   string
		PulseDataDir      string
		PrismBinaryPath   string
		PrismExamplesRoot string
	}{
		AgentID:           agentID,
		Model:             c.Model,
		MaxTokens:         c.MaxTokens,
		SessionsRoot:      c.SessionsRoot,
		AnthropicKeyEnv:   c.AnthropicKeyEnv,
		PulseBinaryPath:   c.PulseBinaryPath,
		PulseDataDir:      c.PulseDataDir,
		PrismBinaryPath:   c.PrismBinaryPath,
		PrismExamplesRoot: c.PrismExamplesRoot,
	}
	var sb strings.Builder
	if err := brickAgentTmpl.Execute(&sb, data); err != nil {
		return nil, wrapError(InvalidConfig, "render agent config", err)
	}
	return []byte(sb.String()), nil
}

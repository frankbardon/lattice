package agenthub

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// renderFor builds a validated Config and renders its engine YAML for agentID.
func renderFor(t *testing.T, c Config, agentID string) map[string]any {
	t.Helper()
	if err := c.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	raw, err := c.renderConfig(agentID)
	if err != nil {
		t.Fatalf("renderConfig: %v", err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("rendered config is not valid YAML: %v\n%s", err, raw)
	}
	return out
}

func pluginsOf(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	p, ok := cfg["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("config has no plugins map: %#v", cfg["plugins"])
	}
	return p
}

func activeOf(t *testing.T, plugins map[string]any) []string {
	t.Helper()
	raw, ok := plugins["active"].([]any)
	if !ok {
		t.Fatalf("plugins.active is not a list: %#v", plugins["active"])
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		out[i], _ = v.(string)
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// TestRenderConfig_Lifecycle: with no OutputSchema the json_schema gate is
// absent (E4-S1 free-form behaviour) and the agent_id is wired through.
func TestRenderConfig_Lifecycle(t *testing.T) {
	c := DefaultConfig()
	c.PulseBinaryPath = "/usr/bin/pulse"
	c.PulseDataDir = "/data"
	cfg := renderFor(t, c, "brick-1")

	plugins := pluginsOf(t, cfg)
	active := activeOf(t, plugins)
	if contains(active, "nexus.gate.json_schema") {
		t.Fatal("json_schema gate must be absent without an OutputSchema")
	}
	if _, ok := plugins["nexus.gate.json_schema"]; ok {
		t.Fatal("json_schema gate config must be absent without an OutputSchema")
	}
	core, _ := cfg["core"].(map[string]any)
	if core["agent_id"] != "brick-1" {
		t.Fatalf("agent_id = %v, want brick-1", core["agent_id"])
	}
}

// TestRenderConfig_StructuredOutput: with an OutputSchema set the json_schema
// gate is activated and fed the schema + retry count — the build-loop wiring
// that FORCES the agent to emit a conforming template.
func TestRenderConfig_StructuredOutput(t *testing.T) {
	const schema = `{"type":"object","required":["pulse_request","prism_spec"]}`
	c := DefaultConfig()
	c.PulseBinaryPath = "/usr/bin/pulse"
	c.PulseDataDir = "/data"
	c.OutputSchema = schema
	cfg := renderFor(t, c, "brick-1")

	plugins := pluginsOf(t, cfg)
	if !contains(activeOf(t, plugins), "nexus.gate.json_schema") {
		t.Fatal("json_schema gate must be active with an OutputSchema")
	}
	gate, ok := plugins["nexus.gate.json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("json_schema gate config missing: %#v", plugins["nexus.gate.json_schema"])
	}
	if gate["schema"] != schema {
		t.Fatalf("gate schema = %v, want the supplied schema", gate["schema"])
	}
	if gate["max_retries"] != defaultOutputRetries {
		t.Fatalf("gate max_retries = %v, want %d", gate["max_retries"], defaultOutputRetries)
	}
}

// TestRenderConfig_PrismOptional: the prism MCP server is only wired when a
// binary is configured.
func TestRenderConfig_PrismOptional(t *testing.T) {
	c := DefaultConfig()
	c.PulseBinaryPath = "/usr/bin/pulse"
	c.PulseDataDir = "/data"

	withoutPrism := renderFor(t, c, "b")
	mcp := pluginsOf(t, withoutPrism)["nexus.mcp.client"].(map[string]any)
	if got := len(mcp["servers"].([]any)); got != 1 {
		t.Fatalf("servers = %d without prism, want 1", got)
	}

	c.PrismBinaryPath = "/usr/bin/prism"
	withPrism := renderFor(t, c, "b")
	mcp = pluginsOf(t, withPrism)["nexus.mcp.client"].(map[string]any)
	if got := len(mcp["servers"].([]any)); got != 2 {
		t.Fatalf("servers = %d with prism, want 2", got)
	}
}

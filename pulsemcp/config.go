package pulsemcp

import (
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Config describes how to spawn and talk to the Pulse stdio MCP child process.
// It is loaded from YAML (see LoadConfig) with ${ENV} interpolation, matching
// the project-wide config convention, and is also usable as a plain struct.
type Config struct {
	// BinaryPath is the path to the pulse binary to exec. It is NOT resolved
	// against PATH automatically by exec; callers should give an absolute path
	// or one resolvable from the working directory. There is intentionally no
	// hardcoded default absolute path — DefaultConfig falls back to the bare
	// command name "pulse" so a PATH-installed binary works out of the box.
	BinaryPath string `yaml:"binary_path"`

	// DataDir is the Pulse cohort directory, passed as --data-dir and exported
	// as PULSE_DATA_DIR to the child. Pulse refuses to start without it.
	DataDir string `yaml:"data_dir"`

	// ExtraArgs are appended after "mcp" and the --data-dir flag (e.g.
	// "--bind-on-open=false"). Optional.
	ExtraArgs []string `yaml:"extra_args"`

	// ExtraEnv are additional KEY=VALUE pairs exported to the child on top of
	// the parent environment and PULSE_DATA_DIR. Optional.
	ExtraEnv []string `yaml:"extra_env"`

	// CallTimeout bounds a single tool call. Zero means no per-call timeout
	// beyond the caller's context. Optional.
	CallTimeout time.Duration `yaml:"call_timeout"`

	// StartTimeout bounds the spawn + MCP handshake. Defaults to 30s when zero.
	StartTimeout time.Duration `yaml:"start_timeout"`

	// RestartBackoff is the delay before respawning after an unexpected exit.
	// Defaults to 1s when zero. Set negative to disable supervision/restart.
	RestartBackoff time.Duration `yaml:"restart_backoff"`
}

// DefaultConfig returns a Config with the bare "pulse" command (resolved via
// PATH) and sensible timeouts. DataDir is left empty and MUST be set by the
// caller before Start.
func DefaultConfig() Config {
	return Config{
		BinaryPath:     "pulse",
		StartTimeout:   30 * time.Second,
		RestartBackoff: time.Second,
	}
}

// validate checks the config is usable and applies zero-value defaults for the
// timeout fields. It mutates the receiver's defaults but not the user-supplied
// fields.
func (c *Config) validate() error {
	if c.BinaryPath == "" {
		return newError(InvalidConfig, "binary_path is required")
	}
	if c.DataDir == "" {
		return newError(InvalidConfig, "data_dir is required")
	}
	if c.StartTimeout == 0 {
		c.StartTimeout = 30 * time.Second
	}
	if c.RestartBackoff == 0 {
		c.RestartBackoff = time.Second
	}
	return nil
}

// envVarPattern matches ${VAR} interpolation tokens.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// interpolateEnv replaces every ${VAR} token in s with the corresponding
// environment variable value (empty string when unset), matching the
// project-wide config interpolation convention. A literal "$" that is not part
// of a ${...} token is left untouched.
func interpolateEnv(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(tok string) string {
		name := envVarPattern.FindStringSubmatch(tok)[1]
		return os.Getenv(name)
	})
}

// LoadConfig reads a YAML config file and applies ${ENV} interpolation to the
// raw bytes before unmarshalling, so any string field may reference
// environment variables (e.g. binary_path: ${PULSE_BIN}). Missing variables
// interpolate to empty. The returned Config is validated.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, wrapError(InvalidConfig, "read config file", err)
	}
	return ParseConfig(raw)
}

// ParseConfig parses YAML config bytes with ${ENV} interpolation. It is the
// in-memory counterpart of LoadConfig.
func ParseConfig(raw []byte) (Config, error) {
	interpolated := interpolateEnv(string(raw))
	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(interpolated), &cfg); err != nil {
		return Config{}, wrapError(InvalidConfig, "parse config yaml", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

package pulsemcp

import (
	"testing"
	"time"
)

func TestParseConfig_InterpolatesEnvAndDefaults(t *testing.T) {
	t.Setenv("TEST_PULSE_BIN", "/opt/pulse/bin/pulse")
	t.Setenv("TEST_PULSE_DATA", "/var/data/pulse")

	raw := []byte(`
binary_path: ${TEST_PULSE_BIN}
data_dir: ${TEST_PULSE_DATA}
extra_args:
  - --bind-on-open=false
call_timeout: 5s
`)
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.BinaryPath != "/opt/pulse/bin/pulse" {
		t.Errorf("binary_path = %q, want interpolated path", cfg.BinaryPath)
	}
	if cfg.DataDir != "/var/data/pulse" {
		t.Errorf("data_dir = %q, want interpolated path", cfg.DataDir)
	}
	if len(cfg.ExtraArgs) != 1 || cfg.ExtraArgs[0] != "--bind-on-open=false" {
		t.Errorf("extra_args = %v, want [--bind-on-open=false]", cfg.ExtraArgs)
	}
	if cfg.CallTimeout != 5*time.Second {
		t.Errorf("call_timeout = %v, want 5s", cfg.CallTimeout)
	}
	// Zero-value timeouts get defaults.
	if cfg.StartTimeout != 30*time.Second {
		t.Errorf("start_timeout default = %v, want 30s", cfg.StartTimeout)
	}
	if cfg.RestartBackoff != time.Second {
		t.Errorf("restart_backoff default = %v, want 1s", cfg.RestartBackoff)
	}
}

func TestParseConfig_MissingEnvInterpolatesEmptyThenFails(t *testing.T) {
	// data_dir references an unset variable -> empty -> validation rejects it.
	raw := []byte(`
binary_path: pulse
data_dir: ${DEFINITELY_UNSET_VAR_XYZ}
`)
	_, err := ParseConfig(raw)
	if err == nil {
		t.Fatal("expected error for empty data_dir, got nil")
	}
	if got := CodeOf(err); got != InvalidConfig {
		t.Errorf("code = %s, want %s", got, InvalidConfig)
	}
}

func TestConfigValidate_RequiresFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"empty binary", Config{DataDir: "/d"}},
		{"empty data dir", Config{BinaryPath: "pulse"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if err := cfg.validate(); err == nil {
				t.Fatal("expected validation error, got nil")
			} else if CodeOf(err) != InvalidConfig {
				t.Errorf("code = %s, want %s", CodeOf(err), InvalidConfig)
			}
		})
	}
}

func TestInterpolateEnv_LeavesBareDollarUntouched(t *testing.T) {
	t.Setenv("FOO", "bar")
	got := interpolateEnv("price is $5 and var ${FOO}")
	want := "price is $5 and var bar"
	if got != want {
		t.Errorf("interpolateEnv = %q, want %q", got, want)
	}
}

func TestNewManager_RejectsBadConfig(t *testing.T) {
	if _, err := NewManager(Config{}, Options{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := NewManager(Config{BinaryPath: "pulse", DataDir: "/d"}, Options{}); err != nil {
		t.Fatalf("unexpected error for valid config: %v", err)
	}
}

func TestCall_NotStarted(t *testing.T) {
	m, err := NewManager(Config{BinaryPath: "pulse", DataDir: "/d"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Call(t.Context(), ToolProcess, []byte(`{}`)); CodeOf(err) != NotStarted {
		t.Errorf("code = %s, want %s", CodeOf(err), NotStarted)
	}
}

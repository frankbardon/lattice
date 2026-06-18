// Package pulsemcp spawns and supervises the Pulse statistical engine as a
// stdio MCP child process (`pulse mcp --data-dir <dir>`) and exposes a small Go
// client for calling its tools.
//
// Pulse is reached as a stdio JSON-RPC MCP server, not as a normal in-process
// Go import for data calls — the child-process + stdio seam is deliberate (it
// is the same surface brick agents reach through Nexus' nexus.mcp.client). This
// package owns that process lifecycle (start, supervise, restart-on-crash,
// graceful stop) behind a Manager and offers Call / Process / Predict helpers
// that marshal a pulse types.Request and unmarshal a types.Response.
//
// The Manager is independent of render and scene so it can be reused by both
// the server-side renderer (Epic 2) and the agent hub (Epic 4).
package pulsemcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/frankbardon/pulse/types"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// Pulse tool names. These are the stable MCP tool identifiers exposed by
// `pulse mcp`; mirrored here so callers do not depend on Pulse internals.
const (
	ToolProcess = "pulse_process"
	ToolPredict = "pulse_predict"
)

// healthInterval is how often the supervisor pings the child to detect an
// unexpected exit. It is small enough to recover quickly but large enough to
// add negligible load.
const healthInterval = time.Second

// Options configures a Manager. The zero value logs to slog.Default().
type Options struct {
	// Logger is the structured logger. Defaults to slog.Default() when nil.
	Logger *slog.Logger
}

// session holds one live child process + MCP client. A new session is created
// on Start and on every supervised restart; the previous one is discarded.
type session struct {
	client *mcpclient.Client
	cmd    *exec.Cmd // the spawned child, captured for observability (PID)
}

// Manager owns the Pulse stdio MCP child-process lifecycle and exposes a
// concurrency-safe Call surface. Construct with NewManager, then Start before
// calling, and Stop on shutdown.
type Manager struct {
	cfg    Config
	logger *slog.Logger

	mu      sync.RWMutex
	cur     *session // current live session; nil when stopped
	running bool

	// supervision
	stopCh chan struct{} // closed by Stop to signal the supervisor to quit
	doneCh chan struct{} // closed when the supervisor goroutine has exited
}

// NewManager validates cfg and constructs a Manager. It does not spawn the
// child process; call Start for that.
func NewManager(cfg Config, opts Options) (*Manager, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		cfg:    cfg,
		logger: logger,
	}, nil
}

// Start spawns the pulse child process, performs the MCP handshake, and (unless
// restart is disabled) launches a supervisor that respawns the child on
// unexpected exit. It is an error to Start an already-running Manager.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return newError(InvalidConfig, "manager already started")
	}
	m.mu.Unlock()

	sess, err := m.spawn(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.cur = sess
	m.running = true
	m.stopCh = make(chan struct{})
	m.doneCh = make(chan struct{})
	m.mu.Unlock()

	if m.cfg.RestartBackoff >= 0 {
		go m.supervise()
	} else {
		// Restart disabled: nothing to wait on at Stop.
		close(m.doneCh)
	}
	return nil
}

// spawn launches one pulse child via the mcp-go stdio transport and runs the
// initialize handshake. On any failure the partially-constructed client is
// closed so no child is leaked.
func (m *Manager) spawn(ctx context.Context) (*session, error) {
	args := append([]string{"mcp", "--data-dir", m.cfg.DataDir}, m.cfg.ExtraArgs...)
	env := append([]string{"PULSE_DATA_DIR=" + m.cfg.DataDir}, m.cfg.ExtraEnv...)

	// A CommandFunc lets us capture the *exec.Cmd (for PID observability) while
	// the transport still owns its start + wait lifecycle. The child inherits
	// the parent environment with PULSE_DATA_DIR (and any ExtraEnv) merged on.
	var cmd *exec.Cmd
	cmdFunc := func(_ context.Context, command string, cmdEnv []string, cmdArgs []string) (*exec.Cmd, error) {
		c := exec.Command(command, cmdArgs...)
		c.Env = append(os.Environ(), cmdEnv...)
		cmd = c
		return c, nil
	}

	// NewStdioMCPClientWithOptions launches the subprocess and wires
	// stdin/stdout pipes.
	cli, err := mcpclient.NewStdioMCPClientWithOptions(m.cfg.BinaryPath, env, args,
		transport.WithCommandFunc(cmdFunc))
	if err != nil {
		return nil, wrapError(Spawn, "launch pulse mcp child", err)
	}

	hctx, cancel := context.WithTimeout(ctx, m.cfg.StartTimeout)
	defer cancel()

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "lattice", Version: "1"}
	if _, err := cli.Initialize(hctx, initReq); err != nil {
		_ = cli.Close()
		return nil, wrapError(Spawn, "mcp initialize handshake", err)
	}

	m.logger.Info("pulse mcp child started", "binary", m.cfg.BinaryPath, "data_dir", m.cfg.DataDir, "pid", pidOf(cmd))
	return &session{client: cli, cmd: cmd}, nil
}

// pidOf returns the child PID, or -1 if unavailable.
func pidOf(cmd *exec.Cmd) int {
	if cmd == nil || cmd.Process == nil {
		return -1
	}
	return cmd.Process.Pid
}

// supervise health-checks the current session and respawns the child after
// RestartBackoff when a check fails (unexpected exit / dropped pipe). It exits
// when Stop closes stopCh.
func (m *Manager) supervise() {
	defer close(m.doneCh)

	ticker := time.NewTicker(healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
		}

		m.mu.RLock()
		sess := m.cur
		running := m.running
		m.mu.RUnlock()
		if !running || sess == nil {
			return
		}

		pctx, cancel := context.WithTimeout(context.Background(), healthInterval)
		err := sess.client.Ping(pctx)
		cancel()
		if err == nil {
			continue
		}

		// Stop may have raced with the failed ping.
		select {
		case <-m.stopCh:
			return
		default:
		}
		m.logger.Warn("pulse mcp health check failed; restarting", "error", err, "backoff", m.cfg.RestartBackoff)

		if m.cfg.RestartBackoff > 0 {
			select {
			case <-m.stopCh:
				return
			case <-time.After(m.cfg.RestartBackoff):
			}
		}

		_ = sess.client.Close()

		newSess, serr := m.spawn(context.Background())
		if serr != nil {
			m.logger.Error("pulse mcp restart failed; will retry", "error", serr)
			continue // next tick retries
		}
		m.mu.Lock()
		m.cur = newSess
		m.mu.Unlock()
		m.logger.Info("pulse mcp child restarted")
	}
}

// Stop gracefully shuts down the supervisor and the child process. It is safe
// to call on a never-started or already-stopped Manager. The provided context
// bounds how long Stop waits for the supervisor to unwind.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = false
	stopCh := m.stopCh
	doneCh := m.doneCh
	sess := m.cur
	m.cur = nil
	m.mu.Unlock()

	close(stopCh)

	// Wait for the supervisor to exit so it does not respawn a child after we
	// close the current one.
	if doneCh != nil {
		select {
		case <-doneCh:
		case <-ctx.Done():
			m.logger.Warn("pulse mcp stop: supervisor did not exit in time")
		}
	}

	if sess != nil {
		if err := sess.client.Close(); err != nil {
			return wrapError(Internal, "close pulse mcp child", err)
		}
	}
	m.logger.Info("pulse mcp child stopped")
	return nil
}

// client returns the current live MCP client or a NotStarted error.
func (m *Manager) client() (*mcpclient.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running || m.cur == nil {
		return nil, newError(NotStarted, "manager not started")
	}
	return m.cur.client, nil
}

// PID returns the current child process PID, or -1 when not running. It is
// primarily for tests and operational observability; it changes across restarts.
func (m *Manager) PID() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running || m.cur == nil {
		return -1
	}
	return pidOf(m.cur.cmd)
}

// Call invokes an MCP tool by name with the given JSON-encoded request body and
// returns the tool's raw JSON text result. It is the low-level seam under
// Process/Predict; agent-hub and renderer callers can use it for any of Pulse's
// tools. A tool-level error (IsError) is surfaced as a *Error with code
// PULSE_TOOL_ERROR.
func (m *Manager) Call(ctx context.Context, tool string, request json.RawMessage) (json.RawMessage, error) {
	cli, err := m.client()
	if err != nil {
		return nil, err
	}

	if m.cfg.CallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.cfg.CallTimeout)
		defer cancel()
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = tool
	// Pulse's compute tools take a single "request" string argument holding a
	// JSON-encoded types.Request (see pulse internal/mcp/tools.go).
	req.Params.Arguments = map[string]any{"request": string(request)}

	res, err := cli.CallTool(ctx, req)
	if err != nil {
		return nil, wrapError(Call, "call tool "+tool, err)
	}

	text := firstText(res)
	if res.IsError {
		return nil, newError(Tool, "tool "+tool+" reported error: "+text)
	}
	return json.RawMessage(text), nil
}

// Process marshals req, calls pulse_process, and unmarshals the types.Response.
func (m *Manager) Process(ctx context.Context, req *types.Request) (*types.Response, error) {
	return m.callTyped(ctx, ToolProcess, req)
}

// Predict marshals req, calls pulse_predict (validate-only), and unmarshals the
// types.Response.
func (m *Manager) Predict(ctx context.Context, req *types.Request) (*types.Response, error) {
	return m.callTyped(ctx, ToolPredict, req)
}

func (m *Manager) callTyped(ctx context.Context, tool string, req *types.Request) (*types.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, wrapError(Call, "marshal request", err)
	}
	raw, err := m.Call(ctx, tool, body)
	if err != nil {
		return nil, err
	}
	var resp types.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, wrapError(Call, "decode response", err)
	}
	return &resp, nil
}

// firstText returns the first text content block from a tool result, or "".
func firstText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := mcp.AsTextContent(c); ok {
			return tc.Text
		}
	}
	return ""
}

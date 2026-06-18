package layoutagent

import "fmt"

// Code is the typed error code. Values are DOMAIN_CATEGORY strings, mirroring
// the convention used across the in-house repos (parsec errors/codes.go) and
// the agenthub / brickagent / render / scene packages.
type Code string

const (
	// InvalidRequest is returned when a board chat request is malformed (e.g.
	// missing dashboard id or message).
	InvalidRequest Code = "LAYOUTAGENT_INVALID_REQUEST"
	// AgentFailed wraps a failure driving the layout coordinator's Nexus engine
	// (the agent errored, timed out, or the hub could not boot it).
	AgentFailed Code = "LAYOUTAGENT_AGENT_FAILED"
	// InvalidOutput is returned when the coordinator's structured output does not
	// parse or does not satisfy the layout-actions plan schema.
	InvalidOutput Code = "LAYOUTAGENT_INVALID_OUTPUT"
	// ApplyFailed wraps a failure applying a layout action as a scene intent
	// (add/move/resize/delete) through the authoritative scene path.
	ApplyFailed Code = "LAYOUTAGENT_APPLY_FAILED"
	// DelegateFailed wraps a failure delegating a created brick's construction to
	// that brick's builder agent. The brick still exists (its add_brick patch
	// applied); only the content build failed.
	DelegateFailed Code = "LAYOUTAGENT_DELEGATE_FAILED"
	// Internal wraps an unexpected lower-level failure.
	Internal Code = "LAYOUTAGENT_INTERNAL"
)

// Error is a coded layout-agent error. Cause is preserved for log/debug.
type Error struct {
	Code  Code
	Msg   string
	Cause error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Msg, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Msg)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// newError constructs a coded error with no cause.
func newError(code Code, msg string) *Error {
	return &Error{Code: code, Msg: msg}
}

// wrapError constructs a coded error that wraps a lower-level cause.
func wrapError(code Code, msg string, cause error) *Error {
	return &Error{Code: code, Msg: msg, Cause: cause}
}

// CodeOf extracts the Code from err, or Internal if err is not a *Error.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return Internal
}

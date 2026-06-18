package pulsemcp

import "fmt"

// Code is the typed error code. Values are DOMAIN_CATEGORY strings, mirroring
// the convention used across the in-house repos (parsec errors/codes.go) and
// the render/scene packages.
type Code string

const (
	// InvalidConfig is returned when the manager configuration is malformed
	// (e.g. an empty binary path or data dir, or a config file that fails to
	// parse / interpolate).
	InvalidConfig Code = "PULSE_INVALID_CONFIG"
	// NotStarted is returned when a call is made before Start (or after Stop).
	NotStarted Code = "PULSE_NOT_STARTED"
	// Spawn is returned when the pulse child process cannot be launched
	// (binary missing, exec failure) or the MCP handshake fails.
	Spawn Code = "PULSE_SPAWN_FAILED"
	// Call is returned when an MCP tool call fails at the transport level
	// (connection dropped, context cancelled) or the result cannot be decoded.
	Call Code = "PULSE_CALL_FAILED"
	// Tool is returned when the tool itself reports an error (the MCP result
	// has IsError set) — e.g. a malformed request or a missing cohort.
	Tool Code = "PULSE_TOOL_ERROR"
	// Internal wraps an unexpected lower-level failure.
	Internal Code = "PULSE_INTERNAL"
)

// Error is a coded pulse MCP error. Cause is preserved for log/debug.
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

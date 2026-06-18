package agenthub

import "fmt"

// Code is the typed error code. Values are DOMAIN_CATEGORY strings, mirroring
// the convention used across the in-house repos and the pulsemcp / render /
// scene packages.
type Code string

const (
	// InvalidConfig is returned when the hub configuration is malformed (e.g.
	// a missing pulse binary path or a non-positive concurrency bound).
	InvalidConfig Code = "AGENTHUB_INVALID_CONFIG"
	// Closed is returned when Get is called after the hub has been closed.
	Closed Code = "AGENTHUB_CLOSED"
	// AtCapacity is returned when booting a new engine would exceed the
	// per-dashboard concurrency bound and no idle slot can be reclaimed.
	AtCapacity Code = "AGENTHUB_AT_CAPACITY"
	// Boot is returned when an engine cannot be constructed from its config
	// or fails to boot (bad YAML, plugin init failure, missing creds).
	Boot Code = "AGENTHUB_BOOT_FAILED"
	// Drive is returned when driving an engine via io.input/io.output fails
	// (context cancelled, timeout, or the engine reported an error message).
	Drive Code = "AGENTHUB_DRIVE_FAILED"
	// Internal wraps an unexpected lower-level failure.
	Internal Code = "AGENTHUB_INTERNAL"
)

// Error is a coded agent-hub error. Cause is preserved for log/debug.
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

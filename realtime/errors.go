package realtime

import "fmt"

// Code is the typed error code. Values are DOMAIN_CATEGORY strings, mirroring
// the convention used across the in-house repos (parsec errors/codes.go).
type Code string

const (
	// InvalidArgument is returned for malformed input (e.g. empty dashboard id).
	InvalidArgument Code = "REALTIME_INVALID_ARGUMENT"
	// Internal wraps an unexpected lower-level failure (broker, signer).
	Internal Code = "REALTIME_INTERNAL"
)

// Error is a coded realtime error. Cause is preserved for log/debug.
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

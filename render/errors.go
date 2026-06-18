package render

import "fmt"

// Code is the typed error code. Values are DOMAIN_CATEGORY strings, mirroring
// the convention used across the in-house repos (parsec errors/codes.go) and
// the scene package.
type Code string

const (
	// UnknownKind is returned when a brick's kind has no registered renderer.
	UnknownKind Code = "RENDER_UNKNOWN_KIND"
	// InvalidArgument is returned for a malformed registration or render call
	// (e.g. an empty kind, a nil renderer).
	InvalidArgument Code = "RENDER_INVALID_ARGUMENT"
	// Internal wraps an unexpected lower-level failure (e.g. a markdown
	// conversion error).
	Internal Code = "RENDER_INTERNAL"
	// InvalidTemplate is returned when a brick template cannot be parsed into
	// the shape its renderer expects (e.g. a malformed pulse_prism template
	// envelope, an undecodable Pulse request or Prism spec).
	InvalidTemplate Code = "RENDER_INVALID_TEMPLATE"
	// DataSource wraps a failure fetching the data a renderer needs (e.g. the
	// Pulse MCP call behind a pulse_prism brick failed).
	DataSource Code = "RENDER_DATA_SOURCE"
	// Compile wraps a failure turning a spec + data into renderable output
	// (e.g. a Prism compile or SVG render error).
	Compile Code = "RENDER_COMPILE"
)

// Error is a coded render error. Cause is preserved for log/debug.
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

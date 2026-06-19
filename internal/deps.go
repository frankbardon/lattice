// Package internal anchors module dependencies that are reserved up front
// (E1-S1) but not yet wired into code. Later stories replace these blank
// imports with real usage:
//
//   - expr-lang/expr — computed variables (E3-S3)
//
// google/jsonschema-go and spf13/afero are now used for real by the schema
// loader/resolver (internal/schema, E1-S3), so their anchors were removed.
// The expr anchor stays until E3-S3 wires it in; keeping it imported here makes
// it a direct dependency of go.mod so `go mod tidy` does not drop it.
package internal

import (
	_ "github.com/expr-lang/expr"
)

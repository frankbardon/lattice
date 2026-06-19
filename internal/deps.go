// Package internal anchors module dependencies that are reserved up front
// (E1-S1) but not yet wired into code. Later stories replace these blank
// imports with real usage:
//
//   - expr-lang/expr        — computed variables (E3-S3)
//   - google/jsonschema-go  — JSON Schema validation (E1-S3)
//   - spf13/afero           — filesystem abstraction for loading (E1-S3)
//
// Keeping them imported here makes them direct dependencies of go.mod so the
// versions are pinned from the start and `go mod tidy` does not drop them.
package internal

import (
	_ "github.com/expr-lang/expr"
	_ "github.com/google/jsonschema-go/jsonschema"
	_ "github.com/spf13/afero"
)

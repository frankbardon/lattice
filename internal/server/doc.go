// Package server is the thin web layer for the lattice reference renderer.
//
// It hosts a net/http server (driven by the CLI `serve` subcommand) that loads
// and resolves a dashboard document via the E1 resolver and serves an HTML page
// wired with AlpineJS. The page consumes a JSON endpoint exposing the resolved
// tree; resolution is kept entirely server-side.
//
// This is sketch plumbing (E2-S2): the base page mounts an Alpine app and
// fetches the resolved-tree JSON, but the real visual sketch view arrives in
// E2-S3. Resolution failures render an HTML error page showing the CodedError
// rather than crashing the server.
//
// Manual check:
//
//	make build
//	./bin/dashspec serve examples/minimal-dashboard.json   # add --port to change the port (default 8080)
//	open http://localhost:8080/                             # base AlpineJS page
//	curl http://localhost:8080/api/tree                     # resolved-tree JSON
//
// Pointing serve at a document that fails resolution renders the CodedError on
// an HTML error page at "/" (HTTP 422) and returns the error envelope from
// /api/tree; the server stays up.
package server

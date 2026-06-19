// Package cli holds the lattice CLI subcommand definitions (resolve, serve).
//
// main.go assembles these into the root *cli.Command tree. The resolve
// subcommand runs the two-pass resolver and emits the resolved tree (E1-S4);
// the serve subcommand (E2-S2) starts the HTTP reference-renderer web layer,
// re-resolving the document per request and rendering errors as an HTML page.
package cli

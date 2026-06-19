// Package cli holds the lattice CLI subcommand definitions (resolve, serve).
//
// main.go assembles these into the root *cli.Command tree. The resolve
// subcommand runs the two-pass resolver and emits the resolved tree (E1-S4);
// serve behavior arrives in E2-S2.
package cli

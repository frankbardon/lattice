package cli

import (
	"context"
	"fmt"

	cli "github.com/urfave/cli/v3"
)

// ResolveCommand returns the `resolve` subcommand.
//
// Stubbed in E1-S1: it accepts a document path argument and reports that
// resolution is not yet implemented. The two-pass resolver and resolved-tree
// emission arrive in E1-S4.
func ResolveCommand() *cli.Command {
	return &cli.Command{
		Name:      "resolve",
		Usage:     "Resolve and validate a dashboard document, emitting the resolved tree",
		ArgsUsage: "<document>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			return fmt.Errorf("resolve: not yet implemented")
		},
	}
}

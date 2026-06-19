// Package main is the entry point for the lattice CLI binary.
package main

import (
	"context"
	"fmt"
	"os"

	lcli "github.com/frankbardon/lattice/internal/cli"

	cli "github.com/urfave/cli/v3"
)

// version is set by the build system.
var version = "dev"

func main() {
	app := buildApp()
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func buildApp() *cli.Command {
	return &cli.Command{
		Name:    "lattice",
		Usage:   "Resolve and serve declarative dashboard specifications",
		Version: version,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Usage: "Emit structured errors as JSON"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			cli.ShowAppHelp(cmd)
			return nil
		},
		Commands: []*cli.Command{
			lcli.ResolveCommand(),
			lcli.ServeCommand(),
		},
	}
}

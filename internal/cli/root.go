// Package cli wires the howdy commands together.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"howdy/internal/tui"
)

// Version is set at build time with -ldflags "-X howdy/internal/cli.Version=...".
var Version = "dev"

// Execute runs the root command and reports any error on stderr.
func Execute() error {
	root := &cobra.Command{
		Use:     "howdy",
		Short:   "The command line for the Howdy web framework",
		Long:    "howdy creates and works with Howdy projects: Gleam web apps served on the BEAM.",
		Version: Version,
		// Errors are printed once, styled, below.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newCommand())
	root.AddCommand(createCommand())
	root.AddCommand(devCommand())

	err := root.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, tui.ErrorLine(err.Error()))
	}
	return err
}

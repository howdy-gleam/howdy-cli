// Package cli wires the howdy commands together.
package cli

import (
	"errors"
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
	root.AddCommand(addCommand())
	root.AddCommand(devCommand())
	root.AddCommand(routesCommand())
	root.AddCommand(checkCommand())

	cmd, err := root.ExecuteC()
	if err == nil {
		return nil
	}
	if errors.Is(err, errReported) {
		return err
	}
	// A command run with --json reports its error the same way, on stdout.
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		failed(os.Stdout, cmd.CommandPath(), err)
		return err
	}
	fmt.Fprintln(os.Stderr, tui.ErrorLine(err.Error()))
	return err
}

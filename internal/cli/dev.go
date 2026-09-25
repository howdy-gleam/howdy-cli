package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"howdy/internal/tools"
)

func devCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "dev",
		Short: "Run the project with hot reload (gleam dev)",
		Long: `Run the project with hot reload.

Finds the enclosing Gleam project (the nearest directory with a gleam.toml)
and runs 'gleam dev' in it. Output is streamed, and the server stops with
Ctrl-C.`,
		Example: "  howdy dev",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDev()
		},
	}
}

// findProjectDir walks up from the working directory to the nearest
// directory containing a gleam.toml.
func findProjectDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not read the working directory: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "gleam.toml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("this is not a Gleam project: no gleam.toml found in %s or any parent directory", cwdForError())
		}
		dir = parent
	}
}

func cwdForError() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "the current directory"
	}
	return cwd
}

func runDev() error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}
	gleam, err := tools.FindGleam()
	if err != nil {
		return err
	}

	cmd := exec.Command(gleam.Path, "dev")
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

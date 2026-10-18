package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"howdy/internal/generate"
	"howdy/internal/inspect"
	"howdy/internal/tools"
	"howdy/internal/tui"
)

// errReported is returned by a command that has already reported its
// failure, as 'howdy check --json' does, so Execute prints nothing more.
var errReported = errors.New("reported")

func routesCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "routes",
		Short: "List the routes the app serves, with where each is defined",
		Long: `List every route the current project serves: its method and path, the
controller that declares it, and the file and line it is on.

Routes are read from the sources, without building the app: the controllers
registered in app() in src/<project>.gleam, in the order they are
registered, and the routes each one declares with controller.get, post,
put, patch, delete or route (and the endpoint functions of Howdy OpenAPI).
A controller that comes from a package, such as auth's pages or the static
files, is a mount, method *, at the path it is given. Routes from dev/,
such as the admin at /_howdy, are served only by gleam dev.

A route built some other way, such as in a loop or a helper, is not seen.`,
		Example: "  howdy routes\n  howdy routes --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutes(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the routes as a JSON array")
	return cmd
}

func runRoutes(asJSON bool) error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	routes, err := inspect.Routes(dir, project.Name)
	if err != nil {
		return err
	}
	if asJSON {
		if routes == nil {
			routes = []inspect.Route{}
		}
		return printJSON(os.Stdout, routes)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "METHOD\tPATH\tCONTROLLER\tDEFINED AT")
	for _, r := range routes {
		var notes []string
		if r.Mount != "" {
			notes = append(notes, r.Mount)
		}
		if r.Guarded {
			notes = append(notes, "guarded")
		}
		if r.Dev {
			notes = append(notes, "gleam dev only")
		}
		controller := r.Controller
		if len(notes) > 0 {
			controller += " (" + strings.Join(notes, ", ") + ")"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s:%d\n", r.Method, r.Path, controller, r.File, r.Line)
	}
	return w.Flush()
}

func checkCommand() *cobra.Command {
	var asJSON, fix bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check the project: formatting, the build, its dependencies and the tests",
		Long: `Check the current project the way to check any change to it:

  format   gleam format --check, over src, test and dev
  build    gleam build, reporting the compiler's warnings
  deps     every package src imports is in [dependencies] in gleam.toml
  test     gleam test

gleam build and gleam test compile with the dev dependencies, and so with
the packages they bring in, such as howdy_flags with howdy_admin. A
production build, such as gleam export erlang-shipment, leaves them out:
deps finds an import in src of a module only they provide, and says what to
add to gleam.toml.

Every check runs, except deps and the tests when the build fails. The
output of a check that fails is shown, and the command exits with status 1
if any did. Pass --fix to format the sources first, rather than report
them, and --json for one JSON object with each check's outcome and output.`,
		Example: "  howdy check\n  howdy check --fix\n  howdy check --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(asJSON, fix)
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "format the sources with gleam format instead of checking their formatting")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the result as one JSON object")
	return cmd
}

// checkResult is one check of 'howdy check'.
type checkResult struct {
	Name string `json:"name"`
	// Status is "passed", "failed" or "skipped".
	Status  string `json:"status"`
	Command string `json:"command"`
	// Output is what the command printed, when it failed or warned.
	Output   string `json:"output,omitempty"`
	Warnings int    `json:"warnings,omitempty"`
}

func runCheck(asJSON, fix bool) error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}
	gleam, err := tools.FindGleam()
	if err != nil {
		return err
	}

	var sources []string
	for _, d := range []string{"src", "test", "dev"} {
		if info, err := os.Stat(filepath.Join(dir, d)); err == nil && info.IsDir() {
			sources = append(sources, d)
		}
	}
	format := append([]string{"format", "--check"}, sources...)
	if fix {
		format = append([]string{"format"}, sources...)
	}

	run := func(name string, args ...string) checkResult {
		c := checkResult{Name: name, Command: "gleam " + strings.Join(args, " "), Status: "passed"}
		out, err := gleam.Output(dir, args...)
		c.Warnings = strings.Count(out, "warning:")
		if err != nil {
			c.Status = "failed"
		}
		if err != nil || c.Warnings > 0 {
			c.Output = out
		}
		return c
	}
	if !asJSON {
		fmt.Println("\n" + tui.Title("howdy check") + "\n")
	}
	var results []checkResult
	show := func(c checkResult) {
		results = append(results, c)
		if asJSON {
			return
		}
		switch {
		case c.Status == "skipped":
			fmt.Println(tui.CheckLine(tui.Skipped, c.Name, "skipped: the build failed"))
		case c.Status == "failed":
			fmt.Println(tui.CheckLine(tui.Failed, c.Name, c.Command))
		case c.Warnings > 0:
			fmt.Println(tui.CheckLine(tui.Passed, c.Name, fmt.Sprintf("%s, %d warnings", c.Command, c.Warnings)))
		default:
			fmt.Println(tui.CheckLine(tui.Passed, c.Name, c.Command))
		}
		if c.Output != "" {
			fmt.Println("\n" + indent(c.Output, "      ") + "\n")
		}
	}

	show(run("format", format...))
	build := run("build", "build")
	show(build)
	if build.Status == "failed" {
		show(checkResult{Name: "deps", Status: "skipped", Command: depsCommand})
		show(checkResult{Name: "test", Status: "skipped", Command: "gleam test"})
	} else {
		show(checkDependencies(dir))
		show(run("test", "test"))
	}

	var failed []string
	for _, c := range results {
		if c.Status == "failed" {
			failed = append(failed, c.Name)
		}
	}
	if asJSON {
		report := struct {
			OK     bool          `json:"ok"`
			Checks []checkResult `json:"checks"`
		}{len(failed) == 0, results}
		if err := printJSON(os.Stdout, report); err != nil {
			return err
		}
		if len(failed) > 0 {
			return errReported
		}
		return nil
	}
	fmt.Println()
	if len(failed) > 0 {
		return fmt.Errorf("check failed: %s", strings.Join(failed, ", "))
	}
	return nil
}

const depsCommand = "src imports only from [dependencies]"

// checkDependencies is the deps check: it fails on an import in src/ of a
// module from a package that only the dev dependencies bring in.
func checkDependencies(dir string) checkResult {
	c := checkResult{Name: "deps", Command: depsCommand, Status: "passed"}
	found, err := inspect.DevOnlyImports(dir)
	if err != nil {
		c.Status = "failed"
		c.Output = err.Error()
		return c
	}
	if len(found) == 0 {
		return c
	}
	c.Status = "failed"
	c.Output = inspect.DescribeDevOnlyImports(found)
	return c
}

func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

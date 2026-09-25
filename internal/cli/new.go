package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"howdy/internal/scaffold"
	"howdy/internal/tools"
	"howdy/internal/tui"
)

type newOptions struct {
	noDatabase bool
	database   string
	release    string
	minimal    bool
	skipDeps   bool
	plain      bool
}

func newCommand() *cobra.Command {
	opts := newOptions{}
	cmd := &cobra.Command{
		Use:   "new <project_name>",
		Short: "Create a new Howdy project with a simple home page",
		Long: `Create a new Howdy project.

The project is a Gleam application with a themed home page built from Howdy UI,
static assets, hot reload for development, and a test suite. By default it also
gets a SQLite database through Gloo, with an example todo list on the home page
and a JSON API. Pass --database postgres to use PostgreSQL (through pog)
instead, with a docker-compose.yml that starts a local server, or
--no-database for a page-only project.

The development server also mounts the Howdy admin at /_howdy, where you can
browse and edit the database and see every request as a trace of its queries
and logs, recorded by Howdy Telemetry. The project is ready for a console: a
Gleam REPL (GSH) attached to the running server, with shortcuts in
src/console.gleam. Pass --minimal to leave out the admin, telemetry and the
console.

Howdy packages are downloaded from GitHub at a release tag, so they always
match. By default that is Howdy ` + scaffold.Release + `, the release these
templates are written for; pass --howdy to choose another.

The name must be a valid Gleam project name: lowercase letters, digits and
underscores, starting with a letter. A path such as apps/my_site also works;
the last segment is the project name.`,
		Example: "  howdy new my_site\n  howdy new my_site --database postgres\n  howdy new my_site --no-database\n  howdy new my_site --minimal\n  howdy new my_site --howdy 2.1.0\n  howdy new apps/my_site",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.noDatabase && cmd.Flags().Changed("database") {
				return errors.New("--database and --no-database cannot be used together")
			}
			return runNew(args[0], opts)
		},
	}
	cmd.Flags().BoolVar(&opts.noDatabase, "no-database", false, "leave out the database, Gloo and the todo list example")
	cmd.Flags().StringVar(&opts.database, "database", string(scaffold.SQLite), "database to use: sqlite or postgres")
	cmd.Flags().StringVar(&opts.release, "howdy", scaffold.Release, "Howdy release to depend on, such as "+scaffold.Release)
	cmd.Flags().BoolVar(&opts.minimal, "minimal", false, "leave out the admin, telemetry and the console")
	cmd.Flags().BoolVar(&opts.skipDeps, "skip-deps", false, "do not run 'gleam deps download' after creating the files")
	cmd.Flags().BoolVar(&opts.plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func runNew(arg string, opts newOptions) error {
	database, err := scaffold.ParseDatabase(opts.database)
	if err != nil {
		return err
	}
	release, err := scaffold.ParseRelease(opts.release)
	if err != nil {
		return err
	}
	if opts.noDatabase {
		database = scaffold.NoDatabase
	}
	project, err := scaffold.New(arg)
	if err != nil {
		return err
	}
	project.Database = database
	project.Release = release
	project.Minimal = opts.minimal

	var gleam tools.Tool
	steps := []tui.Step{
		{
			Title: "Checking for Gleam",
			Run: func(r tui.Reporter) (string, error) {
				var err error
				gleam, err = tools.FindGleam()
				if err != nil {
					return "", err
				}
				if _, err := tools.FindErlang(); err != nil {
					r.Warn("Erlang (erl) was not found on PATH; Gleam needs it to build and run this project.")
				}
				return gleam.Version, nil
			},
		},
		{
			Title: "Creating project files",
			Run: func(r tui.Reporter) (string, error) {
				count, err := project.Write(func(path string) {
					r.Log(path)
					r.Detail(path)
					time.Sleep(45 * time.Millisecond)
				})
				if err != nil {
					return "", err
				}
				kind := fmt.Sprintf("with a %s todo list", project.Database.Label())
				if project.Database == scaffold.NoDatabase {
					kind = "page only"
				}
				if !project.Minimal {
					kind += ", with the admin, telemetry and console"
				}
				return fmt.Sprintf("%d files in %s, %s, on Howdy %s", count, project.Display(), kind, project.Release), nil
			},
		},
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(project.Dir, "format", "src", "dev", "test")
			},
		},
		{
			Title: "Downloading dependencies",
			Skip: func() (string, bool) {
				if opts.skipDeps {
					return "skipped (--skip-deps)", true
				}
				return "", false
			},
			Run: func(r tui.Reporter) (string, error) {
				r.Detail("gleam deps download")
				if err := gleam.Run(project.Dir, "deps", "download"); err != nil {
					r.Warn("Dependency download failed; run 'gleam deps download' inside the project to see why.")
					return "failed", nil
				}
				return "ready", nil
			},
		},
	}

	err = tui.Run(fmt.Sprintf("howdy new %s", project.Name), steps, opts.plain)
	if err != nil {
		if !errors.Is(err, tui.ErrCancelled) {
			// Leave nothing half-made behind.
			project.Remove()
		}
		return err
	}

	fmt.Print(tui.NextSteps(project.Name, nextSteps(project), notes(project)))
	return nil
}

func nextSteps(project *scaffold.Project) []tui.Command {
	rel := project.Display()
	if abs, err := filepath.Abs(rel); err == nil {
		if cwd, err := os.Getwd(); err == nil {
			if r, err := filepath.Rel(cwd, abs); err == nil {
				rel = r
			}
		}
	}
	commands := []tui.Command{
		{Cmd: "cd " + rel, Note: "into your new project"},
	}
	if project.Database == scaffold.Postgres {
		commands = append(commands, tui.Command{Cmd: "docker compose up -d", Note: "start PostgreSQL, needed by the app and the tests"})
	}
	commands = append(commands, tui.Command{Cmd: "gleam dev", Note: "start with hot reload, then open http://127.0.0.1:8787"})
	if !project.Minimal {
		commands = append(commands, tui.Command{Cmd: "gleam run -m gsh -- " + project.Name + "_dev", Note: "or start it inside a console"})
	}
	commands = append(commands, []tui.Command{
		{Cmd: "gleam test", Note: "run the tests"},
		{Cmd: "gleam run", Note: "start without hot reload"},
	}...)
	if project.Database != scaffold.NoDatabase {
		commands = append(commands, tui.Command{Cmd: "gleam run -m tasks/migrate", Note: "apply migrations without starting the server"})
	}
	return append(commands, tui.Command{Cmd: "gleam run -m tasks/css", Note: "regenerate priv/public/assets/ui.css after changing themes"})
}

func notes(project *scaffold.Project) []string {
	n := project.Name
	notes := []string{
		fmt.Sprintf("The home page lives in src/%s/site/site_controller.gleam. Static files go in priv/public.", n),
	}
	if !project.Minimal {
		notes = append(notes,
			"Under gleam dev the admin is at http://127.0.0.1:8787/_howdy, with every request under Telemetry.",
			"Console shortcuts live in src/console.gleam; README.md shows how to attach one to a running server.",
		)
	}
	if project.Database != scaffold.NoDatabase {
		notes = append(notes,
			fmt.Sprintf("Todos live in src/%s/todos/ (record, database operations, JSON API at /api/todos)", n),
			fmt.Sprintf("and the live list in src/%s/site/todo_list.gleam. Migrations are in src/%s/migrations/.", n, n),
		)
	}
	env := "Copy .env.example to .env to change the port."
	switch project.Database {
	case scaffold.SQLite:
		notes = append(notes, fmt.Sprintf("The database is %s.sqlite3, created and migrated on first start. SQLite needs a C toolchain to build.", n))
	case scaffold.Postgres:
		notes = append(notes, fmt.Sprintf("docker-compose.yml runs PostgreSQL on port 5432 with the databases %s_dev and %s_test; the app migrates on start.", n, n))
		env = "Copy .env.example to .env to change the port or DATABASE_URL."
	}
	return append(notes, env+" See README.md for the project layout.")
}

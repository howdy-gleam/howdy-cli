package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"howdy/internal/config"
	"howdy/internal/generate"
	"howdy/internal/scaffold"
	"howdy/internal/tools"
	"howdy/internal/tui"
)

type newOptions struct {
	noDatabase bool
	database   string
	release    string
	source     string
	minimal    bool
	apiOnly    bool
	noAgents   bool
	noDocker   bool
	skipDeps   bool
	with       []string
	noConfig   bool
	// config is the config file the defaults came from, or "".
	config string
	// repository is the Git repository from the config file, or "".
	repository string
	out        output
}

func newCommand() *cobra.Command {
	opts := newOptions{}
	cmd := &cobra.Command{
		Use:   "new <project_name>",
		Short: "Create a new Howdy project with a simple home page",
		Long: `Create a new Howdy project.

The project is a Gleam application with a themed home page built from Howdy UI,
static assets, hot reload for development, and a test suite. By default it also
gets a PostgreSQL database through Gloo and pog, with an example todo list on
the home page and a JSON API, and a docker-compose.yml that starts a local
server. Pass --database sqlite to use SQLite instead, or --no-database for a
page-only project.

The development server also mounts the Howdy admin at /_howdy, where you can
browse and edit the database and see every request as a trace of its queries
and logs, recorded by Howdy Telemetry. The project is ready for a console: a
Gleam REPL (GSH) attached to the running server, with shortcuts in
src/console.gleam. Pass --minimal to leave out the admin, telemetry and the
console.

AGENTS.md tells coding agents how to work on the project: how to check a
change, which howdy commands write code for them, and where things go.
CLAUDE.md imports it for Claude Code, and .claude/skills/ holds guides for
common tasks. Pass --no-agents to leave all three out.

Pass --api-only for a JSON backend with no pages: it leaves out Howdy UI,
Lustre, the home page, layouts, static files and the HTML 404 page. With a
database it keeps the todos JSON API.

Every project is ready to deploy. GET /up answers 200 while the app runs,
for load balancers and container platforms. The server stops cleanly on
SIGTERM, letting requests in flight finish, and copies of the app that
start together take turns to migrate. A Dockerfile builds an image that
runs the app with bin/start, which also runs migrations on their own
(bin/start migrate). It uses the Gleam and Erlang versions installed here.
Pass --no-docker to leave out the Dockerfile, .dockerignore and bin/start;
'howdy add docker' adds them later.

Pass --with to set up more Howdy packages in the project, as the matching
'howdy add' commands would, each wired into the app, the dev server and the
tests:

  mail            a mailer, an example email and the dev outbox
  auth            sign-in by emailed link or code, and roles; brings mail
  flags           feature flags, kept in the database
  storage         files in an S3-compatible bucket or a local folder
  jobs            background jobs, kept in the database and run beside the server
  notifications   an inbox per user, pushed live and emailed; brings mail and jobs
  i18n            message catalogues in locales/ and each request's locale
  openapi         an OpenAPI document at /openapi.json, read at /docs
  remote          typed calls between services, over the cluster or HTTP

auth, flags, jobs and notifications keep their data in the database, so they
cannot be used with --no-database. With auth, the notifications inbox asks
it who is signed in.

Every Howdy package comes from the same release, so they always match. By
default that is Howdy ` + scaffold.Release + `, the release these templates are written
for; pass --howdy to choose another. --source chooses where the packages are
downloaded from: git takes them from GitHub at the release tag, and hex
takes each package's version in the release from Hex. The default is
` + string(scaffold.DefaultSource) + `.

The name must be a valid Gleam project name: lowercase letters, digits and
underscores, starting with a letter. It cannot be the name of a module that
ships with Erlang, such as pg, auth or timer, which the project's own module
would replace. A path such as apps/my_site also works; the last segment is the
project name.

Your own defaults for these flags can be kept in a config file, which howdy
looks for in your config directory: ~/.config/howdy/config.toml on Linux
($XDG_CONFIG_HOME/howdy/config.toml when that is set), ~/Library/Application
Support/howdy/config.toml or ~/.config/howdy/config.toml on macOS, and
%AppData%\howdy\config.toml on Windows. Set HOWDY_CONFIG to read another
file. A flag given on the command line always wins over the file, and
--no-config ignores it. 'howdy config' shows the file and the settings in
use, and 'howdy config init' writes one to start from.`,
		Example: "  howdy new my_site\n  howdy new my_site --database sqlite\n  howdy new my_site --no-database\n  howdy new my_site --minimal\n  howdy new my_api --api-only\n  howdy new my_site --no-agents\n  howdy new my_site --no-docker\n  howdy new my_site --with auth,storage,jobs,notifications,i18n\n  howdy new my_site --howdy 1.0.0\n  howdy new my_site --source hex\n  howdy new apps/my_site\n  howdy new my_site --dry-run --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.noDatabase && cmd.Flags().Changed("database") {
				return errors.New("--database and --no-database cannot be used together")
			}
			if !opts.noConfig {
				c, err := config.Load()
				if err != nil {
					return err
				}
				if err := opts.applyConfig(cmd.Flags(), c); err != nil {
					return err
				}
			}
			return runNew(args[0], &opts)
		},
	}
	cmd.Flags().BoolVar(&opts.noDatabase, "no-database", false, "leave out the database, Gloo and the todo list example")
	cmd.Flags().StringVar(&opts.database, "database", string(scaffold.Postgres), "database to use: postgres or sqlite")
	cmd.Flags().StringVar(&opts.release, "howdy", scaffold.Release, "Howdy release to depend on, such as "+scaffold.Release)
	cmd.Flags().StringVar(&opts.source, "source", string(scaffold.DefaultSource), "where to download the Howdy packages from: git or hex")
	cmd.Flags().BoolVar(&opts.minimal, "minimal", false, "leave out the admin, telemetry and the console")
	cmd.Flags().BoolVar(&opts.noAgents, "no-agents", false, "leave out AGENTS.md, CLAUDE.md and the skills in .claude/skills/ for coding agents")
	cmd.Flags().BoolVar(&opts.apiOnly, "api-only", false, "a JSON backend only: leave out Howdy UI, pages, layouts and static files")
	cmd.Flags().StringSliceVar(&opts.with, "with", nil, "Howdy packages to set up as well: mail, auth, flags, storage, jobs, notifications, i18n, openapi, remote")
	cmd.Flags().BoolVar(&opts.noDocker, "no-docker", false, "leave out the Dockerfile, .dockerignore and bin/start")
	cmd.Flags().BoolVar(&opts.skipDeps, "skip-deps", false, "do not run 'gleam deps download' after creating the files")
	cmd.Flags().BoolVar(&opts.noConfig, "no-config", false, "ignore the config file and use howdy's own defaults")
	opts.out.flags(cmd, true)
	return cmd
}

func runNew(arg string, opts *newOptions) error {
	database, err := scaffold.ParseDatabase(opts.database)
	if err != nil {
		return err
	}
	release, err := scaffold.ParseRelease(opts.release)
	if err != nil {
		return err
	}
	source, err := scaffold.ParseSource(opts.source)
	if err != nil {
		return err
	}
	if _, err := scaffold.ReleaseSource(source, release); err != nil {
		return fmt.Errorf("%w, or pass --source git", err)
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
	project.Source = source
	project.Repository = opts.repository
	project.Minimal = opts.minimal
	project.API = opts.apiOnly
	project.NoAgents = opts.noAgents
	project.NoDocker = opts.noDocker
	if !project.NoDocker {
		// The image builds and runs with what the project was made with.
		if gleam, err := tools.FindGleam(); err == nil && gleam.GleamRelease() != "" {
			project.Gleam = gleam.GleamRelease()
		}
		if erlang := tools.ErlangRelease(); erlang != "" {
			project.Erlang = erlang
		}
	}
	with, err := withPackages(opts.with)
	if err != nil {
		return fmt.Errorf("--with %w", err)
	}
	for _, p := range with {
		if p.database && database == scaffold.NoDatabase {
			return fmt.Errorf("%s keeps its data in the database, so it cannot be set up in a project without one", p.name)
		}
	}
	files, err := project.Files()
	if err != nil {
		return err
	}
	created := make([]string, len(files))
	for i, f := range files {
		created[i] = f.Path
	}
	command := "howdy new " + project.Name
	var fromConfig []string
	if opts.config != "" {
		fromConfig = []string{"Defaults were read from " + displayPath(opts.config) + "; pass --no-config to ignore it."}
	}
	if opts.out.dryRun {
		if len(with) > 0 {
			if created, err = dryRunWith(project, with); err != nil {
				return err
			}
		}
		return opts.out.report(command, project.Name, created, nil, nil, fromConfig, nil)
	}

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
					if !opts.out.json {
						time.Sleep(45 * time.Millisecond)
					}
				})
				if err != nil {
					return "", err
				}
				kind := fmt.Sprintf("with a %s todo list", project.Database.Label())
				switch {
				case project.API && project.Database == scaffold.NoDatabase:
					kind = "API only"
				case project.API:
					kind = fmt.Sprintf("API only, with a %s todos API", project.Database.Label())
				case project.Database == scaffold.NoDatabase:
					kind = "page only"
				}
				if !project.Minimal {
					kind += ", with the admin, telemetry and console"
				}
				return fmt.Sprintf("%d files in %s, %s, on Howdy %s", count, project.Display(), kind, project.Release), nil
			},
		},
	}
	var manual, added []string
	for _, p := range with {
		steps = append(steps, withStep(project.Dir, p, &added, &manual))
	}
	steps = append(steps, []tui.Step{
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(project.Dir, "format", "src", "dev", "test")
			},
		},
		{
			Title: "Downloading dependencies",
			Skip:  opts.out.skipDeps(opts.skipDeps),
			Run: func(r tui.Reporter) (string, error) {
				r.Detail("gleam deps download")
				if err := gleam.Run(project.Dir, "deps", "download"); err != nil {
					r.Warn("Dependency download failed; run 'gleam deps download' inside the project to see why.")
					return "failed", nil
				}
				return "ready", nil
			},
		},
	}...)

	err = opts.out.run(command, steps)
	if err != nil {
		if !errors.Is(err, tui.ErrCancelled) {
			// Leave nothing half-made behind.
			project.Remove()
		}
		return err
	}

	notes := append(notes(project), fromConfig...)
	if len(with) > 0 {
		created = merge(created, added)
		installed, err := generate.Open(project.Dir)
		if err != nil {
			return err
		}
		for _, p := range with {
			notes = append(notes, p.notes(installed)...)
		}
	}
	return opts.out.report(command, project.Name, created, nil, nextSteps(project), notes, manual)
}

// withPackages reads --with: the packages to set up, each once, in the
// order they must be added.
func withPackages(names []string) ([]pkg, error) {
	wanted := map[string]bool{}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if _, ok := findPackage(name); !ok {
			known := make([]string, len(packages))
			for i, p := range packages {
				known[i] = p.name
			}
			return nil, fmt.Errorf("%q is not a package howdy new sets up: use %s", name, strings.Join(known, ", "))
		}
		wanted[name] = true
	}
	var with []pkg
	for _, p := range packages {
		if wanted[p.name] {
			with = append(with, p)
		}
	}
	return with, nil
}

// withStep sets up one package in the project just written, as 'howdy add'
// would, adding the paths it writes to added and what it could not wire
// to manual.
func withStep(dir string, p pkg, added, manual *[]string) tui.Step {
	return tui.Step{
		Title: strings.Replace(p.title, "Adding", "Setting up", 1),
		Run: func(r tui.Reporter) (string, error) {
			notes, err := install(dir, p, func(path string) {
				r.Log(path)
				*added = append(*added, path)
			})
			*manual = append(*manual, notes...)
			if err != nil {
				return "", err
			}
			if len(notes) > 0 {
				return "needs a hand, see below", nil
			}
			return p.wired, nil
		},
	}
}

// install adds a package to the project in dir, wires it in and notes it
// for coding agents, logging each path it changes. It returns what it
// could not wire.
func install(dir string, p pkg, log func(string)) ([]string, error) {
	project, err := generate.Open(dir)
	if err != nil {
		return nil, err
	}
	i, err := p.open(project)
	if err != nil {
		return nil, err
	}
	changed, err := i.Write()
	for _, path := range changed {
		log(path)
	}
	if err != nil {
		return nil, err
	}
	notes := i.Register()
	agents := []string{p.name}
	if p.agents != nil {
		agents = p.agents(i)
	}
	for _, name := range agents {
		paths, err := project.NoteForAgents(name)
		for _, path := range paths {
			log(path)
		}
		if err != nil {
			return notes, err
		}
	}
	return notes, nil
}

// dryRunWith writes the project and its packages to a temporary directory
// and lists the files there, for --dry-run.
func dryRunWith(project *scaffold.Project, with []pkg) ([]string, error) {
	tmp, err := os.MkdirTemp("", "howdy-dry-run-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	copy := *project
	copy.Dir = filepath.Join(tmp, project.Name)
	if _, err := copy.Write(nil); err != nil {
		return nil, err
	}
	for _, p := range with {
		if _, err := install(copy.Dir, p, func(string) {}); err != nil {
			return nil, err
		}
	}
	return listFiles(copy.Dir)
}

// merge adds the paths in more to paths, once each, sorted.
func merge(paths, more []string) []string {
	seen := map[string]bool{}
	var all []string
	for _, path := range append(paths, more...) {
		if !seen[path] {
			seen[path] = true
			all = append(all, path)
		}
	}
	sort.Strings(all)
	return all
}

// listFiles lists the files under dir, relative to it, sorted.
func listFiles(dir string) ([]string, error) {
	sums, err := snapshot(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(sums))
	for path := range sums {
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
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
	if project.API {
		commands = append(commands, tui.Command{Cmd: "gleam dev", Note: "start with hot reload, then curl http://127.0.0.1:8787/up"})
	} else {
		commands = append(commands, tui.Command{Cmd: "gleam dev", Note: "start with hot reload, then open http://127.0.0.1:8787"})
	}
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
	if !project.API {
		commands = append(commands, tui.Command{Cmd: "gleam run -m tasks/css", Note: "regenerate priv/public/assets/ui.css after changing themes"})
	}
	if !project.NoDocker {
		commands = append(commands, tui.Command{Cmd: "docker build -t " + project.Name + " .", Note: "build the image to deploy; README.md shows how to run it"})
	}
	return commands
}

func notes(project *scaffold.Project) []string {
	n := project.Name
	notes := []string{
		fmt.Sprintf("The home page lives in src/%s/features/home/pages/index.gleam. Static files go in priv/public.", n),
	}
	if project.API {
		notes = []string{
			fmt.Sprintf("Add features in src/%s/features/ and register them in src/%s.gleam; unknown routes answer", n, n),
			"with Howdy's JSON 404. GET /up answers 200 while the app runs, for load balancers.",
		}
	}
	if !project.Minimal {
		notes = append(notes,
			"Under gleam dev the admin is at http://127.0.0.1:8787/_howdy, with every request under Telemetry.",
			"Console shortcuts live in src/console.gleam; README.md shows how to attach one to a running server.",
		)
	}
	if project.Database != scaffold.NoDatabase {
		notes = append(notes,
			fmt.Sprintf("Todos live in src/%s/features/todos/ (record, database operations, JSON API at /api/todos)", n),
			fmt.Sprintf("and the live list in src/%s/features/home/components/todo_list_live_component.gleam. Migrations are in src/%s/migrations/.", n, n),
		)
		if project.API {
			notes[len(notes)-1] = fmt.Sprintf("Migrations are in src/%s/migrations/.", n)
			notes[len(notes)-2] = fmt.Sprintf("Todos live in src/%s/features/todos/ (record, database operations, JSON API at /api/todos).", n)
		}
	}
	env := "Copy .env.example to .env to change the port."
	switch project.Database {
	case scaffold.SQLite:
		notes = append(notes, fmt.Sprintf("The database is %s.sqlite3, created and migrated on first start. SQLite needs a C toolchain to build.", n))
	case scaffold.Postgres:
		notes = append(notes, fmt.Sprintf("docker-compose.yml runs PostgreSQL on port 5432 with the databases %s_dev and %s_test; the app migrates on start.", n, n))
		env = "Copy .env.example to .env to change the port or DATABASE_URL."
	}
	if !project.NoDocker {
		notes = append(notes, fmt.Sprintf("The Dockerfile builds on Gleam %s and Erlang/OTP %s, the versions installed here.", project.Gleam, project.Erlang))
	}
	return append(notes, env+" See README.md for the project layout.")
}

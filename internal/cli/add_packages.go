package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"howdy/internal/generate"
	"howdy/internal/tools"
	"howdy/internal/tui"
)

// installer is a Howdy package being added to a project: Write adds it to
// gleam.toml and creates its files, and Register wires it in, returning
// what it could not do itself.
type installer interface {
	Write() ([]string, error)
	Register() []string
}

// pkg describes a package for 'howdy add' and 'howdy new --with'.
type pkg struct {
	// name is the subcommand and the value of --with, such as "jobs".
	name string
	// database is set for a package that keeps its data in the database.
	database bool
	// title is the step that adds it, such as "Adding Howdy Jobs".
	title string
	// wire is the step that wires it in, and wired what it reports.
	wire, wired string
	// open checks the project can take the package.
	open func(*generate.Project) (installer, error)
	// agents are the sections it adds to AGENTS.md.
	agents func(installer) []string
	// next and notes are what to do once it is added.
	next  func(*generate.Project) []tui.Command
	notes func(*generate.Project) []string
}

// packages are the Howdy packages 'howdy new --with' sets up, in the order
// they are added: auth and notifications use the mail added before them,
// notifications use auth to know whose inbox a request is for, and the jobs
// runner. Those without their own 'howdy add' command here have one in
// add.go, with the same generator.
var packages = []pkg{
	{
		name:  "mail",
		title: "Adding Howdy Mail",
		wire:  "Wiring up email", wired: "services, outbox, previews, tests",
		open: func(p *generate.Project) (installer, error) { return generate.NewMail(p) },
		notes: func(p *generate.Project) []string {
			return []string{
				fmt.Sprintf("Emails live in src/%s/emails/, sent with mail.send(services.mailer, ...). For gleam run, set MAIL_FROM", p.Name),
				"and one of SMTP_URL, RESEND_API_KEY or SENDGRID_API_KEY (see .env.example); without one, email is logged.",
			}
		},
	},
	{
		name:     "auth",
		database: true,
		title:    "Adding Howdy Auth",
		wire:     "Wiring up sign-in and roles", wired: "services, routes, migrations, tests",
		open: func(p *generate.Project) (installer, error) { return generate.NewAuth(p) },
		agents: func(i installer) []string {
			return i.(*generate.Auth).AgentNotes()
		},
		notes: func(p *generate.Project) []string {
			register := "Register at /auth/register; guard a controller with controller.guarded(\"/account\", auth.required(services.identity))."
			if p.API {
				register = "Register with POST /api/auth/register; guard a controller with controller.guarded(\"/account\", auth.required(services.identity))."
			}
			return []string{register, "Set APP_ORIGIN to the site's public address before deploying (see .env.example)."}
		},
	},
	{
		name:     "flags",
		database: true,
		title:    "Adding Howdy Flags",
		wire:     "Wiring up the flags", wired: "services, migrations, tests",
		open: func(p *generate.Project) (installer, error) { return generate.NewFlags(p) },
		notes: func(p *generate.Project) []string {
			return []string{
				fmt.Sprintf("Define flags in src/%s/feature_flags.gleam; check one with flags.enabled(services.features, flag, for: flags.user(id)),", p.Name),
				"and manage them with gleam run -m tasks/flags help.",
			}
		},
	},
	{
		name:  "storage",
		title: "Adding Howdy Storage",
		wire:  "Wiring up file storage", wired: "services, /files, tests",
		open: func(p *generate.Project) (installer, error) { return generate.NewStorage(p) },
		next: func(*generate.Project) []tui.Command {
			return []tui.Command{{Cmd: "gleam test", Note: "run the tests, including storage's"}}
		},
		notes: func(p *generate.Project) []string {
			return []string{
				fmt.Sprintf("Files are stored by src/%s/platform/file_storage.gleam: in storage/ unless S3_BUCKET names a bucket.", p.Name),
				"Set STORAGE_SECRET (see .env.example) for signed URLs to files kept in storage/.",
			}
		},
	},
	{
		name:     "jobs",
		database: true,
		title:    "Adding Howdy Jobs",
		wire:     "Wiring up background jobs", wired: "runner, migrations",
		open: func(p *generate.Project) (installer, error) { return generate.NewJobs(p) },
		next: func(p *generate.Project) []tui.Command {
			next := []tui.Command{
				{Cmd: "gleam run -m tasks/jobs stats", Note: "how many jobs are in each state"},
				{Cmd: "gleam test", Note: "run the tests, including the example job's"},
			}
			if generate.HasAdmin(p) {
				next = append([]tui.Command{{Cmd: "gleam dev", Note: "then open http://127.0.0.1:8787/_howdy/jobs"}}, next...)
			}
			return next
		},
		notes: func(p *generate.Project) []string {
			return []string{
				fmt.Sprintf("Define jobs in src/%s/background_jobs.gleam and handle them in runner(). Queue one with", p.Name),
				"jobs.new(background_jobs.say_hello(), \"Ada\") |> jobs.insert(into: conn), in the transaction that needs it.",
			}
		},
	},
	{
		name:     "notifications",
		database: true,
		title:    "Adding Howdy Notifications",
		wire:     "Wiring up notifications", wired: "services, /notifications, jobs, migrations, tests",
		open: func(p *generate.Project) (installer, error) { return generate.NewNotifications(p) },
		agents: func(i installer) []string {
			return i.(*generate.Notifications).AgentNotes()
		},
		next: func(*generate.Project) []tui.Command {
			return []tui.Command{{Cmd: "gleam test", Note: "run the tests, including the notifier's"}}
		},
		notes: func(p *generate.Project) []string {
			notes := []string{
				fmt.Sprintf("Send with notifications.draft(kind, title) |> notifications.send(services.notifier, to: user_id); see src/%s/platform/notifier.gleam.", p.Name),
			}
			if !p.Auth {
				notes = append(notes, "The inbox at /notifications refuses everyone until routes() in notifier.gleam names the signed-in user, as with 'howdy add auth'.")
			}
			return notes
		},
	},
	{
		name:  "i18n",
		title: "Adding Howdy I18n",
		wire:  "Wiring up translations", wired: "locale middleware, dev regeneration",
		open: func(p *generate.Project) (installer, error) { return generate.NewI18n(p) },
		next: func(*generate.Project) []tui.Command {
			return []tui.Command{
				{Cmd: "gleam run -m howdy/i18n/cli -- add de", Note: "add a language to translate into"},
				{Cmd: "gleam run -m howdy/i18n/cli -- gen", Note: "regenerate the messages after changing locales/"},
			}
		},
		notes: func(p *generate.Project) []string {
			return []string{
				fmt.Sprintf("Messages live in locales/en.json; src/%s/messages.gleam is generated from them, and gleam dev regenerates it.", p.Name),
				fmt.Sprintf("Write in the reader's language with messages.greeting(locales.translator(ctx), name: \"Ada\"); see src/%s/platform/locales.gleam.", p.Name),
			}
		},
	},
	{
		name:  "openapi",
		title: "Adding Howdy OpenAPI",
		wire:  "Serving the document", wired: "/openapi.json, /docs",
		open: func(p *generate.Project) (installer, error) { return generate.NewOpenAPI(p) },
		notes: func(p *generate.Project) []string {
			return []string{
				fmt.Sprintf("The API is documented at /openapi.json and /docs, from src/%s/platform/api_docs.gleam. Only routes written", p.Name),
				"with howdy/openapi/endpoint are listed, so it starts empty.",
			}
		},
	},
	{
		name:  "remote",
		title: "Adding Howdy Remote",
		wire:  "Wiring up the procedure server", wired: "services, /rpc, cluster, tests",
		open: func(p *generate.Project) (installer, error) { return generate.NewRemote(p) },
		notes: func(p *generate.Project) []string {
			return []string{
				fmt.Sprintf("Define procedures in src/%s/procedures.gleam and answer them in src/%s/platform/rpc.gleam.", p.Name, p.Name),
				"Set RPC_TOKEN to a long random secret to serve them over HTTP (see .env.example).",
			}
		},
	},
}

func findPackage(name string) (pkg, bool) {
	for _, p := range packages {
		if p.name == name {
			return p, true
		}
	}
	return pkg{}, false
}

func addStorageCommand() *cobra.Command {
	return addPackageCommand("storage",
		"Add file storage with Howdy Storage: an S3-compatible bucket or a local folder, in the app's services",
		`Add file storage to the current project with Howdy Storage.

It adds howdy_storage, from the same release as the project's other Howdy
packages, to gleam.toml, then writes

  src/<project>/platform/file_storage.gleam         makes the storage from the environment
  test/<project>/platform/file_storage_test.gleam   stores and reads a file in memory

The app's services get files: the bucket named by S3_BUCKET (or
BUCKET_NAME) on AWS S3, Cloudflare R2, Tigris, MinIO or another
S3-compatible service, reached with the AWS_* variables, or without one, the
folder storage/. The folder's signed URLs are served at /files once
STORAGE_SECRET is set. The tests keep files in memory. Store a file with
storage.put_file(services.files, key, path, content_type: ...).`)
}

func addJobsCommand() *cobra.Command {
	return addPackageCommand("jobs",
		"Add background jobs with Howdy Jobs, kept in the app's database and run beside the server",
		`Add background jobs to the current project with Howdy Jobs.

Jobs are kept in the app's database, so the project needs howdy_database:
a project made without --no-database.

It adds howdy_jobs, from the same release as the project's other Howdy
packages, to gleam.toml, then writes

  src/<project>/background_jobs.gleam         the jobs, an example say_hello, and the runner
  src/tasks/jobs.gleam                        manages them: gleam run -m tasks/jobs help
  test/<project>/background_jobs_test.gleam   queues the example and does it

src/<project>.gleam gets jobs_runner, which main and the dev server start
beside the server, and platform/database.gleam runs the jobs' migrations
after the app's own. With the dev admin, /_howdy gets Jobs: queues,
schedules, and every job with its failures. Queue a job in the transaction
that needs it: jobs.new(job, args) |> jobs.insert(into: conn).`)
}

func addNotificationsCommand() *cobra.Command {
	return addPackageCommand("notifications",
		"Add notifications with Howdy Notifications: an inbox per user, pushed live and emailed by background jobs",
		`Add notifications to the current project with Howdy Notifications.

Notifications are kept in the app's database, so the project needs
howdy_database: a project made without --no-database. They are emailed as
background jobs, so a project without mail or jobs gets them too, as with
'howdy add mail' and 'howdy add jobs'.

It adds howdy_notifications, from the same release as the project's other
Howdy packages, to gleam.toml, then writes

  src/<project>/platform/notifier.gleam         makes the notifier and serves the inbox
  test/<project>/platform/notifier_test.gleam   sends a notification and reads it

The app's services get notifier, built in main, the dev server and the
test support; the app serves each user's inbox at /notifications, which
howdy_ui's bell follows; the jobs runner delivers the email; and
platform/database.gleam runs the notifications' migrations after the app's
own. The inbox needs to know who is signed in: with Howdy Auth it asks
auth, and otherwise it refuses everyone until routes() in notifier.gleam
says. Send with notifications.draft(kind, title) |>
notifications.send(services.notifier, to: user_id).`)
}

func addI18nCommand() *cobra.Command {
	return addPackageCommand("i18n",
		"Add translations with Howdy I18n: message catalogues, a module generated from them, and each request's locale",
		`Add translations to the current project with Howdy I18n.

It adds howdy_i18n, from the same release as the project's other Howdy
packages, to gleam.toml, then writes

  locales/en.json                        the source catalogue, in ICU MessageFormat
  src/<project>/messages.gleam           a function for each message, generated from locales/
  src/<project>/platform/locales.gleam   which locale each request wants, and its translator
  test/<project>/messages_test.gleam     checks the messages

Every response is stamped with the locale it was written for, chosen from
?locale=, the locale cookie and the browser's languages. Under gleam dev the
messages are regenerated whenever a catalogue changes; otherwise run
gleam run -m howdy/i18n/cli -- gen. Write in the reader's language with
messages.greeting(locales.translator(ctx), name: "Ada").`)
}

// addPackageCommand is 'howdy add <name>' for one of packages.
func addPackageCommand(name, short, long string) *cobra.Command {
	var skipDeps bool
	out := &output{}
	cmd := &cobra.Command{
		Use:     name,
		Short:   short,
		Long:    long,
		Example: "  howdy add " + name,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _ := findPackage(name)
			return runAddPackage(out, skipDeps, p)
		},
	}
	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "do not run 'gleam deps download' after changing gleam.toml")
	out.flags(cmd, true)
	return cmd
}

func runAddPackage(out *output, skipDeps bool, p pkg) error {
	defer out.cleanup()
	dir, err := out.projectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	i, err := p.open(project)
	if err != nil {
		return err
	}

	var gleam tools.Tool
	var manual []string
	steps := []tui.Step{
		{
			Title: "Checking for Gleam",
			Run: func(r tui.Reporter) (string, error) {
				var err error
				gleam, err = tools.FindGleam()
				return gleam.Version, err
			},
		},
	}
	steps = append(steps, installSteps(project, p, i, &manual)...)
	steps = append(steps,
		tui.Step{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(dir, "format", "src", "dev", "test")
			},
		},
		tui.Step{
			Title: "Downloading dependencies",
			Skip:  out.skipDeps(skipDeps),
			Run: func(r tui.Reporter) (string, error) {
				r.Detail("gleam deps download")
				if err := gleam.Run(dir, "deps", "download"); err != nil {
					r.Warn("Dependency download failed; run 'gleam deps download' to see why.")
					return "failed", nil
				}
				return "ready", nil
			},
		},
	)
	command := "howdy add " + p.name
	if err := out.run(command, steps); err != nil {
		return err
	}
	return out.done(command, p.name, p.next(project), p.notes(project), manual)
}

// installSteps add a package to the project, wire it in and note it for
// coding agents, adding what it could not wire to manual.
func installSteps(project *generate.Project, p pkg, i installer, manual *[]string) []tui.Step {
	agents := []string{p.name}
	if p.agents != nil {
		agents = p.agents(i)
	}
	return []tui.Step{
		{
			Title: p.title,
			Run: func(r tui.Reporter) (string, error) {
				changed, err := i.Write()
				for _, path := range changed {
					r.Log(path)
				}
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%d files", len(changed)), nil
			},
		},
		{
			Title: p.wire,
			Run: func(r tui.Reporter) (string, error) {
				notes := i.Register()
				*manual = append(*manual, notes...)
				if len(notes) > 0 {
					return "needs a hand, see below", nil
				}
				return p.wired, nil
			},
		},
		noteForAgents(project, agents...),
	}
}

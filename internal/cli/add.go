package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"howdy/internal/generate"
	"howdy/internal/tools"
	"howdy/internal/tui"
)

// addCommand installs Howdy packages into the current project and wires
// them up, where 'create' writes the project's own code.
func addCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a Howdy package to the current project and wire it up",
	}
	cmd.AddCommand(addMailCommand())
	cmd.AddCommand(addAuthCommand())
	cmd.AddCommand(addRemoteCommand())
	cmd.AddCommand(addOpenAPICommand())
	cmd.AddCommand(addFlagsCommand())
	cmd.AddCommand(addStorageCommand())
	cmd.AddCommand(addJobsCommand())
	cmd.AddCommand(addNotificationsCommand())
	cmd.AddCommand(addI18nCommand())
	return cmd
}

func addFlagsCommand() *cobra.Command {
	var skipDeps bool
	out := &output{}
	cmd := &cobra.Command{
		Use:   "flags",
		Short: "Add feature flags with Howdy Flags, kept in the app's database and managed in the dev admin",
		Long: `Add feature flags to the current project with Howdy Flags.

Flags keep their settings in the app's database, so the project needs
howdy_database: a project made without --no-database.

It adds howdy_flags, from the same release as the project's other Howdy
packages, to gleam.toml, then writes

  src/<project>/feature_flags.gleam         the flags, an example early_access, and starting them
  src/tasks/flags.gleam                     manages them: gleam run -m tasks/flags help
  test/<project>/feature_flags_test.gleam   turns the example on for one user

The app's services get features, started in main, the dev server and the
test support, and platform/database.gleam runs the flags' migrations after
the app's own. With the dev admin, /_howdy gets Flags: kill switches,
rules, groups and history. Check a flag with
flags.enabled(services.features, feature_flags.early_access(), for: flags.user(id)).`,
		Example: "  howdy add flags",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddFlags(out, skipDeps)
		},
	}
	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "do not run 'gleam deps download' after changing gleam.toml")
	out.flags(cmd, true)
	return cmd
}

func runAddFlags(out *output, skipDeps bool) error {
	defer out.cleanup()
	dir, err := out.projectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	f, err := generate.NewFlags(project)
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
		{
			Title: "Adding Howdy Flags",
			Run: func(r tui.Reporter) (string, error) {
				changed, err := f.Write()
				for _, path := range changed {
					r.Log(path)
				}
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("howdy_flags %s, %d files", f.Version("howdy_flags"), len(changed)), nil
			},
		},
		{
			Title: "Wiring up the flags",
			Run: func(r tui.Reporter) (string, error) {
				manual = f.Register()
				if len(manual) > 0 {
					return "needs a hand, see below", nil
				}
				if f.Admin {
					return "services, migrations, dev admin, tests", nil
				}
				return "services, migrations, tests", nil
			},
		},
		noteForAgents(project, "flags"),
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(dir, "format", "src", "dev", "test")
			},
		},
		{
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
	}
	if err := out.run("howdy add flags", steps); err != nil {
		return err
	}

	var notes []string
	if f.Admin {
		notes = append(notes, "The admin at /_howdy manages the flags under Flags: kill switches, rules, groups and history.")
	}
	notes = append(notes,
		fmt.Sprintf("Define flags in %s and add them to all(). Check one with", f.FlagsFile()),
		"flags.enabled(services.features, feature_flags.early_access(), for: flags.user(id)).",
	)
	commands := []tui.Command{
		{Cmd: "gleam run -m tasks/flags list", Note: "every flag and its state"},
		{Cmd: "gleam run -m tasks/flags rollout early_access 5", Note: "turn it on for 5% of users"},
		{Cmd: "gleam test", Note: "run the tests, including the flags'"},
	}
	if f.Admin {
		commands = append([]tui.Command{{Cmd: "gleam dev", Note: "then open http://127.0.0.1:8787/_howdy/flags"}}, commands...)
	}
	return out.done("howdy add flags", "flags", commands, notes, manual)
}

func addRemoteCommand() *cobra.Command {
	var skipDeps bool
	out := &output{}
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Add typed calls between services with Howdy Remote, over Erlang distribution or HTTP",
		Long: `Add remote procedure calls to the current project with Howdy Remote.

It adds howdy_remote, from the same release as the project's other Howdy
packages, to gleam.toml, then writes

  src/<project>/procedures.gleam           the procedures the app serves: a name, input and output
  src/<project>/platform/rpc.gleam         the server that answers them, and the RPC token
  test/<project>/platform/rpc_test.gleam   calls the example procedure both ways

and serves the procedures two ways:

  cluster   main and the dev server start the server, so other nodes
            connected to this one call it with remote.cluster()
  HTTP      the app mounts POST /rpc/<procedure name>, for callers that send
            authorization: Bearer $RPC_TOKEN; without RPC_TOKEN it refuses all

The token is in the app's services as rpc_token. Call another service with
remote.call(target, procedure, input, timeout: 5000).`,
		Example: "  howdy add remote",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddRemote(out, skipDeps)
		},
	}
	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "do not run 'gleam deps download' after changing gleam.toml")
	out.flags(cmd, true)
	return cmd
}

func runAddRemote(out *output, skipDeps bool) error {
	defer out.cleanup()
	dir, err := out.projectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	rm, err := generate.NewRemote(project)
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
		{
			Title: "Adding Howdy Remote",
			Run: func(r tui.Reporter) (string, error) {
				changed, err := rm.Write()
				for _, path := range changed {
					r.Log(path)
				}
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("howdy_remote %s, %d files", rm.Version("howdy_remote"), len(changed)), nil
			},
		},
		{
			Title: "Wiring up the procedure server",
			Run: func(r tui.Reporter) (string, error) {
				manual = rm.Register()
				if len(manual) > 0 {
					return "needs a hand, see below", nil
				}
				return "services, /rpc, cluster, tests", nil
			},
		},
		noteForAgents(project, "remote"),
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(dir, "format", "src", "dev", "test")
			},
		},
		{
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
	}
	if err := out.run("howdy add remote", steps); err != nil {
		return err
	}

	var notes []string
	notes = append(notes,
		fmt.Sprintf("Define procedures in %s and answer them in server() in %s.", rm.ProceduresFile(), rm.RPCFile()),
		"Call another service with remote.call(remote.cluster(), its_api.get_user(), 42, timeout: 5000), or a",
		"remote.node(name) or remote.http(url, token:) target, and answer a request with |> remote.respond(ctx, to_json).",
		"Set RPC_TOKEN to a long random secret to serve over HTTP (see .env.example); connect nodes only on a private network.",
	)
	commands := []tui.Command{
		{Cmd: "ERL_FLAGS=\"-sname " + project.Name + " -setcookie <cookie>\" gleam dev", Note: "serve the procedures to other nodes"},
		{Cmd: "gleam test", Note: "call the example procedure on the cluster and over HTTP"},
	}
	return out.done("howdy add remote", "remote", commands, notes, manual)
}

func addOpenAPICommand() *cobra.Command {
	var skipDeps bool
	out := &output{}
	cmd := &cobra.Command{
		Use:   "openapi",
		Short: "Add an OpenAPI document with Howdy OpenAPI, served by the app and browsable in the dev admin",
		Long: `Add an OpenAPI 3.1 document to the current project with Howdy OpenAPI.

It adds howdy_openapi, from the same release as the project's other Howdy
packages, to gleam.toml, then writes

  src/<project>/platform/api_docs.gleam           the document's title, version and security schemes
  test/<project>/platform/api_docs_test.gleam     checks the document and reference are served

and serves, after the app's controllers,

  /openapi.json   the document
  /docs           a reference page to read it

The document lists the routes written with howdy/openapi/endpoint, from the
schemas they read. Existing controllers are left as they are, so it starts
empty: move a route to an endpoint to publish it. With the dev admin,
/_howdy finds the document and lists every endpoint under API, where you
can call them.`,
		Example: "  howdy add openapi",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddOpenAPI(out, skipDeps)
		},
	}
	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "do not run 'gleam deps download' after changing gleam.toml")
	out.flags(cmd, true)
	return cmd
}

func runAddOpenAPI(out *output, skipDeps bool) error {
	defer out.cleanup()
	dir, err := out.projectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	o, err := generate.NewOpenAPI(project)
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
		{
			Title: "Adding Howdy OpenAPI",
			Run: func(r tui.Reporter) (string, error) {
				changed, err := o.Write()
				for _, path := range changed {
					r.Log(path)
				}
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("howdy_openapi %s, %d files", o.Version("howdy_openapi"), len(changed)), nil
			},
		},
		{
			Title: "Serving the document",
			Run: func(r tui.Reporter) (string, error) {
				manual = o.Register()
				if len(manual) > 0 {
					return "needs a hand, see below", nil
				}
				if o.Admin {
					return "/openapi.json, /docs, dev admin", nil
				}
				return "/openapi.json, /docs", nil
			},
		},
		noteForAgents(project, "openapi"),
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(dir, "format", "src", "dev", "test")
			},
		},
		{
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
	}
	if err := out.run("howdy add openapi", steps); err != nil {
		return err
	}

	var notes []string
	if o.Admin {
		notes = append(notes, "The admin at /_howdy lists every documented endpoint under API, and can call them.")
	}
	notes = append(notes,
		"Existing controllers are unchanged, so the document starts empty. Publish a route by writing it with",
		"howdy/openapi/endpoint, e.g. controller.new(\"user\") |> endpoint.get(\"/:id\", by_id()): see "+o.DocsFile()+".",
	)
	commands := []tui.Command{
		{Cmd: "gleam dev", Note: "then read the API at http://127.0.0.1:8787/docs"},
		{Cmd: "gleam test", Note: "check the document is served"},
	}
	return out.done("howdy add openapi", "openapi", commands, notes, manual)
}

func addAuthCommand() *cobra.Command {
	var skipDeps bool
	out := &output{}
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Add sign-in and roles with Howdy Auth, wired into the app, the tests and the dev admin",
		Long: `Add sign-in and roles to the current project with Howdy Auth.

Auth keeps its users, sessions and roles in the app's database, so the
project needs howdy_database: a project made without --no-database. Sign-in
tokens are emailed, so a project without mail gets it too, as with
'howdy add mail'.

It adds howdy_auth, from the same release as the project's other Howdy
packages, and writes src/<project>/platform/accounts.gleam, which builds the
app's identity and permissions:

  sign in       a link or six-digit code emailed to the address
  register      the same way; the account is made once the email is used
  roles         defined in code or in the dev admin, checked in guards
  pages         /auth/login, /auth/register and /auth/account
  JSON API      /api/auth

A project made with --api-only gets the JSON API without the pages, and
emails carry the code without a link.

The app's services get identity and permissions, built in main, the dev
server and the test support, and platform/database.gleam runs auth's
migrations after the app's own. With the dev admin, /_howdy gets users,
groups and roles, lets you sign in as anyone, and previews auth's emails.
Guard a controller with controller.guarded("/account",
auth.required(services.identity)).`,
		Example: "  howdy add auth",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddAuth(out, skipDeps)
		},
	}
	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "do not run 'gleam deps download' after changing gleam.toml")
	out.flags(cmd, true)
	return cmd
}

func runAddAuth(out *output, skipDeps bool) error {
	defer out.cleanup()
	dir, err := out.projectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	a, err := generate.NewAuth(project)
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
		{
			Title: "Adding Howdy Auth",
			Run: func(r tui.Reporter) (string, error) {
				changed, err := a.Write()
				for _, path := range changed {
					r.Log(path)
				}
				if err != nil {
					return "", err
				}
				if a.Mail != nil {
					return fmt.Sprintf("howdy_auth %s and howdy_mail %s, %d files", a.Version("howdy_auth"), a.Mail.Version("howdy_mail"), len(changed)), nil
				}
				return fmt.Sprintf("howdy_auth %s, %d files", a.Version("howdy_auth"), len(changed)), nil
			},
		},
		{
			Title: "Wiring up sign-in and roles",
			Run: func(r tui.Reporter) (string, error) {
				manual = a.Register()
				if len(manual) > 0 {
					return "needs a hand, see below", nil
				}
				if a.Admin {
					return "services, routes, migrations, dev admin, tests", nil
				}
				return "services, routes, migrations, tests", nil
			},
		},
		noteForAgents(project, a.AgentNotes()...),
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(dir, "format", "src", "dev", "test")
			},
		},
		{
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
	}
	if err := out.run("howdy add auth", steps); err != nil {
		return err
	}

	var notes []string
	if a.Admin {
		notes = append(notes, "The sign-in email arrives in the admin at /_howdy under Mail; users, groups and roles are there too.")
	}
	if a.Mail != nil {
		notes = append(notes, "Mail was added too, as sign-in links are emailed: see src/"+project.Name+"/platform/email.gleam.")
	}
	notes = append(notes,
		"Guard a controller with controller.guarded(\"/account\", auth.required(services.identity)); ctx.guard.user is then the user.",
		"Set APP_ORIGIN to the site's public address before deploying (see .env.example): links and cookies are for it.",
	)
	commands := []tui.Command{
		{Cmd: "gleam dev", Note: "then register at http://127.0.0.1:8787/auth/register"},
	}
	if project.API {
		commands[0].Note = "then POST {\"email\": ...} to http://127.0.0.1:8787/api/auth/register"
	}
	commands = append(commands,
		tui.Command{Cmd: "gleam test", Note: "run the tests, including auth's"},
	)
	return out.done("howdy add auth", "auth", commands, notes, manual)
}

func addMailCommand() *cobra.Command {
	var skipDeps bool
	out := &output{}
	cmd := &cobra.Command{
		Use:   "mail",
		Short: "Add email: Howdy Mail, a mailer in the app's services, and the outbox in the dev admin",
		Long: `Add email to the current project with Howdy Mail.

It adds howdy_mail, from the same release as the project's other Howdy
packages, and smail for writing templates to gleam.toml, then writes

  src/<project>/platform/email.gleam        the mailer gleam run uses, from the environment
  src/<project>/emails/welcome_email.gleam  an example email, and its preview
  src/<project>/emails/previews.gleam       every email built from sample data
  test/<project>/emails/previews_test.gleam checks that every email can be sent

and puts a mailer in the app's services, so controllers send with
services.mailer:

  gleam run   sends through SMTP_URL, RESEND_API_KEY or SENDGRID_API_KEY,
              whichever is set, and logs each email when none is
  gleam dev   keeps email in an outbox in tmp/mail; with the dev admin,
              /_howdy shows it under Mail, with the previews
  gleam test  keeps email in an in-memory outbox

Add more emails with 'howdy create email <name>'.`,
		Example: "  howdy add mail",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddMail(out, skipDeps)
		},
	}
	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "do not run 'gleam deps download' after changing gleam.toml")
	out.flags(cmd, true)
	return cmd
}

func runAddMail(out *output, skipDeps bool) error {
	defer out.cleanup()
	dir, err := out.projectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	m, err := generate.NewMail(project)
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
		{
			Title: "Adding Howdy Mail",
			Run: func(r tui.Reporter) (string, error) {
				changed, err := m.Write()
				for _, path := range changed {
					r.Log(path)
				}
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("howdy_mail %s, %d files", m.Version("howdy_mail"), len(changed)), nil
			},
		},
		{
			Title: "Wiring up the mailer",
			Run: func(r tui.Reporter) (string, error) {
				manual = m.Register()
				if len(manual) > 0 {
					return "needs a hand, see below", nil
				}
				if m.Admin {
					return "services, dev outbox and admin, tests", nil
				}
				return "services, dev outbox, tests", nil
			},
		},
		noteForAgents(project, "mail"),
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(dir, "format", "src", "dev", "test")
			},
		},
		{
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
	}
	if err := out.run("howdy add mail", steps); err != nil {
		return err
	}

	var notes []string
	n := project.Name
	commands := []tui.Command{}
	if m.Admin {
		commands = append(commands, tui.Command{Cmd: "gleam dev", Note: "then open http://127.0.0.1:8787/_howdy for Mail and its previews"})
	} else {
		commands = append(commands, tui.Command{Cmd: "gleam dev", Note: "email the app sends is written to tmp/mail as .eml files"})
	}
	commands = append(commands,
		tui.Command{Cmd: "howdy create email <name>", Note: "add another email, with its preview"},
		tui.Command{Cmd: "gleam test", Note: "check that every email can be sent"},
	)
	notes = append(notes, []string{
		fmt.Sprintf("Send from a controller with the mailer in the app's services: pass services.mailer to it in src/%s.gleam, then", n),
		"mail.send(mailer, welcome_email.build(to: mail.address(\"ada@example.com\"))).",
		fmt.Sprintf("Emails live in src/%s/emails/; write them with smail. For gleam run, set MAIL_FROM and one of", n),
		"SMTP_URL, RESEND_API_KEY or SENDGRID_API_KEY (see .env.example); without one, email is logged.",
	}...)
	return out.done("howdy add mail", "mail", commands, notes, manual)
}

// noteForAgents is the step that tells coding agents working on the
// project about each package: a section in AGENTS.md, and a skill for
// packages that have one.
func noteForAgents(project *generate.Project, pkgs ...string) tui.Step {
	return tui.Step{
		Title: "Noting it for coding agents",
		Run: func(r tui.Reporter) (string, error) {
			var changed []string
			for _, pkg := range pkgs {
				paths, err := project.NoteForAgents(pkg)
				changed = append(changed, paths...)
				if err != nil {
					return "", err
				}
			}
			if len(changed) == 0 {
				return "no AGENTS.md to add to", nil
			}
			for _, path := range changed {
				r.Log(path)
			}
			return strings.Join(slices.Compact(changed), ", "), nil
		},
	}
}

package cli

import (
	"fmt"

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
	return cmd
}

func addAuthCommand() *cobra.Command {
	var plain, skipDeps bool
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

The app's services get identity and permissions, built in main, the dev
server and the test support, and platform/database.gleam runs auth's
migrations after the app's own. With the dev admin, /_howdy gets users,
groups and roles, lets you sign in as anyone, and previews auth's emails.
Guard a controller with controller.guarded("/account",
auth.required(services.identity)).`,
		Example: "  howdy add auth",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddAuth(plain, skipDeps)
		},
	}
	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "do not run 'gleam deps download' after changing gleam.toml")
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func runAddAuth(plain, skipDeps bool) error {
	dir, err := findProjectDir()
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
	var notes []string
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
					return fmt.Sprintf("howdy_auth and howdy_mail %s, %d files", a.Ref, len(changed)), nil
				}
				return fmt.Sprintf("howdy_auth %s, %d files", a.Ref, len(changed)), nil
			},
		},
		{
			Title: "Wiring up sign-in and roles",
			Run: func(r tui.Reporter) (string, error) {
				notes = a.Register()
				if len(notes) > 0 {
					return "needs a hand, see below", nil
				}
				if a.Admin {
					return "services, routes, migrations, dev admin, tests", nil
				}
				return "services, routes, migrations, tests", nil
			},
		},
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(dir, "format", "src", "dev", "test")
			},
		},
		{
			Title: "Downloading dependencies",
			Skip: func() (string, bool) {
				if skipDeps {
					return "skipped (--skip-deps)", true
				}
				return "", false
			},
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
	if err := tui.Run("howdy add auth", steps, plain); err != nil {
		return err
	}

	for i, note := range notes {
		notes[i] = "Needs a hand: " + note
	}
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
	commands = append(commands,
		tui.Command{Cmd: "gleam test", Note: "run the tests, including auth's"},
	)
	fmt.Print(tui.NextSteps("auth", commands, notes))
	return nil
}

func addMailCommand() *cobra.Command {
	var plain, skipDeps bool
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
			return runAddMail(plain, skipDeps)
		},
	}
	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "do not run 'gleam deps download' after changing gleam.toml")
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func runAddMail(plain, skipDeps bool) error {
	dir, err := findProjectDir()
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
	var notes []string
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
				return fmt.Sprintf("howdy_mail %s, %d files", m.Ref, len(changed)), nil
			},
		},
		{
			Title: "Wiring up the mailer",
			Run: func(r tui.Reporter) (string, error) {
				notes = m.Register()
				if len(notes) > 0 {
					return "needs a hand, see below", nil
				}
				if m.Admin {
					return "services, dev outbox and admin, tests", nil
				}
				return "services, dev outbox, tests", nil
			},
		},
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				return "gleam format", gleam.Run(dir, "format", "src", "dev", "test")
			},
		},
		{
			Title: "Downloading dependencies",
			Skip: func() (string, bool) {
				if skipDeps {
					return "skipped (--skip-deps)", true
				}
				return "", false
			},
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
	if err := tui.Run("howdy add mail", steps, plain); err != nil {
		return err
	}

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
	// What could not be done goes first, so it is not missed.
	for i, note := range notes {
		notes[i] = "Needs a hand: " + note
	}
	notes = append(notes, []string{
		fmt.Sprintf("Send from a controller with the mailer in the app's services: pass services.mailer to it in src/%s.gleam, then", n),
		"mail.send(mailer, welcome_email.build(to: mail.address(\"ada@example.com\"))).",
		fmt.Sprintf("Emails live in src/%s/emails/; write them with smail. For gleam run, set MAIL_FROM and one of", n),
		"SMTP_URL, RESEND_API_KEY or SENDGRID_API_KEY (see .env.example); without one, email is logged.",
	}...)
	fmt.Print(tui.NextSteps("mail", commands, notes))
	return nil
}

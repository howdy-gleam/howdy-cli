package generate

import (
	"errors"
	"fmt"
	"howdy/internal/scaffold"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Notifications adds howdy_notifications to a project: an inbox for each
// user kept in the app's database, pushed live to their open pages and
// emailed for the kinds that call for it, as `notifier` in the app's
// services. Email goes out as background jobs, so a project without mail
// or jobs gets them first.
type Notifications struct {
	Project *Project
	// Mail and Jobs are set when the project needs them first.
	Mail *Mail
	Jobs *Jobs
	scaffold.Source
}

// NewNotifications checks that the project can take notifications and
// does not have them yet.
func NewNotifications(p *Project) (*Notifications, error) {
	source, _, err := installable(p, "howdy_notifications", "notifications", true)
	if err != nil {
		return nil, err
	}
	n := &Notifications{Project: p, Source: source}
	toml, err := os.ReadFile(filepath.Join(p.Dir, "gleam.toml"))
	if err != nil {
		return nil, err
	}
	// The inbox asks auth who is signed in, when there is auth to ask.
	p.Auth = p.Auth || dependency("howdy_auth").Match(toml)
	if !dependency("howdy_mail").Match(toml) {
		if n.Mail, err = NewMail(p); err != nil {
			return nil, err
		}
	}
	if !dependency("howdy_jobs").Match(toml) {
		if n.Jobs, err = NewJobs(p); err != nil {
			return nil, err
		}
	}
	return n, nil
}

// NotifierFile is the module that makes the app's notifier and serves the
// inbox.
func (n *Notifications) NotifierFile() string {
	return filepath.ToSlash(filepath.Join("src", n.Project.Name, "platform", "notifier.gleam"))
}

// Files renders the notifier module and, when the project has test
// support, its test.
func (n *Notifications) Files() ([]File, error) {
	p := n.Project
	targets := []struct{ template, path string }{
		{"notifier.gleam.tmpl", n.NotifierFile()},
	}
	if hasSupport(p) {
		targets = append(targets, struct{ template, path string }{
			"notifier_test.gleam.tmpl", filepath.Join("test", p.Name, "platform", "notifier_test.gleam"),
		})
	}
	return renderAll(p, targets)
}

// Write adds mail and jobs when the project needs them, howdy_notifications
// to gleam.toml and APP_ORIGIN to .env.example, and creates the files. It
// returns the paths written or changed.
func (n *Notifications) Write() ([]string, error) {
	p := n.Project
	var changed []string
	add := func(paths []string) {
		for _, path := range paths {
			if !slices.Contains(changed, path) {
				changed = append(changed, path)
			}
		}
	}
	if n.Mail != nil {
		written, err := n.Mail.Write()
		add(written)
		if err != nil {
			return changed, err
		}
	}
	if n.Jobs != nil {
		written, err := n.Jobs.Write()
		add(written)
		if err != nil {
			return changed, err
		}
	}
	files, err := n.Files()
	if err != nil {
		return changed, err
	}
	line := n.Dependency("howdy_notifications") + "\n"
	written, err := installFiles(p, line, files)
	add(written)
	if err != nil {
		return changed, err
	}
	if appendOnce(filepath.Join(p.Dir, ".env.example"), "APP_ORIGIN", `
# Where people reach the site, for links in notification emails: scheme,
# host and port only.
# APP_ORIGIN=https://example.com
`) {
		add([]string{".env.example"})
	}
	return changed, nil
}

const runnerBuilt = "background_jobs.runner(services.db)\n"

// Register wires mail and jobs first when they were added, then runs the
// notifications' migrations after the app's, puts the notifier in the
// app's services wherever they are built, serves the inbox, and teaches
// the jobs runner to deliver them. For each file without the shape 'howdy
// new' gives it, the returned notes say what to add by hand.
func (n *Notifications) Register() (notes []string) {
	p := n.Project
	if n.Mail != nil {
		notes = append(notes, n.Mail.Register()...)
	}
	if n.Jobs != nil {
		notes = append(notes, n.Jobs.Register()...)
	}
	module := "import " + p.Name + "/platform/notifier"
	routes := "notifier.routes(services.notifier)"
	if p.Auth {
		routes = "notifier.routes(services.notifier, services.identity)"
	}

	root := filepath.Join("src", p.Name+".gleam")
	err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		text, err := addServicesField(text, "notifier: notifications.Notifier")
		if err != nil {
			return "", err
		}
		if text, err = addToServices(text, "notifier", "notifier.new(db, mailer)"); err != nil {
			return "", err
		}
		if !howdyNew.MatchString(text) {
			return "", errors.New("no howdy.new() in app")
		}
		text = howdyNew.ReplaceAllString(text, "${0}${1}|> howdy.controller("+routes+")\n")
		return addImports(text, "import howdy/notifications", module), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Add notifications to the app in %s: notifier: notifications.Notifier in Services, built in main with notifier.new(db, mailer), and |> howdy.controller(%s) after howdy.new() in app (import howdy/notifications and %s/platform/notifier).", root, routes, p.Name))
	}

	// Delivered by the jobs runner, wherever it is started.
	err = editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		at := strings.Index(text, runnerBuilt)
		if at < 0 {
			return "", errors.New("no jobs_runner building background_jobs.runner(services.db)")
		}
		at += len(runnerBuilt)
		return text[:at] + "  |> notifications.handle_jobs(services.notifier)\n" + text[at:], nil
	})
	if err != nil {
		notes = append(notes, "Teach the app's jobs runner to deliver notifications by email: add |> notifications.handle_jobs(notifier) to it, with the app's notifier.")
	}

	database := filepath.Join("src", p.Name, "platform", "database.gleam")
	err = editFile(filepath.Join(p.Dir, database), func(text string) (string, error) {
		return addPackageMigration(text, "notifier.migrate(db)", module)
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Run the notifications' migrations in %s: call notifier.migrate(db) after the app's own, before the app starts.", database))
	}

	dev := p.devFile()
	err = editFile(filepath.Join(p.Dir, dev), func(text string) (string, error) {
		text, err := addToServices(text, "notifier", "notifier.new(db, mailer)")
		if err != nil {
			return "", err
		}
		return addImports(text, module), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Give the dev server notifications in %s: pass notifier: notifier.new(db, mailer) to Services.", dev))
	}

	support := filepath.Join("test", p.Name, "support.gleam")
	if hasSupport(p) {
		err := editFile(filepath.Join(p.Dir, support), func(text string) (string, error) {
			text, err := addToServices(text, "notifier", "notifier.new(db, mailer)")
			if err != nil {
				return "", err
			}
			return addImports(text, module), nil
		})
		if err != nil {
			notes = append(notes, fmt.Sprintf("Give the tests notifications in %s: pass notifier: notifier.new(db, mailer) to Services.", support))
		}
	}
	return notes
}

// AgentNotes names the packages whose sections 'howdy add notifications'
// adds to AGENTS.md: notifications, and mail and jobs when they are added
// too.
func (n *Notifications) AgentNotes() []string {
	pkgs := []string{}
	if n.Mail != nil {
		pkgs = append(pkgs, "mail")
	}
	if n.Jobs != nil {
		pkgs = append(pkgs, "jobs")
	}
	return append(pkgs, "notifications")
}

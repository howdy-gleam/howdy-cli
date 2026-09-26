package generate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Auth adds howdy_auth's core to a project: sign-in by a link or code
// emailed to the address, registration, and roles. It needs the project's
// database, and adds mail first when the project has none. Other ways in
// are providers, added afterwards.
type Auth struct {
	Project *Project
	// Mail is set when the project needs mail first.
	Mail     *Mail
	Git, Ref string
	Admin    bool
}

// NewAuth checks that the project can take auth and does not have it yet.
func NewAuth(p *Project) (*Auth, error) {
	toml, err := os.ReadFile(filepath.Join(p.Dir, "gleam.toml"))
	if err != nil {
		return nil, err
	}
	switch {
	case dependency("howdy_auth").Match(toml):
		return nil, errors.New("this project already has auth: howdy_auth is in gleam.toml")
	case !dependency("howdy_database").Match(toml):
		return nil, errors.New("auth keeps its users and sessions in the app's database through howdy_database, which this project does not have; create a project with a database ('howdy new' without --no-database) to use auth")
	}
	m := howdyDependency.FindSubmatch(toml)
	if m == nil {
		return nil, errors.New("gleam.toml does not take howdy from Git as 'howdy new' writes it (howdy = { git = \"...\", ref = \"...\" }), so the matching howdy_auth is unknown; add howdy_auth by hand")
	}
	root := readText(filepath.Join(p.Dir, "src", p.Name+".gleam"))
	if !servicesType.MatchString(root) || !servicesMade.MatchString(root) {
		return nil, fmt.Errorf("src/%s.gleam was made before 'howdy new' built the app from a services record, which auth is added to; see the auth docs to add it by hand", p.Name)
	}
	a := &Auth{Project: p, Git: string(m[1]), Ref: string(m[2])}
	a.Admin = bytes.Contains([]byte(readText(filepath.Join(p.Dir, p.devFile()))), []byte("admin.mount("))
	if !dependency("howdy_mail").Match(toml) {
		if a.Mail, err = NewMail(p); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *Auth) accountsFile() string {
	return filepath.ToSlash(filepath.Join("src", a.Project.Name, "platform", "accounts.gleam"))
}

func (a *Auth) testFile() string {
	return filepath.ToSlash(filepath.Join("test", a.Project.Name, "platform", "accounts_test.gleam"))
}

// Write adds mail when the project needs it, howdy_auth to gleam.toml and
// APP_ORIGIN to .env.example, and creates platform/accounts.gleam and its
// test. It returns the paths written or changed.
func (a *Auth) Write() ([]string, error) {
	p := a.Project
	var changed []string
	if a.Mail != nil {
		written, err := a.Mail.Write()
		changed = append(changed, written...)
		if err != nil {
			return changed, err
		}
	}
	accounts, err := render("accounts.gleam.tmpl", p)
	if err != nil {
		return changed, err
	}
	files := []File{{Path: a.accountsFile(), Content: accounts}}
	if _, err := os.Stat(filepath.Join(p.Dir, "test", p.Name, "support.gleam")); err == nil {
		test, err := render("accounts_test.gleam.tmpl", p)
		if err != nil {
			return changed, err
		}
		files = append(files, File{Path: a.testFile(), Content: test})
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(p.Dir, f.Path)); err == nil {
			return changed, fmt.Errorf("%s already exists; auth was not added", f.Path)
		}
	}
	line := fmt.Sprintf("howdy_auth = { git = %q, ref = %q, path = \"auth\" }\n", a.Git, a.Ref)
	if err := insertDependencies(filepath.Join(p.Dir, "gleam.toml"), line); err != nil {
		return changed, err
	}
	if a.Mail == nil {
		changed = append(changed, "gleam.toml")
	}
	written, err := writeFiles(p.Dir, files)
	changed = append(changed, written...)
	if err != nil {
		return changed, err
	}
	if appendOnce(filepath.Join(p.Dir, ".env.example"), "APP_ORIGIN", `
# Where people reach the site, for sign-in links, cookies and origin checks:
# scheme, host and port only, and HTTPS anywhere but this machine.
# APP_ORIGIN=https://example.com
`) && a.Mail == nil {
		changed = append(changed, ".env.example")
	}
	return changed, nil
}

var howdyNew = regexp.MustCompile(`(?m)^(\s*)howdy\.new\(\)\n`)

// Register wires auth in: mail first when it was added, auth's migrations
// after the app's, the identity and permissions in the app's services
// wherever they are built, the routes, and with the dev admin, its users,
// roles and auth's email previews. For each file without the shape
// 'howdy new' gives it, the returned notes say what to add by hand.
func (a *Auth) Register() (notes []string) {
	p := a.Project
	if a.Mail != nil {
		notes = append(notes, a.Mail.Register()...)
	}
	accounts := "import " + p.Name + "/platform/accounts"
	build := func(text string) (string, error) {
		text, err := addToServices(text, "identity", "accounts.identity(db, mailer)")
		if err != nil {
			return "", err
		}
		return addToServices(text, "permissions", "accounts.permissions(db)")
	}

	root := filepath.Join("src", p.Name+".gleam")
	err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		text, err := addServicesField(text, "identity: auth.Auth")
		if err != nil {
			return "", err
		}
		if text, err = addServicesField(text, "permissions: access.Authorization"); err != nil {
			return "", err
		}
		if text, err = build(text); err != nil {
			return "", err
		}
		if !howdyNew.MatchString(text) {
			return "", errors.New("no howdy.new() in app")
		}
		// The routes go first, ahead of the static catch-all. An API
		// project gets the JSON API without the pages.
		routes := "${0}${1}|> howdy.controller(routes.api(services.identity, at: \"/api/auth\"))\n"
		imports := []string{"import howdy/auth", "import howdy/auth/routes", "import howdy/authorization as access", accounts}
		if !p.API {
			routes += "${1}|> howdy.controller(pages.routes(services.identity, at: \"/auth\", api_at: \"/api/auth\"))\n"
			imports = append(imports, "import howdy/auth/pages")
		}
		text = howdyNew.ReplaceAllString(text, routes)
		return addImports(text, imports...), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Add auth to the app in %s: identity: auth.Auth and permissions: access.Authorization in Services, built in main with accounts.identity(db, mailer) and accounts.permissions(db), and in app |> howdy.controller(routes.api(services.identity, at: \"/api/auth\")) and |> howdy.controller(pages.routes(services.identity, at: \"/auth\", api_at: \"/api/auth\")).", root))
	}

	database := filepath.Join("src", p.Name, "platform", "database.gleam")
	err = editFile(filepath.Join(p.Dir, database), func(text string) (string, error) {
		return addPackageMigration(text, "accounts.migrate(db)", accounts)
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Run auth's migrations in %s: call accounts.migrate(db) after the app's own, before the app starts.", database))
	}

	dev := p.devFile()
	err = editFile(filepath.Join(p.Dir, dev), func(text string) (string, error) {
		text, err := build(text)
		if err != nil {
			return "", err
		}
		if a.Admin {
			const anchor = "|> admin.telemetry(recorder)"
			if !strings.Contains(text, anchor) {
				return "", errors.New("no admin.telemetry to follow")
			}
			text = strings.Replace(text, anchor, anchor+"\n|> admin.auth(identity)\n|> admin.authorization(permissions)\n|> admin.mail_previews(accounts.previews(mailer), send_with: mailer)", 1)
		}
		return addImports(text, accounts), nil
	})
	if err != nil {
		admin := ""
		if a.Admin {
			admin = ", and add |> admin.auth(identity) |> admin.authorization(permissions) |> admin.mail_previews(accounts.previews(mailer), send_with: mailer) to the admin"
		}
		notes = append(notes, fmt.Sprintf("Give the dev server auth in %s: pass identity: accounts.identity(db, mailer) and permissions: accounts.permissions(db) to Services%s.", dev, admin))
	}

	support := filepath.Join("test", p.Name, "support.gleam")
	if _, err := os.Stat(filepath.Join(p.Dir, support)); err == nil {
		err := editFile(filepath.Join(p.Dir, support), func(text string) (string, error) {
			text, err := build(text)
			if err != nil {
				return "", err
			}
			return addImports(text, accounts), nil
		})
		if err != nil {
			notes = append(notes, fmt.Sprintf("Give the tests auth in %s: pass identity: accounts.identity(db, mailer) and permissions: accounts.permissions(db) to Services.", support))
		}
	}
	return notes
}

const (
	appMigrate     = "pub fn migrate(db: Repo) -> Result(Int, String) {"
	appMigrateOnly = "fn migrate_app(db: Repo) -> Result(Int, String) {"
)

// addPackageMigration makes `migrate` in platform/database.gleam run a Howdy
// package's migrations, through call, after the app's own. The first package
// renames the app's `migrate` to `migrate_app` and wraps it; later ones join
// the wrapper.
func addPackageMigration(text, call string, imports ...string) (string, error) {
	step := "  use _ <- result.try(" + call + ")\n"
	if strings.Contains(text, appMigrateOnly) {
		start := strings.Index(text, appMigrate)
		if start < 0 {
			return "", errors.New("no migrate function")
		}
		end := strings.Index(text[start:], "\n  Ok(applied)\n")
		if end < 0 {
			return "", errors.New("migrate does not end with Ok(applied)")
		}
		at := start + end + 1
		text = text[:at] + step + text[at:]
		return addImports(text, imports...), nil
	}
	if !strings.Contains(text, appMigrate) {
		return "", errors.New("no migrate function")
	}
	text = strings.Replace(text, appMigrate, appMigrateOnly, 1)
	text = strings.TrimRight(text, "\n") + `

/// Apply the app's migrations, then those of the Howdy packages it uses,
/// which howdy/migration keeps. The app, the dev server, the tests and
/// ` + "`gleam run -m tasks/migrate`" + ` all migrate through here.
pub fn migrate(db: Repo) -> Result(Int, String) {
  use applied <- result.try(migrate_app(db))
` + step + `  Ok(applied)
}
`
	return addImports(text, append([]string{"import gleam/result"}, imports...)...), nil
}

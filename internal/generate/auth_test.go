package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

func addAuth(t *testing.T, p *Project) *Auth {
	t.Helper()
	a, err := NewAuth(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := a.Register(); len(notes) > 0 {
		t.Fatalf("wiring needs a hand: %v", notes)
	}
	return a
}

func TestAddAuth(t *testing.T) {
	for _, c := range []struct {
		db       scaffold.Database
		minimal  bool
		withMail bool
	}{
		{scaffold.SQLite, false, true},
		{scaffold.SQLite, false, false},
		{scaffold.Postgres, false, false},
		{scaffold.SQLite, true, false},
	} {
		p := scaffolded(t, c.db, c.minimal)
		if c.withMail {
			addMail(t, p)
		}
		a := addAuth(t, p)
		label := c.db.Label()
		if (a.Mail == nil) != c.withMail {
			t.Errorf("%s: mail should be added exactly when the project has none", label)
		}
		for path, wants := range map[string][]string{
			"gleam.toml": {`howdy_auth = { git = "https://github.com/mikeyjones/howdy.git", ref = "v` + scaffold.Release + `", path = "auth" }`, "howdy_mail = "},
			"src/demo_site.gleam": {
				"mailer: mail.Mailer, identity: auth.Auth, permissions: access.Authorization)",
				"let identity = accounts.identity(db, mailer)\n  let permissions = accounts.permissions(db)\n  let services = Services(db:, mailer:, identity:, permissions:)",
				`|> howdy.controller(routes.api(services.identity, at: "/api/auth"))`,
				"import demo_site/platform/accounts\n",
			},
			"src/demo_site/platform/database.gleam":       {"fn migrate_app(db: Repo)", "use _ <- result.try(accounts.migrate(db))"},
			"src/demo_site/platform/accounts.gleam":       {"auth.new(", "auth.allow_registration", `emails.new(mailer, app_name: "Demo site")`},
			"dev/demo_site_dev.gleam":                     {"let identity = accounts.identity(db, mailer)", "identity:, permissions:)"},
			"test/demo_site/support.gleam":                {"let identity = accounts.identity(db, mailer)"},
			"test/demo_site/platform/accounts_test.gleam": {`testing.get("/auth/login")`},
			".env.example":                                {"APP_ORIGIN"},
		} {
			content := read(t, p, path)
			for _, want := range wants {
				if !strings.Contains(content, want) {
					t.Errorf("%s: %s should contain %q:\n%s", label, path, want, content)
				}
			}
		}
		dev := read(t, p, "dev/demo_site_dev.gleam")
		if strings.Contains(dev, "admin.auth(identity)") == c.minimal {
			t.Errorf("%s: the admin should get users and roles exactly when it is installed", label)
		}
		// The app's own migrations still register where they did.
		slice, err := NewMigration(p, "notes", []string{"title:string"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := slice.Write(); err != nil {
			t.Fatal(err)
		}
		if notes := slice.Register(); len(notes) > 0 {
			t.Errorf("%s: a migration after auth needs a hand: %v", label, notes)
		}
		if _, err := NewAuth(p); err == nil || !strings.Contains(err.Error(), "already has auth") {
			t.Errorf("%s: auth should not be added twice, got %v", label, err)
		}
	}
}

func TestAddAuthNeedsDatabase(t *testing.T) {
	p := scaffolded(t, scaffold.NoDatabase, false)
	if _, err := NewAuth(p); err == nil || !strings.Contains(err.Error(), "howdy_database") {
		t.Errorf("got %v", err)
	}
}

func TestAddAuthNeedsServices(t *testing.T) {
	p := scaffolded(t, scaffold.SQLite, false)
	root := filepath.Join(p.Dir, "src", "demo_site.gleam")
	src, _ := os.ReadFile(root)
	os.WriteFile(root, []byte(strings.Replace(string(src), "  let services = Services(db:)\n", "", 1)), 0o644)
	if _, err := NewAuth(p); err == nil || !strings.Contains(err.Error(), "services record") {
		t.Errorf("got %v", err)
	}
}

package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

func scaffolded(t *testing.T, database scaffold.Database, minimal bool) *Project {
	t.Helper()
	s, err := scaffold.New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	s.Database, s.Minimal = database, minimal
	// These tests check the Git dependency lines; see TestAddToHexProject.
	s.Source = scaffold.Git
	if _, err := s.Write(nil); err != nil {
		t.Fatal(err)
	}
	p, err := Open(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func addMail(t *testing.T, p *Project) *Mail {
	t.Helper()
	m, err := NewMail(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := m.Register(); len(notes) > 0 {
		t.Fatalf("wiring needs a hand: %v", notes)
	}
	return m
}

func TestAddMail(t *testing.T) {
	for _, c := range []struct {
		db      scaffold.Database
		minimal bool
		fields  string
	}{
		{scaffold.SQLite, false, "Services(db: Repo, mailer: mail.Mailer)"},
		{scaffold.Postgres, false, "Services(db: Repo, mailer: mail.Mailer)"},
		{scaffold.NoDatabase, false, "Services(mailer: mail.Mailer)"},
		{scaffold.SQLite, true, "Services(db: Repo, mailer: mail.Mailer)"},
		{scaffold.NoDatabase, true, "Services(mailer: mail.Mailer)"},
	} {
		p := scaffolded(t, c.db, c.minimal)
		m := addMail(t, p)
		label := c.db.Label()
		if c.minimal {
			label += " minimal"
		}
		if m.Admin == c.minimal {
			t.Errorf("%s: Admin = %v", label, m.Admin)
		}
		for path, wants := range map[string][]string{
			"gleam.toml": {
				`howdy_mail = { git = "https://github.com/howdy-gleam/howdy.git", ref = "v` + scaffold.Release + `", path = "mail" }` + "\n" +
					`smail = ">= 2.0.1 and < 3.0.0"` + "\n\n[dev_dependencies]",
			},
			"src/demo_site.gleam":                       {c.fields, "let mailer = email.mailer()\n", "import howdy/mail\n", "import demo_site/platform/email\n"},
			"dev/demo_site_dev.gleam":                   {`let assert Ok(box) = outbox.start_in("tmp/mail")`, "mailer:)", "import howdy/mail/outbox\n"},
			"test/demo_site/support.gleam":              {"mail.mailer(outbox.adapter(outbox.start()))", "mailer:)"},
			"src/demo_site/platform/email.gleam":        {"pub fn mailer() -> mail.Mailer", `"MAIL_FROM", "Demo site <hello@example.com>"`},
			"src/demo_site/emails/welcome_email.gleam":  {`mail.subject("Welcome to Demo site")`, "pub fn preview() -> Preview"},
			"src/demo_site/emails/previews.gleam":       {"[welcome_email.preview()]"},
			"test/demo_site/emails/previews_test.gleam": {"mail.prepare(mailer, message)"},
			".gitignore":   {"tmp/mail/"},
			".env.example": {"MAIL_FROM=Demo site <hello@example.com>"},
		} {
			content := read(t, p, path)
			for _, want := range wants {
				if !strings.Contains(content, want) {
					t.Errorf("%s: %s should contain %q:\n%s", label, path, want, content)
				}
			}
		}
		dev := read(t, p, "dev/demo_site_dev.gleam")
		if strings.Contains(dev, "admin.mail(box)") == c.minimal {
			t.Errorf("%s: the admin should show mail exactly when it is installed:\n%s", label, dev)
		}
		if _, err := NewMail(p); err == nil || !strings.Contains(err.Error(), "already has mail") {
			t.Errorf("%s: mail should not be added twice, got %v", label, err)
		}
	}
}

// A project whose files no longer have the shape 'howdy new' gave them
// gets notes, not broken edits.
func TestAddMailLeavesUnknownShapes(t *testing.T) {
	p := scaffolded(t, scaffold.SQLite, false)
	root := filepath.Join(p.Dir, "src", "demo_site.gleam")
	os.WriteFile(root, []byte("// mine\n"), 0o644)
	m, err := NewMail(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Write(); err != nil {
		t.Fatal(err)
	}
	notes := m.Register()
	if len(notes) != 1 || !strings.Contains(notes[0], "add mailer: mail.Mailer to the Services type") {
		t.Errorf("notes = %v", notes)
	}
	if got, _ := os.ReadFile(root); string(got) != "// mine\n" {
		t.Error("an unknown file should be left alone")
	}
}

func TestAddMailNeedsGitHowdy(t *testing.T) {
	p := scaffolded(t, scaffold.NoDatabase, true)
	toml := filepath.Join(p.Dir, "gleam.toml")
	src, _ := os.ReadFile(toml)
	os.WriteFile(toml, []byte(strings.Replace(string(src), `howdy = { git = "https://github.com/howdy-gleam/howdy.git", ref = "v`+scaffold.Release+`" }`, `howdy = { path = "../howdy" }`, 1)), 0o644)
	if _, err := NewMail(p); err == nil || !strings.Contains(err.Error(), "add howdy_mail by hand") {
		t.Errorf("got %v", err)
	}
}

func TestCreateEmail(t *testing.T) {
	p := scaffolded(t, scaffold.NoDatabase, false)
	if _, err := NewEmail(p, "invoice"); err == nil || !strings.Contains(err.Error(), "howdy add mail") {
		t.Errorf("an email needs mail first, got %v", err)
	}
	addMail(t, p)
	for _, name := range []string{"password_reset_email", "invoice"} {
		e, err := NewEmail(p, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Write(); err != nil {
			t.Fatal(err)
		}
		if notes := e.Register(); len(notes) > 0 {
			t.Fatal(notes)
		}
	}
	previews := read(t, p, "src/demo_site/emails/previews.gleam")
	if !strings.Contains(previews, "[welcome_email.preview(), password_reset_email.preview(), invoice_email.preview()]") ||
		!strings.Contains(previews, "import demo_site/emails/invoice_email\n") {
		t.Errorf("previews.gleam:\n%s", previews)
	}
	email := read(t, p, "src/demo_site/emails/password_reset_email.gleam")
	for _, want := range []string{`mail.subject("Password reset")`, `preview.new("Password reset"`, `mail.tag("password_reset")`} {
		if !strings.Contains(email, want) {
			t.Errorf("password_reset_email.gleam should contain %q", want)
		}
	}
	if _, err := NewEmail(p, "invoice"); err == nil || !strings.Contains(err.Error(), "the invoice email already exists") {
		t.Errorf("got %v", err)
	}
}

// A project made before the services record, like one from an earlier
// 'howdy new': the admin still gets the outbox and previews, and the notes
// say the app has no mailer yet.
func TestAddMailBeforeServices(t *testing.T) {
	p := scaffolded(t, scaffold.SQLite, false)
	dev := filepath.Join(p.Dir, "dev", "demo_site_dev.gleam")
	src, _ := os.ReadFile(dev)
	old := strings.Replace(string(src), "  let services = demo_site.Services(db:)\n", "", 1)
	old = strings.ReplaceAll(old, "demo_site.expose(services)", "demo_site.expose(db)")
	old = strings.ReplaceAll(old, "demo_site.configured(services)", "demo_site.configured(db)")
	os.WriteFile(dev, []byte(old), 0o644)
	root := filepath.Join(p.Dir, "src", "demo_site.gleam")
	src, _ = os.ReadFile(root)
	os.WriteFile(root, []byte(strings.Replace(string(src), "  let services = Services(db:)\n", "", 1)), 0o644)
	before, _ := os.ReadFile(root)

	m, err := NewMail(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Write(); err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(m.Register(), "\n")
	wired := read(t, p, "dev/demo_site_dev.gleam")
	for _, want := range []string{
		"let assert Ok(box) = outbox.start_in(\"tmp/mail\")\n  let mailer = mail.mailer(outbox.adapter(box))",
		"|> admin.mail(box)", "|> admin.mail_previews(previews.all(), send_with: mailer)",
		"import demo_site/emails/previews\n",
	} {
		if !strings.Contains(wired, want) {
			t.Errorf("the dev file should contain %q:\n%s", want, wired)
		}
	}
	if strings.Index(wired, "outbox.start_in") > strings.Index(wired, "let dashboard =") {
		t.Error("the outbox should come before the dashboard that uses it")
	}
	if after, _ := os.ReadFile(root); string(after) != string(before) {
		t.Error("a root without a services line should be left alone")
	}
	for _, want := range []string{"made before 'howdy new' built the app from a services record", "shows under Mail"} {
		if !strings.Contains(notes, want) {
			t.Errorf("the notes should say %q:\n%s", want, notes)
		}
	}
}

package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

// installer is a package generator as 'howdy add' drives it.
type installer interface {
	Write() ([]string, error)
	Register() []string
}

func install(t *testing.T, i installer, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := i.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := i.Register(); len(notes) > 0 {
		t.Fatalf("wiring needs a hand: %v", notes)
	}
}

func addJobs(t *testing.T, p *Project) {
	t.Helper()
	j, err := NewJobs(p)
	install(t, j, err)
}

func addNotifications(t *testing.T, p *Project) *Notifications {
	t.Helper()
	n, err := NewNotifications(p)
	install(t, n, err)
	return n
}

// containsAll checks each file holds each of its strings.
func containsAll(t *testing.T, label string, p *Project, files map[string][]string) {
	t.Helper()
	for path, wants := range files {
		content := read(t, p, path)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Errorf("%s: %s should contain %q:\n%s", label, path, want, content)
			}
		}
	}
}

func TestAddStorage(t *testing.T) {
	for _, c := range []struct {
		label string
		p     func(t *testing.T) *Project
	}{
		{"sqlite", func(t *testing.T) *Project { return scaffolded(t, scaffold.SQLite, false) }},
		{"postgres", func(t *testing.T) *Project { return scaffolded(t, scaffold.Postgres, false) }},
		{"no database", func(t *testing.T) *Project { return scaffolded(t, scaffold.NoDatabase, true) }},
		{"api", func(t *testing.T) *Project { return scaffoldedAPI(t, scaffold.SQLite) }},
	} {
		p := c.p(t)
		s, err := NewStorage(p)
		install(t, s, err)
		containsAll(t, c.label, p, map[string][]string{
			"gleam.toml": {`howdy_storage = { git = "https://github.com/mikeyjones/howdy.git", ref = "v` + scaffold.Release + `", path = "storage" }`},
			"src/demo_site.gleam": {
				"files: storage.Storage",
				"let files = file_storage.from_env()\n",
				"howdy.new()\n  |> file_storage.serve\n",
				"import howdy/storage\n",
				"import demo_site/platform/file_storage\n",
			},
			"src/demo_site/platform/file_storage.gleam":       {"s3.from_env()", "local.serve(config, at: \"/files\", secret:)"},
			"dev/demo_site_dev.gleam":                         {"let files = file_storage.from_env()\n", "files"},
			"test/demo_site/support.gleam":                    {"let files = memory.new()\n", "import howdy/storage/memory\n"},
			"test/demo_site/platform/file_storage_test.gleam": {"memory.new()"},
			".gitignore":   {"/storage/"},
			".env.example": {"STORAGE_SECRET", "S3_BUCKET"},
		})
		if _, err := NewStorage(p); err == nil || !strings.Contains(err.Error(), "already has file storage") {
			t.Errorf("%s: storage should not be added twice, got %v", c.label, err)
		}
	}
}

func TestAddJobs(t *testing.T) {
	for _, c := range []struct {
		db      scaffold.Database
		minimal bool
	}{
		{scaffold.SQLite, false},
		{scaffold.Postgres, false},
		{scaffold.SQLite, true},
	} {
		p := scaffolded(t, c.db, c.minimal)
		addJobs(t, p)
		label := c.db.Label()
		containsAll(t, label, p, map[string][]string{
			"gleam.toml": {`howdy_jobs = { git = "https://github.com/mikeyjones/howdy.git", ref = "v` + scaffold.Release + `", path = "jobs" }`},
			"src/demo_site.gleam": {
				"pub fn jobs_runner(services: Services) -> jobs.Runner {\n  background_jobs.runner(services.db)\n}",
				"|> supervisor.add(jobs.supervised(jobs_runner(services)))\n    |> supervisor.add(howdy.supervised(configured(services)))",
				"import howdy/jobs\n",
				"import demo_site/background_jobs\n",
			},
			"src/demo_site/platform/database.gleam":     {"fn migrate_app(db: Repo)", "use _ <- result.try(background_jobs.migrate(db))"},
			"src/demo_site/background_jobs.gleam":       {`jobs.define("say_hello"`, "jobs.schema()"},
			"src/tasks/jobs.gleam":                      {"cli.main(fn() { Ok(demo_site.database()) })"},
			"dev/demo_site_dev.gleam":                   {"let runner = demo_site.jobs_runner(services)\n  let assert Ok(_) = jobs.start(runner)\n"},
			"test/demo_site/background_jobs_test.gleam": {"jobs.drain(background_jobs.runner(db))"},
		})
		dev := read(t, p, "dev/demo_site_dev.gleam")
		if strings.Contains(dev, "admin.jobs(runner)") == c.minimal {
			t.Errorf("%s: the admin should show the jobs exactly when it is installed", label)
		}
		if _, err := NewJobs(p); err == nil || !strings.Contains(err.Error(), "already has background jobs") {
			t.Errorf("%s: jobs should not be added twice, got %v", label, err)
		}
	}
}

func TestAddJobsNeedsDatabase(t *testing.T) {
	p := scaffolded(t, scaffold.NoDatabase, false)
	if _, err := NewJobs(p); err == nil || !strings.Contains(err.Error(), "howdy_database") {
		t.Errorf("got %v", err)
	}
	if _, err := NewNotifications(p); err == nil || !strings.Contains(err.Error(), "howdy_database") {
		t.Errorf("got %v", err)
	}
}

// Notifications bring mail and jobs when the project has neither, and the
// jobs runner delivers them.
func TestAddNotifications(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		p := scaffolded(t, scaffold.SQLite, minimal)
		n := addNotifications(t, p)
		if n.Mail == nil || n.Jobs == nil {
			t.Fatalf("minimal %v: mail and jobs should be added too", minimal)
		}
		if got := strings.Join(n.AgentNotes(), ","); got != "mail,jobs,notifications" {
			t.Errorf("agent notes = %s", got)
		}
		label := "notifications"
		containsAll(t, label, p, map[string][]string{
			"gleam.toml": {"howdy_mail = ", "howdy_jobs = ", `path = "notifications" }`},
			"src/demo_site.gleam": {
				"notifier: notifications.Notifier",
				"let mailer = email.mailer()\n  let notifier = notifier.new(db, mailer)\n",
				"howdy.new()\n  |> howdy.controller(notifier.routes(services.notifier))\n",
				"background_jobs.runner(services.db)\n  |> notifications.handle_jobs(services.notifier)\n",
			},
			"src/demo_site/platform/database.gleam": {
				"use _ <- result.try(background_jobs.migrate(db))\n  use _ <- result.try(notifier.migrate(db))\n",
			},
			"src/demo_site/platform/notifier.gleam":       {"Error(service.Unauthorized)", "notifications.schema()"},
			"dev/demo_site_dev.gleam":                     {"let notifier = notifier.new(db, mailer)\n", "jobs_runner(services)"},
			"test/demo_site/support.gleam":                {"let notifier = notifier.new(db, mailer)\n"},
			"test/demo_site/platform/notifier_test.gleam": {"notifications.unread_count"},
			".env.example":                                {"APP_ORIGIN"},
		})
		if _, err := NewNotifications(p); err == nil || !strings.Contains(err.Error(), "already has notifications") {
			t.Errorf("notifications should not be added twice, got %v", err)
		}
	}
}

// With jobs and auth already there, notifications use them: the runner
// delivers them, and auth says whose inbox a request is for.
func TestAddNotificationsToJobsAndAuth(t *testing.T) {
	p := scaffolded(t, scaffold.SQLite, false)
	addAuth(t, p)
	addJobs(t, p)
	n := addNotifications(t, p)
	if n.Mail != nil || n.Jobs != nil {
		t.Fatal("mail and jobs are there already")
	}
	containsAll(t, "auth", p, map[string][]string{
		"src/demo_site.gleam":                   {"notifier.routes(services.notifier, services.identity)", "|> notifications.handle_jobs(services.notifier)"},
		"src/demo_site/platform/notifier.gleam": {"auth.required(identity)(ctx)"},
	})
	root := read(t, p, "src/demo_site.gleam")
	if strings.Count(root, "jobs.supervised(") != 1 || strings.Count(root, "pub fn jobs_runner(") != 1 {
		t.Errorf("the jobs should start once:\n%s", root)
	}
}

func TestAddI18n(t *testing.T) {
	for _, p := range []*Project{
		scaffolded(t, scaffold.SQLite, false),
		scaffolded(t, scaffold.NoDatabase, true),
		scaffoldedAPI(t, scaffold.NoDatabase),
	} {
		i, err := NewI18n(p)
		install(t, i, err)
		label := "i18n"
		if p.API {
			label = "i18n api"
		}
		containsAll(t, label, p, map[string][]string{
			"gleam.toml":                           {`path = "i18n" }`, "gleam_time = "},
			"locales/en.json":                      {`"@@locale": "en"`, `"greeting": "Hello, {name}!"`},
			"src/demo_site/messages.gleam":         {"pub const locales = [\"en\"]", "pub fn greeting("},
			"src/demo_site/platform/locales.gleam": {"locale.negotiator(messages.locales, default: messages.source)", "import demo_site/messages\n"},
			"src/demo_site.gleam": {
				"howdy.new()\n  |> howdy.middleware(locale.middleware(locales.negotiator()))\n",
				"import howdy/locale\n",
				"import demo_site/platform/locales\n",
			},
			"dev/demo_site_dev.gleam":            {"|> dev.watch(\"locales\")\n", "cli.execute([\"gen\"])", "import howdy/i18n/cli\n"},
			"test/demo_site/messages_test.gleam": {"messages.greeting(t, name: \"Ada\")"},
		})
		if strings.Count(read(t, p, "gleam.toml"), "gleam_time") != 1 {
			t.Errorf("%s: gleam_time should be a dependency once", label)
		}
		if _, err := NewI18n(p); err == nil || !strings.Contains(err.Error(), "already has translations") {
			t.Errorf("%s: i18n should not be added twice, got %v", label, err)
		}
	}
}

func TestAddToHexProject(t *testing.T) {
	s, err := scaffold.New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	s.Source = scaffold.Hex
	if _, err := s.Write(nil); err != nil {
		t.Fatal(err)
	}
	p, err := Open(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	addJobs(t, p)
	a, err := NewAuth(p)
	install(t, a, err)
	toml := read(t, p, "gleam.toml")
	for _, want := range []string{
		`howdy_jobs = ">= 1.0.0 and < 2.0.0"`, `howdy_auth = ">= 1.0.0 and < 2.0.0"`, `howdy_mail = ">= 1.0.0 and < 2.0.0"`,
	} {
		if !strings.Contains(toml, want) {
			t.Errorf("gleam.toml should contain %s:\n%s", want, toml)
		}
	}
	if strings.Contains(toml, "git = ") {
		t.Errorf("a project on Hex should take no package from Git:\n%s", toml)
	}

	// A release this howdy does not know the package versions of.
	path := filepath.Join(p.Dir, "gleam.toml")
	if err := os.WriteFile(path, []byte(strings.Replace(toml, `howdy = ">= 1.0.0`, `howdy = ">= 9.0.0`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStorage(p); err == nil || !strings.Contains(err.Error(), "howdy 9.0.0 from Hex") {
		t.Errorf("storage on an unknown Hex release: %v", err)
	}
}

// A project made before 'howdy new' put the server in a supervision tree
// starts it with howdy.start, and gets the processes the packages add
// started beside it in the same way.
func TestAddToUnsupervisedMain(t *testing.T) {
	p := scaffolded(t, scaffold.SQLite, false)
	root := filepath.Join(p.Dir, "src", "demo_site.gleam")
	text := read(t, p, "src/demo_site.gleam")
	tree := regexp.MustCompile(`(?s)  // The server runs in a supervision tree.*?howdy\.start_application\([^)]*\)\n`)
	if !tree.MatchString(text) {
		t.Fatalf("main has no supervision tree:\n%s", text)
	}
	text = tree.ReplaceAllString(text, "  let assert Ok(_) = configured(services) |> howdy.start\n")
	os.WriteFile(root, []byte(text), 0o644)
	toml := filepath.Join(p.Dir, "gleam.toml")
	before, _ := os.ReadFile(toml)
	os.WriteFile(toml, regexp.MustCompile(`(?m)^gleam_otp = .*\n`).ReplaceAll(before, nil), 0o644)

	addJobs(t, p)
	addFlags(t, p)
	addRemote(t, p)
	containsAll(t, "howdy.start", p, map[string][]string{
		"gleam.toml": {`gleam_otp = ">= 1.2.0 and < 2.0.0"`},
		"src/demo_site.gleam": {
			"let assert Ok(_) = jobs.start(jobs_runner(services))\n  let assert Ok(_) = configured(services) |> howdy.start",
			"let features = feature_flags.start(db)\n",
			"rpc.start()\n  let rpc_token = rpc.token()\n",
		},
	})
	text = read(t, p, "src/demo_site.gleam")
	if strings.Contains(text, "supervisor.") || strings.Contains(text, ".supervised(") {
		t.Errorf("main without a supervision tree should not get children for one:\n%s", text)
	}
	if got := read(t, p, "gleam.toml"); strings.Count(got, "gleam_otp") != 1 {
		t.Errorf("gleam_otp should be added once:\n%s", got)
	}
}

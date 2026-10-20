package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

func addRemote(t *testing.T, p *Project) *Remote {
	t.Helper()
	r, err := NewRemote(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := r.Register(); len(notes) > 0 {
		t.Fatalf("wiring needs a hand: %v", notes)
	}
	return r
}

func TestAddRemote(t *testing.T) {
	for _, c := range []struct {
		db      scaffold.Database
		minimal bool
	}{
		{scaffold.SQLite, false},
		{scaffold.Postgres, false},
		{scaffold.NoDatabase, false},
		{scaffold.NoDatabase, true},
	} {
		p := scaffolded(t, c.db, c.minimal)
		addRemote(t, p)
		label := c.db.Label()
		services := "Services(db: Repo, rpc_token: String)"
		made := "let services = Services(db:, rpc_token:)"
		test := "use db <- support.with_db()"
		if c.db == scaffold.NoDatabase {
			services, made, test = "Services(rpc_token: String)", "let services = Services(rpc_token:)", "support.app()"
		}
		for path, wants := range map[string][]string{
			"gleam.toml": {`howdy_remote = { git = "https://github.com/howdy-gleam/howdy.git", ref = "v` + scaffold.Release + `", path = "remote" }`},
			"src/demo_site.gleam": {
				services,
				"pub fn app(services: Services)",
				"howdy.new()\n  |> howdy.controller(rpc.controller(services.rpc_token))\n",
				"let rpc_token = rpc.token()\n  " + made,
				"|> supervisor.add(rpc.supervised())\n    |> supervisor.add(howdy.supervised(configured(services)))",
				"import demo_site/platform/rpc\n",
			},
			"src/demo_site/procedures.gleam":         {`"demo_site.ping"`},
			"src/demo_site/platform/rpc.gleam":       {"remote.handle(procedures.ping()", `remote.controller(server(), at: "/rpc", token:)`},
			"dev/demo_site_dev.gleam":                {"rpc.start()\n  let rpc_token = rpc.token()\n", "import demo_site/platform/rpc\n"},
			"test/demo_site/support.gleam":           {`let rpc_token = "test-rpc-token"`},
			"test/demo_site/platform/rpc_test.gleam": {"Bearer test-rpc-token", test},
			".env.example":                           {"# RPC_TOKEN="},
		} {
			content := read(t, p, path)
			for _, want := range wants {
				if !strings.Contains(content, want) {
					t.Errorf("%s: %s should contain %q:\n%s", label, path, want, content)
				}
			}
		}
		if root := read(t, p, "src/demo_site.gleam"); strings.Contains(root, "rpc.start()") {
			t.Errorf("%s: main should supervise the procedure server, not start it:\n%s", label, root)
		}
		if root := read(t, p, "src/demo_site.gleam"); strings.Contains(root, "_services") {
			t.Errorf("%s: app should use its services now:\n%s", label, root)
		}
		if _, err := NewRemote(p); err == nil || !strings.Contains(err.Error(), "already has remote") {
			t.Errorf("%s: remote should not be added twice, got %v", label, err)
		}
	}
}

func TestAddRemoteWithOtherPackages(t *testing.T) {
	// Either way round, every package ends up in the services.
	before := scaffolded(t, scaffold.SQLite, false)
	addRemote(t, before)
	addAuth(t, before)
	after := scaffolded(t, scaffold.SQLite, false)
	addAuth(t, after)
	addRemote(t, after)
	for _, p := range []*Project{before, after} {
		root := read(t, p, "src/demo_site.gleam")
		for _, want := range []string{"rpc_token: String", "identity: auth.Auth", "mailer: mail.Mailer", "rpc.controller(services.rpc_token)", `routes.api(services.identity, at: "/api/auth")`} {
			if !strings.Contains(root, want) {
				t.Errorf("src/demo_site.gleam should contain %q:\n%s", want, root)
			}
		}
		if support := read(t, p, "test/demo_site/support.gleam"); !strings.Contains(support, "rpc_token:") || !strings.Contains(support, "identity:") {
			t.Errorf("the test support should build every service:\n%s", support)
		}
	}
}

func TestAddRemoteNeedsServices(t *testing.T) {
	p := scaffolded(t, scaffold.SQLite, false)
	root := filepath.Join(p.Dir, "src", "demo_site.gleam")
	src, _ := os.ReadFile(root)
	os.WriteFile(root, []byte(strings.Replace(string(src), "  let services = Services(db:)\n", "", 1)), 0o644)
	if _, err := NewRemote(p); err == nil || !strings.Contains(err.Error(), "services record") {
		t.Errorf("got %v", err)
	}
}

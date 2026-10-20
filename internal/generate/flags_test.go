package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

func addFlags(t *testing.T, p *Project) *Flags {
	t.Helper()
	f, err := NewFlags(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := f.Register(); len(notes) > 0 {
		t.Fatalf("wiring needs a hand: %v", notes)
	}
	return f
}

func TestAddFlags(t *testing.T) {
	for _, c := range []struct {
		db      scaffold.Database
		minimal bool
	}{
		{scaffold.SQLite, false},
		{scaffold.Postgres, false},
		{scaffold.SQLite, true},
	} {
		p := scaffolded(t, c.db, c.minimal)
		addFlags(t, p)
		label := c.db.Label()
		for path, wants := range map[string][]string{
			"gleam.toml": {`howdy_flags = { git = "https://github.com/mikeyjones/howdy.git", ref = "v` + scaffold.Release + `", path = "flags" }`},
			"src/demo_site.gleam": {
				"Services(db: Repo, features: flags.Flags)",
				"let #(features, flag_keeper) = feature_flags.supervised(db)\n  let services = Services(db:, features:)",
				"|> supervisor.add(flag_keeper)\n    |> supervisor.add(howdy.supervised(configured(services)))",
				"import howdy/flags\n",
				"import demo_site/feature_flags\n",
			},
			"src/demo_site/platform/database.gleam":   {"fn migrate_app(db: Repo)", "use _ <- result.try(feature_flags.migrate(db))"},
			"src/demo_site/feature_flags.gleam":       {`flags.flag("early_access"`, "flags.register(all())", "|> flags.supervised", "flags_database.schema()"},
			"src/tasks/flags.gleam":                   {"cli.main(feature_flags.all()", "flags_database.store(demo_site.database())"},
			"dev/demo_site_dev.gleam":                 {"let features = feature_flags.start(db)", "features:)"},
			"test/demo_site/support.gleam":            {"let features = feature_flags.start(db)"},
			"test/demo_site/feature_flags_test.gleam": {"flags.allow(features"},
		} {
			content := read(t, p, path)
			for _, want := range wants {
				if !strings.Contains(content, want) {
					t.Errorf("%s: %s should contain %q:\n%s", label, path, want, content)
				}
			}
		}
		dev := read(t, p, "dev/demo_site_dev.gleam")
		if strings.Contains(dev, "admin.flags(features)") == c.minimal {
			t.Errorf("%s: the admin should manage the flags exactly when it is installed", label)
		}
		if _, err := NewFlags(p); err == nil || !strings.Contains(err.Error(), "already has feature flags") {
			t.Errorf("%s: flags should not be added twice, got %v", label, err)
		}
	}
}

// Auth and flags each run their migrations after the app's, whichever is
// added first.
func TestAddFlagsAndAuth(t *testing.T) {
	for _, flagsFirst := range []bool{true, false} {
		p := scaffolded(t, scaffold.SQLite, false)
		if flagsFirst {
			addFlags(t, p)
			addAuth(t, p)
		} else {
			addAuth(t, p)
			addFlags(t, p)
		}
		database := read(t, p, "src/demo_site/platform/database.gleam")
		for _, want := range []string{
			"use _ <- result.try(accounts.migrate(db))\n",
			"use _ <- result.try(feature_flags.migrate(db))\n",
		} {
			if strings.Count(database, want) != 1 {
				t.Errorf("flags first %v: database.gleam should contain %q once:\n%s", flagsFirst, want, database)
			}
		}
		if strings.Count(database, "fn migrate_app(") != 1 || strings.Count(database, "pub fn migrate(") != 1 {
			t.Errorf("flags first %v: database.gleam should wrap the app's migrate once:\n%s", flagsFirst, database)
		}
		root := read(t, p, "src/demo_site.gleam")
		if !strings.Contains(root, "features: flags.Flags") || !strings.Contains(root, "identity: auth.Auth") {
			t.Errorf("flags first %v: the services should have both:\n%s", flagsFirst, root)
		}
		dev := read(t, p, "dev/demo_site_dev.gleam")
		if !strings.Contains(dev, "admin.flags(features)") || !strings.Contains(dev, "admin.auth(identity)") {
			t.Errorf("flags first %v: the admin should have both:\n%s", flagsFirst, dev)
		}
	}
}

func TestAddFlagsNeedsDatabase(t *testing.T) {
	p := scaffolded(t, scaffold.NoDatabase, false)
	if _, err := NewFlags(p); err == nil || !strings.Contains(err.Error(), "howdy_database") {
		t.Errorf("got %v", err)
	}
}

func TestAddFlagsNeedsServices(t *testing.T) {
	p := scaffolded(t, scaffold.SQLite, false)
	root := filepath.Join(p.Dir, "src", "demo_site.gleam")
	src, _ := os.ReadFile(root)
	os.WriteFile(root, []byte(strings.Replace(string(src), "  let services = Services(db:)\n", "", 1)), 0o644)
	if _, err := NewFlags(p); err == nil || !strings.Contains(err.Error(), "services record") {
		t.Errorf("got %v", err)
	}
}

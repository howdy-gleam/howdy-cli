package scaffold

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDependency(t *testing.T) {
	git, err := ReleaseSource(Git, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	hex, err := ReleaseSource(Hex, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		source    Source
		pkg, want string
	}{
		{git, "howdy", `howdy = { git = "https://github.com/mikeyjones/howdy.git", ref = "v1.0.0" }`},
		{git, "howdy_ui", `howdy_ui = { git = "https://github.com/mikeyjones/howdy.git", ref = "v1.0.0", path = "ui" }`},
		{git, "howdy_dev", `howdy_dev = { git = "https://github.com/mikeyjones/howdy.git", ref = "v1.0.0", path = "howdy_dev" }`},
		{hex, "howdy", `howdy = ">= 1.0.0 and < 2.0.0"`},
		{hex, "howdy_dev", `howdy_dev = ">= 1.0.0 and < 2.0.0"`},
	} {
		if got := c.source.Dependency(c.pkg); got != c.want {
			t.Errorf("%s Dependency(%q) = %s, want %s", c.source.Kind, c.pkg, got, c.want)
		}
	}
	if git.Version("howdy_ui") != "v1.0.0" || hex.Version("howdy_ui") != "1.0.0" {
		t.Errorf("versions of howdy_ui: %q from Git, %q from Hex", git.Version("howdy_ui"), hex.Version("howdy_ui"))
	}
	defer func() {
		if recover() == nil {
			t.Error("Dependency should panic for a package that is not a Howdy package")
		}
	}()
	git.Dependency("howdy_nope")
}

func TestReleases(t *testing.T) {
	packages, ok := releases[Release]
	if !ok {
		t.Fatalf("releases.json has no Howdy %s, the release new projects use", Release)
	}
	for pkg := range packageDirs {
		if _, ok := packages[pkg]; !ok {
			t.Errorf("releases.json has no %s in Howdy %s", pkg, Release)
		}
	}
	if _, err := ReleaseSource(Hex, "0.0.1"); err == nil {
		t.Error("Hex should refuse a release releases.json does not list")
	}
	if _, err := ReleaseSource(Git, "0.0.1"); err != nil {
		t.Errorf("Git should take any release tag: %v", err)
	}
}

func TestParseSource(t *testing.T) {
	for s, want := range map[string]SourceKind{"git": Git, "hex": Hex, " Hex ": Hex} {
		if got, err := ParseSource(s); err != nil || got != want {
			t.Errorf("ParseSource(%q) = %q, %v; want %q", s, got, err, want)
		}
	}
	if _, err := ParseSource("github"); err == nil {
		t.Error("ParseSource should refuse an unknown source")
	}
}

func TestHexSource(t *testing.T) {
	for _, db := range []Database{NoDatabase, SQLite, Postgres} {
		for _, api := range []bool{false, true} {
			p, err := New(filepath.Join(t.TempDir(), "demo_site"))
			if err != nil {
				t.Fatal(err)
			}
			if p.Source != DefaultSource {
				t.Fatalf("the source should be %s by default", DefaultSource)
			}
			p.Database, p.API, p.Source = db, api, Hex
			files, err := p.Files()
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				content := string(f.Content)
				if !strings.HasSuffix(f.Path, ".md") && f.Path != "gleam.toml" {
					continue
				}
				for _, gitOnly := range []string{"github.com/mikeyjones/howdy", "tag `v", "ref = "} {
					if strings.Contains(content, gitOnly) {
						t.Errorf("%s (%s, api %v) mentions %q in a project on Hex", f.Path, db, api, gitOnly)
					}
				}
				if f.Path == "gleam.toml" && !strings.Contains(content, `howdy = ">= 1.0.0 and < 2.0.0"`) {
					t.Errorf("gleam.toml should take howdy from Hex:\n%s", content)
				}
			}
		}
	}
}

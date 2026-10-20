package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

func TestAddDockerToAnOlderProject(t *testing.T) {
	s, err := scaffold.New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	s.Database, s.NoDocker = scaffold.Postgres, true
	if _, err := s.Write(nil); err != nil {
		t.Fatal(err)
	}
	// As 'howdy new' wrote it before /up and the OTP application.
	root := filepath.Join(s.Dir, "src", "demo_site.gleam")
	src, _ := os.ReadFile(root)
	text := string(src)
	for _, cut := range []string{
		"  // GET /up answers 200 while the app runs, for load balancers and\n  // container platforms, ahead of the routes, middleware and request log.\n  |> howdy.health_check(health.new())\n",
		"import howdy/health\n",
	} {
		if !strings.Contains(text, cut) {
			t.Fatalf("the template no longer has %q", cut)
		}
		text = strings.Replace(text, cut, "", 1)
	}
	text = strings.Replace(text, "howdy.start_application(", "howdy.start(", 1)
	os.WriteFile(root, []byte(text), 0o644)

	p, err := Open(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDocker(p, "1.18.1", "")
	written, kept, err := d.Write()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(written, " ") != ".dockerignore Dockerfile bin/start" || len(kept) != 0 {
		t.Errorf("written %v, kept %v", written, kept)
	}
	dockerfile := read(t, p, "Dockerfile")
	if !strings.Contains(dockerfile, "ARG GLEAM_VERSION=v1.18.1") || !strings.Contains(dockerfile, "ARG ERLANG_VERSION="+scaffold.Erlang) {
		t.Errorf("Dockerfile versions:\n%s", dockerfile)
	}
	notes := d.Register()
	app := read(t, p, "src/demo_site.gleam")
	if !strings.Contains(app, "howdy.new()\n  // GET /up") || !strings.Contains(app, "|> howdy.health_check(health.new())\n") || !strings.Contains(app, "import howdy/health\n") {
		t.Errorf("the health check is not mounted:\n%s", app)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "howdy.start_application(name: \"demo_site_server\")") {
		t.Errorf("notes = %q", notes)
	}

	// Again: nothing is written over, and /up is not mounted twice.
	os.WriteFile(filepath.Join(p.Dir, "Dockerfile"), []byte("# mine\n"), 0o644)
	written, kept, err = d.Write()
	if err != nil || len(written) != 0 || len(kept) != 3 {
		t.Errorf("second run: written %v, kept %v, %v", written, kept, err)
	}
	d.Register()
	if got := strings.Count(read(t, p, "src/demo_site.gleam"), "howdy.health_check("); got != 1 {
		t.Errorf("health_check mounted %d times", got)
	}
	if read(t, p, "Dockerfile") != "# mine\n" {
		t.Error("the project's own Dockerfile was replaced")
	}
}

func TestAddDockerToANewProjectNeedsNothing(t *testing.T) {
	p := scaffolded(t, scaffold.SQLite, false)
	if notes := NewDocker(p, "", "").Register(); len(notes) > 0 {
		t.Errorf("a project 'howdy new' made needs no hand: %q", notes)
	}
}

func TestDockerfileInstallsGitOnlyForGitDependencies(t *testing.T) {
	for _, source := range []scaffold.SourceKind{scaffold.Hex, scaffold.Git} {
		for _, db := range []scaffold.Database{scaffold.NoDatabase, scaffold.Postgres} {
			s, err := scaffold.New(filepath.Join(t.TempDir(), "demo_site"))
			if err != nil {
				t.Fatal(err)
			}
			s.Database, s.Source = db, source
			files, err := s.Files()
			if err != nil {
				t.Fatal(err)
			}
			var made string
			for _, f := range files {
				if f.Path == "Dockerfile" {
					made = string(f.Content)
				}
			}
			s.NoDocker = true
			if _, err := s.Write(nil); err != nil {
				t.Fatal(err)
			}
			p, err := Open(s.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := NewDocker(p, "", "").Write(); err != nil {
				t.Fatal(err)
			}
			added := read(t, p, "Dockerfile")
			for how, dockerfile := range map[string]string{"howdy new": made, "howdy add docker": added} {
				label := string(source) + ", " + db.Label() + ", " + how
				if strings.Contains(dockerfile, "apk add --no-cache git") != (source == scaffold.Git) {
					t.Errorf("%s: the image should install Git only for Git dependencies:\n%s", label, dockerfile)
				}
				if strings.Contains(dockerfile, "build-base") != (db != scaffold.NoDatabase) {
					t.Errorf("%s: the image should install a C toolchain only with a database:\n%s", label, dockerfile)
				}
			}
		}
	}
}

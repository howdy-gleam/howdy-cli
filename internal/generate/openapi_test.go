package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

func addOpenAPI(t *testing.T, p *Project) *OpenAPI {
	t.Helper()
	o, err := NewOpenAPI(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := o.Register(); len(notes) > 0 {
		t.Fatalf("wiring needs a hand: %v", notes)
	}
	return o
}

func TestAddOpenAPI(t *testing.T) {
	for _, c := range []struct {
		p     *Project
		admin bool
		// before is the line the document is served ahead of.
		before string
	}{
		{scaffolded(t, scaffold.SQLite, false), true, "// The root static controller"},
		{scaffolded(t, scaffold.Postgres, false), true, "// The root static controller"},
		{scaffolded(t, scaffold.NoDatabase, false), true, "// The root static controller"},
		{scaffolded(t, scaffold.NoDatabase, true), false, "// The root static controller"},
		{scaffoldedAPI(t, scaffold.SQLite), true, "// A request no route matches"},
		{scaffoldedAPI(t, scaffold.NoDatabase), true, "// A request no route matches"},
	} {
		p := c.p
		dev := read(t, p, "dev/demo_site_dev.gleam")
		root := read(t, p, "src/demo_site.gleam")
		o := addOpenAPI(t, p)
		if o.Admin != c.admin {
			t.Errorf("%s: Admin = %v, want %v", p.Database, o.Admin, c.admin)
		}

		for path, wants := range map[string][]string{
			"gleam.toml":                                  {`howdy_openapi = { git = "https://github.com/howdy-gleam/howdy.git", ref = "v` + scaffold.Release + `", path = "openapi" }`},
			"src/demo_site/platform/api_docs.gleam":       {`openapi.new(title: "Demo site", version: "0.1.0")`, `openapi.serve(spec(), at: document_path)`},
			"test/demo_site/platform/api_docs_test.gleam": {"api_docs.document_path", `Ok("Demo site")`},
		} {
			content := read(t, p, path)
			for _, want := range wants {
				if !strings.Contains(content, want) {
					t.Errorf("%s: %s should contain %q:\n%s", p.Database, path, want, content)
				}
			}
		}

		// The document is served after every controller, ahead of the
		// catch-all, and nothing else in the app changes.
		after := read(t, p, "src/demo_site.gleam")
		serve := "  // After the controllers it documents: see platform/api_docs.gleam.\n  |> api_docs.serve\n  " + c.before
		if !strings.Contains(after, serve) {
			t.Errorf("%s: the app should serve the document ahead of %q:\n%s", p.Database, c.before, after)
		}
		without := strings.Replace(after, "import demo_site/platform/api_docs\n", "", 1)
		without = strings.Replace(without, "  // After the controllers it documents: see platform/api_docs.gleam.\n  |> api_docs.serve\n", "", 1)
		if without != root {
			t.Errorf("%s: only the import and the serve line should change:\n%s", p.Database, after)
		}
		// The admin finds the document itself.
		if got := read(t, p, "dev/demo_site_dev.gleam"); got != dev {
			t.Errorf("%s: the dev server should not change:\n%s", p.Database, got)
		}

		if _, err := NewOpenAPI(p); err == nil || !strings.Contains(err.Error(), "already has OpenAPI") {
			t.Errorf("%s: openapi should not be added twice, got %v", p.Database, err)
		}
	}
}

func TestAddOpenAPILeavesNewControllersDocumented(t *testing.T) {
	// A slice created afterwards is mounted ahead of the document, so an
	// endpoint in it would be documented.
	p := scaffolded(t, scaffold.SQLite, false)
	addOpenAPI(t, p)
	s, err := NewSlice(p, "posts", "post", []string{"title:string"})
	if err != nil {
		t.Fatal(err)
	}
	s.Routes = true
	if _, err := s.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := s.Register(); len(notes) > 0 {
		t.Fatal(notes)
	}
	root := read(t, p, "src/demo_site.gleam")
	at := strings.Index(root, "posts_controller.controller(db)")
	if at < 0 || at > strings.Index(root, "api_docs.serve") {
		t.Errorf("new controllers should be mounted before the document:\n%s", root)
	}
}

func TestAddOpenAPIUnknownShape(t *testing.T) {
	p := scaffolded(t, scaffold.NoDatabase, false)
	root := filepath.Join(p.Dir, "src", "demo_site.gleam")
	src, _ := os.ReadFile(root)
	changed := strings.Replace(string(src), `|> howdy.controller(static.serve("/", from: public_directory()))`, "", 1)
	os.WriteFile(root, []byte(changed), 0o644)
	o, err := NewOpenAPI(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := o.Register(); len(notes) != 1 || !strings.Contains(notes[0], "|> api_docs.serve") {
		t.Errorf("got notes %v", notes)
	}
	if got := read(t, p, "src/demo_site.gleam"); got != changed {
		t.Errorf("the app should be left alone:\n%s", got)
	}
}

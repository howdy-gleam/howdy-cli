package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

func scaffoldedAPI(t *testing.T, database scaffold.Database) *Project {
	t.Helper()
	s, err := scaffold.New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	s.Database, s.API = database, true
	if _, err := s.Write(nil); err != nil {
		t.Fatal(err)
	}
	p, err := Open(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAPIProjectRefusesPages(t *testing.T) {
	p := scaffoldedAPI(t, scaffold.SQLite)
	if !p.API {
		t.Fatal("a project without howdy_ui should be API only")
	}
	if scaffolded(t, scaffold.SQLite, false).API {
		t.Error("a project with howdy_ui should not be API only")
	}
	for what, err := range map[string]error{
		"page":           second(NewPage(p, "blog", "about", false, "")),
		"live page":      second(NewPage(p, "blog", "editor", true, "")),
		"component":      second(NewComponent(p, "", "price_tag", false)),
		"live component": second(NewComponent(p, "", "feed", true)),
		"layout":         second(NewLayout(p, "admin")),
		"pages":          p.NeedsPages("pages"),
	} {
		if err == nil || !strings.Contains(err.Error(), "API only") {
			t.Errorf("%s: got %v", what, err)
		}
	}
}

func second[T any](_ T, err error) error { return err }

// A JSON controller and every package still go in, with auth's JSON API
// and without its pages.
func TestAPIProjectTakesControllersAndPackages(t *testing.T) {
	p := scaffoldedAPI(t, scaffold.SQLite)
	slice, err := NewSlice(p, "users", "user", []string{"name:string"})
	if err != nil {
		t.Fatal(err)
	}
	slice.Routes = true
	if _, err := slice.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := slice.Register(); len(notes) > 0 {
		t.Fatalf("the controller needs a hand: %v", notes)
	}
	addAuth(t, p)
	addRemote(t, p)

	root := read(t, p, "src/demo_site.gleam")
	for _, want := range []string{"users_controller.controller(db)", `routes.api(services.identity, at: "/api/auth")`, "rpc.controller(services.rpc_token)"} {
		if !strings.Contains(root, want) {
			t.Errorf("src/demo_site.gleam should contain %q:\n%s", want, root)
		}
	}
	if strings.Contains(root, "pages.routes") || strings.Contains(root, "howdy/auth/pages") {
		t.Errorf("an API project should not mount auth's pages:\n%s", root)
	}
	if accounts := read(t, p, "src/demo_site/platform/accounts.gleam"); strings.Contains(accounts, "with_email_links") {
		t.Errorf("an API project's emails have no page to link to:\n%s", accounts)
	}
	if test := read(t, p, "test/demo_site/platform/accounts_test.gleam"); strings.Contains(test, "/auth/login") {
		t.Errorf("an API project has no sign-in page to test:\n%s", test)
	}
}

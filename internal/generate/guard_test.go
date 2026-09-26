package generate

import (
	"slices"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

// withAuth scaffolds a project and adds auth, then opens it again so the
// project knows it has howdy_auth.
func withAuth(t *testing.T) *Project {
	t.Helper()
	p := scaffolded(t, scaffold.SQLite, false)
	addAuth(t, p)
	p, err := Open(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Auth {
		t.Fatal("a project with howdy_auth should have auth")
	}
	return p
}

func contains(t *testing.T, p *Project, path string, wants ...string) {
	t.Helper()
	content := read(t, p, path)
	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("%s should contain %q:\n%s", path, want, content)
		}
	}
}

func TestAddAuthNeedsHowdyAuth(t *testing.T) {
	p := project(t, scaffold.SQLite)
	if err := p.NeedsAuth(); err == nil || !strings.Contains(err.Error(), "howdy add auth") {
		t.Errorf("got %v", err)
	}
	if _, err := NewPage(p, "account", "index", false, "", true); err == nil {
		t.Error("a page with --add-auth should need auth")
	}
	if _, err := NewComponent(p, "", "feed", true, true); err == nil {
		t.Error("a live component with --add-auth should need auth")
	}
}

func TestGuardedSlice(t *testing.T) {
	p := withAuth(t)
	c, err := NewSlice(p, "tasks", "task", []string{"title", "done:bool"})
	if err != nil {
		t.Fatal(err)
	}
	c.Routes, c.Pages, c.Auth = true, true, true
	written, err := c.Write()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(written, "test/demo_site/support.gleam") {
		t.Errorf("the test support should get sign_in: wrote %v", written)
	}
	if notes := c.Register(); len(notes) > 0 {
		t.Fatalf("registration needs a hand: %v", notes)
	}
	for _, module := range []string{"tasks_controller", "tasks_api_controller"} {
		contains(t, p, "src/demo_site/tasks/"+module+".gleam",
			"import howdy/auth.{type Auth}",
			"pub fn controller(db: Repo, identity: Auth) {",
			"auth.required(identity))",
			"  |> controller.build\n}",
		)
		contains(t, p, "test/demo_site/tasks/"+module+"_test.gleam",
			"let token = support.sign_in(db)",
			`testing.header("authorization", "Bearer " <> token)`,
			"pub fn signing_in_is_required_test() {",
		)
		contains(t, p, "src/demo_site.gleam", module+".controller(db, services.identity)")
	}
	contains(t, p, "test/demo_site/support.gleam",
		"pub fn sign_in(db: Repo) -> String {",
		"import howdy/auth/secret\n",
	)

	// A second guarded slice leaves the helper as it is.
	c, err = NewSlice(p, "notes", "note", []string{"body"})
	if err != nil {
		t.Fatal(err)
	}
	c.Routes, c.Auth = true, true
	if written, err = c.Write(); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(written, "test/demo_site/support.gleam") {
		t.Errorf("sign_in should be added once: wrote %v", written)
	}
	if n := strings.Count(read(t, p, "test/demo_site/support.gleam"), "pub fn sign_in("); n != 1 {
		t.Errorf("support has sign_in %d times", n)
	}
}

func TestUnguardedSliceIsUnchanged(t *testing.T) {
	p := withAuth(t)
	c, err := NewSlice(p, "notes", "note", []string{"body"})
	if err != nil {
		t.Fatal(err)
	}
	c.Routes = true
	written, err := c.Write()
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(written, "test/demo_site/support.gleam") {
		t.Errorf("an unguarded slice should leave the support alone: wrote %v", written)
	}
	if notes := c.Register(); len(notes) > 0 {
		t.Fatalf("registration needs a hand: %v", notes)
	}
	controller := read(t, p, "src/demo_site/notes/notes_controller.gleam")
	if strings.Contains(controller, "guarded") || strings.Contains(controller, "controller.build") {
		t.Errorf("the controller should not be guarded:\n%s", controller)
	}
	contains(t, p, "src/demo_site.gleam", "notes_controller.controller(db))")
}

func TestGuardedPageController(t *testing.T) {
	p := withAuth(t)
	pg, err := NewPage(p, "account", "index", false, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := pg.Register(); len(notes) > 0 {
		t.Fatalf("registration needs a hand: %v", notes)
	}
	contains(t, p, "src/demo_site.gleam", "account_controller.controller(services.identity))")
	contains(t, p, "test/demo_site/account/pages/index_page_test.gleam",
		`testing.header("authorization", "Bearer " <> support.sign_in(db))`,
		"pub fn index_page_requires_signing_in_test() {",
	)

	// Pages and live components added later join the guard, without the
	// flag, and their routes go before controller.build.
	board := createPage(t, p, "account", "board", true)
	if !board.Auth {
		t.Error("a page added to a guarded controller should know it is guarded")
	}
	feed := createComponent(t, p, "account", "feed", true)
	if !feed.Auth {
		t.Error("a live component in a guarded controller should know it is guarded")
	}
	// gleam format indents them when the command runs.
	controller := read(t, p, "src/demo_site/account/account_controller.gleam")
	last := 0
	for _, route := range []string{"index_page.show", "board_page.show", "board_page.socket", "feed_component.socket", "|> controller.build"} {
		at := strings.Index(controller, route)
		if at < last {
			t.Errorf("%s should follow the routes before it:\n%s", route, controller)
		}
		last = at
	}
	contains(t, p, "test/demo_site/account/pages/board_page_test.gleam", "support.sign_in(db)")
	contains(t, p, "test/demo_site/account/components/feed_component_test.gleam", "support.sign_in(db)")
}

func TestAddAuthRefusesAnUnguardedController(t *testing.T) {
	p := withAuth(t)
	createPage(t, p, "blog", "about", false)
	if _, err := NewPage(p, "blog", "faq", false, "", true); err == nil || !strings.Contains(err.Error(), "without a guard") {
		t.Errorf("got %v", err)
	}
	if _, err := NewComponent(p, "blog", "feed", true, true); err == nil || !strings.Contains(err.Error(), "without a guard") {
		t.Errorf("got %v", err)
	}
	if _, err := NewComponent(p, "", "card", false, true); err == nil || !strings.Contains(err.Error(), "not live") {
		t.Errorf("got %v", err)
	}
}

func TestGuardedGlobalComponents(t *testing.T) {
	p := withAuth(t)
	c, err := NewComponent(p, "", "notifications", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := c.Register(); len(notes) > 0 {
		t.Fatalf("registration needs a hand: %v", notes)
	}
	contains(t, p, "src/demo_site/components/components_controller.gleam",
		"pub fn controller(identity: Auth) {",
		`|> controller.get("/notifications/live", notifications_component.socket)
  |> controller.build`,
	)
	contains(t, p, "src/demo_site.gleam", "components_controller.controller(services.identity))")
}

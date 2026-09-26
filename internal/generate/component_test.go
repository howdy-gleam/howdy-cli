package generate

import (
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

func createComponent(t *testing.T, p *Project, controller, name string, live bool) *Component {
	t.Helper()
	c, err := NewComponent(p, controller, name, live, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := c.Register(); len(notes) > 0 {
		t.Fatalf("registration needs a hand: %v", notes)
	}
	return c
}

func TestGlobalComponent(t *testing.T) {
	p := opened(t, scaffold.NoDatabase)
	root := read(t, p, "src/demo_site.gleam")
	c := createComponent(t, p, "", "price_tag_component", false)
	if c.Module() != "price_tag_component" || c.Import() != "demo_site/components/price_tag_component" {
		t.Errorf("got %s, %s", c.Module(), c.Import())
	}
	if !strings.Contains(read(t, p, "src/demo_site/components/price_tag_component.gleam"), "pub fn view() -> Element(msg)") {
		t.Error("a component should have a view")
	}
	if !strings.Contains(read(t, p, "test/demo_site/components/price_tag_component_test.gleam"), "price_tag_component.view()") {
		t.Error("the test should render the component")
	}
	if read(t, p, "src/demo_site.gleam") != root {
		t.Error("a component that is not live registers nothing")
	}
	if _, err := NewComponent(p, "", "price_tag", false, false); err == nil || !strings.Contains(err.Error(), "already exists among global components") {
		t.Errorf("a component should not be created twice, got %v", err)
	}
}

func TestGlobalLiveComponents(t *testing.T) {
	p := opened(t, scaffold.SQLite)
	first := createComponent(t, p, "", "clock", true)
	if first.Exists || first.URL() != "/components/clock/live" {
		t.Errorf("got %+v, URL %s", first, first.URL())
	}
	second := createComponent(t, p, "", "notifications", true)
	if !second.Exists {
		t.Error("the second live component should reuse the components controller")
	}
	controller := read(t, p, "src/demo_site/components/components_controller.gleam")
	for _, want := range []string{
		`controller.new("components")`,
		`|> controller.get("/clock/live", clock_component.socket)`,
		`|> controller.get("/notifications/live", notifications_component.socket)`,
	} {
		if !strings.Contains(controller, want) {
			t.Errorf("components_controller.gleam should contain %q:\n%s", want, controller)
		}
	}
	root := read(t, p, "src/demo_site.gleam")
	if strings.Count(root, "components_controller.controller()") != 1 {
		t.Errorf("the components controller should be registered once:\n%s", root)
	}
	component := read(t, p, "src/demo_site/components/clock_component.gleam")
	for _, want := range []string{`live.mount("/components/clock/live")`, "pub fn socket(", "pub fn app() -> lustre.App(Nil, Model, Msg)"} {
		if !strings.Contains(component, want) {
			t.Errorf("clock_component.gleam should contain %q", want)
		}
	}
	if !strings.Contains(read(t, p, "test/demo_site/components/clock_component_test.gleam"), `testing.get("/components/clock/live") |> testing.send(support.app(db))`) {
		t.Error("the test should reach the socket through the app")
	}
}

func TestControllerComponents(t *testing.T) {
	p := opened(t, scaffold.SQLite)
	if _, err := NewComponent(p, "blog", "card", false, false); err == nil || !strings.Contains(err.Error(), "there is no blog controller") {
		t.Errorf("a component needs its controller, got %v", err)
	}
	c, err := NewSlice(p, "posts", "post", []string{"title"})
	if err != nil {
		t.Fatal(err)
	}
	c.Pages = true
	if _, err := c.Write(); err != nil {
		t.Fatal(err)
	}
	c.Register()

	card := createComponent(t, p, "posts", "card", false)
	if card.File() != "src/demo_site/features/posts/components/card_component.gleam" {
		t.Errorf("File = %s", card.File())
	}
	comments := createComponent(t, p, "posts", "comments", true)
	if comments.URL() != "/posts/components/comments/live" {
		t.Errorf("URL = %s", comments.URL())
	}
	controller := read(t, p, "src/demo_site/features/posts/posts_controller.gleam")
	socket := strings.Index(controller, `controller.get("/components/comments/live", comments_component.socket)`)
	id := strings.Index(controller, `controller.get("/:id"`)
	if socket < 0 || socket > id {
		t.Errorf("the socket should be routed before /:id:\n%s", controller)
	}
	if !strings.Contains(controller, "import demo_site/features/posts/components/comments_component\n") {
		t.Error("the controller should import the component")
	}
	if !strings.Contains(read(t, p, "test/demo_site/features/posts/components/comments_component_test.gleam"), `testing.get("/posts/components/comments/live")`) {
		t.Error("the test should reach the socket")
	}
	if _, err := NewComponent(p, "posts", "comments", true, false); err == nil {
		t.Error("a live component should not be created twice")
	}
}

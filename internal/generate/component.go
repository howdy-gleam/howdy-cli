package generate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// Component is a reusable piece of view, global in src/<project>/components/
// or belonging to a controller in src/<project>/<controller>/components/.
// A live one is a Lustre server component with a WebSocket route: in its
// controller, or for a global one in components/components_controller.gleam,
// which is created with the first.
type Component struct {
	Project *Project
	// Controller is the controller it belongs to, or "" for a global one.
	Controller string
	// Name names the module without its suffix, e.g. "card" for
	// card_component.gleam.
	Name string
	Live bool
	// Prefix is the path the controller serving a live component's socket
	// is mounted at, without slashes.
	Prefix string
	// Exists reports whether that controller is already in the project.
	Exists bool
}

// globalComponents is the folder of global components, and the controller
// in it that serves their sockets.
const globalComponents = "components"

// NewComponent validates the names, checks that a controller's component
// belongs to a controller that exists, and that the component does not.
func NewComponent(p *Project, controller, name string, live bool) (*Component, error) {
	if err := p.NeedsPages("components"); err != nil {
		return nil, err
	}
	name = strings.TrimSuffix(name, "_component")
	if !identifier.MatchString(name) {
		return nil, invalidName(name)
	}
	c := &Component{Project: p, Controller: controller, Name: name, Live: live, Prefix: globalComponents}
	if controller != "" {
		if !identifier.MatchString(controller) {
			return nil, invalidName(controller)
		}
		if _, err := os.Stat(filepath.Join(p.Dir, c.ControllerFile())); err != nil {
			return nil, fmt.Errorf("there is no %s controller (%s); create one with 'howdy create page %s <page>', or leave out the controller for a global component", controller, c.ControllerFile(), controller)
		}
		c.Prefix = controller
	}
	if _, err := os.Stat(filepath.Join(p.Dir, c.File())); err == nil {
		where := "global"
		if controller != "" {
			where = "the " + controller + " controller's"
		}
		return nil, fmt.Errorf("the %s component already exists among %s components: %s; pick another name, or edit that component", name, where, c.File())
	}
	if !live {
		return c, nil
	}
	src, err := os.ReadFile(filepath.Join(p.Dir, c.ControllerFile()))
	switch {
	case err == nil:
		c.Exists = true
		if m := controllerPrefix.FindSubmatch(src); m != nil {
			c.Prefix = strings.Trim(string(m[1]), "/")
		}
		if hasRoute(src, c.Route()) {
			return nil, fmt.Errorf("%s already has a route at %s; pick another name", c.ControllerFile(), c.URL())
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	return c, nil
}

// Module is the component's module name, e.g. "card_component".
func (c *Component) Module() string { return c.Name + "_component" }

// Title names the component for people, e.g. "Price tag".
func (c *Component) Title() string { return humanize(c.Name, true) }

func (c *Component) folder() string {
	if c.Controller == "" {
		return filepath.Join("src", c.Project.Name, globalComponents)
	}
	return filepath.Join("src", c.Project.Name, c.Controller, "components")
}

// File is the component's path, relative to the project directory.
func (c *Component) File() string {
	return filepath.ToSlash(filepath.Join(c.folder(), c.Module()+".gleam"))
}

// Import is the component's module path, e.g. "my_app/blog/components/card_component".
func (c *Component) Import() string {
	return strings.TrimPrefix(strings.TrimSuffix(c.File(), ".gleam"), "src/")
}

// ControllerFile is the controller that serves a live component's socket:
// its own controller, or the one beside the global components.
func (c *Component) ControllerFile() string {
	if c.Controller == "" {
		return filepath.ToSlash(filepath.Join(c.folder(), "components_controller.gleam"))
	}
	return filepath.ToSlash(filepath.Join("src", c.Project.Name, c.Controller, c.Controller+"_controller.gleam"))
}

// Route is the socket's path within its controller, and URL the full path.
func (c *Component) Route() string {
	if c.Controller == "" {
		return "/" + c.Name + "/live"
	}
	return "/components/" + c.Name + "/live"
}

func (c *Component) URL() string {
	if c.Prefix == "" {
		return c.Route()
	}
	return "/" + c.Prefix + c.Route()
}

// HasSupport reports whether the project has test/<project>/support.gleam,
// which 'howdy new' writes into every project, and which builds the app
// with its services for the tests.
func (c *Component) HasSupport() bool {
	_, err := os.Stat(filepath.Join(c.Project.Dir, "test", c.Project.Name, "support.gleam"))
	return err == nil
}

// HasDatabase reports whether the test uses a database from test/<project>/support.gleam.
func (c *Component) HasDatabase() bool { return c.Project.Database != "" }

func (c *Component) testFile() string {
	return filepath.ToSlash(filepath.Join("test", strings.TrimPrefix(filepath.ToSlash(c.folder()), "src/"), c.Module()+"_test.gleam"))
}

// Files renders the component, the global components controller if a live
// component needs it and it is missing, and a test when the project has
// the tests 'howdy new' gives it.
func (c *Component) Files() ([]File, error) {
	p := c.Project
	targets := [][2]string{}
	if c.Live {
		targets = append(targets, [2]string{"live_component.gleam.tmpl", c.File()})
		if !c.Exists {
			targets = append(targets, [2]string{"components_controller.gleam.tmpl", c.ControllerFile()})
		}
	} else {
		targets = append(targets, [2]string{"component.gleam.tmpl", c.File()})
	}
	test := !c.Live
	if c.Live && c.HasDatabase() {
		_, err := os.Stat(filepath.Join(p.Dir, "test", p.Name, "support.gleam"))
		test = err == nil
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "test")); err == nil && test {
		targets = append(targets, [2]string{"component_test.gleam.tmpl", c.testFile()})
	}
	var files []File
	for _, t := range targets {
		tmpl, err := template.New(t[0]).Funcs(funcs).Option("missingkey=error").ParseFS(templates, "templates/"+t[0])
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, c); err != nil {
			return nil, fmt.Errorf("template %s: %w", t[0], err)
		}
		files = append(files, File{Path: t[1], Content: out.Bytes()})
	}
	return files, nil
}

// Write creates the files, refusing to replace any that exist. It returns
// the paths written.
func (c *Component) Write() ([]string, error) {
	files, err := c.Files()
	if err != nil {
		return nil, err
	}
	return writeFiles(c.Project.Dir, files)
}

// Register routes a live component's socket in its controller and, for the
// first global live component, registers the components controller in
// src/<project>.gleam. A component that is not live needs nothing. For
// each file without the shape the edit expects, the notes say what to add
// by hand.
func (c *Component) Register() (notes []string) {
	if !c.Live {
		return nil
	}
	p := c.Project
	route := fmt.Sprintf("|> controller.get(%q, %s.socket)\n", c.Route(), c.Module())
	importLine := "import " + c.Import()
	if err := appendToFunction(filepath.Join(p.Dir, c.ControllerFile()), "controller", importLine, route); err != nil {
		notes = append(notes, fmt.Sprintf("Route the component's socket in %s: %s, and add %s to the routes in its controller function.",
			c.ControllerFile(), importLine, strings.TrimSpace(route)))
	}
	if c.Exists {
		return notes
	}
	root := filepath.Join("src", p.Name+".gleam")
	module := "components_controller"
	err := edit(filepath.Join(p.Dir, root),
		fmt.Sprintf("import %s/%s/%s", p.Name, globalComponents, module),
		regexp.MustCompile(`(?m)^(\s*)howdy\.new\(\)\n`),
		"${0}${1}|> howdy.controller("+module+".controller())\n",
	)
	if err != nil {
		notes = append(notes, fmt.Sprintf("Register the controller in %s: import %s/%s/%s and add |> howdy.controller(%s.controller()) before the static controller.",
			root, p.Name, globalComponents, module, module))
	}
	return notes
}

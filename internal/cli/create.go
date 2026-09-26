package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"howdy/internal/generate"
	"howdy/internal/tools"
	"howdy/internal/tui"
)

func createCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Add code to the current Howdy project",
	}
	cmd.AddCommand(createControllerCommand())
	cmd.AddCommand(createPagesCommand())
	cmd.AddCommand(createRepoCommand())
	cmd.AddCommand(createMigrationCommand())
	cmd.AddCommand(createPageCommand(false))
	cmd.AddCommand(createPageCommand(true))
	cmd.AddCommand(createLayoutCommand())
	cmd.AddCommand(createComponentCommand(false))
	cmd.AddCommand(createComponentCommand(true))
	cmd.AddCommand(createEmailCommand())
	return cmd
}

func createEmailCommand() *cobra.Command {
	var plain bool
	cmd := &cobra.Command{
		Use:   "email <name>",
		Short: "Create an email, with its preview for the dev admin",
		Long: `Create src/<project>/emails/<name>_email.gleam: an email written with smail,
with build(to:) to make the message and preview() to build it from sample
data. The preview is added to emails/previews.gleam, so the dev admin shows
it under Mail and the tests check that it can be sent.

The project needs mail first: 'howdy add mail'.`,
		Example: "  howdy create email password_reset\n  howdy create email order_shipped",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreateEmail(args[0], plain)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func runCreateEmail(name string, plain bool) error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	email, err := generate.NewEmail(project, name)
	if err != nil {
		return err
	}
	var notes []string
	steps := []tui.Step{
		{
			Title: "Creating the email",
			Run: func(r tui.Reporter) (string, error) {
				return email.Write()
			},
		},
		{
			Title: "Adding its preview",
			Run: func(r tui.Reporter) (string, error) {
				notes = email.Register()
				if len(notes) > 0 {
					return "needs a hand, see below", nil
				}
				return fmt.Sprintf("src/%s/emails/previews.gleam", project.Name), nil
			},
		},
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				gleam, err := tools.FindGleam()
				if err != nil {
					r.Warn("gleam was not found on PATH; run 'gleam format' yourself.")
					return "skipped", nil
				}
				return "gleam format", gleam.Run(dir, "format", "src")
			},
		},
	}
	if err := tui.Run(fmt.Sprintf("howdy create email %s", email.Name), steps, plain); err != nil {
		return err
	}
	notes = append([]string{
		fmt.Sprintf("Write the email in %s, and give build the arguments it needs.", email.File()),
		fmt.Sprintf("Send it with mail.send(mailer, %s.build(to: address)), using the mailer in the app's services.", email.Module()),
	}, notes...)
	fmt.Print(tui.NextSteps(email.Module(), []tui.Command{
		{Cmd: "gleam dev", Note: "then see it under Mail in the admin at http://127.0.0.1:8787/_howdy"},
		{Cmd: "gleam test", Note: "check that it can be sent"},
	}, notes))
	return nil
}

func createComponentCommand(live bool) *cobra.Command {
	var plain, addAuth bool
	cmd := &cobra.Command{
		Use:   "component [controller] <name>",
		Short: "Create a component, a piece of view to reuse on pages",
		Long: `Create a component: a view function that returns an element, for pages to
reuse. With one name it is global, in src/<project>/components/; with a
controller first it belongs to that controller, in
src/<project>/features/<controller>/components/. The module is <name>_component.gleam
and pages call <name>_component.view(). The controller must exist.

A component that already exists is refused and nothing is written. A test
that it renders is written beside the other tests.`,
		Example: "  howdy create component price_tag\n  howdy create component blog post_card",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			controller, name := "", args[0]
			if len(args) == 2 {
				controller, name = args[0], args[1]
			}
			return runCreateComponent(controller, name, live, addAuth, plain)
		},
	}
	if live {
		cmd.Use = "live_component [controller] <name>"
		cmd.Short = "Create a live component, a Lustre server component to put on pages"
		cmd.Long = `Create a live component: a Lustre server component whose model, update and
view run on the server, with the browser showing each change it sends over
a WebSocket. With one name it is global, in src/<project>/components/; with
a controller first it belongs to that controller, in
src/<project>/features/<controller>/components/. The controller must exist.

The module is <name>_component.gleam. Pages put it on the page with
<name>_component.mount(), and render with their layout's render_live, which
loads the script it connects with. Its WebSocket is routed for you: in the
controller at /<controller>/components/<name>/live, or for a global
component at /components/<name>/live, in components/components_controller.gleam,
which the first global live component creates and registers in
src/<project>.gleam.

A component that already exists is refused and nothing is written. A test
that it mounts and serves its socket is written beside the other tests.

With --add-auth, the components controller the first global live component
creates guards every route with Howdy Auth, so only someone signed in
reaches them; the project needs 'howdy add auth' first. A controller that
exists keeps its guard, or lack of one, and --add-auth is refused for one
without.`
		cmd.Example = "  howdy create live_component notifications\n  howdy create live_component blog comments\n  howdy create live_component notifications --add-auth"
		cmd.Flags().BoolVar(&addAuth, "add-auth", false, "guard the components controller it creates with Howdy Auth, so only someone signed in reaches it (needs 'howdy add auth')")
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func runCreateComponent(controller, name string, live, addAuth, plain bool) error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	component, err := generate.NewComponent(project, controller, name, live, addAuth)
	if err != nil {
		return err
	}

	var notes []string
	steps := []tui.Step{
		{
			Title: "Creating files",
			Run: func(r tui.Reporter) (string, error) {
				written, err := component.Write()
				for _, path := range written {
					r.Log(path)
				}
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%d files", len(written)), nil
			},
		},
	}
	if live {
		steps = append(steps, tui.Step{
			Title: "Routing its socket",
			Run: func(r tui.Reporter) (string, error) {
				notes = component.Register()
				if len(notes) > 0 {
					return "needs a hand, see below", nil
				}
				return component.ControllerFile(), nil
			},
		})
	}
	steps = append(steps, tui.Step{
		Title: "Formatting Gleam sources",
		Run: func(r tui.Reporter) (string, error) {
			gleam, err := tools.FindGleam()
			if err != nil {
				r.Warn("gleam was not found on PATH; run 'gleam format' yourself.")
				return "skipped", nil
			}
			return "gleam format", gleam.Run(dir, "format", "src", "test")
		},
	})
	kind := "component"
	if live {
		kind = "live_component"
	}
	title := strings.TrimSpace(fmt.Sprintf("howdy create %s %s %s", kind, controller, component.Name))
	if err := tui.Run(strings.Join(strings.Fields(title), " "), steps, plain); err != nil {
		return err
	}

	use := fmt.Sprintf("Use it in a page: import %s and add %s.view() to the page's content.", component.Import(), component.Module())
	if live {
		use = fmt.Sprintf("Put it on a page: import %s, add %s.mount() to the content, and render the page with render_live.", component.Import(), component.Module())
		notes = append(notes, fmt.Sprintf("Its socket is at %s, routed in %s.", component.URL(), component.ControllerFile()))
	}
	notes = append([]string{fmt.Sprintf("Build the component in %s.", component.File()), use}, notes...)
	fmt.Print(tui.NextSteps(component.Module(), []tui.Command{
		{Cmd: "gleam test", Note: "run the tests, including the new one for the component"},
	}, notes))
	return nil
}

func createLayoutCommand() *cobra.Command {
	var plain bool
	cmd := &cobra.Command{
		Use:   "layout <name>",
		Short: "Create a layout that pages can render through",
		Long: `Create src/<project>/layouts/<name>_layout.gleam, a layout: everything around
a page's content, from the HTML document with the theme and stylesheets to
the header. It starts as a copy of the default layout that 'howdy new'
writes, ready for you to change.

A page renders through a layout with render, or render_live when it has
live sections, which also loads the script they connect with. Pass
--layout <name> to 'howdy create page', 'live_page' or 'pages' to use it.`,
		Example: "  howdy create layout admin\n  howdy create page admin index --layout admin",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreateLayout(args[0], plain)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func runCreateLayout(name string, plain bool) error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	layout, err := generate.NewLayout(project, name)
	if err != nil {
		return err
	}
	steps := []tui.Step{
		{
			Title: "Creating the layout",
			Run: func(r tui.Reporter) (string, error) {
				return layout.File(), layout.Write()
			},
		},
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				gleam, err := tools.FindGleam()
				if err != nil {
					r.Warn("gleam was not found on PATH; run 'gleam format' yourself.")
					return "skipped", nil
				}
				return "gleam format", gleam.Run(dir, "format", "src")
			},
		},
	}
	if err := tui.Run(fmt.Sprintf("howdy create layout %s", layout.Name), steps, plain); err != nil {
		return err
	}
	fmt.Print(tui.NextSteps(layout.Module(), []tui.Command{
		{Cmd: fmt.Sprintf("howdy create page <controller> <page> --layout %s", layout.Name), Note: "make a page that uses it"},
	}, []string{
		fmt.Sprintf("Change the layout in %s; it starts as a copy of the default.", layout.File()),
		fmt.Sprintf("To move an existing page onto it, import %s/layouts/%s and call %s.render, or render_live for a live page.", project.Name, layout.Module(), layout.Module()),
	}))
	return nil
}

func createPageCommand(live bool) *cobra.Command {
	var plain, addAuth bool
	var layout string
	cmd := &cobra.Command{
		Use:   "page <controller> <page>",
		Short: "Create a page and add it to a controller, creating the controller if needed",
		Long: `Create src/<project>/features/<controller>/pages/<page>_page.gleam, a page ready for
your content, and add its route to <controller>_controller.gleam, which sits
beside the pages folder. The page is at /<controller>/<page>, or at /<controller> for a page
named index.

If the controller does not exist it is created and registered in
src/<project>.gleam. If it does, the page is added to it and served under
whatever path the controller already uses. A page that already exists, or
a route the controller already has, is refused and nothing is written.

A test that the page responds is written to test/<project>/features/<controller>/pages/.

With --add-auth, a controller it creates guards every route with Howdy
Auth, so only someone signed in reaches them; the project needs 'howdy add
auth' first. A controller that exists keeps its guard, or lack of one, and
--add-auth is refused for one without.`,
		Example: "  howdy create page blog about\n  howdy create page blog index\n  howdy create page home contact\n  howdy create page admin index --layout admin\n  howdy create page account index --add-auth",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreatePage(args[0], args[1], live, layout, addAuth, plain)
		},
	}
	if live {
		cmd.Use = "live_page <controller> <page>"
		cmd.Short = "Create a page with a live section and add it to a controller, creating the controller if needed"
		cmd.Long = `Create src/<project>/features/<controller>/pages/<page>_page.gleam, a page with a
live section ready for your content: a Lustre server component whose model,
update and view run on the server, with the browser showing each change it
sends over a WebSocket. The page's routes are added to
<controller>_controller.gleam, which sits beside the pages folder: the page at
/<controller>/<page> (or /<controller> for a page named index) and its
WebSocket at /<controller>/<page>/live.

If the controller does not exist it is created and registered in
src/<project>.gleam. If it does, the page is added to it and served under
whatever path the controller already uses. A page that already exists, or
a route the controller already has, is refused and nothing is written.

A test that the page responds is written to test/<project>/features/<controller>/pages/.

With --add-auth, a controller it creates guards every route with Howdy
Auth, so only someone signed in reaches them; the project needs 'howdy add
auth' first. A controller that exists keeps its guard, or lack of one, and
--add-auth is refused for one without.`
		cmd.Example = "  howdy create live_page dashboard index\n  howdy create live_page blog editor\n  howdy create live_page admin board --layout admin"
	}
	cmd.Flags().StringVar(&layout, "layout", "", "render the page through this layout from src/<project>/layouts/ instead of the default")
	cmd.Flags().BoolVar(&addAuth, "add-auth", false, "guard the controller it creates with Howdy Auth, so only someone signed in reaches its pages (needs 'howdy add auth')")
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func runCreatePage(controller, name string, live bool, layout string, addAuth, plain bool) error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	page, err := generate.NewPage(project, controller, name, live, layout, addAuth)
	if err != nil {
		return err
	}
	registered := page.ControllerFile()
	if !page.Exists {
		registered += fmt.Sprintf(", src/%s.gleam", project.Name)
	}

	var notes []string
	steps := []tui.Step{
		{
			Title: "Creating files",
			Run: func(r tui.Reporter) (string, error) {
				written, err := page.Write()
				for _, path := range written {
					r.Log(path)
				}
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%d files", len(written)), nil
			},
		},
		{
			Title: "Adding the routes",
			Run: func(r tui.Reporter) (string, error) {
				notes = page.Register()
				if len(notes) > 0 {
					return "needs a hand, see below", nil
				}
				return registered, nil
			},
		},
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				gleam, err := tools.FindGleam()
				if err != nil {
					r.Warn("gleam was not found on PATH; run 'gleam format' yourself.")
					return "skipped", nil
				}
				return "gleam format", gleam.Run(dir, "format", "src", "test")
			},
		},
	}
	kind := "page"
	if live {
		kind = "live_page"
	}
	if err := tui.Run(fmt.Sprintf("howdy create %s %s %s", kind, controller, page.Name), steps, plain); err != nil {
		return err
	}

	if !page.Exists {
		notes = append(notes, fmt.Sprintf("The new %s controller is registered in src/%s.gleam.", controller, project.Name))
	}
	if page.Auth {
		notes = append(notes, fmt.Sprintf("Only someone signed in reaches %s; ctx.guard.user is the signed-in user.", page.URL()))
	}
	notes = append(notes, fmt.Sprintf("Add the page's content in %s. It renders through %s.", page.File(), project.LayoutFile(page.Layout)))
	fmt.Print(tui.NextSteps(page.Module(), []tui.Command{
		{Cmd: "gleam dev", Note: "then open http://127.0.0.1:8787" + page.URL()},
		{Cmd: "gleam test", Note: "run the tests, including the new one for the page"},
	}, notes))
	return nil
}

func createControllerCommand() *cobra.Command {
	var plain, addAuth bool
	cmd := &cobra.Command{
		Use:   "controller <plural> <singular> <field:type>...",
		Short: "Create a JSON controller with its record, database operations and migration",
		Long: `Create a slice of the current project: a folder under
src/<project>/features/ named after the plural, holding

  <singular>.gleam            the record: types, validation and JSON codecs
  <plural>.gleam              the database operations: all, find, create, update, delete
  <plural>_controller.gleam   the routes under /api/<plural>

  GET    /api/<plural>        every record
  GET    /api/<plural>/:id    one record
  POST   /api/<plural>        create
  PUT    /api/<plural>/:id    update
  DELETE /api/<plural>/:id    delete

It also writes a migration that creates the <plural> table and tests for
the database operations and the routes, then registers the controller in src/<project>.gleam and the
migration in platform/database.gleam. The project must have a database;
SQLite or PostgreSQL is detected from gleam.toml.

Fields are name:type pairs. Types are string, int, float and bool; a bare
name is a string. Every record also gets an integer id.

With --add-auth, every route is guarded with Howdy Auth and answers 401 to
anyone not signed in; the project needs 'howdy add auth' first. The
controller takes the app's identity, and its tests sign in.`,
		Example: "  howdy create controller users user name:string age:int\n  howdy create controller blog_posts blog_post title body:string published:bool\n  howdy create controller notes note body --add-auth",
		Args:    cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreateSlice("controller", args[0], args[1], args[2:], "", addAuth, plain)
		},
	}
	cmd.Flags().BoolVar(&addAuth, "add-auth", false, "guard every route with Howdy Auth, so only someone signed in reaches them (needs 'howdy add auth')")
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func createRepoCommand() *cobra.Command {
	var plain bool
	cmd := &cobra.Command{
		Use:   "repo <plural> <singular> <field:type>...",
		Short: "Create a record with its database operations and migration, without a controller",
		Long: `Create a slice of the current project without routes: a folder under
src/<project>/features/ named after the plural, holding

  <singular>.gleam   the record: types, validation and JSON codecs
  <plural>.gleam     the database operations: all, find, create, update, delete

It also writes a migration that creates the <plural> table and a test for
the database operations, then registers the migration in
platform/database.gleam. Use it for data that is reached from your own
pages or other slices rather than a JSON API; 'howdy create controller'
writes the same files plus the routes.

Fields are name:type pairs. Types are string, int, float and bool; a bare
name is a string. Every record also gets an integer id.`,
		Example: "  howdy create repo users user name:string email:string age:int",
		Args:    cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreateSlice("repo", args[0], args[1], args[2:], "", false, plain)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func createPagesCommand() *cobra.Command {
	var plain, api, addAuth bool
	var layout string
	cmd := &cobra.Command{
		Use:   "pages <plural> <singular> <field:type>...",
		Short: "Create HTML pages to list, show, add, edit and delete a record, with its database operations and migration",
		Long: `Create a slice of the current project with pages for people: a folder under
src/<project>/features/ named after the plural, holding

  <singular>.gleam              the record: types, validation and JSON codecs
  <plural>.gleam                the database operations: all, find, create, update, delete
  <plural>_controller.gleam     the routes under /<plural>
  pages/index_page.gleam        every record, in a table
  pages/show_page.gleam         one record
  pages/new_page.gleam          the form for a new record
  pages/edit_page.gleam         the form to change one
  pages/helpers.gleam           the form both use, and where a record lives

  GET  /<plural>             every record, in a table
  GET  /<plural>/new         the form for a new record
  POST /<plural>             create
  GET  /<plural>/:id         one record
  GET  /<plural>/:id/edit    the form to change it
  POST /<plural>/:id         update
  POST /<plural>/:id/delete  delete

The pages are built from Howdy UI as plain links and forms, so they work
without JavaScript; a form that fails validation is shown again with its
values and the errors next to the fields. They render through the default
layout in src/<project>/layouts/, or the one --layout names. The form
helpers go in platform/pages.gleam and the styles are added to
priv/public/assets/style.css; both happen once and serve every set of pages.

It also writes a migration that creates the <plural> table and tests for
the database operations and the pages, then registers the pages in
src/<project>.gleam and the migration in platform/database.gleam. Pass
--api for the JSON controller of 'howdy create controller' as well, which
is then called <plural>_api_controller.gleam. 'howdy create page <plural>
<name>' adds another page to the same controller.

Fields are name:type pairs. Types are string, int, float and bool; a bare
name is a string. Every record also gets an integer id.

With --add-auth, every route, and with --api the JSON ones too, is guarded
with Howdy Auth and answers 401 to anyone not signed in; the project needs
'howdy add auth' first. The controllers take the app's identity, and their
tests sign in.`,
		Example: "  howdy create pages users user name:string age:int\n  howdy create pages blog_posts blog_post title body published:bool --api\n  howdy create pages notes note body --add-auth",
		Args:    cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := "pages"
			if api {
				kind = "pages+api"
			}
			return runCreateSlice(kind, args[0], args[1], args[2:], layout, addAuth, plain)
		},
	}
	cmd.Flags().BoolVar(&api, "api", false, "also create the JSON controller under /api/<plural>")
	cmd.Flags().BoolVar(&addAuth, "add-auth", false, "guard every route with Howdy Auth, so only someone signed in reaches them (needs 'howdy add auth')")
	cmd.Flags().StringVar(&layout, "layout", "", "render the pages through this layout from src/<project>/layouts/ instead of the default")
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func createMigrationCommand() *cobra.Command {
	var plain bool
	cmd := &cobra.Command{
		Use:   "migration <table> <field:type>...",
		Short: "Create a migration for a new table",
		Long: `Create src/<project>/migrations/create_<table>.gleam, a migration that
creates the table with an integer id and the given columns, numbered one
above the highest migration in the project. It is added to the list in
platform/database.gleam so it runs the next time the app starts or
'gleam run -m tasks/migrate' is run. Nothing else is written.

Fields are name:type pairs with the types string, int, float and bool.
Column types follow the project's database, SQLite or PostgreSQL.`,
		Example: "  howdy create migration users name:string email:string age:int",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreateSlice("migration", args[0], "", args[1:], "", false, plain)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

// runCreateSlice serves every subcommand; kind is "controller", "pages",
// "pages+api", "repo" or "migration", and singular is empty for a migration.
func runCreateSlice(kind, plural, singular string, fields []string, layout string, addAuth, plain bool) error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}
	project, err := generate.Load(dir)
	if err != nil {
		return err
	}
	var slice *generate.Slice
	if kind == "migration" {
		slice, err = generate.NewMigration(project, plural, fields)
	} else {
		slice, err = generate.NewSlice(project, plural, singular, fields)
	}
	if err != nil {
		return err
	}
	slice.Routes = kind == "controller" || kind == "pages+api"
	slice.Pages = strings.HasPrefix(kind, "pages")
	if slice.Pages {
		if err := project.NeedsPages("pages"); err != nil {
			return err
		}
		if slice.Layout, err = project.ResolveLayout(layout); err != nil {
			return err
		}
	}
	if addAuth {
		if err := project.NeedsAuth(); err != nil {
			return err
		}
		slice.Auth = true
	}
	kind = strings.TrimSuffix(kind, "+api")
	registering, registered := "Registering the migration", "platform/database.gleam"
	if slice.Routes || slice.Pages {
		registering = "Registering the routes and migration"
		registered = fmt.Sprintf("src/%s.gleam, platform/database.gleam", project.Name)
	}

	var notes []string
	steps := []tui.Step{
		{
			Title: "Creating files",
			Run: func(r tui.Reporter) (string, error) {
				written, err := slice.Write()
				for _, path := range written {
					r.Log(path)
				}
				if err != nil {
					return "", err
				}
				if len(written) == 1 {
					return written[0], nil
				}
				return fmt.Sprintf("%d files", len(written)), nil
			},
		},
		{
			Title: registering,
			Run: func(r tui.Reporter) (string, error) {
				notes = slice.Register()
				if len(notes) > 0 {
					return "needs a hand, see below", nil
				}
				return registered, nil
			},
		},
		{
			Title: "Formatting Gleam sources",
			Run: func(r tui.Reporter) (string, error) {
				gleam, err := tools.FindGleam()
				if err != nil {
					r.Warn("gleam was not found on PATH; run 'gleam format' yourself.")
					return "skipped", nil
				}
				return "gleam format", gleam.Run(dir, "format", "src", "test")
			},
		},
	}
	title := fmt.Sprintf("howdy create %s %s", kind, plural)
	if err := tui.Run(title, steps, plain); err != nil {
		return err
	}

	notes = append(notes, fmt.Sprintf("The %s table is created by migration %d the next time the app starts.", plural, slice.Version))
	if kind == "migration" {
		notes = append(notes, fmt.Sprintf("Edit src/%s/migrations/%s.gleam to add defaults, unique columns or indexes before it runs.", project.Name, slice.MigrationModule()))
		fmt.Print(tui.NextSteps(slice.MigrationModule(), []tui.Command{
			{Cmd: "gleam run -m tasks/migrate", Note: "apply it now, without starting the server"},
		}, notes))
		return nil
	}
	notes = append(notes, fmt.Sprintf("Add validation rules in %s.gleam.", filepath.ToSlash(filepath.Join(project.FeatureDir(plural), singular))))
	if slice.Auth {
		notes = append(notes, "Only someone signed in reaches the routes; ctx.guard.user is the signed-in user.")
	}
	commands := []tui.Command{{Cmd: "gleam test", Note: fmt.Sprintf("run the tests, including the new ones for %s", plural)}}
	switch {
	case slice.Pages:
		commands = append(commands, tui.Command{Cmd: "gleam dev", Note: fmt.Sprintf("then open http://127.0.0.1:8787/%s", plural)})
		notes = append(notes, fmt.Sprintf("The pages are in %s/pages/ and their routes in %s.gleam; the layout every set of pages shares is src/%s/platform/pages.gleam.", filepath.ToSlash(project.FeatureDir(plural)), slice.PagesModule(), project.Name))
	case slice.Routes:
		commands = append(commands, tui.Command{Cmd: "gleam dev", Note: fmt.Sprintf("then try http://127.0.0.1:8787/api/%s", plural)})
	default:
		notes = append(notes, fmt.Sprintf("Use it from your code with import %s/%s and %s.all(db).", project.FeatureImport(plural), plural, plural))
	}
	fmt.Print(tui.NextSteps(plural, commands, notes))
	return nil
}

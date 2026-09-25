package cli

import (
	"fmt"
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
	return cmd
}

func createPageCommand(live bool) *cobra.Command {
	var plain bool
	cmd := &cobra.Command{
		Use:   "page <controller> <page>",
		Short: "Create a page and add it to a controller, creating the controller if needed",
		Long: `Create src/<project>/<controller>/pages/<page>_page.gleam, a page ready for
your content, and add its route to <controller>_controller.gleam, which sits
beside the pages folder. The page is at /<controller>/<page>, or at /<controller> for a page
named index.

If the controller does not exist it is created and registered in
src/<project>.gleam. If it does, the page is added to it and served under
whatever path the controller already uses. A page that already exists, or
a route the controller already has, is refused and nothing is written.

A test that the page responds is written to test/<project>/<controller>/pages/.`,
		Example: "  howdy create page blog about\n  howdy create page blog index\n  howdy create page site contact",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreatePage(args[0], args[1], live, plain)
		},
	}
	if live {
		cmd.Use = "live_page <controller> <page>"
		cmd.Short = "Create a page with a live section and add it to a controller, creating the controller if needed"
		cmd.Long = `Create src/<project>/<controller>/pages/<page>_page.gleam, a page with a
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

A test that the page responds is written to test/<project>/<controller>/pages/.`
		cmd.Example = "  howdy create live_page dashboard index\n  howdy create live_page blog editor"
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func runCreatePage(controller, name string, live, plain bool) error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}
	project, err := generate.Open(dir)
	if err != nil {
		return err
	}
	page, err := generate.NewPage(project, controller, name, live)
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
	notes = append(notes, fmt.Sprintf("Add the page's content in %s.", page.File()))
	fmt.Print(tui.NextSteps(page.Module(), []tui.Command{
		{Cmd: "gleam dev", Note: "then open http://127.0.0.1:8787" + page.URL()},
		{Cmd: "gleam test", Note: "run the tests, including the new one for the page"},
	}, notes))
	return nil
}

func createControllerCommand() *cobra.Command {
	var plain bool
	cmd := &cobra.Command{
		Use:   "controller <plural> <singular> <field:type>...",
		Short: "Create a JSON controller with its record, database operations and migration",
		Long: `Create a slice of the current project: a folder under src/<project>/ named
after the plural, holding

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
name is a string. Every record also gets an integer id.`,
		Example: "  howdy create controller users user name:string age:int\n  howdy create controller blog_posts blog_post title body:string published:bool",
		Args:    cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreateSlice("controller", args[0], args[1], args[2:], plain)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func createRepoCommand() *cobra.Command {
	var plain bool
	cmd := &cobra.Command{
		Use:   "repo <plural> <singular> <field:type>...",
		Short: "Create a record with its database operations and migration, without a controller",
		Long: `Create a slice of the current project without routes: a folder under
src/<project>/ named after the plural, holding

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
			return runCreateSlice("repo", args[0], args[1], args[2:], plain)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

func createPagesCommand() *cobra.Command {
	var plain, api bool
	cmd := &cobra.Command{
		Use:   "pages <plural> <singular> <field:type>...",
		Short: "Create HTML pages to list, show, add, edit and delete a record, with its database operations and migration",
		Long: `Create a slice of the current project with pages for people: a folder under
src/<project>/ named after the plural, holding

  <singular>.gleam       the record: types, validation and JSON codecs
  <plural>.gleam         the database operations: all, find, create, update, delete
  <plural>_pages.gleam   the routes and pages under /<plural>

  GET  /<plural>             every record, in a table
  GET  /<plural>/new         the form for a new record
  POST /<plural>             create
  GET  /<plural>/:id         one record
  GET  /<plural>/:id/edit    the form to change it
  POST /<plural>/:id         update
  POST /<plural>/:id/delete  delete

The pages are built from Howdy UI as plain links and forms, so they work
without JavaScript; a form that fails validation is shown again with its
values and the errors next to the fields. The layout and form helpers go in
platform/pages.gleam and the styles in priv/public/assets/pages.css; both
are written once and shared by every set of pages.

It also writes a migration that creates the <plural> table and tests for
the database operations and the pages, then registers the pages in
src/<project>.gleam and the migration in platform/database.gleam. Pass
--api for the JSON controller of 'howdy create controller' as well.

Fields are name:type pairs. Types are string, int, float and bool; a bare
name is a string. Every record also gets an integer id.`,
		Example: "  howdy create pages users user name:string age:int\n  howdy create pages blog_posts blog_post title body published:bool --api",
		Args:    cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := "pages"
			if api {
				kind = "pages+api"
			}
			return runCreateSlice(kind, args[0], args[1], args[2:], plain)
		},
	}
	cmd.Flags().BoolVar(&api, "api", false, "also create the JSON controller under /api/<plural>")
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
			return runCreateSlice("migration", args[0], "", args[1:], plain)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print progress as plain lines instead of animating")
	return cmd
}

// runCreateSlice serves every subcommand; kind is "controller", "pages",
// "pages+api", "repo" or "migration", and singular is empty for a migration.
func runCreateSlice(kind, plural, singular string, fields []string, plain bool) error {
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
	notes = append(notes, fmt.Sprintf("Add validation rules in src/%s/%s/%s.gleam.", project.Name, plural, singular))
	commands := []tui.Command{{Cmd: "gleam test", Note: fmt.Sprintf("run the tests, including the new ones for %s", plural)}}
	switch {
	case slice.Pages:
		commands = append(commands, tui.Command{Cmd: "gleam dev", Note: fmt.Sprintf("then open http://127.0.0.1:8787/%s", plural)})
		notes = append(notes, fmt.Sprintf("The pages are in src/%s/%s/%s.gleam; the layout they share is src/%s/platform/pages.gleam.", project.Name, plural, slice.PagesModule(), project.Name))
	case slice.Routes:
		commands = append(commands, tui.Command{Cmd: "gleam dev", Note: fmt.Sprintf("then try http://127.0.0.1:8787/api/%s", plural)})
	default:
		notes = append(notes, fmt.Sprintf("Use it from your code with import %s/%s/%s and %s.all(db).", project.Name, plural, plural, plural))
	}
	fmt.Print(tui.NextSteps(plural, commands, notes))
	return nil
}

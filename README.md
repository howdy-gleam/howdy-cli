# howdy CLI

The command line for the [Howdy](https://github.com/mikeyjones/howdy) web framework, written in Go.

```sh
howdy new <project_name>
```

creates a Gleam project with a themed home page built from Howdy UI, static
assets, hot reload for development (`gleam dev`), and a test suite. By
default it also includes a SQLite database through Gloo with an example todo
list: a migration, a `todos` slice (record, database operations, JSON API at
`/api/todos`), and a live todo list on the home page rendered as a Lustre
server component. Pass `--database postgres` for the same project on
PostgreSQL: Gloo connects through a pog pool configured from `DATABASE_URL`,
and the project gets a `docker-compose.yml` that starts PostgreSQL with a
development and a test database. `--database sqlite` is the default, and
`--no-database` gives a page-only project. The layout is a trimmed-down
version of `howdy_example`.

Every project is also set up for development with the rest of Howdy:

- **Howdy Admin** (`howdy_admin`, a dev dependency) is mounted at `/_howdy`
  by `dev/<project>_dev.gleam`, so it only runs under `gleam dev`. It
  browses and edits the database and shows the recorded telemetry.
- **Howdy Telemetry** (`howdy_telemetry`) records every request under
  `gleam dev` for the admin, with the database Repo traced so each query is
  a span. `gleam run` sends traces over OTLP when
  `OTEL_EXPORTER_OTLP_ENDPOINT` is set, and records nothing otherwise.
- **The console**: [GSH](https://hexdocs.pm/gsh/) is a dev dependency with
  `[tools.gsh]` in `gleam.toml`, the app exposes its services through
  `howdy/console`, and `src/console.gleam` holds shortcuts.
  `gleam run -m gsh -- <project>_dev` starts the dev server inside a
  console; the project README also shows how to attach to one that is
  already running.

Pass `--minimal` to leave all three out.

Before writing anything it checks that `gleam` is installed (and warns if
`erl` is missing), writes the files, formats them with `gleam format`,
downloads dependencies,
and prints what to do next. Progress is animated on a terminal; use
`--plain` for line-by-line output, and `--skip-deps` to skip
`gleam deps download`. Howdy, Howdy UI and Howdy Dev use Git dependencies
from `https://github.com/mikeyjones/howdy.git`; Git must be installed to
download them. No local Howdy checkout is needed.

Projects depend on a Howdy release tag, never a branch, so every Howdy
package in a project comes from the same release. By default that is the
release this CLI's templates were written and tested against,
`scaffold.Release` in `internal/scaffold/scaffold.go`. Pass `--howdy 2.1.0`
to use another; a release the templates predate may need changes to the
generated code. When a Howdy release changes what a project needs, update
the templates and `scaffold.Release` together.

Project names follow Gleam's rules: lowercase letters, digits and
underscores, starting with a letter. A path such as `apps/my_site` is
accepted; the last segment becomes the project name.

## Adding a controller

Inside a project with a database:

```sh
howdy create controller users user name:string age:int
```

creates the slice `src/<project>/users/`:

| File | Holds |
| --- | --- |
| `user.gleam` | the `User` record, input validation, JSON codecs and the Gloo table |
| `users.gleam` | the database operations: `all`, `find`, `create`, `update`, `delete` |
| `users_controller.gleam` | the routes below |

| Method | Route | Result |
| --- | --- | --- |
| GET | `/api/users` | every user |
| GET | `/api/users/:id` | one user |
| POST | `/api/users` | create; 201 |
| PUT | `/api/users/:id` | replace every field; 200 |
| DELETE | `/api/users/:id` | 204 |

The first argument (plural) names the folder, table, routes and context
module; the second (singular) names the record module and type. Fields are
`name:type` with the types `string`, `int`, `float` and `bool`; a bare name
is a string, and every record gets an integer `id`. The command also writes
`migrations/create_users.gleam` with the next migration version and tests
in `test/<project>/users/` (`users_test.gleam` for the database operations,
`users_controller_test.gleam` for the routes), registers the controller
in `src/<project>.gleam` and the migration in `platform/database.gleam`,
and runs `gleam format`. If either of those two files no longer has the
shape `howdy new` gave it, it is left alone and the command prints what to
add by hand. Existing files are never overwritten. SQLite or PostgreSQL is
detected from `gleam.toml` (PostgreSQL if it depends on pog).

For pages that people use rather than a JSON API,

```sh
howdy create pages users user name:string age:int
```

takes the same arguments and writes an HTML controller in place of the JSON
one, `users_controller.gleam`, with its own test, and the pages it serves in
`users/pages/`:

| File | Holds |
| --- | --- |
| `users_controller.gleam` | the routes below, and what each does with a submitted form |
| `pages/index_page.gleam` | every user, in a table |
| `pages/show_page.gleam` | one user |
| `pages/new_page.gleam` | the form for a new user |
| `pages/edit_page.gleam` | the form to change one |
| `pages/helpers.gleam` | the form both use, and the path of a user |

| Method | Route | Page |
| --- | --- | --- |
| GET | `/users` | every user, in a table |
| GET | `/users/new` | the form for a new user |
| POST | `/users` | create, then show it |
| GET | `/users/:id` | one user |
| GET | `/users/:id/edit` | the form to change it |
| POST | `/users/:id` | update, then show it |
| POST | `/users/:id/delete` | delete, then back to the table |

The pages are Howdy UI rendered on the server as plain links and forms, so
they work without JavaScript (hence POST for update and delete). A form
that fails validation comes back with its values and an error under each
field. The pages render through the project's default layout, or the one
`--layout <name>` names. Form reading, redirects and the error page live in
`src/<project>/platform/pages.gleam`, written the first time only, and the
styles for tables and forms are added to `priv/public/assets/style.css` the
first time; both serve every set of pages and are safe to edit. Pass `--api` to get
the JSON controller as well, as `users_api_controller.gleam`.
`howdy create page users <name>` adds another page to the same controller,
with its route ahead of `/users/:id` so the id does not take it.

For data without a JSON API, reached from your own pages or other slices,

```sh
howdy create repo users user name:string email:string age:int
```

takes the same arguments and writes the same files except
`users_controller.gleam` and its test, and only registers the migration.

For only the table,

```sh
howdy create migration users name:string email:string age:int
```

writes `migrations/create_users.gleam` and adds it to the list in
`platform/database.gleam`, and nothing else. It takes the table name alone,
without the singular, so every field needs its type here.

For a single page, in any project,

```sh
howdy create page blog about
```

writes `src/<project>/blog/pages/about_page.gleam`, a page ready for your
content, and adds its route to `blog/blog_controller.gleam`, beside the
`pages` folder: the page is at
`/blog/about`, or at `/blog` for a page named `index`. If the controller does
not exist it is created and registered in `src/<project>.gleam`; if it does,
the page is added to its routes and served under the path the controller
already uses (so `howdy create page site contact` gives `/contact`). A page
whose file already exists, or whose path the controller already routes, is
refused and nothing is written. A test that the page responds goes in
`test/<project>/blog/pages/`.

```sh
howdy create live_page blog editor
```

does the same with a live section on the page: a Lustre server component
with its model, update and view in the page module, and its WebSocket
routed at `/blog/editor/live`.

## Layouts

`howdy new` writes `src/<project>/layouts/default_layout.gleam`: everything
around a page's content, from the HTML document with the theme and
stylesheets to a header with the site name and the theme toggle. Pages call
its `render(ctx, title, content)`, or `render_live` when they have live
sections, which also loads the script those connect with; a page without
them does not load it. The home page and every page the `create` commands
write use the default layout.

```sh
howdy create layout admin
```

writes `layouts/admin_layout.gleam`, a copy of the default to change, and

```sh
howdy create page admin index --layout admin
```

makes a page that renders through it. `--layout` works on `page`,
`live_page` and `pages`, and must name a layout that exists; without it the
default is used. A project made before layouts gets the default one the
first time a command needs it.

## Components

A component is a piece of view that pages reuse: a `view()` function that
returns an element.

```sh
howdy create component price_tag
howdy create component blog post_card
```

With one name the component is global, in
`src/<project>/components/price_tag_component.gleam`; with a controller
first it belongs to that controller, in
`src/<project>/blog/components/post_card_component.gleam`, and the
controller must exist. Pages add `price_tag_component.view()` to their
content.

```sh
howdy create live_component notifications
howdy create live_component blog comments
```

writes a live component instead: a Lustre server component with its model,
update and view, `app()` to start it, `socket(ctx)` to serve its WebSocket
and `mount()` to put it on a page, which renders with its layout's
`render_live`. The socket is routed for you: at
`/blog/components/comments/live` in the blog controller, or for a global
component at `/components/notifications/live` in
`components/components_controller.gleam`, which the first global live
component creates and registers in `src/<project>.gleam`. Either way the
route goes ahead of any `/:id` route that would take it.

Every component gets a test beside the others: that it renders, or for a
live one that it mounts and its socket answers. A component that exists
already is refused and nothing is written.

## Build

Requires Go 1.25+.

```sh
go build -o howdy .
./howdy new my_site
```

To install on your PATH:

```sh
go install .
```

## Layout

```text
main.go
internal/
├── cli/        cobra commands (root, new, create, dev)
├── generate/   `howdy create controller`, `pages`, `repo`, `migration`, `page`, `live_page`, `layout`, `component` and `live_component`: templates and registration
├── scaffold/   project validation and template rendering
│   └── templates/
│       ├── base/      the page-only project, rendered with text/template
│       ├── database/  files added or overridden when a database is included
│       ├── postgres/  files overriding database/ for --database postgres
│       └── full/      files only a project without --minimal has
├── tools/      finding and running gleam / erl
└── tui/        Bubble Tea step runner and styles
```

Templates mirror the generated project's layout. `__name__` in a path is
replaced by the project name and a trailing `.tmpl` is dropped. The
`database` set is rendered after `base` and wins for any path in both, and
`postgres` is rendered after `database` in the same way. Files that differ
by only a line or two between the databases stay in `database` and branch on
`{{.Database}}` (`sqlite` or `postgres`) instead. The admin, telemetry and
console are added to the shared files with `{{if .Full}}`, which is set
unless `--minimal` is given; `full` only holds files no other set has. The
published stylesheet `priv/public/assets/ui.css` is what `gleam run -m
tasks/css` produces for the default themes; update it when Howdy UI's
default styles change.

## Test

```sh
go test ./...
```

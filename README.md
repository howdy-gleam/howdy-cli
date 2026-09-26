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

Every project has a 404 page, `features/site/pages/not_found_page.gleam`, rendered
with its default layout. Middleware in `platform/not_found.gleam` shows it
for a 404 when the request's `Accept` header prefers HTML, as a browser's
does; API clients, `fetch` and curl get Howdy's JSON error, and a 404 page a
controller rendered itself is kept.

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

Pass `--api-only` for a JSON backend with no pages: the project leaves out
Howdy UI and Lustre, the home page, layouts, static files, the stylesheet
task and the HTML 404 page, and serves a health check at `GET /health`
(`src/<project>/features/health/health_controller.gleam`) instead. A request no
route matches gets Howdy's JSON 404 error through `howdy.not_found`. With a
database it keeps the todos JSON API, and it combines with `--minimal` and
`--database` as usual. In a project without `howdy_ui` in `gleam.toml` the
`create` commands for pages, layouts and components are refused;
`create controller`, `repo`, `migration` and `email` and every `add` work,
and `howdy add auth` mounts only auth's JSON API, with emails that carry
a code rather than a link to a page.

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

creates the slice `src/<project>/features/users/`:

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
in `test/<project>/features/users/` (`users_test.gleam` for the database operations,
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

writes `src/<project>/features/blog/pages/about_page.gleam`, a page ready for your
content, and adds its route to `blog/blog_controller.gleam`, beside the
`pages` folder: the page is at
`/blog/about`, or at `/blog` for a page named `index`. If the controller does
not exist it is created and registered in `src/<project>.gleam`; if it does,
the page is added to its routes and served under the path the controller
already uses (so `howdy create page site contact` gives `/contact`). A page
whose file already exists, or whose path the controller already routes, is
refused and nothing is written. A test that the page responds goes in
`test/<project>/features/blog/pages/`.

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
`src/<project>/features/blog/components/post_card_component.gleam`, and the
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

## Adding packages

`howdy create` writes a project's own code; `howdy add` installs a Howdy
package into it and wires it up. Every project's app is built from one
`Services` record in `src/<project>.gleam`, made by `main`, by `dev/` and by
the test support, and passed to `app(services)`; `add` puts what it installs
there, so controllers take it from `services`.

```sh
howdy add mail
```

adds `howdy_mail` (from the same release as the project's other Howdy
packages) and `smail` to `gleam.toml`, and writes:

| File | Holds |
| --- | --- |
| `src/<project>/platform/email.gleam` | the mailer `gleam run` uses, from the environment |
| `src/<project>/emails/welcome_email.gleam` | an example email written with smail, with its preview |
| `src/<project>/emails/previews.gleam` | every email built from sample data |
| `test/<project>/emails/previews_test.gleam` | checks every email can be sent |

It then adds `mailer` to the services. `gleam run` sends through
`SMTP_URL`, `RESEND_API_KEY` or `SENDGRID_API_KEY`, whichever is set, from
`MAIL_FROM`, and logs each email when none is. `gleam dev` keeps email in
an outbox that also writes `.eml` files to `tmp/mail` (added to
`.gitignore`), and when the project has the dev admin, `/_howdy` shows the
outbox and the previews under Mail. The tests get an in-memory outbox. A
file that no longer has the shape `howdy new` gave it is left alone, and the
command prints what to add by hand.

```sh
howdy add auth
```

adds sign-in and roles with `howdy_auth`. It needs the project's database,
as auth keeps its users, sessions and roles there through `howdy_database`,
and refuses a project without one. Sign-in tokens are emailed, so a project
without mail gets it as well. It writes `src/<project>/platform/accounts.gleam`,
which builds the app's `identity` (sign-in by a link or six-digit code
emailed to the address, and registration the same way) and `permissions`
(roles), and adds both to the services in `main`, the dev server and the
test support. `platform/database.gleam`'s `migrate` runs auth's migrations
after the app's own, so everything that migrates picks them up. The app
mounts auth's JSON API at `/api/auth` and its starter pages at `/auth`, and
with the dev admin, `/_howdy` gets users, groups and roles and auth's email
previews. Guard a controller with
`controller.guarded("/account", auth.required(services.identity))`. Set
`APP_ORIGIN` to the site's public address before deploying.

Once a project has auth, `--add-auth` on the `create` commands that write
a controller (`controller`, `pages`, `page`, `live_page` and
`live_component`) guards every route of the controller they create:

```sh
howdy create pages notes note body --add-auth
howdy create page account index --add-auth
```

The controller starts with `controller.guarded(prefix,
auth.required(identity))` and ends with `|> controller.build`. It takes the
app's identity, registered as `notes_controller.controller(db,
services.identity)`, and anyone not signed in gets a 401. Handlers find the
user in `ctx.guard.user`. Pages take any context, so they render behind the
guard. The tests sign in with `support.sign_in(db)`, which is added to
`test/<project>/support.gleam` the first time, and check that a request
without a session is refused.

Pages and live components added to a guarded controller later go behind
the guard without the flag: their routes go before `controller.build`. For
a controller that exists without a guard, `--add-auth` is refused rather
than leave its other routes open.

```sh
howdy add remote
```

adds typed calls between services with `howdy_remote`, and writes:

| File | Holds |
| --- | --- |
| `src/<project>/procedures.gleam` | the procedures the app serves, each a name, input and output; an example `ping` |
| `src/<project>/platform/rpc.gleam` | the server that answers them, and the token from `RPC_TOKEN` |
| `test/<project>/platform/rpc_test.gleam` | calls `ping` on the cluster and over HTTP |

`main` and the dev server start the procedure server, so the other nodes of
an Erlang cluster (run with `ERL_FLAGS="-sname <name> -setcookie <cookie>"`)
call it with `remote.cluster()`. The app also mounts
`POST /rpc/<procedure name>` ahead of its other routes, for callers that
send `authorization: Bearer <token>`. The token is `rpc_token` in the
services: `RPC_TOKEN` in `main` and the dev server, where an unset one
refuses every request, and a fixed one in the test support. Call another
service with `remote.call(target, procedure, input, timeout: 5000)`.

```sh
howdy add openapi
```

adds an OpenAPI 3.1 document with `howdy_openapi`, and writes:

| File | Holds |
| --- | --- |
| `src/<project>/platform/api_docs.gleam` | the document's title, version and security schemes, and `serve` |
| `test/<project>/platform/api_docs_test.gleam` | checks the document and its reference page are served |

The app serves the document at `/openapi.json` and a page to read it at
`/docs`, after its controllers so it documents them all and ahead of the
static catch-all. Controllers that `howdy create` adds later go before it
too. The document lists the routes written with `howdy/openapi/endpoint`;
existing controllers are left as they are, so it starts empty until a
route is moved to an endpoint. The dev admin needs no wiring: `admin.mount`
finds the document in the app and lists every endpoint under API, where
they can be called.

```sh
howdy create email password_reset
```

writes `emails/password_reset_email.gleam`, with `build(to:)` and
`preview()`, and adds the preview to `emails/previews.gleam`.

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
├── cli/        cobra commands (root, new, create, add, dev)
├── generate/   `howdy create controller`, `pages`, `repo`, `migration`, `page`, `live_page`, `layout`, `component`, `live_component` and `email`, and `howdy add mail`, `auth`, `remote` and `openapi`: templates and wiring
├── scaffold/   project validation and template rendering
│   └── templates/
│       ├── base/      the page-only project, rendered with text/template
│       ├── database/  files added or overridden when a database is included
│       ├── postgres/  files overriding database/ for --database postgres
│       ├── full/      files only a project without --minimal has
│       └── api/       files only a project with --api-only has
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
unless `--minimal` is given; `full` only holds files no other set has.
`--api-only` works the same way with `{{.API}}`, and its `api` set is
rendered last; the files that only serve pages (the site, layouts,
`priv/`, the stylesheet task and the HTML 404) are left out of an API
project by `uiOnly` in `internal/scaffold/templates.go`. The
published stylesheet `priv/public/assets/ui.css` is what `gleam run -m
tasks/css` produces for the default themes; update it when Howdy UI's
default styles change.

## Test

```sh
go test ./...
```

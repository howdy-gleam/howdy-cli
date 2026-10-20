# howdy CLI

The command line for the [Howdy](https://github.com/howdy-gleam/howdy) web framework, written in Go.

```sh
howdy new <project_name>
```

creates a Gleam project with a themed home page built from Howdy UI, static
assets, hot reload for development (`gleam dev`), and a test suite. By
default it also includes a PostgreSQL database through Gloo with an example
todo list: a migration, a `todos` slice (record, database operations, JSON API
at `/api/todos`), and a live todo list on the home page rendered as a Lustre
server component. Gloo connects through a pog pool configured from
`DATABASE_URL`, and the project gets a `docker-compose.yml` that starts
PostgreSQL with a development and a test database. `--database postgres` is
the default; pass `--database sqlite` for the same project on a SQLite file
instead, and `--no-database` for a page-only project. The layout is a trimmed-down
version of `howdy_example`.

Every project has a 404 page, `features/home/pages/not_found_page.gleam`, rendered
with its default layout. Middleware in `platform/not_found.gleam` shows it
for a 404 when the request's `Accept` header prefers HTML, as a browser's
does; API clients, `fetch` and curl get Howdy's JSON error, and a 404 page a
controller rendered itself is kept.

`main` starts the server in a supervision tree owned by an OTP application,
`<project>_server`, as the
[supervision guide](https://github.com/howdy-gleam/howdy/blob/v1.0.0/docs/guides/supervision.djot)
describes: a crashed server is restarted, and stopping the node, as
`SIGTERM` does, lets requests in flight finish. `howdy add` puts the
processes its packages keep running in the same tree, ahead of the server:
the feature flag keeper, the jobs runner and the remote procedure server.
In a project whose `main` still calls `howdy.start` they are started beside
it instead.

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

Every project also has an `AGENTS.md` and skills for coding agents, with
`CLAUDE.md` importing it, unless `--no-agents` is given: see
[Working with coding agents](#working-with-coding-agents).

Every project is ready to deploy:

- `GET /up` answers `{"status":"ok"}` while the app runs, mounted with
  `howdy.health_check`. It's answered ahead of the routes, middleware
  and request log.
- `main` starts the server in a supervision tree owned by an OTP
  application (`howdy.start_application`). On SIGTERM it stops taking
  connections and gives requests in flight 8 seconds to finish, inside
  `docker stop`'s 10.
- With a database, `migrate` runs the migrations in one transaction
  holding Howdy's migration lock, so replicas starting together take
  turns. `MIGRATE_ON_START=false` skips them at startup.
- A `Dockerfile` builds the app with `gleam export erlang-shipment` and
  runs it on the same `erlang:<otp>-alpine` image, using the Gleam and
  Erlang versions installed where `howdy new` ran.
- `.dockerignore` keeps `build/`, `.env` and local data out of the image.
- `bin/start` starts the app in the image with `run`, `migrate` (with a
  database) or `remote`, an Erlang shell inside it. It names the node from
  `HOWDY_NODE_NAME`, writes `HOWDY_COOKIE` to `~/.erlang.cookie`, and pins
  distribution to `HOWDY_DIST_PORT`.
- A PostgreSQL project's `docker-compose.yml` gets the app under the `app`
  profile.

`--no-docker` leaves out the Dockerfile, `.dockerignore` and `bin/start`;
`howdy add docker` adds them later.

Pass `--api-only` for a JSON backend with no pages: the project leaves out
Howdy UI and Lustre, the home page, layouts, static files, the stylesheet
task and the HTML 404 page. A request no route matches gets Howdy's JSON
404 error through `howdy.not_found`. With a
database it keeps the todos JSON API, and it combines with `--minimal` and
`--database` as usual. In a project without `howdy_ui` in `gleam.toml` the
`create` commands for pages, layouts and components are refused;
`create controller`, `repo`, `migration` and `email` and every `add` work,
and `howdy add auth` mounts only auth's JSON API, with emails that carry
a code rather than a link to a page.

Pass `--with` to set up more Howdy packages as the project is made, each
added and wired in exactly as the matching [`howdy add`](#adding-packages)
command would:

```sh
howdy new my_site --with auth,storage,jobs,notifications,i18n
```

| Package | Sets up |
| --- | --- |
| `mail` | `howdy_mail`: a mailer in the services, an example email, and the dev outbox |
| `auth` | `howdy_auth`: sign-in by emailed link or code, and roles; brings mail |
| `flags` | `howdy_flags`: feature flags kept in the database, `tasks/flags` |
| `storage` | `howdy_storage`: `files` in the services, an S3-compatible bucket or a local folder |
| `jobs` | `howdy_jobs`: background jobs in the database, a runner beside the server, `tasks/jobs` |
| `notifications` | `howdy_notifications`: an inbox per user at `/notifications`, emailed by jobs; brings mail and jobs |
| `i18n` | `howdy_i18n`: catalogues in `locales/`, a generated `messages` module, each request's locale |
| `openapi` | `howdy_openapi`: an OpenAPI document at `/openapi.json`, read at `/docs` |
| `remote` | `howdy_remote`: typed calls between services, over the cluster or `POST /rpc/...` |

They are added in that order whatever order they are given in, so mail is
shared, and with `auth` the notifications inbox asks it who is signed in.
`auth`, `flags`, `jobs` and `notifications` keep their data in the
database, so they are refused with `--no-database`. With `--dry-run` the
project is made in a temporary directory to list every file the packages
add.

Before writing anything it checks that `gleam` is installed (and warns if
`erl` is missing), writes the files, formats them with `gleam format`,
downloads dependencies,
and prints what to do next. Progress is animated on a terminal; use
`--plain` for line-by-line output, and `--skip-deps` to skip
`gleam deps download`. No local Howdy checkout is needed.

Every Howdy package in a project comes from the same Howdy release. By
default that is the release this CLI's templates were written and tested
against, `scaffold.Release` in `internal/scaffold/scaffold.go`. Pass
`--howdy 1.0.0` to use another; a release the templates predate may need
changes to the generated code. When a Howdy release changes what a project
needs, update the templates and `scaffold.Release` together.

The packages come from Hex: each one is written as a version range, from
its version in the release up to its next major
(`howdy_ui = ">= 1.0.0 and < 2.0.0"`). Howdy packages are versioned
separately, so the CLI keeps a copy of the Howdy repository's
`releases/releases.json` in `internal/scaffold/releases.json`; copy it over
with each release. For a release newer than that copy it reads
`releases/releases.json` at the release's tag on GitHub instead, so an
older CLI can still make a project on a newer release.

`--source git` takes them from `https://github.com/howdy-gleam/howdy.git`
at the release tag instead, never a branch, which needs Git installed; the
Dockerfile then installs Git too. Use it for a release that is not on Hex,
or with `repository` in the config file for a fork or private mirror.
`scaffold.DefaultSource` in `internal/scaffold/dependency.go` sets the
default.
`howdy add` follows the project: it reads whether `howdy` comes from Git or
Hex in `gleam.toml` and adds its package the same way.

Project names follow Gleam's rules: lowercase letters, digits and
underscores, starting with a letter. A path such as `apps/my_site` is
accepted; the last segment becomes the project name.

## Config file

Your own defaults for `howdy new` can live in an optional TOML file, so
you need not pass the same flags every time. howdy reads the first of
these there is:

| OS | Path |
| --- | --- |
| Linux | `$XDG_CONFIG_HOME/howdy/config.toml`, or `~/.config/howdy/config.toml` |
| macOS | `~/Library/Application Support/howdy/config.toml`, then `~/.config/howdy/config.toml` |
| Windows | `%AppData%\howdy\config.toml` |

Set `HOWDY_CONFIG` to read another file instead (handy for CI), or to
`none` to read none. `howdy config init` writes a file with every setting
commented out at howdy's own default, and `howdy config` (or `howdy config
--json`) shows which file is in use and where each default comes from.

```toml
[new]
source = "git"                # hex or git, as --source
howdy = "1.0.0"               # the Howdy release every package comes from, as --howdy
repository = "https://git.example.com/me/howdy.git"  # a fork or private mirror, for source = "git"
database = "sqlite"           # postgres, sqlite or none
with = ["mail", "auth"]       # packages to set up, as --with
minimal = false               # as --minimal
api_only = false              # as --api-only
agents = true                 # false for --no-agents
docker = true                 # false for --no-docker
skip_deps = false             # as --skip-deps
```

A flag on the command line always wins over the file: `--database
postgres` beats `database = "none"`, `--with ""` sets up no packages, and
a boolean is turned back with `--minimal=false` or `--no-agents=false`.
`howdy new --no-config` ignores the file altogether, and a project made
from it says which file it read. Every setting is checked, used or not,
and one howdy does not know is an error, so a misspelt key is not quietly
ignored. Leave `howdy` out to follow the release the templates are written
for as howdy is upgraded; set it to stay on one. Each Howdy package's
version comes from the release, so they always match. `howdy add` follows
the project rather than the file, adding each package from wherever
`gleam.toml` takes `howdy`, a custom `repository` included.

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
`users_controller_test.gleam` for the routes), a factory in
`dev/<project>/factories/user_factory.gleam` (see
[Test data and seeds](#test-data-and-seeds)), registers the controller
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

## Test data and seeds

Every record `howdy create controller`, `pages` and `repo` make gets a
factory in `dev/<project>/factories/`, beside the `gleam dev` entry point.
Code in `dev/` can be imported by tests and run with `gleam run -m`, but
`gleam run` and `gleam export erlang-shipment` leave it out, so test data
never ships:

```gleam
import my_app/factories/user_factory
import my_app/features/users/user.{UserInput}

let ada = user_factory.insert(db, UserInput(..user_factory.input(), name: "Ada"))
let others = user_factory.insert_many(db, 3, fn(input) { input })
```

`input()` is input that passes the record's validation, with strings
numbered (`"Name 1"`, `"Name 2"`) so values in unique columns stay apart;
change fields with Gleam's record update syntax. `insert` validates the
input and inserts it through the feature's `create`, crashing if either
fails, and `insert_many` inserts several, each changed by the function.
Edit `input()` when you add validation rules the samples break.

```sh
howdy db seed
howdy db seed --reset
```

migrates the database `.env` names and runs `dev/seed.gleam`, whose `seed`
function inserts the records to start with; new projects seed a few todos.
Seeding again inserts them again, so `--reset` drops every table first
(on PostgreSQL, the `public` schema), then migrates and seeds. It deletes
everything in that database, so never point `.env` at data you want to
keep. A project made before `howdy db seed` existed gets `dev/seed.gleam`
and `dev/seed/reset.gleam` the first time it runs. `howdy db migrate`
applies pending migrations without seeding, like `gleam run -m tasks/migrate`.

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
howdy add flags
```

adds feature flags with `howdy_flags`. It needs the project's database, as
the flags keep their settings there through `howdy_database`, and refuses a
project without one. It writes:

| File | Holds |
| --- | --- |
| `src/<project>/feature_flags.gleam` | the flags, with an example `early_access`, `all()`, and `start` and `migrate` |
| `src/tasks/flags.gleam` | manages them from a terminal: `gleam run -m tasks/flags help` |
| `test/<project>/feature_flags_test.gleam` | turns the example on for one user |

The running flags are `features` in the services, started in `main`, the
dev server and the test support, and `platform/database.gleam`'s `migrate`
runs their migrations after the app's own. With the dev admin, `/_howdy`
gets Flags: kill switches, rules, groups and history. Check a flag with
`flags.enabled(services.features, feature_flags.early_access(), for:
flags.user(id))`.

```sh
howdy add storage
```

adds file storage with `howdy_storage`, and writes:

| File | Holds |
| --- | --- |
| `src/<project>/platform/file_storage.gleam` | `from_env`, which makes the app's storage, and `serve`, which answers its URLs |
| `test/<project>/platform/file_storage_test.gleam` | stores and reads a file in memory |

The storage is `files` in the services. With `S3_BUCKET` (or `BUCKET_NAME`)
set it is that bucket, on AWS S3 or any S3-compatible service, reached with
the standard `AWS_*` variables; otherwise it is the folder `STORAGE_PATH`,
`storage/` unless set (added to `.gitignore`). The folder's signed URLs are
served at `/files`, ahead of the static catch-all, once `STORAGE_SECRET` is
set; without it the folder gives no URLs and `gleam run` warns. The test
support keeps files in memory. `.env.example` lists every variable.

```sh
howdy add jobs
```

adds background jobs with `howdy_jobs`. It needs the project's database,
where the jobs are kept, and refuses a project without one. It writes:

| File | Holds |
| --- | --- |
| `src/<project>/background_jobs.gleam` | the jobs, with an example `say_hello`, the `runner` that does them, and `migrate` |
| `src/tasks/jobs.gleam` | manages them from a terminal: `gleam run -m tasks/jobs help` |
| `test/<project>/background_jobs_test.gleam` | queues the example and does it with `jobs.drain` |

`src/<project>.gleam` gets `jobs_runner(services)`, which `main` and the
dev server start beside the server, and `platform/database.gleam`'s
`migrate` runs the jobs' migrations after the app's own. With the dev admin,
`/_howdy` gets Jobs: queues, schedules, and every job with its failures.
Queue a job in the transaction that needs it, `jobs.new(job, args) |>
jobs.insert(into: conn)`. The runner keeps the code it started with, so
restart `gleam dev` after changing `background_jobs.gleam`.

```sh
howdy add notifications
```

adds notifications with `howdy_notifications`: an inbox for each user,
kept in the database, pushed live to their open pages and emailed for the
kinds that call for it. It needs the project's database, and as email goes
out as background jobs, a project without mail or jobs gets them as well. It
writes `src/<project>/platform/notifier.gleam`, which makes the `notifier`
and serves each user's inbox at `/notifications` (the routes howdy_ui's
bell follows), and its test. The notifier is in the services in `main`, the
dev server and the test support, `jobs_runner` delivers its email, and its
migrations run after the app's own. The inbox must know who is signed in:
in a project with auth it asks `auth.required(services.identity)`; without,
it refuses every request until `routes` in `notifier.gleam` says.

```sh
howdy add i18n
```

adds translations with `howdy_i18n`, and writes:

| File | Holds |
| --- | --- |
| `locales/en.json` | the source catalogue, in ICU MessageFormat |
| `src/<project>/messages.gleam` | a function for each message, generated from `locales/` |
| `src/<project>/platform/locales.gleam` | the negotiator choosing each request's locale, and `translator(ctx)` |
| `test/<project>/messages_test.gleam` | checks the messages |

The app stamps every response with its locale (`content-language`, and
`vary` for caches), chosen from `?locale=`, the `locale` cookie and the
browser's languages. The dev server watches `locales/` and regenerates
`messages.gleam` before each build; otherwise run
`gleam run -m howdy/i18n/cli -- gen`, and add a language with
`gleam run -m howdy/i18n/cli -- add de`.

```sh
howdy add docker
```

adds the files `howdy new` writes for containers to a project made with
`--no-docker`, or before they existed: the `Dockerfile`, `.dockerignore`
and `bin/start`, for the Gleam and Erlang installed here. Files the project
already has are left alone. The image's `HEALTHCHECK` asks `GET /up`, so the
app gets `|> howdy.health_check(health.new())` after `howdy.new()` if it
lacks one. A `main` that starts the server outside an OTP application is
reported as a step by hand, since SIGTERM would cut off requests in flight.

```sh
howdy create email password_reset
```

writes `emails/password_reset_email.gleam`, with `build(to:)` and
`preview()`, and adds the preview to `emails/previews.gleam`.

## Working with coding agents

Every project gets an `AGENTS.md`, a guide for coding agents written for
the options it was made with: how to check a change, which `howdy`
commands to use rather than writing code by hand, where things go, and the
project's conventions. `CLAUDE.md` imports it for Claude Code. Each
`howdy add` appends a short section on the package it installs, once, to
an `AGENTS.md` that is there; a project made before the file existed is
left alone, so `howdy add` never adds them to a project made with
`--no-agents`.

Projects also get task guides as Claude Code skills in `.claude/skills/`,
which `AGENTS.md` lists so other agents can read them too:

| Skill | For | In |
| --- | --- | --- |
| `howdy-feature` | building a feature end to end with the generators | every project |
| `howdy-controllers` | routes, requests, responses, errors and middleware | every project |
| `howdy-testing` | `howdy/testing` and what to test | every project |
| `howdy-debugging` | `howdy check`, `howdy routes`, logs, the admin and console | every project |
| `howdy-database` | migrations, adding a column, Gloo queries; with `reference.md` | projects with a database |
| `howdy-pages` | Howdy UI, forms, live views, styles; with `components.md` | projects with pages |
| `howdy-auth` | guards, the signed-in user, roles, tests | written by `howdy add auth` |

They are templates in `internal/scaffold/templates/*/.claude/skills/` and
`internal/generate/templates/skill_*.md.tmpl`, and describe the APIs of
the release in `scaffold.Release`: update them with the templates.

Pass `--no-agents` to `howdy new` to leave out `AGENTS.md`, `CLAUDE.md`
and `.claude/skills/`.

Every `new`, `create` and `add` command takes

- `--json` to print one JSON object in place of the progress and closing
  notes: the files `created` and `modified`, `manual_steps` it could not
  do itself (such as registering a controller in a file whose shape it did
  not recognise), `warnings`, the `next` commands to run and `notes`. A
  command that fails prints `{"ok": false, "error": ...}` and exits with
  status 1.
- `--dry-run` to see which files it would create and modify without
  changing anything. The command runs on a copy of the project in a
  temporary directory, including `gleam format`, and the copy is then
  compared with the project; dependencies are not downloaded.

Two commands describe a project:

```sh
howdy routes         # every route: method, path, controller and file:line
howdy check          # gleam format --check, gleam build, deps, gleam test
```

`howdy routes` reads the sources rather than building the app: the
controllers registered in `app()` in `src/<project>.gleam`, in order, and
the routes each declares, with the mounts that packages serve (static
files, auth, the OpenAPI document) and what `dev/` adds under `gleam dev`.
`howdy check` runs every check, skipping `deps` and the tests when the
build fails, shows the output of the ones that fail and the compiler's
warnings, and exits with status 1 if any failed; `--fix` formats instead of
checking the formatting. Both take `--json`. `deps` fails when `src/`
imports a module from a package that only a dev dependency brings in, such
as `howdy_flags` with `howdy_admin`: `gleam build` and `gleam test` have
it, but a production build such as `gleam export erlang-shipment` does not.
It says the line to add to `[dependencies]`.

## Upgrading

```sh
howdy upgrade
```

replaces the running `howdy` with the latest release from
[GitHub Releases](https://github.com/howdy-gleam/howdy-cli/releases): it
downloads the build for your platform, checks it against the release's
`checksums.txt` and swaps it in place. Nothing changes when `howdy` is
already up to date.

- `howdy upgrade --check` only reports whether a newer release is out.
- `howdy upgrade --version 1.2.0` installs that release, older or newer.
- `howdy upgrade --force` reinstalls, and replaces a development build
  (`howdy --version` says `dev`), which otherwise has no version to compare.
- `--json` prints the current and target versions and whether it upgraded.

If `howdy` is installed somewhere you cannot write to, such as
`/usr/local/bin`, run it with `sudo`. Set `GITHUB_TOKEN` if GitHub rate
limits the lookup, or if the repository is private. Pre-releases (tags such as `v1.3.0-rc.1`) are never
picked as the latest; install one with `--version`.

## Releasing

Push a version tag:

```sh
git tag v1.3.0
git push origin v1.3.0
```

`.github/workflows/release.yml` runs [GoReleaser](https://goreleaser.com)
(`.goreleaser.yaml`), which runs the tests, builds `howdy` for Linux, macOS
and Windows on amd64 and arm64 with `cli.Version` set from the tag, and
publishes a GitHub release with an archive per platform
(`howdy_<os>_<arch>.tar.gz`, `.zip` on Windows) and `checksums.txt`.
`howdy upgrade` relies on those names (`upgrade.ArchiveName`) and on the
repository in `upgrade.Repo`, `howdy-gleam/howdy-cli`. To try the build
without publishing:

```sh
goreleaser release --snapshot --clean
```

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
├── cli/        cobra commands (root, new, create, add, dev, db, routes, check, upgrade, config), and the --json and --dry-run output
├── config/     finding and reading the optional config file of defaults for `howdy new`
├── generate/   `howdy create controller`, `pages`, `repo`, `migration`, `page`, `live_page`, `layout`, `component`, `live_component` and `email`, and `howdy add mail`, `auth`, `remote`, `openapi`, `flags`, `storage`, `jobs`, `notifications`, `i18n` and `docker`: templates and wiring
├── inspect/    reading a project's sources for `howdy routes`
├── scaffold/   project validation and template rendering
│   └── templates/
│       ├── base/      the page-only project, rendered with text/template
│       ├── database/  files added or overridden when a database is included
│       ├── postgres/  files overriding database/ for --database postgres
│       ├── full/      files only a project without --minimal has
│       ├── api/       files only a project with --api-only has
│       └── docker/    the Dockerfile, .dockerignore and bin/start, left out with --no-docker
├── tools/      finding and running gleam / erl
├── tui/        Bubble Tea step runner and styles
└── upgrade/    `howdy upgrade`: finding, verifying and installing a GitHub release
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
rendered after the others but `docker`, which `howdy add docker` also
renders on its own; the files that only serve pages (the site, layouts,
`priv/`, the stylesheet task and the HTML 404) are left out of an API
project by `uiOnly` in `internal/scaffold/templates.go`. The
published stylesheet `priv/public/assets/ui.css` is what `gleam run -m
tasks/css` produces for the default themes; update it when Howdy UI's
default styles change.

## Test

```sh
go test ./...
```

// Package generate adds code to an existing Howdy project.
package generate

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templates embed.FS

// Project is an existing Howdy project with a database.
type Project struct {
	// Dir is the directory holding gleam.toml.
	Dir string
	// Name is the Gleam project name.
	Name string
	// Database is "sqlite" or "postgres".
	Database string
	// HowdyDatabase reports whether the project depends on howdy_database,
	// whose `database.returning` replaces the helper older projects carry
	// in each context.
	HowdyDatabase bool
}

var (
	projectName = regexp.MustCompile(`(?m)^name\s*=\s*"([a-z][a-z0-9_]*)"`)
	dependency  = func(name string) *regexp.Regexp { return regexp.MustCompile(`(?m)^` + name + `\s*=`) }
)

// Load reads gleam.toml in dir and works out which database the project uses:
// PostgreSQL if it depends on pog, otherwise SQLite if it depends on Gloo.
func Load(dir string) (*Project, error) {
	toml, err := os.ReadFile(filepath.Join(dir, "gleam.toml"))
	if err != nil {
		return nil, err
	}
	m := projectName.FindSubmatch(toml)
	if m == nil {
		return nil, errors.New("could not find the project name in gleam.toml")
	}
	p := &Project{Dir: dir, Name: string(m[1]), HowdyDatabase: dependency("howdy_database").Match(toml)}
	switch {
	case !dependency("gloo").Match(toml):
		return nil, errors.New("this project has no database: 'howdy create controller' needs Gloo, which 'howdy new' sets up unless --no-database is passed")
	case dependency("pog").Match(toml):
		p.Database = "postgres"
	default:
		p.Database = "sqlite"
	}
	return p, nil
}

// Slice is a feature folder to generate: a record, its database operations,
// a migration and tests, with or without a JSON controller.
type Slice struct {
	Project *Project
	// Plural names the folder, the table, the routes and the context
	// module, e.g. "users".
	Plural string
	// Singular names the record module, e.g. "user".
	Singular string
	// Type is the record's type name, e.g. "User".
	Type   string
	Fields []Field
	// Routes adds the JSON controller and its test.
	Routes bool
	// Pages adds the HTML pages and their test, and the shared helpers
	// they need if the project does not have them yet.
	Pages bool
	// MigrationOnly leaves out everything but the migration.
	MigrationOnly bool
	// Version is the migration version, one above the highest in the project.
	Version int
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Gleam keywords cannot name a module or a field.
var gleamKeywords = words(`
	as assert auto case const delegate derive echo else fn if implement import
	let macro opaque panic pub test todo type use`)

// Gloo's migrations do not quote identifiers, so SQL keywords cannot name the
// table (the plural) or a column. The singular only names Gleam code, so
// "orders order" and "users user" are fine.
var sqlKeywords = words(`
	all and any asc between by check column constraint create default delete
	desc distinct drop end exists from group having in index insert into is
	join key limit not null offset on or order primary references select set
	table to union unique update user values where`)

// Modules the generated code imports, which a slice module of the same name
// would clash with, and the variable the templates use for a record.
var moduleNames = words(`
	body controller database decode errors howdy int item json migration param query
	repo result schema service sql support testing validate value`)

// Variables and the column the templates define themselves.
var fieldNames = words(`ctx db errors form id input item items path`)

// Type names the generated record would shadow.
var reservedTypes = words(`Int Float String Bool List Result Nil BitArray Option Repo`)

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// NewMigration validates a table name and its fields for a migration on
// its own. Fields must be written name:type here: a bare word is more likely
// the singular that the other commands take than a string column.
func NewMigration(p *Project, table string, fields []string) (*Slice, error) {
	for _, arg := range fields {
		if !strings.Contains(arg, ":") {
			return nil, fmt.Errorf("%q has no type: write %s:string for a column, or leave it out if it is the record name, which 'create migration' does not take", arg, arg)
		}
	}
	c, err := newSlice(p, table, fields)
	if err != nil {
		return nil, err
	}
	c.MigrationOnly = true
	return c, nil
}

// NewSlice validates the names and fields, given as name:type pairs.
func NewSlice(p *Project, plural, singular string, fields []string) (*Slice, error) {
	if !identifier.MatchString(singular) {
		return nil, invalidName(singular)
	}
	if plural == singular {
		return nil, fmt.Errorf("the controller name and the record name are both %q; they name two modules in one folder, so they must differ (for example users and user)", plural)
	}
	for _, name := range []string{plural, singular} {
		switch {
		case gleamKeywords[name]:
			return nil, fmt.Errorf("%q is a Gleam keyword; pick another name", name)
		case moduleNames[name]:
			return nil, fmt.Errorf("%q would clash with a module the generated code imports; pick another name", name)
		}
	}
	if reservedTypes[pascal(singular)] {
		return nil, fmt.Errorf("%q would shadow Gleam's %s type; pick another name", singular, pascal(singular))
	}
	c, err := newSlice(p, plural, fields)
	if err != nil {
		return nil, err
	}
	c.Singular, c.Type = singular, pascal(singular)
	return c, nil
}

func invalidName(name string) error {
	return fmt.Errorf("%q is not a valid name: use lowercase letters, digits and underscores, starting with a letter", name)
}

// newSlice checks what every command needs: the table, the columns and the
// migration version.
func newSlice(p *Project, plural string, fields []string) (*Slice, error) {
	if !identifier.MatchString(plural) {
		return nil, invalidName(plural)
	}
	if sqlKeywords[plural] {
		return nil, fmt.Errorf("%q is an SQL keyword and cannot name the table; pick another name", plural)
	}
	c := &Slice{Project: p, Plural: plural}
	if len(fields) == 0 {
		return nil, errors.New("give at least one field, for example name:string")
	}
	seen := map[string]bool{}
	for _, arg := range fields {
		f, err := parseField(arg)
		if err != nil {
			return nil, err
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("field %q is given twice", f.Name)
		}
		seen[f.Name] = true
		f.database = p.Database
		c.Fields = append(c.Fields, f)
	}
	version, err := p.nextMigrationVersion()
	if err != nil {
		return nil, err
	}
	c.Version = version
	return c, nil
}

func pascal(name string) string {
	var b strings.Builder
	for _, part := range strings.Split(name, "_") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

var migrationVersion = regexp.MustCompile(`version:\s*([0-9_]+)`)

func (p *Project) nextMigrationVersion() (int, error) {
	paths, err := filepath.Glob(filepath.Join(p.Dir, "src", p.Name, "migrations", "*.gleam"))
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		for _, m := range migrationVersion.FindAllSubmatch(src, -1) {
			n, err := strconv.Atoi(strings.ReplaceAll(string(m[1]), "_", ""))
			if err == nil && n > highest {
				highest = n
			}
		}
	}
	return highest + 1, nil
}

var funcs = template.FuncMap{"inc": func(i int) int { return i + 1 }}

// File is a rendered file, with a path relative to the project directory.
type File struct {
	Path    string
	Content []byte
	// Shared files serve every slice, so one that exists is kept as it is
	// rather than being an error.
	Shared bool
}

// Files renders the slice. Tests are left out of a project without
// test/<project>/support.gleam, which they rely on.
func (c *Slice) Files() ([]File, error) {
	p := c.Project
	slice := filepath.Join("src", p.Name, c.Plural)
	type target struct {
		template, path string
		shared         bool
	}
	var targets []target
	if !c.MigrationOnly {
		targets = append(targets,
			target{template: "record.gleam.tmpl", path: filepath.Join(slice, c.Singular+".gleam")},
			target{template: "context.gleam.tmpl", path: filepath.Join(slice, c.Plural+".gleam")},
		)
	}
	targets = append(targets, target{template: "migration.gleam.tmpl", path: filepath.Join("src", p.Name, "migrations", c.MigrationModule()+".gleam")})
	if c.Routes {
		targets = append(targets, target{template: "controller.gleam.tmpl", path: filepath.Join(slice, c.ControllerModule()+".gleam")})
	}
	if c.Pages {
		targets = append(targets,
			target{template: "pages.gleam.tmpl", path: filepath.Join(slice, c.PagesModule()+".gleam")},
			target{"pages_helpers.gleam.tmpl", filepath.Join("src", p.Name, "platform", "pages.gleam"), true},
			target{"pages.css.tmpl", filepath.Join("priv", "public", "assets", "pages.css"), true},
		)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "test", p.Name, "support.gleam")); err == nil && !c.MigrationOnly {
		tests := filepath.Join("test", p.Name, c.Plural)
		targets = append(targets, target{template: "context_test.gleam.tmpl", path: filepath.Join(tests, c.Plural+"_test.gleam")})
		if c.Routes {
			targets = append(targets, target{template: "controller_test.gleam.tmpl", path: filepath.Join(tests, c.ControllerModule()+"_test.gleam")})
		}
		if c.Pages {
			targets = append(targets, target{template: "pages_test.gleam.tmpl", path: filepath.Join(tests, c.PagesModule()+"_test.gleam")})
		}
	}
	var files []File
	for _, t := range targets {
		tmpl, err := template.New(t.template).Funcs(funcs).Option("missingkey=error").ParseFS(templates, "templates/"+t.template)
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, c); err != nil {
			return nil, fmt.Errorf("template %s: %w", t.template, err)
		}
		files = append(files, File{Path: t.path, Content: out.Bytes(), Shared: t.shared})
	}
	return files, nil
}

// Write creates the files, refusing to replace any that exist, except that
// shared files already in the project are kept. It returns the paths written.
func (c *Slice) Write() ([]string, error) {
	files, err := c.Files()
	if err != nil {
		return nil, err
	}
	exists := map[string]bool{}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(c.Project.Dir, f.Path)); err == nil {
			if !f.Shared {
				return nil, fmt.Errorf("%s already exists; nothing was written", f.Path)
			}
			exists[f.Path] = true
		}
	}
	var written []string
	for _, f := range files {
		if exists[f.Path] {
			continue
		}
		target := filepath.Join(c.Project.Dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(target, f.Content, 0o644); err != nil {
			return written, err
		}
		written = append(written, f.Path)
	}
	return written, nil
}

// MigrationModule is the migration's module name, e.g. "create_users".
func (c *Slice) MigrationModule() string { return "create_" + c.Plural }

// ControllerModule is the controller's module name, e.g. "users_controller".
func (c *Slice) ControllerModule() string { return c.Plural + "_controller" }

// Postgres reports whether the project uses PostgreSQL rather than SQLite.
func (c *Slice) Postgres() bool { return c.Project.Database == "postgres" }

// Columns is the quoted column list for RETURNING, id first.
func (c *Slice) Columns() string {
	cols := []string{`\"id\"`}
	for _, f := range c.Fields {
		cols = append(cols, `\"`+f.Name+`\"`)
	}
	return strings.Join(cols, ", ")
}

// HasKind reports whether any field has the kind, so templates can import
// only what they use.
func (c *Slice) HasKind(kind string) bool {
	for _, f := range c.Fields {
		if string(f.Kind) == kind {
			return true
		}
	}
	return false
}

// HasNonFloat reports whether any field is sent through gloo/sql.
func (c *Slice) HasNonFloat() bool {
	return c.HasKind("string") || c.HasKind("int") || c.HasKind("bool")
}

// FirstString is the first string field, used by the validation test.
func (c *Slice) FirstString() *Field {
	for i := range c.Fields {
		if c.Fields[i].Kind == String {
			return &c.Fields[i]
		}
	}
	return nil
}

// PagesModule is the HTML controller's module name, e.g. "users_pages".
func (c *Slice) PagesModule() string { return c.Plural + "_pages" }

// Title, SingularTitle, Label and PluralLabel name the slice for people:
// "Blog posts", "Blog post", "blog post" and "blog posts".
func (c *Slice) Title() string         { return humanize(c.Plural, true) }
func (c *Slice) SingularTitle() string { return humanize(c.Singular, true) }
func (c *Slice) Label() string         { return humanize(c.Singular, false) }
func (c *Slice) PluralLabel() string   { return humanize(c.Plural, false) }

// ErrorsArg names the errors parameter of the pages' fields function, which
// checkboxes do not use.
func (c *Slice) ErrorsArg() string {
	if c.HasNonBool() {
		return "errors"
	}
	return "_errors"
}

// HasNonBool reports whether any field is typed into an input.
func (c *Slice) HasNonBool() bool {
	return c.HasKind("string") || c.HasKind("int") || c.HasKind("float")
}

// Probe is the field the pages test looks for in the HTML: the first string
// if there is one, since its sample text is the least likely to appear by
// chance.
func (c *Slice) Probe() *Field {
	if f := c.FirstString(); f != nil {
		return f
	}
	return &c.Fields[0]
}

// Invalid is a form that must be rejected, for the pages test, or nil when
// every form is valid because all fields are checkboxes.
type Invalid struct{ Pairs string }

func (c *Slice) Invalid() *Invalid {
	var bad *Field
	for i := range c.Fields {
		if c.Fields[i].Kind != Bool {
			bad = &c.Fields[i]
			break
		}
	}
	if bad == nil {
		return nil
	}
	var pairs strings.Builder
	for _, f := range c.Fields {
		if f.Name == bad.Name {
			// Blank fails not_empty for a string and parsing for a number.
			fmt.Fprintf(&pairs, "#(%q, \" \"), ", f.Name)
		} else {
			pairs.WriteString(f.FormPair(false))
		}
	}
	return &Invalid{Pairs: pairs.String()}
}

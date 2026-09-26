package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Register wires the generated slice into the project: the migration into
// platform/database.gleam and any controllers into the app in
// src/<project>.gleam. The edits rely on the shape 'howdy new' gives those
// files; for each one that no longer matches, the returned notes say what
// to add by hand.
func (c *Slice) Register() (notes []string) {
	p := c.Project
	root := filepath.Join("src", p.Name+".gleam")
	var modules []string
	if c.Routes {
		modules = append(modules, c.ControllerModule())
	}
	if c.Pages {
		modules = append(modules, c.PagesModule())
	}
	for _, module := range modules {
		controller := module + ".controller(db)"
		if c.Auth {
			controller = module + ".controller(db, services.identity)"
		}
		err := edit(filepath.Join(p.Dir, root),
			"import "+p.FeatureImport(c.Plural)+"/"+module,
			// Before the first controller, so it stays ahead of the static
			// catch-all.
			regexp.MustCompile(`(?m)^(\s*)howdy\.new\(\)\n`),
			"${0}${1}|> howdy.controller("+controller+")\n",
		)
		if err != nil {
			notes = append(notes, fmt.Sprintf("Register the controller in %s: import %s/%s and add |> howdy.controller(%s) before the static controller.",
				root, p.FeatureImport(c.Plural), module, controller))
		}
	}

	database := filepath.Join("src", p.Name, "platform", "database.gleam")
	err := edit(filepath.Join(p.Dir, database),
		fmt.Sprintf("import %s/migrations/%s", p.Name, c.MigrationModule()),
		// The last migration in the list given to runner.run.
		regexp.MustCompile(`(?s)(runner\.run\(\s*db,\s*\[.*?\.migration\(\))(,?\s*\])`),
		"${1}, "+c.MigrationModule()+".migration()${2}",
	)
	if err != nil {
		notes = append(notes, fmt.Sprintf("Register the migration in %s: import %s/migrations/%s and add %s.migration() to the list.",
			database, p.Name, c.MigrationModule(), c.MigrationModule()))
	}
	return notes
}

var lastImport = regexp.MustCompile(`(?m)^import [^\n]*\n`)

// edit adds an import after the file's last import and applies one
// replacement, or changes nothing if the pattern is not found.
func edit(path, importLine string, pattern *regexp.Regexp, replacement string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(src)
	loc := pattern.FindStringIndex(text)
	imports := lastImport.FindAllStringIndex(text, -1)
	if loc == nil || len(imports) == 0 {
		return fmt.Errorf("%s does not have the expected shape", path)
	}
	text = text[:loc[0]] + pattern.ReplaceAllString(text[loc[0]:loc[1]], replacement) + text[loc[1]:]
	if !strings.Contains(text, importLine+"\n") {
		at := imports[len(imports)-1][1]
		text = text[:at] + importLine + "\n" + text[at:]
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

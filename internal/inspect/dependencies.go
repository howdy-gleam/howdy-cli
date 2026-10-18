package inspect

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DevOnlyImport is a module that src/ imports from a package only the dev
// dependencies bring in. `gleam build` and `gleam test` compile with the
// dev dependencies, so they accept it, but a production build, such as
// `gleam export erlang-shipment`, leaves the package out and fails.
type DevOnlyImport struct {
	// File and Line are where the import is, relative to the project.
	File string `json:"file"`
	Line int    `json:"line"`
	// Module is the imported module, such as "howdy/flags".
	Module string `json:"module"`
	// Package is the package the module is in, such as "howdy_flags".
	Package string `json:"package"`
	// Fix is what puts the package in [dependencies]: the line to add to
	// gleam.toml for a package from git or a path, or the `gleam add`
	// command for one from Hex.
	Fix string `json:"fix"`
}

// DescribeDevOnlyImports explains found, a package at a time: the imports
// of it and what to add to gleam.toml.
func DescribeDevOnlyImports(found []DevOnlyImport) string {
	var order []string
	imports := map[string][]DevOnlyImport{}
	for _, f := range found {
		if _, ok := imports[f.Package]; !ok {
			order = append(order, f.Package)
		}
		imports[f.Package] = append(imports[f.Package], f)
	}
	var b strings.Builder
	for i, name := range order {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s is imported in src but is not in [dependencies]: only the dev dependencies bring it in, so a production build such as gleam export leaves it out.\n", name)
		for _, f := range imports[name] {
			fmt.Fprintf(&b, "  %s:%d imports %s\n", f.File, f.Line, f.Module)
		}
		if fix := imports[name][0].Fix; strings.HasPrefix(fix, "gleam add ") {
			fmt.Fprintf(&b, "Add it with: %s\n", fix)
		} else {
			fmt.Fprintf(&b, "Add it to [dependencies] in gleam.toml:\n  %s\n", fix)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// manifestPackage is one package locked in manifest.toml.
type manifestPackage struct {
	name, source, repo, commit, path string
	requirements                     []string
}

var (
	sectionHeader  = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)
	tableKey       = regexp.MustCompile(`^\s*([a-z][a-z0-9_]*)\s*=`)
	manifestEntry  = regexp.MustCompile(`^\s*\{\s*name\s*=\s*"([^"]+)"`)
	manifestString = func(key string) *regexp.Regexp {
		return regexp.MustCompile(`\b` + key + `\s*=\s*"([^"]*)"`)
	}
	manifestSource       = manifestString("source")
	manifestRepo         = manifestString("repo")
	manifestCommit       = manifestString("commit")
	manifestPath         = manifestString("path")
	manifestRequirements = regexp.MustCompile(`\brequirements\s*=\s*\[([^\]]*)\]`)
	quoted               = regexp.MustCompile(`"([^"]*)"`)
	gitDependency        = regexp.MustCompile(`git\s*=\s*"([^"]*)"[^}]*\bref\s*=\s*"([^"]*)"`)
)

// DevOnlyImports lists the imports in the src/ of the project in dir of
// modules from packages that only its dev dependencies bring in, in file
// and line order. It reads gleam.toml, manifest.toml and the packages
// `gleam build` downloaded into build/packages; a package that is not
// there yet is not looked at.
func DevOnlyImports(dir string) ([]DevOnlyImport, error) {
	toml, err := os.ReadFile(filepath.Join(dir, "gleam.toml"))
	if err != nil {
		return nil, err
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.toml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sections := tomlSections(string(toml))
	packages := manifestPackages(string(manifest))

	// Everything a production build has: the dependencies and theirs.
	production := map[string]bool{}
	queue := append([]string(nil), sections["dependencies"]...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if production[name] {
			continue
		}
		production[name] = true
		if p, ok := packages[name]; ok {
			queue = append(queue, p.requirements...)
		}
	}

	// The modules of every other package.
	owner := map[string]string{}
	for name, p := range packages {
		if production[name] {
			continue
		}
		src := filepath.Join(dir, "build", "packages", name, "src")
		if p.source == "local" {
			src = filepath.Join(dir, p.path, "src")
		}
		for _, module := range gleamModules(src) {
			owner[module] = name
		}
	}
	if len(owner) == 0 {
		return nil, nil
	}

	var found []DevOnlyImport
	src := filepath.Join(dir, "src")
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".gleam") {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		rel, _ := filepath.Rel(dir, path)
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for line := 1; scanner.Scan(); line++ {
			m := importLine.FindStringSubmatch(scanner.Text())
			if m == nil {
				continue
			}
			if name, ok := owner[m[1]]; ok {
				found = append(found, DevOnlyImport{
					File:    filepath.ToSlash(rel),
					Line:    line,
					Module:  m[1],
					Package: name,
					Fix:     dependencyFix(packages[name], string(toml)),
				})
			}
		}
		return scanner.Err()
	})
	if os.IsNotExist(err) {
		err = nil
	}
	sort.SliceStable(found, func(a, b int) bool {
		if found[a].File != found[b].File {
			return found[a].File < found[b].File
		}
		return found[a].Line < found[b].Line
	})
	return found, err
}

// tomlSections maps each table of a TOML file to the keys set in it.
func tomlSections(toml string) map[string][]string {
	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(toml, "\n") {
		if m := sectionHeader.FindStringSubmatch(line); m != nil {
			current = strings.TrimSpace(m[1])
			continue
		}
		if m := tableKey.FindStringSubmatch(line); m != nil {
			sections[current] = append(sections[current], m[1])
		}
	}
	return sections
}

// manifestPackages reads the packages of manifest.toml, which Gleam writes
// one to a line.
func manifestPackages(manifest string) map[string]manifestPackage {
	packages := map[string]manifestPackage{}
	for _, line := range strings.Split(manifest, "\n") {
		m := manifestEntry.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		p := manifestPackage{name: m[1]}
		for key, re := range map[*string]*regexp.Regexp{
			&p.source: manifestSource, &p.repo: manifestRepo, &p.commit: manifestCommit, &p.path: manifestPath,
		} {
			if v := re.FindStringSubmatch(line); v != nil {
				*key = v[1]
			}
		}
		if r := manifestRequirements.FindStringSubmatch(line); r != nil {
			for _, q := range quoted.FindAllStringSubmatch(r[1], -1) {
				p.requirements = append(p.requirements, q[1])
			}
		}
		packages[p.name] = p
	}
	return packages
}

// gleamModules lists the modules under a package's src directory, such as
// "howdy/flags" for src/howdy/flags.gleam.
func gleamModules(src string) []string {
	var modules []string
	filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".gleam") {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		modules = append(modules, strings.TrimSuffix(filepath.ToSlash(rel), ".gleam"))
		return nil
	})
	return modules
}

// dependencyFix says how to add a package to [dependencies]. A package
// from git takes the ref the project gives its other packages from the same
// repository, as every Howdy package must come from one release.
func dependencyFix(p manifestPackage, toml string) string {
	switch p.source {
	case "git":
		ref := p.commit
		for _, m := range gitDependency.FindAllStringSubmatch(toml, -1) {
			if m[1] == p.repo {
				ref = m[2]
				break
			}
		}
		line := fmt.Sprintf("%s = { git = %q, ref = %q", p.name, p.repo, ref)
		if p.path != "" {
			line += fmt.Sprintf(", path = %q", p.path)
		}
		return line + " }"
	case "local":
		return fmt.Sprintf("%s = { path = %q }", p.name, p.path)
	default:
		return "gleam add " + p.name
	}
}

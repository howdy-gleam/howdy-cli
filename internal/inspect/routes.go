// Package inspect reads a Howdy project's sources to describe it, without
// compiling or running it.
package inspect

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Route is one route the app serves.
type Route struct {
	// Method is GET, POST and so on, or * for a mount, which serves
	// whatever paths the package behind it defines under Path.
	Method string `json:"method"`
	Path   string `json:"path"`
	// File and Line are where the route is defined, relative to the
	// project; for a mount, where it is registered.
	File string `json:"file"`
	Line int    `json:"line"`
	// Controller is the call that makes the controller, such as
	// "users_controller.controller".
	Controller string `json:"controller"`
	// Guarded reports a controller made with controller.guarded, whose
	// routes need what the guard checks, such as someone signed in.
	Guarded bool `json:"guarded"`
	// Dev reports a route only `gleam dev` serves.
	Dev bool `json:"dev,omitempty"`
	// Mount describes what serves the paths under a mount, such as
	// "static files from priv/public".
	Mount string `json:"mount,omitempty"`
}

var (
	importLine = regexp.MustCompile(`(?m)^import ([a-z0-9_/]+)(?:\.\{[^}]*\})?(?: as ([a-z0-9_]+))?`)
	// A controller registered with the app, |> howdy.controller(<call>),
	// the OpenAPI document that 'howdy add openapi' serves with
	// |> api_docs.serve, or a health check, |> howdy.health_check(...).
	registration = regexp.MustCompile(`howdy\.controller\(\s*([a-z0-9_]+)\.([a-z0-9_]+)\(|\|>\s*([a-z0-9_]+)\.serve\b|howdy\.(health_check)\(`)
	healthPath   = regexp.MustCompile(`\.at\(\s*(?:path:\s*)?"([^"]*)"`)
	documentPath = regexp.MustCompile(`(?:pub )?const document_path = "([^"]*)"`)
	prefixCall   = regexp.MustCompile(`controller\.(new|guarded)\(\s*"([^"]*)"`)
	routeCall    = regexp.MustCompile(`(?:controller|endpoint)\.(?:(get|post|put|patch|delete)\(\s*"([^"]*)"|route\(\s*http\.(\w+),\s*"([^"]*)")`)
	atArgument   = regexp.MustCompile(`(?:at|api_at):\s*"([^"]*)"`)
	firstString  = regexp.MustCompile(`^\s*"([^"]*)"`)
	adminMount   = regexp.MustCompile(`admin\.mount\(`)
	adminPrefix  = regexp.MustCompile(`admin\.at\(\s*"([^"]*)"`)
)

// Routes lists the routes of the project named name in dir, in the order
// the app registers their controllers, with the routes each controller
// declares in order. It reads the app function in src/<name>.gleam and
// each controller module it registers; a controller from a package, such
// as auth's, is listed as a mount at the path it is given. The dev entry
// point's additions, such as the admin, are marked Dev.
func Routes(dir, name string) ([]Route, error) {
	rootFile := filepath.ToSlash(filepath.Join("src", name+".gleam"))
	root, err := os.ReadFile(filepath.Join(dir, rootFile))
	if err != nil {
		return nil, err
	}
	routes := registered(dir, rootFile, string(root), "app", false)

	devFile := filepath.ToSlash(filepath.Join("dev", name+"_dev.gleam"))
	if dev, err := os.ReadFile(filepath.Join(dir, devFile)); err == nil {
		text := string(dev)
		routes = append(routes, registered(dir, devFile, text, "main", true)...)
		if loc := adminMount.FindStringIndex(text); loc != nil {
			prefix := "/_howdy"
			if m := adminPrefix.FindStringSubmatch(text); m != nil {
				prefix = m[1]
			}
			routes = append(routes, Route{
				Method: "*", Path: join(prefix, ""), File: devFile, Line: lineOf(text, loc[0]),
				Controller: "admin.mount", Dev: true, Mount: "the Howdy admin",
			})
		}
	}
	return routes, nil
}

// registered lists the routes of every controller registered in the
// function fn of a module, whose source is text.
func registered(dir, file, text, fn string, dev bool) []Route {
	start, end := function(text, fn)
	if start < 0 {
		return nil
	}
	imports := map[string]string{}
	for _, m := range importLine.FindAllStringSubmatch(text, -1) {
		alias := m[2]
		if alias == "" {
			alias = m[1][strings.LastIndex(m[1], "/")+1:]
		}
		imports[alias] = m[1]
	}
	var routes, health []Route
	body := text[start:end]
	for _, loc := range registration.FindAllStringSubmatchIndex(body, -1) {
		if loc[6] >= 0 {
			routes = append(routes, apiDocs(dir, file, imports[body[loc[6]:loc[7]]], lineOf(text, start+loc[0]), dev)...)
			continue
		}
		if loc[8] >= 0 {
			// Answered at /up unless health.at gives another path.
			path := "/up"
			if m := healthPath.FindStringSubmatch(arguments(body[loc[1]:])); m != nil {
				path = m[1]
			}
			health = append(health, Route{
				Method: "GET", Path: path, File: file, Line: lineOf(text, start+loc[0]),
				Controller: "howdy.health_check", Dev: dev, Mount: "the health check",
			})
			continue
		}
		alias, call := body[loc[2]:loc[3]], body[loc[4]:loc[5]]
		args := arguments(body[loc[1]:])
		at := Route{File: file, Line: lineOf(text, start+loc[0]), Controller: alias + "." + call, Dev: dev}
		module := imports[alias]
		src, err := os.ReadFile(filepath.Join(dir, "src", module+".gleam"))
		if module != "" && err == nil {
			moduleFile := filepath.ToSlash(filepath.Join("src", module+".gleam"))
			if found := declared(moduleFile, string(src), call); len(found) > 0 {
				for _, r := range found {
					r.Controller, r.Dev = at.Controller, dev
					routes = append(routes, r)
				}
				continue
			}
			// A local function that makes a controller some other way,
			// such as ui.stylesheet(at: ...).
			if s, e := function(string(src), call); s >= 0 {
				args = string(src)[s:e]
			}
		}
		at.Method, at.Mount = "*", mountOf(module)
		switch m := atArgument.FindStringSubmatch(args); {
		case m != nil:
			at.Path = join(m[1], "")
		case firstString.MatchString(args):
			at.Path = join(firstString.FindStringSubmatch(args)[1], "")
		default:
			at.Path = "?"
		}
		routes = append(routes, at)
	}
	// Health checks are answered ahead of every route.
	return append(health, routes...)
}

// apiDocs lists the document and reference page of Howdy OpenAPI, served
// by the module platform/api_docs.gleam that 'howdy add openapi' writes.
func apiDocs(dir, file, module string, line int, dev bool) []Route {
	if !strings.HasSuffix(module, "/platform/api_docs") {
		return nil
	}
	document := "/openapi.json"
	if src, err := os.ReadFile(filepath.Join(dir, "src", module+".gleam")); err == nil {
		if m := documentPath.FindSubmatch(src); m != nil {
			document = join(string(m[1]), "")
		}
	}
	at := Route{Method: "GET", File: file, Line: line, Controller: "api_docs.serve", Dev: dev}
	doc, reference := at, at
	doc.Path, doc.Mount = document, "the OpenAPI document"
	reference.Path, reference.Mount = "/docs", "the OpenAPI reference page"
	return []Route{doc, reference}
}

// declared lists the routes the function fn in a controller module
// declares on the controller it makes.
func declared(file, text, fn string) []Route {
	start, end := function(text, fn)
	if start < 0 {
		return nil
	}
	body := text[start:end]
	p := prefixCall.FindStringSubmatchIndex(body)
	if p == nil {
		return nil
	}
	prefix, guarded := body[p[4]:p[5]], body[p[2]:p[3]] == "guarded"
	var routes []Route
	for _, m := range routeCall.FindAllStringSubmatchIndex(body[p[1]:], -1) {
		sub := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return body[p[1]+m[2*i] : p[1]+m[2*i+1]]
		}
		method, path := sub(1), sub(2)
		if method == "" {
			method, path = sub(3), sub(4)
		}
		routes = append(routes, Route{
			Method: strings.ToUpper(method), Path: join(prefix, path),
			File: file, Line: lineOf(text, start+p[1]+m[0]), Guarded: guarded,
		})
	}
	return routes
}

// function finds the body of the function name in a module: from its
// `fn name(` to the closing brace at the start of a line.
func function(text, name string) (start, end int) {
	loc := regexp.MustCompile(`(?m)^(?:pub )?fn ` + regexp.QuoteMeta(name) + `\(`).FindStringIndex(text)
	if loc == nil {
		return -1, -1
	}
	close := regexp.MustCompile(`(?m)^\}`).FindStringIndex(text[loc[1]:])
	if close == nil {
		return loc[0], len(text)
	}
	return loc[0], loc[1] + close[1]
}

// arguments is the text of a call's arguments, from just after its opening
// parenthesis to the one that closes it.
func arguments(text string) string {
	depth := 1
	for i, c := range text {
		switch c {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return text[:i]
			}
		}
	}
	return text
}

// join makes the path a route matches from the controller's prefix and the
// route's own path, as Howdy does: both split into segments, empty ones
// dropped.
func join(prefix, path string) string {
	var segments []string
	for _, s := range strings.Split(prefix+"/"+path, "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	return "/" + strings.Join(segments, "/")
}

func lineOf(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

// mountOf describes the package behind a mount, for the ones Howdy
// writes, or is empty.
func mountOf(module string) string {
	switch {
	case module == "howdy/static":
		return "static files"
	case module == "howdy/auth/routes":
		return "Howdy Auth's JSON API"
	case module == "howdy/auth/pages":
		return "Howdy Auth's pages"
	case strings.HasSuffix(module, "/platform/rpc"):
		return "Howdy Remote's procedures, POST /rpc/<name>"
	}
	return ""
}

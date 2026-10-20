package generate

import (
	"errors"
	"fmt"
	"howdy/internal/scaffold"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Remote adds howdy_remote to a project: typed procedure calls between
// services, over Erlang distribution or HTTP. The app serves an example
// procedure to the cluster from main and the dev server, and over HTTP at
// /rpc for callers with the token in its services.
type Remote struct {
	Project *Project
	scaffold.Source
}

// testToken is the RPC token the test support gives the app.
const testToken = "test-rpc-token"

// NewRemote checks that the project can take howdy_remote and does not have
// it yet.
func NewRemote(p *Project) (*Remote, error) {
	toml, err := os.ReadFile(filepath.Join(p.Dir, "gleam.toml"))
	if err != nil {
		return nil, err
	}
	if dependency("howdy_remote").Match(toml) {
		return nil, errors.New("this project already has remote calls: howdy_remote is in gleam.toml")
	}
	source, err := howdySource(toml, "howdy_remote")
	if err != nil {
		return nil, err
	}
	root := readText(filepath.Join(p.Dir, "src", p.Name+".gleam"))
	if !servicesType.MatchString(root) || !servicesMade.MatchString(root) {
		return nil, fmt.Errorf("src/%s.gleam was made before 'howdy new' built the app from a services record, which the RPC token is added to; see the remote docs to add it by hand", p.Name)
	}
	return &Remote{Project: p, Source: source}, nil
}

// ProceduresFile is the module that defines the procedures.
func (r *Remote) ProceduresFile() string {
	return filepath.ToSlash(filepath.Join("src", r.Project.Name, "procedures.gleam"))
}

// RPCFile is the module that serves them.
func (r *Remote) RPCFile() string {
	return filepath.ToSlash(filepath.Join("src", r.Project.Name, "platform", "rpc.gleam"))
}

// Files renders the procedures, the server and, when the project has test
// support, its test.
func (r *Remote) Files() ([]File, error) {
	p := r.Project
	data := struct {
		Name, Token string
		Database    bool
	}{p.Name, testToken, p.Database != ""}
	targets := []struct{ template, path string }{
		{"procedures.gleam.tmpl", r.ProceduresFile()},
		{"rpc_platform.gleam.tmpl", r.RPCFile()},
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "test", p.Name, "support.gleam")); err == nil {
		targets = append(targets, struct{ template, path string }{
			"rpc_test.gleam.tmpl", filepath.Join("test", p.Name, "platform", "rpc_test.gleam"),
		})
	}
	var files []File
	for _, t := range targets {
		out, err := render(t.template, data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: filepath.ToSlash(t.path), Content: out})
	}
	return files, nil
}

// Write adds howdy_remote to gleam.toml and RPC_TOKEN to .env.example, and
// creates the files. It returns the paths written or changed.
func (r *Remote) Write() ([]string, error) {
	p := r.Project
	files, err := r.Files()
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(p.Dir, f.Path)); err == nil {
			return nil, fmt.Errorf("%s already exists; nothing was written", f.Path)
		}
	}
	line := r.Dependency("howdy_remote") + "\n"
	if err := insertDependencies(filepath.Join(p.Dir, "gleam.toml"), line); err != nil {
		return nil, err
	}
	changed := []string{"gleam.toml"}
	written, err := writeFiles(p.Dir, files)
	changed = append(changed, written...)
	if err != nil {
		return changed, err
	}
	if appendOnce(filepath.Join(p.Dir, ".env.example"), "RPC_TOKEN", `
# The bearer token callers of /rpc must send: a long random secret. Unset,
# /rpc refuses every request. Generate one with: openssl rand -hex 32
# RPC_TOKEN=
`) {
		changed = append(changed, ".env.example")
	}
	return changed, nil
}

var unusedServices = regexp.MustCompile(`pub fn app\(_services: `)

// Register mounts /rpc in the app with the token from the app's services,
// and starts the procedure server wherever the services are built for a
// running app: main and the dev server. The test support gets a fixed
// token. For each file without the shape 'howdy new' gives it, the returned
// notes say what to add by hand.
func (r *Remote) Register() (notes []string) {
	p := r.Project
	rpc := "import " + p.Name + "/platform/rpc"
	start := "// Serve the procedures to connected nodes: see platform/rpc.gleam.\nrpc.start()\n"

	root := filepath.Join("src", p.Name+".gleam")
	err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		text, err := addServicesField(text, "rpc_token: String")
		if err != nil {
			return "", err
		}
		if text, err = addToServices(text, "rpc_token", "rpc.token()", start); err != nil {
			return "", err
		}
		if !howdyNew.MatchString(text) {
			return "", errors.New("no howdy.new() in app")
		}
		if unusedServices.MatchString(text) {
			text = strings.Replace(text, " Rename `_services` to\n/// `services` once a controller needs something from it.", "", 1)
			text = unusedServices.ReplaceAllString(text, "pub fn app(services: ")
		}
		// The route goes first, ahead of the static catch-all.
		text = howdyNew.ReplaceAllString(text, "${0}${1}|> howdy.controller(rpc.controller(services.rpc_token))\n")
		return addImports(text, rpc), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Add remote calls to the app in %s: rpc_token: String in Services, built in main with rpc.token() after calling rpc.start(), and in app |> howdy.controller(rpc.controller(services.rpc_token)) (import %s/platform/rpc).", root, p.Name))
	}

	dev := p.devFile()
	err = editFile(filepath.Join(p.Dir, dev), func(text string) (string, error) {
		text, err := addToServices(text, "rpc_token", "rpc.token()", start)
		if err != nil {
			return "", err
		}
		return addImports(text, rpc), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Give the dev server remote calls in %s: call rpc.start() and pass rpc_token: rpc.token() to Services.", dev))
	}

	support := filepath.Join("test", p.Name, "support.gleam")
	if _, err := os.Stat(filepath.Join(p.Dir, support)); err == nil {
		err := editFile(filepath.Join(p.Dir, support), func(text string) (string, error) {
			return addToServices(text, "rpc_token", fmt.Sprintf("%q", testToken))
		})
		if err != nil {
			notes = append(notes, fmt.Sprintf("Give the tests an RPC token in %s: pass rpc_token: %q to Services.", support, testToken))
		}
	}
	return notes
}

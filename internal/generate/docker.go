package generate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"howdy/internal/scaffold"
)

// Docker adds the files 'howdy new' writes for containers to an existing
// project: the Dockerfile, .dockerignore and bin/start.
type Docker struct {
	Project *Project
	// Gleam and Erlang are the versions the Dockerfile builds and runs
	// with, such as "1.19.2" and "29".
	Gleam, Erlang string
}

// NewDocker prepares the container files for p, built with the given Gleam
// and Erlang releases, or the scaffold's own where they are "".
func NewDocker(p *Project, gleam, erlang string) *Docker {
	if gleam == "" {
		gleam = scaffold.Gleam
	}
	if erlang == "" {
		erlang = scaffold.Erlang
	}
	return &Docker{Project: p, Gleam: gleam, Erlang: erlang}
}

func (d *Docker) scaffold() *scaffold.Project {
	p := d.Project
	return &scaffold.Project{
		Name: p.Name, Dir: p.Dir, Database: scaffold.Database(p.Database), API: p.API,
		Gleam: d.Gleam, Erlang: d.Erlang,
	}
}

// Write writes the files the project does not have yet, and returns the
// paths written and those it already had, which are left alone.
func (d *Docker) Write() (written, kept []string, err error) {
	return d.scaffold().WriteDocker()
}

// Register mounts the health check the image's HEALTHCHECK asks, in a
// project from before 'howdy new' mounted it. The returned notes say what
// else an older project needs by hand to run well in a container.
func (d *Docker) Register() (notes []string) {
	p := d.Project
	root := filepath.Join("src", p.Name+".gleam")
	src, err := os.ReadFile(filepath.Join(p.Dir, root))
	if err != nil {
		return []string{fmt.Sprintf("%s was not found: mount the health check in the app with |> howdy.health_check(health.new()) (import howdy/health).", root)}
	}
	text := string(src)
	if !strings.Contains(text, "howdy.health_check(") {
		err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
			if !howdyNew.MatchString(text) {
				return "", errors.New("no howdy.new() in app")
			}
			text = howdyNew.ReplaceAllString(text, "${0}${1}// GET /up answers 200 while the app runs, for load balancers and\n${1}// container platforms, ahead of the routes, middleware and request log.\n${1}|> howdy.health_check(health.new())\n")
			return addImports(text, "import howdy/health"), nil
		})
		if err != nil {
			notes = append(notes, fmt.Sprintf("Mount the health check the image asks for in %s: |> howdy.health_check(health.new()) in app (import howdy/health).", root))
		}
	}
	if !strings.Contains(text, "howdy.start_application(") {
		otp := ""
		if toml, err := os.ReadFile(filepath.Join(p.Dir, "gleam.toml")); err == nil && !dependency("gleam_otp").Match(toml) {
			otp = `, with gleam_otp = ">= 1.2.0 and < 2.0.0" in [dependencies]`
		}
		notes = append(notes, fmt.Sprintf("main in %s starts the server outside an OTP application, so SIGTERM cuts off requests in flight: start it with supervisor.new(supervisor.OneForOne) |> supervisor.add(howdy.supervised(configured(services))) |> howdy.start_application(name: %q) (import gleam/otp/static_supervisor as supervisor%s).", root, p.Name+"_server", otp))
	}
	if p.Database != "" {
		if _, err := os.Stat(filepath.Join(p.Dir, "src", "tasks", "migrate.gleam")); err != nil {
			notes = append(notes, "bin/start migrate runs src/tasks/migrate.gleam, which this project does not have: add a main there that applies the migrations.")
		}
	}
	return notes
}

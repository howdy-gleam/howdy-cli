package generate

import (
	"bytes"
	"errors"
	"fmt"
	"howdy/internal/scaffold"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Jobs adds howdy_jobs to a project: background jobs kept in the app's
// database, done by a runner that main and the dev server start beside the
// server, shown in the dev admin and managed with `gleam run -m tasks/jobs`.
type Jobs struct {
	Project *Project
	scaffold.Source
	Admin bool
}

// installable holds what every 'howdy add' of a Howdy package checks
// first: that the package is not there yet, that the project has the
// database it needs, if it needs one, and the Git source and tag its other
// Howdy packages come from, so the new one matches them. It also checks
// that the app is built from a services record, which the package is added
// to, and whether the dev entry point mounts the admin.
func installable(p *Project, pkg, what string, database bool) (source scaffold.Source, admin bool, err error) {
	toml, err := os.ReadFile(filepath.Join(p.Dir, "gleam.toml"))
	if err != nil {
		return scaffold.Source{}, false, err
	}
	switch {
	case dependency(pkg).Match(toml):
		return scaffold.Source{}, false, fmt.Errorf("this project already has %s: %s is in gleam.toml", what, pkg)
	case database && !dependency("howdy_database").Match(toml):
		return scaffold.Source{}, false, fmt.Errorf("%s keep their data in the app's database through howdy_database, which this project does not have; create a project with a database ('howdy new' without --no-database) to use them", what)
	}
	if source, err = howdySource(toml, pkg); err != nil {
		return scaffold.Source{}, false, err
	}
	root := readText(filepath.Join(p.Dir, "src", p.Name+".gleam"))
	if !servicesType.MatchString(root) || !servicesMade.MatchString(root) {
		return scaffold.Source{}, false, fmt.Errorf("src/%s.gleam was made before 'howdy new' built the app from a services record, which %s are added to; see the docs to add %s by hand", p.Name, what, pkg)
	}
	return source, HasAdmin(p), nil
}

// NewJobs checks that the project can take background jobs and does not
// have them yet.
func NewJobs(p *Project) (*Jobs, error) {
	source, admin, err := installable(p, "howdy_jobs", "background jobs", true)
	if err != nil {
		return nil, err
	}
	return &Jobs{Project: p, Source: source, Admin: admin}, nil
}

// JobsFile is the module that defines the jobs and the runner that does
// them.
func (j *Jobs) JobsFile() string {
	return filepath.ToSlash(filepath.Join("src", j.Project.Name, "background_jobs.gleam"))
}

// TaskFile is the task module that manages them from the command line.
func (j *Jobs) TaskFile() string {
	return filepath.ToSlash(filepath.Join("src", "tasks", "jobs.gleam"))
}

// Files renders the jobs module, the command-line task and, when the
// project has test support, its test.
func (j *Jobs) Files() ([]File, error) {
	p := j.Project
	targets := []struct{ template, path string }{
		{"background_jobs.gleam.tmpl", j.JobsFile()},
		{"jobs_task.gleam.tmpl", j.TaskFile()},
	}
	if hasSupport(p) {
		targets = append(targets, struct{ template, path string }{
			"background_jobs_test.gleam.tmpl", filepath.Join("test", p.Name, "background_jobs_test.gleam"),
		})
	}
	return renderAll(p, targets)
}

// Write adds howdy_jobs to gleam.toml and creates the files. It returns
// the paths written or changed.
func (j *Jobs) Write() ([]string, error) {
	files, err := j.Files()
	if err != nil {
		return nil, err
	}
	line := j.Dependency("howdy_jobs") + "\n"
	return installFiles(j.Project, line, files)
}

var (
	appMain     = regexp.MustCompile(`(?m)^pub fn main\(\) -> Nil \{\n`)
	appStart    = regexp.MustCompile(`(?m)^([ \t]*)let assert Ok\(_\) = configured\(services\) \|> howdy\.start\n`)
	adminRecord = "|> admin.telemetry(recorder)"
)

// jobsRunner is the function 'howdy add jobs' puts in src/<project>.gleam,
// building the runner from the app's services so packages that queue jobs,
// such as notifications, can add their handlers to it.
const jobsRunner = `/// What does the app's background jobs: the queues and the code for each
/// kind of job, in background_jobs.gleam. ` + "`main` and `dev/`" + ` start it beside
/// the server.
pub fn jobs_runner(services: Services) -> jobs.Runner {
  background_jobs.runner(services.db)
}

`

// Register runs the jobs' migrations after the app's, adds jobs_runner to
// the app, starts it in main and the dev server, and with the dev admin,
// shows the jobs there. For each file without the shape 'howdy new' gives
// it, the returned notes say what to add by hand.
func (j *Jobs) Register() (notes []string) {
	p := j.Project
	module := "import " + p.Name + "/background_jobs"

	root := filepath.Join("src", p.Name+".gleam")
	err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		main := appMain.FindStringIndex(text)
		start := appStart.FindStringSubmatchIndex(text)
		if main == nil || start == nil {
			return "", errors.New("no main starting the configured app")
		}
		indent := text[start[2]:start[3]]
		text = text[:start[0]] +
			indent + "// Background jobs run beside the server: see background_jobs.gleam.\n" +
			indent + "let assert Ok(_) = jobs.start(jobs_runner(services))\n" +
			text[start[0]:]
		text = text[:main[0]] + jobsRunner + text[main[0]:]
		return addImports(text, "import howdy/jobs", module), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Start the jobs in %s: add pub fn jobs_runner(services: Services) -> jobs.Runner { background_jobs.runner(services.db) }, and in main, before the server starts, let assert Ok(_) = jobs.start(jobs_runner(services)) (import howdy/jobs and %s/background_jobs).", root, p.Name))
	}

	database := filepath.Join("src", p.Name, "platform", "database.gleam")
	err = editFile(filepath.Join(p.Dir, database), func(text string) (string, error) {
		return addPackageMigration(text, "background_jobs.migrate(db)", module)
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Run the jobs' migrations in %s: call background_jobs.migrate(db) after the app's own, before the app starts.", database))
	}

	dev := p.devFile()
	err = editFile(filepath.Join(p.Dir, dev), func(text string) (string, error) {
		loc := servicesMade.FindStringSubmatchIndex(text)
		if loc == nil {
			return "", errors.New("no `let services = Services(...)` line")
		}
		indent := text[loc[2]:loc[3]]
		text = text[:loc[1]] +
			indent + "let runner = " + p.Name + ".jobs_runner(services)\n" +
			indent + "let assert Ok(_) = jobs.start(runner)\n" +
			text[loc[1]:]
		if j.Admin {
			if !strings.Contains(text, adminRecord) {
				return "", errors.New("no admin.telemetry to follow")
			}
			text = strings.Replace(text, adminRecord, adminRecord+"\n|> admin.jobs(runner)", 1)
		}
		return addImports(text, "import howdy/jobs"), nil
	})
	if err != nil {
		admin := ""
		if j.Admin {
			admin = ", and add |> admin.jobs(runner) to the admin"
		}
		notes = append(notes, fmt.Sprintf("Start the jobs under gleam dev in %s: after the services, let runner = %s.jobs_runner(services) and let assert Ok(_) = jobs.start(runner)%s.", dev, p.Name, admin))
	}
	return notes
}

// HasAdmin reports whether the project's dev entry point mounts the admin.
func HasAdmin(p *Project) bool {
	return bytes.Contains([]byte(readText(filepath.Join(p.Dir, p.devFile()))), []byte("admin.mount("))
}

// hasSupport reports whether the project has the test support module the
// generated tests rely on.
func hasSupport(p *Project) bool {
	_, err := os.Stat(filepath.Join(p.Dir, "test", p.Name, "support.gleam"))
	return err == nil
}

// renderAll renders each template for the project to its path.
func renderAll(data any, targets []struct{ template, path string }) ([]File, error) {
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

// installFiles adds a package's lines to gleam.toml's [dependencies] and
// writes its files, none of which may exist yet. It returns the paths
// written or changed.
func installFiles(p *Project, dependencies string, files []File) ([]string, error) {
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(p.Dir, f.Path)); err == nil {
			return nil, fmt.Errorf("%s already exists; nothing was written", f.Path)
		}
	}
	if err := insertDependencies(filepath.Join(p.Dir, "gleam.toml"), dependencies); err != nil {
		return nil, err
	}
	written, err := writeFiles(p.Dir, files)
	return append([]string{"gleam.toml"}, written...), err
}

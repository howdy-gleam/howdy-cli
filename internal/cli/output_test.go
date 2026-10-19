package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"howdy/internal/scaffold"
	"howdy/internal/tools"
)

// newProject writes a project with howdy new's templates and makes it the
// working directory.
func newProject(t *testing.T) string {
	t.Helper()
	p, err := scaffold.New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write(nil); err != nil {
		t.Fatal(err)
	}
	// As 'howdy new' leaves it, so formatting after a command changes only
	// what the command wrote. Without gleam the commands skip formatting.
	if gleam, err := tools.FindGleam(); err == nil {
		if err := gleam.Run(p.Dir, "format", "src", "dev", "test"); err != nil {
			t.Fatal(err)
		}
	}
	// Build output and version control are not the project's files.
	os.MkdirAll(filepath.Join(p.Dir, "build", "dev"), 0o755)
	os.WriteFile(filepath.Join(p.Dir, "build", "dev", "big.beam"), []byte("x"), 0o644)
	t.Chdir(p.Dir)
	return p.Dir
}

// capture runs f and returns what it printed to stdout.
func capture(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	err = f()
	w.Close()
	os.Stdout = stdout
	return string(<-done), err
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := newProject(t)
	before, err := snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := before["build/dev/big.beam"]; ok {
		t.Fatal("the snapshot should leave out build/")
	}

	out := &output{json: true, dryRun: true}
	printed, err := capture(t, func() error {
		return runCreateSlice("controller", "users", "user", []string{"name", "age:int"}, "", false, out)
	})
	if err != nil {
		t.Fatal(err)
	}
	var r result
	if err := json.Unmarshal([]byte(printed), &r); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, printed)
	}
	if !r.OK || !r.DryRun || r.Command != "howdy create controller users" {
		t.Errorf("result = %+v", r)
	}
	wantCreated := map[string]bool{
		"src/demo_site/features/users/user.gleam":                   true,
		"src/demo_site/features/users/users.gleam":                  true,
		"dev/demo_site/factories/user_factory.gleam":                true,
		"src/demo_site/features/users/users_controller.gleam":       true,
		"src/demo_site/migrations/create_users.gleam":               true,
		"test/demo_site/features/users/users_test.gleam":            true,
		"test/demo_site/features/users/users_controller_test.gleam": true,
	}
	if len(r.Created) != len(wantCreated) {
		t.Errorf("created = %v", r.Created)
	}
	for _, path := range r.Created {
		if !wantCreated[path] {
			t.Errorf("unexpected created file %s", path)
		}
	}
	for _, want := range []string{"src/demo_site.gleam", "src/demo_site/platform/database.gleam"} {
		found := false
		for _, path := range r.Modified {
			found = found || path == want
		}
		if !found {
			t.Errorf("modified = %v, want %s among them", r.Modified, want)
		}
	}
	if len(r.Next) != 0 {
		t.Errorf("a dry run should suggest nothing to run next: %v", r.Next)
	}

	after, err := snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("the dry run changed the project: %d files, then %d", len(before), len(after))
	}
	for path, sum := range before {
		if after[path] != sum {
			t.Errorf("the dry run changed %s", path)
		}
	}
	if _, err := os.Stat(out.copy); !os.IsNotExist(err) {
		t.Errorf("the copy %s should be removed", out.copy)
	}
}

func TestJSONReportsChanges(t *testing.T) {
	newProject(t)
	out := &output{json: true}
	printed, err := capture(t, func() error {
		return runCreateSlice("migration", "tags", "", []string{"label:string"}, "", false, out)
	})
	if err != nil {
		t.Fatal(err)
	}
	var r result
	if err := json.Unmarshal([]byte(printed), &r); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, printed)
	}
	if len(r.Created) != 1 || r.Created[0] != "src/demo_site/migrations/create_tags.gleam" {
		t.Errorf("created = %v", r.Created)
	}
	if len(r.Modified) != 1 || r.Modified[0] != "src/demo_site/platform/database.gleam" {
		t.Errorf("modified = %v", r.Modified)
	}
	if r.ManualSteps == nil || r.Warnings == nil || len(r.Next) == 0 {
		t.Errorf("result = %+v", r)
	}
	if _, err := os.Stat("src/demo_site/migrations/create_tags.gleam"); err != nil {
		t.Error("the migration should be written")
	}
}

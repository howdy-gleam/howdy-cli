package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"howdy/internal/config"
)

// useConfig points HOWDY_CONFIG at a file holding content.
func useConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.Env, path)
	return path
}

// dryRunNew runs 'howdy new demo_site --dry-run --json' with args and
// returns what it reports.
func dryRunNew(t *testing.T, args ...string) (result, error) {
	t.Helper()
	t.Chdir(t.TempDir())
	cmd := newCommand()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs(append([]string{"demo_site", "--dry-run", "--json"}, args...))
	printed, err := capture(t, cmd.Execute)
	var r result
	if err == nil {
		if err := json.Unmarshal([]byte(printed), &r); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, printed)
		}
	}
	return r, err
}

func TestNewTakesDefaultsFromTheConfigFile(t *testing.T) {
	path := useConfig(t, `
[new]
database = "none"
minimal = true
agents = false
with = ["storage"]
`)
	r, err := dryRunNew(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"docker-compose.yml", "AGENTS.md", "src/console.gleam"} {
		if slices.Contains(r.Created, absent) {
			t.Errorf("%s should be left out by the config file", absent)
		}
	}
	if !slices.Contains(r.Created, "src/demo_site/platform/file_storage.gleam") {
		t.Errorf("storage from the config file should be set up, got %v", r.Created)
	}
	if !strings.Contains(strings.Join(r.Notes, "\n"), "Defaults were read from "+displayPath(path)) {
		t.Errorf("the notes should name the config file, got %v", r.Notes)
	}
}

func TestNewFlagsWinOverTheConfigFile(t *testing.T) {
	useConfig(t, `
[new]
database = "none"
agents = false
with = ["storage"]
`)
	r, err := dryRunNew(t, "--database", "postgres", "--no-agents=false", "--with", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, present := range []string{"docker-compose.yml", "AGENTS.md"} {
		if !slices.Contains(r.Created, present) {
			t.Errorf("%s should be there: the flags win", present)
		}
	}
	if slices.Contains(r.Created, "src/demo_site/platform/file_storage.gleam") {
		t.Error("--with \"\" should set up no packages")
	}
}

func TestNewNoConfigIgnoresTheFile(t *testing.T) {
	useConfig(t, "[new]\ndatabase = \"none\"\nnot_a_setting = 1\n")
	r, err := dryRunNew(t, "--no-config")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.Created, "docker-compose.yml") {
		t.Error("--no-config should keep howdy's default database")
	}
	for _, note := range r.Notes {
		if strings.Contains(note, "Defaults were read") {
			t.Errorf("no config file was read, but the notes say %q", note)
		}
	}
}

func TestNewRefusesABadConfigFile(t *testing.T) {
	for setting, want := range map[string]string{
		`database = "mysql"`:      "new.database",
		`source = "svn"`:          "new.source",
		`howdy = "latest"`:        "new.howdy",
		`with = ["mail", "chat"]`: "new.with",
		`repository = " "`:        "new.repository",
	} {
		path := useConfig(t, "[new]\n"+setting+"\n")
		_, err := dryRunNew(t, "--database", "sqlite")
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), path) {
			t.Errorf("%s: want an error naming %s and the file, got %v", setting, want, err)
		}
	}
}

func TestNewRefusesDatabasePackagesWithoutADatabase(t *testing.T) {
	useConfig(t, "[new]\nwith = [\"jobs\"]\n")
	_, err := dryRunNew(t, "--no-database")
	if err == nil || !strings.Contains(err.Error(), "jobs keeps its data in the database") {
		t.Fatalf("got %v", err)
	}
}

func TestConfigShowsWhereEachDefaultComesFrom(t *testing.T) {
	path := useConfig(t, "[new]\nsource = \"hex\"\nrepository = \"https://example.com/howdy.git\"\n")
	printed, err := capture(t, func() error { return runConfig(true) })
	if err != nil {
		t.Fatal(err)
	}
	var shown struct {
		Path string    `json:"path"`
		New  []setting `json:"new"`
	}
	if err := json.Unmarshal([]byte(printed), &shown); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, printed)
	}
	if shown.Path != path {
		t.Errorf("path = %q, want %q", shown.Path, path)
	}
	from := map[string]string{}
	for _, s := range shown.New {
		from[s.Key] = s.From + " " + strings.Trim(strings.ReplaceAll(jsonString(t, s.Value), `"`, ""), " ")
	}
	if from["source"] != "config hex" || from["repository"] != "config https://example.com/howdy.git" || from["database"] != "default postgres" {
		t.Errorf("unexpected settings %v", from)
	}
}

func TestConfigInitWritesAFileThatReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "howdy", "config.toml")
	t.Setenv(config.Env, path)
	if _, err := capture(t, func() error { return runConfigInit(false) }); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatalf("the starter file should read back: %v", err)
	}
	if c.New.Source != nil || c.New.With != nil {
		t.Error("every setting in the starter file should be commented out")
	}
	// Uncommenting every line gives a file howdy accepts.
	setting := regexp.MustCompile(`(?m)^# ([a-z_]+ = .*)$`)
	uncommented := setting.ReplaceAllString(starterConfig(), "$1")
	if strings.Count(uncommented, " = ") < 9 {
		t.Fatalf("want every setting uncommented, got\n%s", uncommented)
	}
	os.WriteFile(path, []byte(uncommented), 0o644)
	c, err = config.Load()
	if err != nil {
		t.Fatalf("the starter file uncommented should read back: %v", err)
	}
	opts := newCommandDefaults()
	if err := opts.applyConfig(newCommand().Flags(), c); err != nil {
		t.Fatalf("the starter file uncommented should be valid: %v", err)
	}

	if _, err := capture(t, func() error { return runConfigInit(false) }); err == nil {
		t.Error("init should not replace a file without --force")
	}
	if _, err := capture(t, func() error { return runConfigInit(true) }); err != nil {
		t.Errorf("init --force should replace the file: %v", err)
	}
}

func jsonString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

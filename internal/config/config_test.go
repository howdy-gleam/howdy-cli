package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// home gives the test an empty home and config directory.
func home(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("AppData", filepath.Join(dir, "AppData"))
	t.Setenv(Env, "")
	return dir
}

func TestLoadWithoutAFile(t *testing.T) {
	home(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != "" || c.New.Source != nil || c.New.With != nil {
		t.Fatalf("want an empty config, got %+v", c)
	}
}

func TestLoadReadsTheUserConfigDirectory(t *testing.T) {
	home(t)
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "howdy", "config.toml")
	write(t, path, `
[new]
source = "hex"
howdy = "1.0.0"
database = "sqlite"
with = ["mail", "auth"]
agents = false
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != path {
		t.Fatalf("path = %q, want %q", c.Path, path)
	}
	n := c.New
	if *n.Source != "hex" || *n.Howdy != "1.0.0" || *n.Database != "sqlite" || *n.Agents {
		t.Fatalf("unexpected settings %+v", n)
	}
	if strings.Join(*n.With, ",") != "mail,auth" {
		t.Fatalf("with = %v", *n.With)
	}
	if n.Minimal != nil || n.Repository != nil {
		t.Fatal("settings the file leaves out should be nil")
	}
}

func TestLoadOnMacOSAlsoReadsDotConfig(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	dir := home(t)
	path := filepath.Join(dir, ".config", "howdy", "config.toml")
	write(t, path, "[new]\nminimal = true\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != path || !*c.New.Minimal {
		t.Fatalf("got %+v", c)
	}
}

func TestEnvNamesTheFile(t *testing.T) {
	dir := home(t)
	path := filepath.Join(dir, "ci.toml")
	write(t, path, "[new]\nskip_deps = true\n")
	t.Setenv(Env, path)
	if got := Paths(); len(got) != 1 || got[0] != path {
		t.Fatalf("paths = %v", got)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !*c.New.SkipDeps {
		t.Fatal("want skip_deps from the named file")
	}

	t.Setenv(Env, filepath.Join(dir, "missing.toml"))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), Env) {
		t.Fatalf("a missing named file should fail naming %s, got %v", Env, err)
	}
}

func TestEnvNoneReadsNoFile(t *testing.T) {
	home(t)
	dir, _ := os.UserConfigDir()
	write(t, filepath.Join(dir, "howdy", "config.toml"), "[new]\nminimal = true\n")
	t.Setenv(Env, "none")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != "" || c.New.Minimal != nil {
		t.Fatalf("want no file read, got %+v", c)
	}
}

func TestReadRefusesUnknownSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "[new]\ndatabse = \"sqlite\"\n")
	_, err := Read(path)
	if err == nil || !strings.Contains(err.Error(), "new.databse") || !strings.Contains(err.Error(), path) {
		t.Fatalf("want an error naming new.databse and the file, got %v", err)
	}
}

func TestReadReportsBadTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "[new]\nminimal = \"yes\"\n")
	if _, err := Read(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("want an error naming the file, got %v", err)
	}
}

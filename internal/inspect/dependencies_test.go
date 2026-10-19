package inspect

import (
	"os"
	"path/filepath"
	"testing"

	"howdy/internal/scaffold"
)

const howdyRepo = "https://github.com/mikeyjones/howdy.git"

// dependencyProject writes a project that depends on howdy, with
// howdy_admin, which brings in howdy_flags, as a dev dependency, and the
// packages as gleam build leaves them in build/packages.
func dependencyProject(t *testing.T, dependencies string, sources map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"gleam.toml": `name = "demo_site"
version = "1.0.0"

[dependencies]
gleam_stdlib = ">= 0.44.0 and < 2.0.0"
howdy = { git = "` + howdyRepo + `", ref = "v1.0.0" }
` + dependencies + `
[dev_dependencies]
howdy_admin = { git = "` + howdyRepo + `", ref = "v1.0.0", path = "admin" }
gleeunit = ">= 1.0.0 and < 2.0.0"
`,
		"manifest.toml": `# Do not manually edit this file, it is managed by Gleam.

packages = [
  { name = "gleam_json", version = "3.1.0", build_tools = ["gleam"], requirements = ["gleam_stdlib"], otp_app = "gleam_json", source = "hex", outer_checksum = "AB" },
  { name = "gleam_stdlib", version = "0.65.0", build_tools = ["gleam"], requirements = [], otp_app = "gleam_stdlib", source = "hex", outer_checksum = "CD" },
  { name = "gleeunit", version = "1.6.0", build_tools = ["gleam"], requirements = ["gleam_stdlib"], otp_app = "gleeunit", source = "hex", outer_checksum = "EF" },
  { name = "howdy", version = "1.0.0", build_tools = ["gleam"], requirements = ["gleam_json", "gleam_stdlib"], source = "git", repo = "` + howdyRepo + `", commit = "abc123" },
  { name = "howdy_admin", version = "2.0.0", build_tools = ["gleam"], requirements = ["howdy", "howdy_flags", "markdown"], source = "git", repo = "` + howdyRepo + `", commit = "abc123", path = "admin" },
  { name = "howdy_flags", version = "2.0.1", build_tools = ["gleam"], requirements = ["howdy"], source = "git", repo = "` + howdyRepo + `", commit = "abc123", path = "flags" },
  { name = "markdown", version = "1.0.0", build_tools = ["gleam"], requirements = [], otp_app = "markdown", source = "hex", outer_checksum = "01" },
]

[requirements]
gleam_stdlib = { version = ">= 0.44.0 and < 2.0.0" }
`,
		"build/packages/gleam_json/src/gleam/json.gleam":      "",
		"build/packages/gleam_stdlib/src/gleam/list.gleam":    "",
		"build/packages/gleeunit/src/gleeunit.gleam":          "",
		"build/packages/howdy/src/howdy.gleam":                "",
		"build/packages/howdy_admin/src/howdy/admin.gleam":    "",
		"build/packages/howdy_flags/src/howdy/flags.gleam":    "",
		"build/packages/howdy_flags/src/howdy/flags/db.gleam": "",
		"build/packages/markdown/src/markdown.gleam":          "",
		"build/packages/howdy_flags/src/howdy_flags_ffi.erl":  "",
		"src/demo_site.gleam":                                 "import howdy\nimport gleam/json\n",
	}
	for path, text := range sources {
		files[path] = text
	}
	for path, text := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDevOnlyImportsFindsAPackageOnlyADevDependencyBringsIn(t *testing.T) {
	dir := dependencyProject(t, "", map[string]string{
		"src/demo_site/features.gleam": `//// Flags.

import gleam/list
import howdy/flags.{type Flag, type Flags}
import howdy/flags/db as flags_db
import markdown
`,
		// The tests and dev/ have the dev dependencies.
		"test/demo_site_test.gleam": "import gleeunit\nimport howdy/admin\n",
		"dev/demo_site_dev.gleam":   "import howdy/admin\n",
	})
	found, err := DevOnlyImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := DescribeDevOnlyImports(found)
	want := `howdy_flags is imported in src but is not in [dependencies]: only the dev dependencies bring it in, so a production build such as gleam export leaves it out.
  src/demo_site/features.gleam:4 imports howdy/flags
  src/demo_site/features.gleam:5 imports howdy/flags/db
Add it to [dependencies] in gleam.toml:
  howdy_flags = { git = "` + howdyRepo + `", ref = "v1.0.0", path = "flags" }

markdown is imported in src but is not in [dependencies]: only the dev dependencies bring it in, so a production build such as gleam export leaves it out.
  src/demo_site/features.gleam:6 imports markdown
Add it with: gleam add markdown`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestDevOnlyImportsAcceptsADeclaredOrTransitiveDependency(t *testing.T) {
	flags := `howdy_flags = { git = "` + howdyRepo + `", ref = "v1.0.0", path = "flags" }` + "\n"
	dir := dependencyProject(t, flags, map[string]string{
		// gleam_json comes with howdy, so a production build has it.
		"src/demo_site/features.gleam": "import gleam/json\nimport howdy/flags\n",
	})
	found, err := DevOnlyImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("found %v, want none", found)
	}
}

func TestDevOnlyImportsOfNewProject(t *testing.T) {
	// Before gleam build there is no manifest, and nothing to check.
	dir := project(t, func(p *scaffold.Project) {})
	found, err := DevOnlyImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("found %v, want none", found)
	}
}

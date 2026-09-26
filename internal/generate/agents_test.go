package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoteForAgents(t *testing.T) {
	dir := t.TempDir()
	p := &Project{Dir: dir, Name: "demo_site"}
	if changed, err := p.NoteForAgents("mail"); err != nil || changed {
		t.Fatalf("a project without AGENTS.md: changed %v, %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, AgentsFile)); err == nil {
		t.Fatal("AGENTS.md should not be created")
	}

	path := filepath.Join(dir, AgentsFile)
	os.WriteFile(path, []byte("# Working on Demo site\n\nThe guide.\n"), 0o644)
	for _, pkg := range []string{"mail", "auth", "flags", "openapi", "remote"} {
		if changed, err := p.NoteForAgents(pkg); err != nil || !changed {
			t.Fatalf("%s: changed %v, %v", pkg, changed, err)
		}
		if changed, err := p.NoteForAgents(pkg); err != nil || changed {
			t.Fatalf("%s a second time: changed %v, %v", pkg, changed, err)
		}
	}
	src, _ := os.ReadFile(path)
	text := string(src)
	for _, want := range []string{
		"The guide.\n\n<!-- howdy add mail -->\n## Email\n",
		"`src/demo_site/emails/`",
		"`src/demo_site/platform/accounts.gleam`",
		"`src/demo_site/feature_flags.gleam`",
		"`src/demo_site/platform/api_docs.gleam`",
		"`src/demo_site/platform/rpc.gleam`",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("AGENTS.md should contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "%!") || strings.Count(text, "## Email") != 1 {
		t.Errorf("AGENTS.md is badly written:\n%s", text)
	}
	if _, err := p.NoteForAgents("nope"); err == nil {
		t.Error("an unknown package should be an error")
	}
}

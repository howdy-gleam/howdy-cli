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
	if changed, err := p.NoteForAgents("mail"); err != nil || len(changed) > 0 {
		t.Fatalf("a project without AGENTS.md: changed %v, %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, AgentsFile)); err == nil {
		t.Fatal("AGENTS.md should not be created")
	}

	path := filepath.Join(dir, AgentsFile)
	os.WriteFile(path, []byte("# Working on Demo site\n\nThe guide.\n"), 0o644)
	for _, pkg := range []string{"mail", "auth", "flags", "openapi", "remote"} {
		changed, err := p.NoteForAgents(pkg)
		if err != nil || len(changed) == 0 || changed[0] != AgentsFile {
			t.Fatalf("%s: changed %v, %v", pkg, changed, err)
		}
		if pkg == "auth" && (len(changed) != 2 || changed[1] != ".claude/skills/howdy-auth/SKILL.md") {
			t.Errorf("auth should write its skill: changed %v", changed)
		}
		if changed, err := p.NoteForAgents(pkg); err != nil || len(changed) > 0 {
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
	skill, err := os.ReadFile(filepath.Join(dir, ".claude/skills/howdy-auth/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: howdy-auth\n", "`src/demo_site/platform/accounts.gleam`", "/auth/login"} {
		if !strings.Contains(string(skill), want) {
			t.Errorf("the auth skill should contain %q", want)
		}
	}
	if _, err := p.NoteForAgents("nope"); err == nil {
		t.Error("an unknown package should be an error")
	}
}

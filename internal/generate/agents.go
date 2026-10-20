package generate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// AgentsFile is the guide for coding agents that 'howdy new' writes, and
// that 'howdy add' tells about each package it installs.
const AgentsFile = "AGENTS.md"

// agentNotes are the sections 'howdy add' appends to AGENTS.md, keyed by
// package, with "%[1]s" standing for the project name. Each starts with a
// marker so it is added once.
var agentNotes = map[string]string{
	"mail": `## Email

Howdy Mail is installed. Controllers send with the mailer in the app's
services: ` + "`mail.send(services.mailer, welcome_email.build(to: address))`" + `.
Emails live in ` + "`src/%[1]s/emails/`" + `, written with smail; add one with
` + "`howdy create email <name>`" + `, which also adds its preview, checked by the
tests. Under ` + "`gleam dev`" + ` sent email goes to an outbox in ` + "`tmp/mail`" + `.
`,
	"auth": `## Sign-in

See ` + "`.claude/skills/howdy-auth/SKILL.md`" + ` for guards, roles and tests.

Howdy Auth is installed, with the app's identity and permissions built in
` + "`src/%[1]s/platform/accounts.gleam`" + ` and kept in ` + "`Services`" + `. Pass
` + "`--add-auth`" + ` to ` + "`howdy create`" + ` to guard the routes it writes. Guard a
controller by hand with ` + "`controller.guarded(\"prefix\", auth.required(identity))`" + `;
its routes then have the signed-in user in ` + "`ctx.guard.user`" + `, and tests
sign in with ` + "`support.sign_in`" + `.
`,
	"flags": `## Feature flags

Howdy Flags is installed. Define flags in ` + "`src/%[1]s/feature_flags.gleam`" + `
and add them to ` + "`all()`" + `. Check one with
` + "`flags.enabled(services.features, feature_flags.early_access(), for: flags.user(id))`" + `,
and manage them with ` + "`gleam run -m tasks/flags help`" + `.
`,
	"openapi": `## OpenAPI

Howdy OpenAPI serves ` + "`/openapi.json`" + ` and ` + "`/docs`" + `, configured in
` + "`src/%[1]s/platform/api_docs.gleam`" + `. Only routes written with
` + "`howdy/openapi/endpoint`" + ` (` + "`endpoint.get`" + `, ` + "`endpoint.post`" + ` and so on, from the
schemas they read) are documented, so write new JSON routes that way.
`,
	"docker": `## Deploying

` + "`Dockerfile`" + ` builds the image to deploy, and ` + "`bin/start`" + ` starts the app in
it: ` + "`bin/start run`" + `, ` + "`bin/start migrate`" + ` for a database, or ` + "`bin/start remote`" + `
for a shell inside it. The image's health check asks ` + "`GET /up`" + `. Something the
app keeps running, such as a pool or a poller, goes in the supervision tree
` + "`main`" + ` starts, so it stops cleanly with the server.
`,
	"remote": `## Remote calls

Howdy Remote is installed. The procedures the app serves are defined in
` + "`src/%[1]s/procedures.gleam`" + ` and answered in ` + "`server()`" + ` in
` + "`src/%[1]s/platform/rpc.gleam`" + `. Call another service with
` + "`remote.call(target, procedure, input, timeout: 5000)`" + `.
`,
}

// agentSkills are the skills 'howdy add' writes for coding agents, keyed
// by package: a template, rendered with the project's Name and API, and
// the skill's name, a folder under .claude/skills/.
var agentSkills = map[string][2]string{
	"auth": {"skill_auth.md.tmpl", "howdy-auth"},
}

// NoteForAgents tells coding agents working on the project about pkg: it
// appends the package's section to AGENTS.md and writes its skill, if it
// has one. It returns the paths it changed. A project without AGENTS.md,
// made before 'howdy new' wrote one, is left alone, and so is one that
// has the section already; an existing skill is not replaced.
func (p *Project) NoteForAgents(pkg string) ([]string, error) {
	note, ok := agentNotes[pkg]
	if !ok {
		return nil, fmt.Errorf("no agent notes for %q", pkg)
	}
	path := filepath.Join(p.Dir, AgentsFile)
	src, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var changed []string
	marker := "<!-- howdy add " + pkg + " -->"
	if text := string(src); !strings.Contains(text, marker) {
		text = strings.TrimRight(text, "\n") + "\n\n" + marker + "\n" + fmt.Sprintf(note, p.Name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return nil, err
		}
		changed = append(changed, AgentsFile)
	}
	if skill, ok := agentSkills[pkg]; ok {
		content, err := renderSkill(skill[0], p)
		if err != nil {
			return changed, err
		}
		file := filepath.Join(".claude", "skills", skill[1], "SKILL.md")
		written, err := writeFiles(p.Dir, []File{{Path: file, Content: content, Shared: true}})
		for _, w := range written {
			changed = append(changed, filepath.ToSlash(w))
		}
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func renderSkill(name string, p *Project) ([]byte, error) {
	tmpl, err := template.New(name).Option("missingkey=error").ParseFS(templates, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, map[string]any{"Name": p.Name, "API": p.API})
	return out.Bytes(), err
}

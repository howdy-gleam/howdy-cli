package generate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	"remote": `## Remote calls

Howdy Remote is installed. The procedures the app serves are defined in
` + "`src/%[1]s/procedures.gleam`" + ` and answered in ` + "`server()`" + ` in
` + "`src/%[1]s/platform/rpc.gleam`" + `. Call another service with
` + "`remote.call(target, procedure, input, timeout: 5000)`" + `.
`,
}

// NoteForAgents appends the section about pkg to the project's AGENTS.md,
// so agents working on it know the package is there and how to use it. It
// reports whether the file changed: a project without AGENTS.md, made
// before 'howdy new' wrote one, or one that has the section already, is
// left alone.
func (p *Project) NoteForAgents(pkg string) (bool, error) {
	note, ok := agentNotes[pkg]
	if !ok {
		return false, fmt.Errorf("no agent notes for %q", pkg)
	}
	path := filepath.Join(p.Dir, AgentsFile)
	src, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	marker := "<!-- howdy add " + pkg + " -->"
	text := string(src)
	if strings.Contains(text, marker) {
		return false, nil
	}
	text = strings.TrimRight(text, "\n") + "\n\n" + marker + "\n" + fmt.Sprintf(note, p.Name)
	return true, os.WriteFile(path, []byte(text), 0o644)
}

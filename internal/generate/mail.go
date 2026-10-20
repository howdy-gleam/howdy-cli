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
	"text/template"
)

// Mail adds howdy_mail to a project: the dependencies, a production mailer
// from the environment in platform/email.gleam, an example email with its
// preview in emails/, and a mailer in the app's services, from the outbox
// under `gleam dev` and in the tests. With the dev admin installed, the
// admin shows the outbox and the previews.
type Mail struct {
	Project *Project
	// Source is where the project's Howdy packages come from, read from
	// its howdy dependency so howdy_mail matches them.
	scaffold.Source
	// Admin reports whether the dev entry point mounts the admin.
	Admin bool
}

var (
	howdyFromGit = regexp.MustCompile(`(?m)^howdy\s*=\s*\{\s*git\s*=\s*"([^"]+)",\s*ref\s*=\s*"([^"]+)"\s*\}`)
	// From Hex, the lower bound of the range is the Howdy release, which is
	// the version of the core package.
	howdyFromHex = regexp.MustCompile(`(?m)^howdy\s*=\s*"\s*>=\s*([0-9]+\.[0-9]+\.[0-9]+)[^"]*"`)
)

// howdySource reads where a project's Howdy packages come from out of its
// howdy dependency in gleam.toml, so the package pkg added to it matches
// them.
func howdySource(toml []byte, pkg string) (scaffold.Source, error) {
	var source scaffold.Source
	if m := howdyFromGit.FindSubmatch(toml); m != nil {
		source = scaffold.Source{Kind: scaffold.Git, Git: string(m[1]), Ref: string(m[2])}
	} else if m := howdyFromHex.FindSubmatch(toml); m != nil {
		var err error
		if source, err = scaffold.ReleaseSource(scaffold.Hex, string(m[1])); err != nil {
			return scaffold.Source{}, fmt.Errorf("gleam.toml takes howdy %s from Hex, so the matching %s is unknown: %w", m[1], pkg, err)
		}
	} else {
		return scaffold.Source{}, fmt.Errorf("gleam.toml takes howdy neither from Git nor from Hex as 'howdy new' writes it (howdy = { git = \"...\", ref = \"...\" } or howdy = \">= 1.0.0 and < 2.0.0\"), so the matching %s is unknown; add %s by hand", pkg, pkg)
	}
	return source, source.Check(pkg)
}

// NewMail checks that the project can take mail and does not have it yet.
func NewMail(p *Project) (*Mail, error) {
	toml, err := os.ReadFile(filepath.Join(p.Dir, "gleam.toml"))
	if err != nil {
		return nil, err
	}
	if dependency("howdy_mail").Match(toml) {
		return nil, errors.New("this project already has mail: howdy_mail is in gleam.toml")
	}
	source, err := howdySource(toml, "howdy_mail")
	if err != nil {
		return nil, err
	}
	dev, _ := os.ReadFile(filepath.Join(p.Dir, p.devFile()))
	return &Mail{Project: p, Source: source, Admin: bytes.Contains(dev, []byte("admin.mount("))}, nil
}

func (p *Project) devFile() string {
	return filepath.ToSlash(filepath.Join("dev", p.Name+"_dev.gleam"))
}

// Title names the project for people, e.g. "My site".
func (p *Project) Title() string { return humanize(p.Name, true) }

// Files renders the new files: the mailer, the welcome email, the list of
// previews and its test.
func (m *Mail) Files() ([]File, error) {
	p := m.Project
	welcome := &Email{Project: p, Name: "welcome", subject: "Welcome to " + p.Title()}
	targets := []struct {
		template, path string
		data           any
	}{
		{"mail_platform.gleam.tmpl", filepath.Join("src", p.Name, "platform", "email.gleam"), p},
		{"email.gleam.tmpl", welcome.File(), welcome},
		{"previews.gleam.tmpl", filepath.Join("src", p.Name, "emails", "previews.gleam"), welcome},
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "test")); err == nil {
		targets = append(targets, struct {
			template, path string
			data           any
		}{"previews_test.gleam.tmpl", filepath.Join("test", p.Name, "emails", "previews_test.gleam"), welcome})
	}
	var files []File
	for _, t := range targets {
		out, err := render(t.template, t.data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: filepath.ToSlash(t.path), Content: out})
	}
	return files, nil
}

func render(name string, data any) ([]byte, error) {
	tmpl, err := template.New(name).Funcs(funcs).Option("missingkey=error").ParseFS(templates, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return out.Bytes(), nil
}

// Write adds the dependencies to gleam.toml, the outbox to .gitignore and
// the variables to .env.example, and creates the files. It returns the
// paths written or changed.
func (m *Mail) Write() ([]string, error) {
	p := m.Project
	files, err := m.Files()
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(p.Dir, f.Path)); err == nil {
			return nil, fmt.Errorf("%s already exists; nothing was written", f.Path)
		}
	}
	lines := m.Dependency("howdy_mail") + "\nsmail = \">= 2.0.1 and < 3.0.0\"\n"
	if err := insertDependencies(filepath.Join(p.Dir, "gleam.toml"), lines); err != nil {
		return nil, err
	}
	changed := []string{"gleam.toml"}
	written, err := writeFiles(p.Dir, files)
	changed = append(changed, written...)
	if err != nil {
		return changed, err
	}
	if appendOnce(filepath.Join(p.Dir, ".gitignore"), "tmp/mail", "\n# Email the dev outbox keeps. It can hold sign-in links.\ntmp/mail/\n") {
		changed = append(changed, ".gitignore")
	}
	if appendOnce(filepath.Join(p.Dir, ".env.example"), "MAIL_FROM", fmt.Sprintf(`
# Email under gleam run: who it comes from, and the first provider set.
# With none, email is logged instead. gleam dev keeps it in tmp/mail.
MAIL_FROM=%s <hello@example.com>
# SMTP_URL=smtp://user:password@smtp.example.com:587
# RESEND_API_KEY=
# SENDGRID_API_KEY=
`, p.Title())) {
		changed = append(changed, ".env.example")
	}
	return changed, nil
}

// insertDependencies adds lines at the end of gleam.toml's [dependencies].
func insertDependencies(path, lines string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(src)
	start := strings.Index(text, "[dependencies]\n")
	if start < 0 {
		return errors.New("gleam.toml has no [dependencies] section")
	}
	body := start + len("[dependencies]\n")
	end := len(text)
	if next := strings.Index(text[body:], "\n["); next >= 0 {
		end = body + next + 1
	}
	section := strings.TrimRight(text[body:end], "\n") + "\n"
	rest := text[end:]
	if rest != "" {
		rest = "\n" + rest
	}
	return os.WriteFile(path, []byte(text[:body]+section+lines+rest), 0o644)
}

// appendOnce adds text to the end of a file that does not mention marker.
// A missing file is left alone. It reports whether the file changed.
func appendOnce(path, marker, text string) bool {
	src, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(src), marker) {
		return false
	}
	return os.WriteFile(path, []byte(strings.TrimRight(string(src), "\n")+"\n"+text), 0o644) == nil
}

var (
	servicesType = regexp.MustCompile(`pub type Services \{\s*Services(?:\(([^)]*)\))?\s*\}`)
	dashboard    = regexp.MustCompile(`(?m)^([ \t]*)let dashboard =`)
	servicesMade = regexp.MustCompile(`(?m)^([ \t]*)let services =\s*((?:[a-z_0-9]+\.)?Services)(?:\(([^)]*)\))?[ \t]*\n`)
)

// Register puts a mailer in the app's services: the field in
// src/<project>.gleam, the production mailer in main, the outbox in the dev
// entry point, with the admin's mail pages when it has the admin, and an
// outbox in the test support. For each file without the shape 'howdy new'
// gives it, the returned notes say what to add by hand.
func (m *Mail) Register() (notes []string) {
	p := m.Project
	root := filepath.Join("src", p.Name+".gleam")
	err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		text, err := addServicesField(text, "mailer: mail.Mailer")
		if err != nil {
			return "", err
		}
		text, err = addToServices(text, "mailer", "email.mailer()")
		if err != nil {
			return "", err
		}
		return addImports(text, "import howdy/mail", "import "+p.Name+"/platform/email"), nil
	})
	if err != nil {
		if err != nil && strings.Contains(err.Error(), "let services") {
			notes = append(notes, fmt.Sprintf("%s was made before 'howdy new' built the app from a services record (app(db) rather than app(services)), so main does not create a mailer yet: call email.mailer() there and pass it to the controllers that send.", root))
		} else {
			notes = append(notes, fmt.Sprintf("Add the mailer to the app's services in %s: import howdy/mail and %s/platform/email, add mailer: mail.Mailer to the Services type, and in main pass mailer: email.mailer() to Services.", root, p.Name))
		}
	}

	dev := p.devFile()
	outbox := "// Email goes to an outbox instead of being sent, and each message is\n" +
		"// also written to tmp/mail as an .eml file any mail client opens.\n"
	if m.Admin {
		outbox = "// Email goes to an outbox instead of being sent: the admin shows it\n" +
			"// under Mail, and each message is also written to tmp/mail as .eml.\n"
	}
	outbox += "let assert Ok(box) = outbox.start_in(\"tmp/mail\")\n"
	mailerLine := "mail.mailer(outbox.adapter(box)) |> mail.default_from(email.sender())"
	inServices := true
	err = editFile(filepath.Join(p.Dir, dev), func(text string) (string, error) {
		wired, err := addToServices(text, "mailer", mailerLine, outbox)
		if err != nil {
			// A dev file from before the services record: the admin can
			// still show the outbox and previews, with the outbox ahead of
			// the dashboard that uses it.
			loc := dashboard.FindStringSubmatchIndex(text)
			if !m.Admin || loc == nil {
				return "", err
			}
			inServices = false
			indent := text[loc[2]:loc[3]]
			var lines strings.Builder
			for _, line := range strings.Split(strings.TrimRight(outbox, "\n"), "\n") {
				lines.WriteString(indent + line + "\n")
			}
			lines.WriteString(indent + "let mailer = " + mailerLine + "\n")
			wired = text[:loc[0]] + lines.String() + text[loc[0]:]
		}
		text = wired
		imports := []string{"import howdy/mail", "import howdy/mail/outbox", "import " + p.Name + "/platform/email"}
		if m.Admin {
			const anchor = "|> admin.telemetry(recorder)"
			if !strings.Contains(text, anchor) {
				return "", errors.New("no admin.telemetry to follow")
			}
			text = strings.Replace(text, anchor, anchor+"\n|> admin.mail(box)\n|> admin.mail_previews(previews.all(), send_with: mailer)", 1)
			imports = append(imports, "import "+p.Name+"/emails/previews")
		}
		return addImports(text, imports...), nil
	})
	switch {
	case err != nil:
		hint := ""
		if m.Admin {
			hint = fmt.Sprintf(", and add |> admin.mail(box) |> admin.mail_previews(previews.all(), send_with: mailer) to the admin (import %s/emails/previews)", p.Name)
		}
		notes = append(notes, fmt.Sprintf("Give the dev server an outbox in %s: let assert Ok(box) = outbox.start_in(\"tmp/mail\") and pass mailer: %s to Services%s.", dev, mailerLine, hint))
	case !inServices:
		notes = append(notes, fmt.Sprintf("%s keeps email in an outbox that the admin shows under Mail, with the previews. To send from the app in development too, pass its mailer to the controllers that send there.", dev))
	}

	support := filepath.Join("test", p.Name, "support.gleam")
	if _, err := os.Stat(filepath.Join(p.Dir, support)); err == nil {
		err := editFile(filepath.Join(p.Dir, support), func(text string) (string, error) {
			text, err := addToServices(text, "mailer",
				"mail.mailer(outbox.adapter(outbox.start())) |> mail.default_from(email.sender())")
			if err != nil {
				return "", err
			}
			return addImports(text, "import howdy/mail", "import howdy/mail/outbox", "import "+p.Name+"/platform/email"), nil
		})
		// Support from before the services record builds the app without
		// one, so it has nothing to add.
		if err != nil && strings.Contains(readText(filepath.Join(p.Dir, support)), "Services") {
			notes = append(notes, fmt.Sprintf("Give the tests a mailer in %s: pass mailer: mail.mailer(outbox.adapter(outbox.start())) |> mail.default_from(email.sender()) to Services.", support))
		}
	}
	return notes
}

func readText(path string) string {
	src, _ := os.ReadFile(path)
	return string(src)
}

// addServicesField adds a field, such as "mailer: mail.Mailer", to the
// Services type.
func addServicesField(text, field string) (string, error) {
	loc := servicesType.FindStringSubmatchIndex(text)
	if loc == nil {
		return "", errors.New("no Services type")
	}
	fields := field
	if loc[2] >= 0 && strings.TrimSpace(text[loc[2]:loc[3]]) != "" {
		fields = strings.TrimRight(strings.TrimSpace(text[loc[2]:loc[3]]), ",") + ", " + field
	}
	return text[:loc[0]] + "pub type Services {\n  Services(" + fields + ")\n}" + text[loc[1]:], nil
}

// addToServices adds a field to the line `let services = Services(...)`,
// binding it to value on the lines before, after any extra lines given. An
// empty value leaves the binding to those lines.
func addToServices(text, field, value string, before ...string) (string, error) {
	loc := servicesMade.FindStringSubmatchIndex(text)
	if loc == nil {
		return "", errors.New("no `let services = Services(...)` line")
	}
	indent := text[loc[2]:loc[3]]
	constructor := text[loc[4]:loc[5]]
	args := field + ":"
	if loc[6] >= 0 && strings.TrimSpace(text[loc[6]:loc[7]]) != "" {
		args = strings.TrimRight(strings.TrimSpace(text[loc[6]:loc[7]]), ",") + ", " + args
	}
	var lines strings.Builder
	for _, block := range before {
		for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
			lines.WriteString(indent + line + "\n")
		}
	}
	if value != "" {
		fmt.Fprintf(&lines, "%slet %s = %s\n", indent, field, value)
	}
	fmt.Fprintf(&lines, "%slet services = %s(%s)\n", indent, constructor, args)
	return text[:loc[0]] + lines.String() + text[loc[1]:], nil
}

// addImports adds each import that is missing after the file's last import.
func addImports(text string, lines ...string) string {
	for _, line := range lines {
		if strings.Contains(text, line+"\n") {
			continue
		}
		imports := lastImport.FindAllStringIndex(text, -1)
		at := 0
		if len(imports) > 0 {
			at = imports[len(imports)-1][1]
		}
		text = text[:at] + line + "\n" + text[at:]
	}
	return text
}

// editFile applies change to a file, and writes it only if change succeeds.
func editFile(path string, change func(string) (string, error)) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text, err := change(string(src))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// Email is an email for 'howdy create email', in src/<project>/emails/.
type Email struct {
	Project *Project
	// Name names the module without its suffix, e.g. "welcome" for
	// welcome_email.gleam.
	Name    string
	subject string
}

// NewEmail checks that the project has mail and not this email yet.
func NewEmail(p *Project, name string) (*Email, error) {
	name = strings.TrimSuffix(name, "_email")
	if !identifier.MatchString(name) {
		return nil, invalidName(name)
	}
	e := &Email{Project: p, Name: name}
	if _, err := os.Stat(filepath.Join(p.Dir, e.previews())); err != nil {
		return nil, fmt.Errorf("this project has no %s; add mail first with 'howdy add mail'", e.previews())
	}
	if _, err := os.Stat(filepath.Join(p.Dir, e.File())); err == nil {
		return nil, fmt.Errorf("the %s email already exists: %s; pick another name, or edit that email", name, e.File())
	}
	return e, nil
}

// Module is the email's module name, e.g. "welcome_email".
func (e *Email) Module() string { return e.Name + "_email" }

// Title names the email for people and the previews, e.g. "Password reset".
func (e *Email) Title() string { return humanize(e.Name, true) }

// Subject is the subject line it starts with.
func (e *Email) Subject() string {
	if e.subject != "" {
		return e.subject
	}
	return e.Title()
}

// File is the email's path, relative to the project directory.
func (e *Email) File() string {
	return filepath.ToSlash(filepath.Join("src", e.Project.Name, "emails", e.Module()+".gleam"))
}

func (e *Email) previews() string {
	return filepath.ToSlash(filepath.Join("src", e.Project.Name, "emails", "previews.gleam"))
}

// Write creates the email. It returns the path written.
func (e *Email) Write() (string, error) {
	content, err := render("email.gleam.tmpl", e)
	if err != nil {
		return "", err
	}
	_, err = writeFiles(e.Project.Dir, []File{{Path: e.File(), Content: content}})
	return e.File(), err
}

// Register adds the email's preview to the list in emails/previews.gleam,
// or returns a note saying what to add by hand.
func (e *Email) Register() (notes []string) {
	importLine := fmt.Sprintf("import %s/emails/%s", e.Project.Name, e.Module())
	err := editFile(filepath.Join(e.Project.Dir, e.previews()), func(text string) (string, error) {
		start := strings.Index(text, "pub fn all(")
		if start < 0 {
			return "", errors.New("no function all")
		}
		afterParams := matching(text, start+len("pub fn all"), '(', ')')
		if afterParams < 0 {
			return "", errors.New("no parameters for all")
		}
		open := strings.IndexByte(text[afterParams:], '{')
		if open < 0 {
			return "", errors.New("no body for all")
		}
		end := matching(text, afterParams+open, '{', '}')
		if end < 0 {
			return "", errors.New("unbalanced braces in all")
		}
		closing := strings.LastIndexByte(text[:end], ']')
		if closing < afterParams+open {
			return "", errors.New("all does not end with a list")
		}
		before := strings.TrimRight(text[:closing], " \t\n")
		separator := ", "
		if strings.HasSuffix(before, "[") || strings.HasSuffix(before, ",") {
			separator = " "
		}
		text = before + separator + e.Module() + ".preview()" + text[closing:]
		return addImports(text, importLine), nil
	})
	if err != nil {
		return []string{fmt.Sprintf("Add the preview to the list in %s: %s and add %s.preview() to all().", e.previews(), importLine, e.Module())}
	}
	return nil
}

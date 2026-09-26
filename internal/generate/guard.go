package generate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// signIn is the test support's helper for guarded routes, added the first
// time a command writes tests for them.
const signIn = `
/// A session token for a new user, for requests to routes guarded with
/// auth.required: |> testing.header("authorization", "Bearer " <> token).
/// Call it once per database, as each call registers the same address.
pub fn sign_in(db: Repo) -> String {
  let identity =
    accounts.identity(db, mail.mailer(outbox.adapter(outbox.start())))
  let assert Ok(someone) =
    auth.provision(identity, "someone@example.com", by: user.System)
  let assert Ok(session) =
    auth.impersonate(identity, someone.id, by: user.System)
  secret.reveal(session.token)
}
`

// addSignIn adds sign_in to test/<project>/support.gleam if the project has
// the file and it does not have the helper, and returns written with the
// file added when it changed.
func (p *Project) addSignIn(written []string) ([]string, error) {
	support := filepath.ToSlash(filepath.Join("test", p.Name, "support.gleam"))
	path := filepath.Join(p.Dir, support)
	text := readText(path)
	if text == "" || strings.Contains(text, "pub fn sign_in(") {
		return written, nil
	}
	err := editFile(path, func(text string) (string, error) {
		text = strings.TrimRight(text, "\n") + "\n" + signIn
		imports := []string{
			"import howdy/auth",
			"import howdy/auth/secret",
			"import howdy/auth/user",
			"import howdy/mail",
			"import howdy/mail/outbox",
			"import " + p.Name + "/platform/accounts",
		}
		if !strings.Contains(text, "type Repo") {
			imports = append(imports, "import gloo/repo.{type Repo}")
		}
		return addImports(text, imports...), nil
	})
	if err != nil {
		return written, err
	}
	return append(written, support), nil
}

// guardedController finds `controller.guarded(` in a controller's source,
// which Howdy Auth's guard, added by --add-auth, starts with.
var guardedController = regexp.MustCompile(`controller\.guarded\(`)

// guarded works out whether routes added to a controller are behind Howdy
// Auth's guard. addAuth is --add-auth, which guards a controller the
// command creates; src is the controller's source when it exists already,
// whose guard, or lack of one, stays as it is. So --add-auth is refused
// for an existing controller without a guard, rather than leaving its
// other routes open.
func (p *Project) guarded(file string, src []byte, exists, addAuth bool) (bool, error) {
	if addAuth {
		if err := p.NeedsAuth(); err != nil {
			return false, err
		}
	}
	if !exists {
		return addAuth, nil
	}
	guarded := guardedController.Match(src)
	if addAuth && !guarded {
		return false, fmt.Errorf("%s already exists without a guard, and --add-auth only guards a controller it creates; guard it by hand with controller.guarded(prefix, auth.required(identity)) ending in |> controller.build, or leave out --add-auth", file)
	}
	return guarded, nil
}

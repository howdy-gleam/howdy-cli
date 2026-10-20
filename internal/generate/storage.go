package generate

import (
	"errors"
	"fmt"
	"howdy/internal/scaffold"
	"path/filepath"
)

// Storage adds howdy_storage to a project: file storage as `files` in the
// app's services, in an S3-compatible bucket when the environment names
// one and in a local folder otherwise, with the folder's signed URLs served
// by the app. The tests keep files in memory.
type Storage struct {
	Project *Project
	scaffold.Source
}

// NewStorage checks that the project can take storage and does not have
// it yet.
func NewStorage(p *Project) (*Storage, error) {
	source, _, err := installable(p, "howdy_storage", "file storage", false)
	if err != nil {
		return nil, err
	}
	return &Storage{Project: p, Source: source}, nil
}

// StorageFile is the module that makes the app's storage from the
// environment.
func (s *Storage) StorageFile() string {
	return filepath.ToSlash(filepath.Join("src", s.Project.Name, "platform", "file_storage.gleam"))
}

// Files renders the storage module and, when the project has test
// support, its test.
func (s *Storage) Files() ([]File, error) {
	p := s.Project
	targets := []struct{ template, path string }{
		{"file_storage.gleam.tmpl", s.StorageFile()},
	}
	if hasSupport(p) {
		targets = append(targets, struct{ template, path string }{
			"file_storage_test.gleam.tmpl", filepath.Join("test", p.Name, "platform", "file_storage_test.gleam"),
		})
	}
	return renderAll(p, targets)
}

// Write adds howdy_storage to gleam.toml, the folder to .gitignore and the
// variables to .env.example, and creates the files. It returns the paths
// written or changed.
func (s *Storage) Write() ([]string, error) {
	p := s.Project
	files, err := s.Files()
	if err != nil {
		return nil, err
	}
	line := s.Dependency("howdy_storage") + "\n"
	changed, err := installFiles(p, line, files)
	if err != nil {
		return changed, err
	}
	if appendOnce(filepath.Join(p.Dir, ".gitignore"), "/storage", "\n# Files the app stores without a bucket\n/storage/\n") {
		changed = append(changed, ".gitignore")
	}
	if appendOnce(filepath.Join(p.Dir, ".env.example"), "STORAGE_SECRET", `
# File storage. Without a bucket, files go to STORAGE_PATH, and their URLs
# are signed with STORAGE_SECRET: at least 32 random bytes, the same on
# every node, such as the output of: openssl rand -base64 32
# STORAGE_PATH=storage
# STORAGE_SECRET=
# With a bucket they go there instead, on AWS S3 or a compatible service.
# S3_BUCKET=
# AWS_ACCESS_KEY_ID=
# AWS_SECRET_ACCESS_KEY=
# AWS_REGION=us-east-1
# For a service other than AWS, such as Cloudflare R2 or MinIO:
# AWS_ENDPOINT_URL_S3=https://<account>.r2.cloudflarestorage.com
`) {
		changed = append(changed, ".env.example")
	}
	return changed, nil
}

// Register puts the storage in the app's services, from the environment in
// main and the dev server and in memory in the tests, and serves the
// folder's URLs. For each file without the shape 'howdy new' gives it, the
// returned notes say what to add by hand.
func (s *Storage) Register() (notes []string) {
	p := s.Project
	module := "import " + p.Name + "/platform/file_storage"

	root := filepath.Join("src", p.Name+".gleam")
	err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		text, err := addServicesField(text, "files: storage.Storage")
		if err != nil {
			return "", err
		}
		if text, err = addToServices(text, "files", "file_storage.from_env()"); err != nil {
			return "", err
		}
		if !howdyNew.MatchString(text) {
			return "", errors.New("no howdy.new() in app")
		}
		// Ahead of the static catch-all.
		text = howdyNew.ReplaceAllString(text, "${0}${1}|> file_storage.serve\n")
		return addImports(text, "import howdy/storage", module), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Add storage to the app in %s: files: storage.Storage in Services, built in main with file_storage.from_env(), and |> file_storage.serve after howdy.new() in app (import howdy/storage and %s/platform/file_storage).", root, p.Name))
	}

	dev := p.devFile()
	err = editFile(filepath.Join(p.Dir, dev), func(text string) (string, error) {
		text, err := addToServices(text, "files", "file_storage.from_env()")
		if err != nil {
			return "", err
		}
		return addImports(text, module), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Give the dev server storage in %s: pass files: file_storage.from_env() to Services.", dev))
	}

	support := filepath.Join("test", p.Name, "support.gleam")
	if hasSupport(p) {
		err := editFile(filepath.Join(p.Dir, support), func(text string) (string, error) {
			text, err := addToServices(text, "files", "memory.new()")
			if err != nil {
				return "", err
			}
			return addImports(text, "import howdy/storage/memory"), nil
		})
		if err != nil {
			notes = append(notes, fmt.Sprintf("Give the tests storage in %s: pass files: memory.new() to Services (import howdy/storage/memory).", support))
		}
	}
	return notes
}

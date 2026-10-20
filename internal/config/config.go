// Package config reads the optional howdy config file: the user's own
// defaults for 'howdy new', in TOML.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Env names a config file to read instead of the usual places. Set to
// "none" it turns the config file off.
const Env = "HOWDY_CONFIG"

// Config is what the config file sets. A setting the file leaves out is
// nil, so howdy's own default applies.
type Config struct {
	// Path is the file the settings were read from, or "" without one.
	Path string `toml:"-"`
	New  New    `toml:"new"`
}

// New holds the defaults for 'howdy new', each standing in for a flag.
type New struct {
	// Source is "git" or "hex", for --source.
	Source *string `toml:"source"`
	// Howdy is the Howdy release, such as "1.0.0", for --howdy.
	Howdy *string `toml:"howdy"`
	// Repository is the Git repository the packages come from with the
	// git source, such as a fork or a private mirror.
	Repository *string `toml:"repository"`
	// Database is "postgres", "sqlite" or "none", for --database and
	// --no-database.
	Database *string `toml:"database"`
	// With lists the packages to set up, for --with.
	With *[]string `toml:"with"`
	// Minimal, APIOnly and SkipDeps stand for --minimal, --api-only and
	// --skip-deps.
	Minimal  *bool `toml:"minimal"`
	APIOnly  *bool `toml:"api_only"`
	SkipDeps *bool `toml:"skip_deps"`
	// Agents set to false stands for --no-agents.
	Agents *bool `toml:"agents"`
	// Docker set to false stands for --no-docker.
	Docker *bool `toml:"docker"`
}

// Paths are the places Load looks for the config file, in order: the
// file named by HOWDY_CONFIG when it is set, otherwise howdy/config.toml
// in the user's config directory ($XDG_CONFIG_HOME or ~/.config on Linux,
// ~/Library/Application Support on macOS, %AppData% on Windows), and on
// macOS then ~/.config/howdy/config.toml, where command line tools are
// often configured there too.
func Paths() []string {
	if env := os.Getenv(Env); env != "" {
		if env == "none" {
			return nil
		}
		return []string{env}
	}
	var paths []string
	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "howdy", "config.toml"))
	}
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, ".config", "howdy", "config.toml"))
		}
	}
	return paths
}

// Load reads the first config file of Paths there is. Without one it
// returns an empty Config; a file named by HOWDY_CONFIG must exist.
func Load() (*Config, error) {
	named := os.Getenv(Env) != ""
	for _, path := range Paths() {
		_, err := os.Stat(path)
		switch {
		case err == nil:
			return Read(path)
		case named:
			return nil, fmt.Errorf("%s names a config file that cannot be read: %w", Env, err)
		case !errors.Is(err, os.ErrNotExist):
			return nil, fmt.Errorf("config file %s: %w", path, err)
		}
	}
	return &Config{}, nil
}

// Read reads the config file at path. A setting it does not know is an
// error, so a misspelt one is not silently ignored.
func Read(path string) (*Config, error) {
	c := &Config{Path: path}
	meta, err := toml.DecodeFile(path, c)
	if err != nil {
		return nil, fmt.Errorf("config file %s: %w", path, err)
	}
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		keys := make([]string, len(unknown))
		for i, key := range unknown {
			keys[i] = key.String()
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("config file %s: unknown setting %s; see 'howdy config --help' for the settings there are", path, strings.Join(keys, ", "))
	}
	return c, nil
}

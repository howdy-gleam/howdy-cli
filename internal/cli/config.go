package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"howdy/internal/config"
	"howdy/internal/scaffold"
	"howdy/internal/tui"
)

// applyConfig takes each default of 'howdy new' from the config file c
// whose flag was not given, checking every setting the file has, used or
// not, so a mistake in it does not wait for the day it matters.
func (opts *newOptions) applyConfig(flags *pflag.FlagSet, c *config.Config) error {
	n := c.New
	bad := func(key string, err error) error {
		return fmt.Errorf("config file %s: new.%s: %w", c.Path, key, err)
	}
	if n.Source != nil {
		if _, err := scaffold.ParseSource(*n.Source); err != nil {
			return bad("source", err)
		}
		if !flags.Changed("source") {
			opts.source = *n.Source
		}
	}
	if n.Howdy != nil {
		if _, err := scaffold.ParseRelease(*n.Howdy); err != nil {
			return bad("howdy", err)
		}
		if !flags.Changed("howdy") {
			opts.release = *n.Howdy
		}
	}
	if n.Repository != nil {
		repository := strings.TrimSpace(*n.Repository)
		if repository == "" {
			return bad("repository", errors.New("is empty: leave it out to use "+scaffold.Repository))
		}
		opts.repository = repository
	}
	if n.Database != nil {
		none := strings.EqualFold(strings.TrimSpace(*n.Database), "none")
		if _, err := scaffold.ParseDatabase(*n.Database); err != nil && !none {
			return bad("database", fmt.Errorf("%w, or none", err))
		}
		if !flags.Changed("database") && !flags.Changed("no-database") {
			if none {
				opts.noDatabase = true
			} else {
				opts.database = *n.Database
			}
		}
	}
	if n.With != nil {
		if _, err := withPackages(*n.With); err != nil {
			return bad("with", err)
		}
		if !flags.Changed("with") {
			opts.with = *n.With
		}
	}
	setBool := func(value *bool, flag string, target *bool, invert bool) {
		if value != nil && !flags.Changed(flag) {
			*target = *value != invert
		}
	}
	setBool(n.Minimal, "minimal", &opts.minimal, false)
	setBool(n.APIOnly, "api-only", &opts.apiOnly, false)
	setBool(n.SkipDeps, "skip-deps", &opts.skipDeps, false)
	setBool(n.Agents, "no-agents", &opts.noAgents, true)
	opts.config = c.Path
	return nil
}

func configCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show the config file and the defaults it sets for howdy new",
		Long: `Show where howdy looks for its config file, the one it uses, and the
defaults 'howdy new' starts from: each one taken from the file, or howdy's
own when the file leaves it out.

The config file is TOML and optional. howdy reads the first of these there is:

  Linux     $XDG_CONFIG_HOME/howdy/config.toml, or ~/.config/howdy/config.toml
  macOS     ~/Library/Application Support/howdy/config.toml, then
            ~/.config/howdy/config.toml
  Windows   %AppData%\howdy\config.toml

Set HOWDY_CONFIG to the path of another file to read that one instead, or
to none to read no file at all. 'howdy config init' writes a file to start
from, with every setting commented out.

Its [new] table holds the defaults for 'howdy new'. A flag given on the
command line always wins, and 'howdy new --no-config' ignores the file:

  [new]
  source = "hex"                # git or hex, as --source
  howdy = "` + scaffold.Release + `"              # the Howdy release, as --howdy
  repository = "https://..."    # the Git repository the packages come from
  database = "sqlite"           # postgres, sqlite or none
  with = ["auth", "storage"]    # packages to set up, as --with
  minimal = false               # as --minimal
  api_only = false              # as --api-only
  agents = true                 # false for --no-agents
  skip_deps = false             # as --skip-deps

repository is for the git source: a fork or a private mirror of the Howdy
repository, with the same release tags. 'howdy add' follows the project
rather than the file, adding each package from wherever gleam.toml takes
howdy.

A setting howdy does not know is an error, so a misspelt one is not
silently ignored.`,
		Example: "  howdy config\n  howdy config --json\n  howdy config init\n  HOWDY_CONFIG=./ci.toml howdy new my_site",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfig(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the file and the settings as one JSON object")
	cmd.AddCommand(configInitCommand())
	return cmd
}

// setting is one default of 'howdy new', as 'howdy config' shows it.
type setting struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
	// From is "config" when the file sets it and "default" otherwise.
	From string `json:"from"`
}

func runConfig(asJSON bool) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	opts := newCommandDefaults()
	if err := opts.applyConfig(pflag.NewFlagSet("", pflag.ContinueOnError), c); err != nil {
		return err
	}
	from := func(set bool) string {
		if set {
			return "config"
		}
		return "default"
	}
	n := c.New
	database := opts.database
	if opts.noDatabase {
		database = "none"
	}
	repository := opts.repository
	if repository == "" {
		repository = scaffold.Repository
	}
	with := opts.with
	if with == nil {
		with = []string{}
	}
	settings := []setting{
		{"source", opts.source, from(n.Source != nil)},
		{"howdy", opts.release, from(n.Howdy != nil)},
		{"repository", repository, from(n.Repository != nil)},
		{"database", database, from(n.Database != nil)},
		{"with", with, from(n.With != nil)},
		{"minimal", opts.minimal, from(n.Minimal != nil)},
		{"api_only", opts.apiOnly, from(n.APIOnly != nil)},
		{"agents", !opts.noAgents, from(n.Agents != nil)},
		{"skip_deps", opts.skipDeps, from(n.SkipDeps != nil)},
	}
	searched := config.Paths()
	if asJSON {
		if searched == nil {
			searched = []string{}
		}
		return printJSON(os.Stdout, struct {
			Path     string    `json:"path"`
			Searched []string  `json:"searched"`
			New      []setting `json:"new"`
		}{c.Path, searched, settings})
	}

	switch {
	case c.Path != "":
		fmt.Println("Config file: " + displayPath(c.Path))
	case len(searched) == 0:
		fmt.Println("No config file: " + config.Env + " is none.")
	default:
		shown := make([]string, len(searched))
		for i, path := range searched {
			shown[i] = displayPath(path)
		}
		fmt.Println("No config file. howdy looks for " + strings.Join(shown, ", then ") + ";")
		fmt.Println("'howdy config init' writes one to start from.")
	}
	fmt.Println()
	fmt.Println("Defaults for howdy new:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, s := range settings {
		value := fmt.Sprint(s.Value)
		if list, ok := s.Value.([]string); ok {
			value = strings.Join(list, ", ")
			if value == "" {
				value = "(none)"
			}
		}
		from := "howdy's default"
		if s.From == "config" {
			from = "from the config file"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\n", s.Key, value, tui.Detail(from))
	}
	return w.Flush()
}

// newCommandDefaults are the options of 'howdy new' before any flag or
// config file: howdy's own defaults.
func newCommandDefaults() *newOptions {
	return &newOptions{
		database: string(scaffold.Postgres),
		release:  scaffold.Release,
		source:   string(scaffold.DefaultSource),
	}
}

func configInitCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a config file to start from",
		Long: `Write a config file where howdy looks for one first (or to HOWDY_CONFIG
when that is set), with every setting commented out at howdy's own
default. Uncomment a line to change that default for 'howdy new'.

An existing file is left alone unless --force is given.`,
		Example: "  howdy config init\n  HOWDY_CONFIG=./ci.toml howdy config init",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigInit(force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace a config file that is already there")
	return cmd
}

func runConfigInit(force bool) error {
	paths := config.Paths()
	if len(paths) == 0 {
		return errors.New(config.Env + " is none, so there is nowhere to write a config file; unset it or name a file")
	}
	path := paths[0]
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to replace it", displayPath(path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(starterConfig()), 0o644); err != nil {
		return err
	}
	fmt.Println(tui.Success("Wrote " + displayPath(path)))
	return nil
}

// starterConfig is the file 'howdy config init' writes: every setting,
// commented out at howdy's own default.
func starterConfig() string {
	return `# Defaults for howdy. A flag given on the command line always wins, and
# 'howdy new --no-config' ignores this file. 'howdy config' shows the
# settings in use. Uncomment a line to change a default.

[new]
# Where to download the Howdy packages from: "git" (the release tag on
# GitHub) or "hex".
# source = "` + string(scaffold.DefaultSource) + `"

# The Howdy release every package comes from. Leave it out to follow the
# release this howdy's templates are written for, which moves on with
# 'howdy upgrade'.
# howdy = "` + scaffold.Release + `"

# The Git repository the packages come from with source = "git": a fork or
# a private mirror of Howdy, with the same release tags.
# repository = "` + scaffold.Repository + `"

# The database: "postgres", "sqlite" or "none".
# database = "postgres"

# Howdy packages to set up in every new project, as --with does: mail,
# auth, flags, storage, jobs, notifications, i18n, openapi and remote.
# with = ["mail", "auth"]

# Leave out the admin, telemetry and the console, as --minimal does.
# minimal = false

# A JSON backend with no pages, as --api-only makes.
# api_only = false

# AGENTS.md, CLAUDE.md and the skills in .claude/skills/ for coding agents;
# false leaves them out, as --no-agents does.
# agents = true

# Do not run 'gleam deps download' after creating the files.
# skip_deps = false
`
}

// displayPath is path with the home directory written as ~.
func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return path
}

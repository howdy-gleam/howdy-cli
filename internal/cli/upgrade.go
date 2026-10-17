package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"howdy/internal/tui"
	"howdy/internal/upgrade"
)

type upgradeOptions struct {
	check, force, plain, json bool
	version                   string
}

func upgradeCommand() *cobra.Command {
	opts := upgradeOptions{}
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade howdy to the latest release",
		Long: `Upgrade howdy to the latest release.

Looks up the newest release of howdy on GitHub (github.com/` + upgrade.Repo + `),
downloads the build for this platform, checks it against the release's
checksums and replaces this howdy with it. Nothing changes when howdy is
already up to date.

Pass --check to only see whether a newer release is out, or --version to
install a particular release, older or newer. A development build (one not
made from a release) has no version to compare, so it is only replaced with
--force or --version.

If howdy is installed somewhere you cannot write to, run the upgrade with
the permissions to change it, such as with sudo. Set GITHUB_TOKEN if GitHub
rate limits the lookup.`,
		Example: "  howdy upgrade\n  howdy upgrade --check\n  howdy upgrade --version 1.2.0\n  howdy upgrade --check --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(cmd.Context(), &opts)
		},
	}
	cmd.Flags().BoolVar(&opts.check, "check", false, "only report whether a newer release is out")
	cmd.Flags().StringVar(&opts.version, "version", "", "install this release, such as 1.2.0, instead of the latest")
	cmd.Flags().BoolVar(&opts.force, "force", false, "install even when howdy is up to date or a development build")
	cmd.Flags().BoolVar(&opts.plain, "plain", false, "print progress as plain lines instead of animating")
	cmd.Flags().BoolVar(&opts.json, "json", false, "print the result as one JSON object: the current and target versions and whether howdy was upgraded")
	return cmd
}

// upgradeResult is what --json prints.
type upgradeResult struct {
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Current string `json:"current"`
	Target  string `json:"target"`
	// Available is whether Target is newer than Current.
	Available bool   `json:"available"`
	Upgraded  bool   `json:"upgraded"`
	Path      string `json:"path,omitempty"`
	URL       string `json:"url"`
}

func runUpgrade(ctx context.Context, opts *upgradeOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// A build made without -ldflags reports "dev", and one made by hand may
	// carry anything else that is not a version.
	_, devErr := upgrade.Compare(Version, Version)
	dev := devErr != nil
	if dev && !opts.check && !opts.force && opts.version == "" {
		return fmt.Errorf("this howdy is a development build (%s), not a release, so there is no version to compare.\n  Pass --force to replace it with the latest release, or --version to choose one", Version)
	}
	if opts.version != "" {
		if _, err := upgrade.Compare(opts.version, opts.version); err != nil {
			return err
		}
	}

	client := upgrade.NewClient()
	var (
		release   upgrade.Release
		binary    []byte
		exe       string
		available bool
		install   bool
	)
	command := "howdy upgrade"
	if opts.check {
		command += " --check"
	}
	skip := func() (string, bool) {
		switch {
		case opts.check:
			return "skipped (--check)", true
		case !install:
			return "already up to date", true
		}
		return "", false
	}
	steps := []tui.Step{
		{
			Title: "Looking up the release",
			Run: func(r tui.Reporter) (string, error) {
				var err error
				if opts.version != "" {
					r.Detail("release v" + opts.version)
					release, err = client.Tagged(ctx, opts.version)
				} else {
					r.Detail("latest release of " + client.Repo)
					release, err = client.Latest(ctx)
				}
				if err != nil {
					return "", err
				}
				cmp := -1
				if !dev {
					if cmp, err = upgrade.Compare(Version, release.Version()); err != nil {
						return "", err
					}
				}
				available = cmp < 0
				install = opts.force || cmp < 0 || (opts.version != "" && cmp != 0)
				return fmt.Sprintf("%s (this is %s)", release.Version(), Version), nil
			},
		},
		{
			Title: "Downloading " + upgrade.ArchiveName(runtime.GOOS, runtime.GOARCH),
			Skip:  skip,
			Run: func(r tui.Reporter) (string, error) {
				var err error
				if exe, err = upgrade.Executable(); err != nil {
					return "", err
				}
				if binary, err = client.Download(ctx, release); err != nil {
					return "", err
				}
				return fmt.Sprintf("%.1f MB, checksum verified", float64(len(binary))/(1<<20)), nil
			},
		},
		{
			Title: "Installing",
			Skip:  skip,
			Run: func(r tui.Reporter) (string, error) {
				return exe, upgrade.Replace(exe, binary)
			},
		},
	}

	var err error
	if opts.json {
		err = tui.RunSilent(steps, &silentReporter{})
	} else {
		err = tui.Run(command, steps, opts.plain)
	}
	if err != nil {
		return err
	}
	upgraded := install && !opts.check

	if opts.json {
		r := upgradeResult{
			Command: command, OK: true, Current: Version, Target: release.Version(),
			Available: available, Upgraded: upgraded, URL: release.URL,
		}
		if upgraded {
			r.Path = exe
		}
		return printJSON(os.Stdout, r)
	}

	fmt.Println()
	switch {
	case upgraded:
		fmt.Println(tui.Title("howdy is now " + release.Version()))
		fmt.Println(tui.Detail("  " + exe))
		fmt.Println(tui.Detail("  What's new: " + release.URL))
	case opts.check && (available || dev):
		fmt.Println(tui.Title(fmt.Sprintf("howdy %s is out (this is %s)", release.Version(), Version)))
		run := "howdy upgrade"
		if dev {
			run += " --force"
		}
		fmt.Println(tui.Detail("  Run '" + run + "' to install it. What's new: " + release.URL))
	case opts.check && release.Version() != Version:
		fmt.Println(tui.Title(fmt.Sprintf("This is howdy %s, newer than %s", Version, release.Version())))
		fmt.Println(tui.Detail("  Run 'howdy upgrade --version " + release.Version() + "' to install it."))
	default:
		fmt.Println(tui.Title(fmt.Sprintf("howdy %s is up to date", Version)))
	}
	fmt.Println()
	return nil
}

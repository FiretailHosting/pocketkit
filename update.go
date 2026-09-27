package pocketkit

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"
)

// Version is the running build's version.
//
// Release builds stamp it:
//
//	go build -ldflags "-X github.com/Ovi1kanobe/pocketkit.Version=v1.2.3"
//
// Left empty it falls back to the version the module was built from, and
// finally to "dev", which never satisfies an update check.
var Version = ""

// ChecksumsFile is the name of the checksum asset the release workflow
// publishes. Every download is verified against it before anything is replaced.
const ChecksumsFile = "checksums.txt"

// devVersion marks a build that did not come from a release.
const devVersion = "dev"

// BuildVersion reports the running version.
func BuildVersion() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return devVersion
}

// moduleSlug derives "owner/repo" from the app's own module path, so an app
// scaffolded at github.com/you/app needs no update configuration at all.
func moduleSlug() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	parts := strings.Split(info.Main.Path, "/")
	if len(parts) < 3 || parts[0] != "github.com" {
		return ""
	}
	return parts[1] + "/" + parts[2]
}

// slug returns the repository releases are pulled from.
func (a *App) slug() string {
	if a.cfg.Slug != "" {
		return a.cfg.Slug
	}
	return moduleSlug()
}

// bindUpdateCommand adds `update` to every pocketkit app.
func (a *App) bindUpdateCommand() {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update this binary to the latest GitHub release",
		Long: "Replaces the running binary with the latest release asset for this\n" +
			"platform, after verifying it against " + ChecksumsFile + ".\n\n" +
			"For a private repository, supply a token with --token or by setting\n" +
			"GITHUB_TOKEN; releases are then fetched through the GitHub API.",
		RunE: func(cmd *cobra.Command, args []string) error {
			check, _ := cmd.Flags().GetBool("check")
			token, _ := cmd.Flags().GetString("token")
			return a.runUpdate(cmd.Context(), check, token)
		},
	}

	cmd.Flags().Bool("check", false, "report whether an update is available without installing it")
	cmd.Flags().String("token", "", "GitHub token for a private repository (defaults to $GITHUB_TOKEN)")

	a.RootCmd.AddCommand(cmd)

	a.RootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version of this binary",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s %s %s/%s\n", a.RootCmd.Name(), BuildVersion(), runtime.GOOS, runtime.GOARCH)
		},
	})
}

func (a *App) runUpdate(ctx context.Context, checkOnly bool, token string) error {
	slug := a.slug()
	if slug == "" {
		return fmt.Errorf("cannot tell which GitHub repository to update from; " +
			"pass pocketkit.WithUpdates(\"owner/repo\")")
	}

	current := BuildVersion()
	if current == devVersion && !checkOnly {
		return fmt.Errorf("this is a %s build with no version to compare against; "+
			"install a release, or build with -ldflags \"-X github.com/Ovi1kanobe/pocketkit.Version=vX.Y.Z\"", devVersion)
	}

	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{APIToken: token})
	if err != nil {
		return fmt.Errorf("github source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
		// Assets are downloaded through the API and checked against the
		// published checksums before anything on disk is touched.
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: ChecksumsFile},
	})
	if err != nil {
		return fmt.Errorf("updater: %w", err)
	}

	release, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(slug))
	if err != nil {
		return fmt.Errorf("looking up the latest release of %s: %w%s", slug, err, tokenHint(token))
	}
	if !found {
		return fmt.Errorf("no release of %s has an asset for %s/%s", slug, runtime.GOOS, runtime.GOARCH)
	}

	if current != devVersion && !release.GreaterThan(current) {
		fmt.Printf("%s is already the latest version.\n", current)
		return nil
	}

	if checkOnly {
		fmt.Printf("%s is available (running %s).\n%s\n", release.Version(), current, release.URL)
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating this binary: %w", err)
	}

	fmt.Printf("Updating %s -> %s...\n", current, release.Version())

	if err := updater.UpdateTo(ctx, release, exe); err != nil {
		return fmt.Errorf("installing %s: %w%s", release.Version(), err, tokenHint(token))
	}

	fmt.Printf("Updated to %s.\n", release.Version())
	return nil
}

// tokenHint explains the most common cause of failure: a private repository
// reached without credentials, which the API reports as a plain 404.
func tokenHint(token string) string {
	if token != "" {
		return ""
	}
	return "\n\nIf this repository is private, pass --token or set GITHUB_TOKEN."
}

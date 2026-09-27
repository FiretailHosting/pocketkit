package pocketkit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	selfupdate "github.com/FiretailHosting/go-selfupdate"
	"github.com/spf13/cobra"
)

// Version is the running build's version.
//
// Release builds stamp it with the tag:
//
//	go build -ldflags "-X github.com/FiretailHosting/pocketkit.Version=v1.2.3"
//
// Left empty it falls back to the version the module was built from, and
// finally to "dev", which never satisfies an update check.
var Version = ""

// devVersion marks a build that did not come from a release.
const devVersion = "dev"

// BuildVersion reports the running version as stamped, tag form included.
func BuildVersion() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		// Only a real tagged build counts. A module that has ever been tagged
		// reports a pseudo-version here for ordinary builds, e.g.
		// "0.1.1-0.20260927033104-abcdef+dirty", which is neither the running
		// code's version nor something the updater can compare.
		if v := info.Main.Version; isReleaseVersion(strings.TrimPrefix(v, "v")) {
			return v
		}
	}
	return devVersion
}

// isReleaseVersion reports whether v is a bare X.Y.Z of decimal numbers, the
// only shape the updater compares and the only shape --version may print.
func isReleaseVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// bareVersion is BuildVersion without the leading "v".
//
// The updater compares bare X.Y.Z strings and, after downloading, runs the new
// binary with --version and requires exactly that on stdout. So this is both
// what we report and what we compare.
func bareVersion() string {
	return strings.TrimPrefix(BuildVersion(), "v")
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

// binaryName is the release asset's base name. It comes from the module path
// rather than os.Args[0], so it matches what the release workflow built even
// when the binary has been renamed on disk.
func binaryName() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path != "" {
		return path.Base(info.Main.Path)
	}
	return ""
}

// slug returns the repository releases are pulled from.
func (a *App) slug() string {
	if a.cfg.Slug != "" {
		return a.cfg.Slug
	}
	return moduleSlug()
}

// updater builds the self-updater for this app.
func (a *App) updater(token string) (selfupdate.Updater, error) {
	slug := a.slug()
	owner, repo, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || repo == "" {
		return selfupdate.Updater{}, fmt.Errorf(
			"cannot tell which GitHub repository to update from; pass pocketkit.WithUpdates(\"owner/repo\")")
	}

	name := binaryName()
	if name == "" {
		return selfupdate.Updater{}, fmt.Errorf("cannot determine this binary's release name")
	}

	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	return selfupdate.Updater{
		Owner:   owner,
		Repo:    repo,
		Name:    name,
		Version: bareVersion(),
		Token:   token,
	}, nil
}

// bindUpdateCommand adds `update`, `version` and a --version flag to every app.
func (a *App) bindUpdateCommand() {
	// The updater runs the downloaded binary with --version and requires the
	// bare X.Y.Z on stdout, so this template must stay exactly this plain.
	a.RootCmd.Version = bareVersion()
	a.RootCmd.SetVersionTemplate("{{.Version}}\n")

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update this binary to the latest GitHub release",
		Long: "Replaces the running binary with the latest release asset for this\n" +
			"machine, after verifying its SHA-256 against checksums.txt and checking\n" +
			"that the new binary reports the expected version.\n\n" +
			"For a private repository, supply a token with --token or by setting\n" +
			"GITHUB_TOKEN; releases are then fetched through the GitHub API.",
		// An update failure is an ordinary runtime error -- usually a missing
		// token or no matching asset -- not a misuse of the command, so the
		// message should not be buried under a usage dump.
		SilenceUsage: true,
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
		Short: "Print this binary's version, OS and architecture",
		Long: "For the bare version on its own -- which is what the updater checks --\n" +
			"use --version instead.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s %s %s/%s\n", binaryName(), BuildVersion(), runtime.GOOS, runtime.GOARCH)
		},
	})
}

func (a *App) runUpdate(ctx context.Context, checkOnly bool, token string) error {
	updater, err := a.updater(token)
	if err != nil {
		return err
	}

	current := updater.Version
	if current == devVersion || current == "" {
		return fmt.Errorf("this is a %s build with no version to compare against; "+
			"install a release, or build with -ldflags \"-X github.com/FiretailHosting/pocketkit.Version=vX.Y.Z\"",
			devVersion)
	}

	if checkOnly {
		return a.reportAvailable(ctx, updater)
	}

	if runtime.GOOS != "linux" {
		return fmt.Errorf("self-update targets Linux amd64/arm64; this is %s/%s. "+
			"Releases publish Linux binaries only, so build from source here",
			runtime.GOOS, runtime.GOARCH)
	}

	installed, err := updater.Install(ctx, mustExecutable())
	if err != nil {
		return fmt.Errorf("updating %s: %w%s", updater.Name, err,
			diagnoseAccess(ctx, a.slug(), updater.Token))
	}

	if installed == current {
		fmt.Printf("v%s is already the latest version.\n", current)
		return nil
	}

	fmt.Printf("Updated v%s -> v%s.\n", current, installed)
	return nil
}

// reportAvailable answers `update --check`.
//
// The updater's own Check is built for background notices: it caches for 24
// hours and reports failures as "nothing new", which would turn an explicit
// check into a silent lie. So an explicit check asks GitHub directly.
func (a *App) reportAvailable(ctx context.Context, updater selfupdate.Updater) error {
	slug := a.slug()

	tag, err := latestTag(ctx, slug, updater.Token)
	if err != nil {
		return fmt.Errorf("looking up the latest release of %s: %w%s", slug, err,
			diagnoseAccess(ctx, slug, updater.Token))
	}

	latest := strings.TrimPrefix(tag, "v")
	if !selfupdate.Newer(latest, updater.Version) {
		fmt.Printf("v%s is already the latest version.\n", updater.Version)
		return nil
	}

	fmt.Printf("v%s is available (running v%s).\nhttps://github.com/%s/releases/tag/%s\n",
		latest, updater.Version, slug, tag)
	return nil
}

// latestTag reads the newest release tag straight from the API.
func latestTag(ctx context.Context, slug, token string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+slug+"/releases/latest", http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}

	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.Tag == "" {
		return "", fmt.Errorf("latest release has no tag")
	}
	return release.Tag, nil
}

func mustExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	return exe
}

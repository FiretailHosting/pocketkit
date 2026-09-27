package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func cmdNew(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: pocketkit new <module-path> [dir]")
	}
	modulePath := args[0]

	dir := filepath.Base(modulePath)
	if len(args) > 1 {
		dir = args[1]
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s already exists", dir)
	}

	name := filepath.Base(dir)

	files := map[string]string{
		"go.mod":                          fmt.Sprintf(tmplGoMod, modulePath),
		"main.go":                         fmt.Sprintf(tmplMain, modulePath),
		"api/ping/GET.go":                 tmplPing,
		"migrations/doc.go":               tmplMigrationsDoc,
		"internal/auth/auth.go":           fmt.Sprintf(tmplAuthConfig, name),
		"migrations/1700000000_rauthy.go": fmt.Sprintf(tmplAuthMigration, modulePath),
		".gitignore":                      tmplGitignore,
		"README.md":                       fmt.Sprintf(tmplReadme, name),
		"frontend/build/.gitkeep":         tmplGitkeep,
		".github/workflows/release.yml":   tmplReleaseWorkflow,
	}

	for rel, content := range files {
		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		return err
	}

	fmt.Printf("created %s\n", dir)

	// Generate the wiring now so the app builds and serves its routes straight
	// away. pocketkit_gen.go is committed, so this is the first version of it.
	if _, _, err := generate(dir); err != nil {
		fmt.Fprintf(os.Stderr, "pocketkit: could not generate route wiring: %v\n", err)
	}

	if err := scaffoldFrontend(dir); err != nil {
		fmt.Fprintf(os.Stderr, "pocketkit: frontend scaffold skipped: %v\n", err)
	}

	fmt.Printf("\nNext:\n  cd %s\n  go mod tidy\n  pocketkit dev\n", dir)
	fmt.Println("\nAfter the first boot applies migrations, run this in a second terminal:\n  pocketkit types\nThen complete the typed-client setup in README.md.")
	return nil
}

// scaffoldFrontend creates the SvelteKit app and applies pocketkit's frontend
// opinions: static adapter, SPA fallback, no SSR, and a dev proxy to the
// PocketBase server so the frontend talks to the same origin in dev and in prod.
func scaffoldFrontend(dir string) error {
	runner, pre, err := svRunner()
	if err != nil {
		return err
	}

	pm := "npm"
	if _, err := exec.LookPath("bun"); err == nil {
		pm = "bun"
	}

	argv := append(pre,
		"create",
		"--template", "minimal",
		"--types", "ts",
		"--no-add-ons",
		"--install", pm,
		"--no-dir-check",
		"frontend",
	)

	cmd := exec.Command(runner, argv...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sv create: %w", err)
	}

	add := exec.Command(pm, "add", "-d", "@sveltejs/adapter-static")
	add.Dir = filepath.Join(dir, "frontend")
	add.Stdout = os.Stdout
	add.Stderr = os.Stderr
	if err := add.Run(); err != nil {
		return fmt.Errorf("installing adapter-static: %w", err)
	}

	overrides := map[string]string{
		"frontend/svelte.config.js":      tmplSvelteConfig,
		"frontend/vite.config.ts":        tmplViteConfig,
		"frontend/src/routes/+layout.ts": tmplLayoutTS,
		"frontend/src/lib/pb.ts":         tmplPBClient,
	}
	for rel, content := range overrides {
		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err
		}
	}

	pbAdd := exec.Command(pm, "add", "pocketbase")
	pbAdd.Dir = filepath.Join(dir, "frontend")
	pbAdd.Stdout = os.Stdout
	pbAdd.Stderr = os.Stderr
	if err := pbAdd.Run(); err != nil {
		return fmt.Errorf("installing pocketbase sdk: %w", err)
	}

	return nil
}

func svRunner() (string, []string, error) {
	if path, err := exec.LookPath("bunx"); err == nil {
		return path, []string{"sv"}, nil
	}
	if path, err := exec.LookPath("npx"); err == nil {
		return path, []string{"--yes", "sv"}, nil
	}
	return "", nil, fmt.Errorf("neither bunx nor npx found on PATH")
}

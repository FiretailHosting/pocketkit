package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// TypegenVersion pins the pocketbase-typegen release pocketkit generates with,
// so type output does not drift between machines or over time.
const TypegenVersion = "1.5.0"

// cmdTypes regenerates the frontend's TypeScript types from the local database.
//
// pocketbase-typegen can also read a running instance over HTTP, but pocketkit
// reads pb_data directly: it needs no credentials and no running server, and it
// reflects the schema the committed migrations actually produced.
func cmdTypes(args []string) error {
	fs := flag.NewFlagSet("types", flag.ExitOnError)
	db := fs.String("db", "pb_data/data.db", "path to the PocketBase database")
	out := fs.String("out", "frontend/src/lib/pocketbase-types.ts", "TypeScript output path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, _, err := moduleRoot(".")
	if err != nil {
		return err
	}

	dbPath := filepath.Join(root, *db)
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("no database at %s -- run `go tool pocketkit dev` once to create it", *db)
	}

	outPath := filepath.Join(root, *out)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	runner, runnerArgs, err := typegenRunner()
	if err != nil {
		return err
	}
	runnerArgs = append(runnerArgs, "--db", dbPath, "--out", outPath)

	cmd := exec.Command(runner, runnerArgs...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pocketbase-typegen: %w", err)
	}

	fmt.Printf("types written to %s\n", *out)
	return nil
}

// typegenRunner prefers bunx and falls back to npx.
func typegenRunner() (string, []string, error) {
	pkg := "pocketbase-typegen@" + TypegenVersion
	if path, err := exec.LookPath("bunx"); err == nil {
		return path, []string{pkg}, nil
	}
	if path, err := exec.LookPath("npx"); err == nil {
		return path, []string{"--yes", pkg}, nil
	}
	return "", nil, fmt.Errorf("neither bunx nor npx found on PATH; install Bun or Node to generate types")
}

// Command pocketkit is the developer CLI for pocketkit apps.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

const usage = `pocketkit -- an opinionated framework for PocketBase

Usage:
  pocketkit <command> [flags]

Commands:
  new <name>   Scaffold a new pocketkit app
  gen          Regenerate route and hook wiring from api/ and hooks/
  routes       List the routes and hooks pocketkit discovered
  dev          Regenerate, then run the app with live reload
  types        Generate frontend TypeScript types from the current schema
  check        Vet and test packages, including parameter routes
  help         Show this message
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "new":
		err = cmdNew(os.Args[2:])
	case "gen":
		err = cmdGen(os.Args[2:])
	case "routes":
		err = cmdRoutes(os.Args[2:])
	case "dev":
		err = cmdDev(os.Args[2:])
	case "types":
		err = cmdTypes(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "pocketkit: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "pocketkit: %v\n", err)
		os.Exit(1)
	}
}

// moduleRoot walks up from dir looking for the go.mod that owns it, and returns
// the directory and the declared module path.
func moduleRoot(dir string) (root, modulePath string, err error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	for {
		gomod := filepath.Join(abs, "go.mod")
		if b, err := os.ReadFile(gomod); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				line = strings.TrimSpace(line)
				if after, ok := strings.CutPrefix(line, "module "); ok {
					return abs, strings.TrimSpace(after), nil
				}
			}
			return "", "", fmt.Errorf("%s declares no module path", gomod)
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", "", fmt.Errorf("no go.mod found in %s or any parent", dir)
		}
		abs = parent
	}
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
}

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ovi1kanobe/pocketkit/internal/gen"
	"github.com/Ovi1kanobe/pocketkit/internal/scan"
)

// GenFile is the name of the file pocketkit writes at the app root.
const GenFile = "pocketkit_gen.go"

// generate scans the app and writes GenFile. It reports whether the file changed.
func generate(dir string) (*scan.Result, bool, error) {
	root, modulePath, err := moduleRoot(dir)
	if err != nil {
		return nil, false, err
	}

	res, err := scan.App(root, modulePath)
	if err != nil {
		return nil, false, err
	}

	pkgName, err := mainPackageName(root)
	if err != nil {
		return nil, false, err
	}

	src, err := gen.File(res, pkgName)
	if err != nil {
		return nil, false, err
	}

	target := filepath.Join(root, GenFile)
	if existing, err := os.ReadFile(target); err == nil && string(existing) == string(src) {
		return res, false, nil
	}
	if err := os.WriteFile(target, src, 0o644); err != nil {
		return nil, false, err
	}
	return res, true, nil
}

// mainPackageName reads the package clause of the app root, defaulting to main
// for a fresh app that has no .go files at the root yet.
func mainPackageName(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" {
			continue
		}
		if name == GenFile {
			continue
		}
		pkg, err := packageClause(filepath.Join(root, name))
		if err != nil {
			continue
		}
		return pkg, nil
	}
	return "main", nil
}

func cmdGen(args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	res, changed, err := generate(dir)
	if err != nil {
		return err
	}

	state := "unchanged"
	if changed {
		state = "written"
	}
	fmt.Printf("%s %s (%d route(s), %d hook(s))\n", GenFile, state, len(res.Routes), len(res.Hooks))
	return nil
}

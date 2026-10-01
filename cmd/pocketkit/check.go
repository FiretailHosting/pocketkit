package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// cmdCheck vets and tests the whole app. `go test ./...` skips directories whose
// name starts with "_", which is exactly how pocketkit spells path parameters,
// so route packages are passed explicitly to make sure nothing goes unchecked.
func cmdCheck(args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	root, _, err := moduleRoot(dir)
	if err != nil {
		return err
	}

	res, _, err := generate(dir)
	if err != nil {
		return err
	}

	seen := map[string]bool{"./...": true}
	pkgs := []string{"./..."}
	for _, r := range res.Routes {
		if !seen[r.ImportPath] {
			seen[r.ImportPath] = true
			pkgs = append(pkgs, r.ImportPath)
		}
	}
	for _, h := range res.Hooks {
		if !seen[h.ImportPath] {
			seen[h.ImportPath] = true
			pkgs = append(pkgs, h.ImportPath)
		}
	}
	sort.Strings(pkgs[1:])

	return checkPackages(root, pkgs)
}

func checkPackages(root string, pkgs []string) error {
	testArgs := []string{"test"}
	raceAvailable, err := cgoEnabled(root)
	if err != nil {
		return err
	}
	if raceAvailable {
		testArgs = append(testArgs, "-race")
	} else {
		fmt.Fprintln(os.Stderr, "pocketkit: CGO is disabled; running tests without the race detector")
	}
	for _, args := range [][]string{
		append([]string{"vet"}, pkgs...),
		append(testArgs, pkgs...),
	} {
		fmt.Printf("go %s: %d package pattern(s)\n", args[0], len(pkgs))
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go %s: %w", args[0], err)
		}
	}
	return nil
}

// cgoEnabled reports whether the Go toolchain builds with CGO, which the race
// detector requires.
func cgoEnabled(root string) (bool, error) {
	cmd := exec.Command("go", "env", "CGO_ENABLED")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("go env CGO_ENABLED: %w", err)
	}
	return strings.TrimSpace(string(output)) == "1", nil
}

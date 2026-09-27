package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateIgnoresExternalTests(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/app\n")
	writeTestFile(t, root, "a_test.go", "package main_test\n")
	writeTestFile(t, root, "main.go", "package main\nfunc main() {}\n")
	if _, _, err := generate(root); err != nil {
		t.Fatal(err)
	}
	pkg, err := packageClause(filepath.Join(root, GenFile))
	if err != nil || pkg != "main" {
		t.Fatalf("generated package = %q, err = %v", pkg, err)
	}
	if _, changed, err := generate(root); err != nil || changed {
		t.Fatalf("second generation changed = %v, err = %v", changed, err)
	}
}

func TestMainPackageNameReportsInvalidSource(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "broken.go", "not a package clause")
	if _, err := mainPackageName(root); err == nil || !strings.Contains(err.Error(), "broken.go") {
		t.Fatalf("expected error identifying broken.go, got %v", err)
	}
}

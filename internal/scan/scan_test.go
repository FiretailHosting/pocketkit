package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestURLFromDir(t *testing.T) {
	apiDir := filepath.FromSlash("/app/api")
	cases := map[string]string{
		"/app/api":                 "/api",
		"/app/api/coins":           "/api/coins",
		"/app/api/coins/_id":       "/api/coins/{id}",
		"/app/api/coins/_id/chart": "/api/coins/{id}/chart",
		"/app/api/files/_path_":    "/api/files/{path...}",
		"/app/api/a/_x/b/_y":       "/api/a/{x}/b/{y}",
	}
	for dir, want := range cases {
		got, err := urlFromDir(apiDir, filepath.FromSlash(dir))
		if err != nil {
			t.Fatalf("urlFromDir(%q): %v", dir, err)
		}
		if got != want {
			t.Errorf("urlFromDir(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestCheckReserved(t *testing.T) {
	if err := checkReserved("/api/coins", "api/coins/GET.go"); err != nil {
		t.Errorf("/api/coins should be allowed, got %v", err)
	}
	// A reserved name nested deeper is fine; only the first segment collides.
	if err := checkReserved("/api/coins/health", "x"); err != nil {
		t.Errorf("/api/coins/health should be allowed, got %v", err)
	}
	err := checkReserved("/api/health", "api/health/GET.go")
	if err == nil {
		t.Fatal("/api/health should be rejected")
	}
	if !strings.Contains(err.Error(), "health check") {
		t.Errorf("error should name the built-in, got: %v", err)
	}
}

func TestAppDiscoversRoutesAndHooks(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("api/coins/GET.go", "package coins\nfunc Handle() error { return nil }\n")
	write("api/coins/POST.go", "package coins\nvar Middlewares = []int{}\nfunc Handle() error { return nil }\n")
	write("api/coins/_id/GET.go", "package coin\nvar Public = true\nfunc Handle() error { return nil }\n")
	write("api/coins/notes.go", "package coins\n// not a route file\n")
	write("hooks/coins/OnRecordAfterCreateSuccess/h.go", "package h\nfunc Handle() error { return nil }\n")

	res, err := App(root, "example.com/m")
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Routes) != 3 {
		t.Fatalf("got %d routes, want 3: %+v", len(res.Routes), res.Routes)
	}

	byKey := map[string]Route{}
	for _, r := range res.Routes {
		byKey[r.Method+" "+r.Path] = r
	}

	get := byKey["GET /api/coins"]
	if get.ImportPath != "example.com/m/api/coins" || get.Package != "coins" {
		t.Errorf("unexpected import/package: %+v", get)
	}
	if get.Public || get.HasMiddlewares {
		t.Errorf("GET /api/coins should be neither public nor have middlewares: %+v", get)
	}
	if !byKey["POST /api/coins"].HasMiddlewares {
		t.Error("POST /api/coins should report middlewares")
	}
	if !byKey["GET /api/coins/{id}"].Public {
		t.Error("GET /api/coins/{id} should be public")
	}

	if len(res.Hooks) != 1 {
		t.Fatalf("got %d hooks, want 1", len(res.Hooks))
	}
	h := res.Hooks[0]
	if h.Name != "OnRecordAfterCreateSuccess" || len(h.Tags) != 1 || h.Tags[0] != "coins" {
		t.Errorf("unexpected hook: %+v", h)
	}
}

func TestMissingHandleIsAnError(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "api", "broken", "GET.go")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("package broken\n"), 0o644)

	_, err := App(root, "example.com/m")
	if err == nil || !strings.Contains(err.Error(), "Handle") {
		t.Fatalf("want a Handle error, got %v", err)
	}
}

func TestPublicFalseDoesNotOptOut(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "api", "x", "GET.go")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("package x\nvar Public = false\nfunc Handle() error { return nil }\n"), 0o644)

	res, err := App(root, "example.com/m")
	if err != nil {
		t.Fatal(err)
	}
	if res.Routes[0].Public {
		t.Error("var Public = false must not opt out of auth")
	}
}

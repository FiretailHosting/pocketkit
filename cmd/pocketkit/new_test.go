package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeFrontendTools(t *testing.T, exitCode int) {
	t.Helper()
	bin := t.TempDir()
	name, script := "%s", "#!/bin/sh\nexit %d\n"
	if runtime.GOOS == "windows" {
		name, script = "%s.cmd", "@exit /b %d\r\n"
	}
	for _, tool := range []string{"npx", "npm"} {
		path := filepath.Join(bin, fmt.Sprintf(name, tool))
		if err := os.WriteFile(path, []byte(fmt.Sprintf(script, exitCode)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
}

func TestNewReportsFrontendFailure(t *testing.T) {
	fakeFrontendTools(t, 23)
	root := filepath.Join(t.TempDir(), "app")
	err := cmdNew([]string{"example.com/app", root})
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 || !strings.Contains(err.Error(), "partial scaffold") {
		t.Fatalf("expected contextual subprocess failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, GenFile)); err != nil {
		t.Fatalf("partial output should remain available: %v", err)
	}
}

func TestNewReportsGenerationFailure(t *testing.T) {
	fakeFrontendTools(t, 0)
	err := cmdNew([]string{"", filepath.Join(t.TempDir(), "app")})
	if err == nil || !strings.Contains(err.Error(), "generate wiring") {
		t.Fatalf("expected generation failure, got %v", err)
	}
}

func TestNewGeneratesFormattedGo(t *testing.T) {
	fakeFrontendTools(t, 0)
	root := filepath.Join(t.TempDir(), "app")
	if err := cmdNew([]string{"example.com/app", root}); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(source)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !bytes.Equal(source, formatted) {
			t.Errorf("%s is not gofmt-formatted", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// scaffoldFixture exercises the real templates and generator without downloading
// a frontend toolchain. Go commands use this checkout instead of a published CLI.
func scaffoldFixture(t *testing.T) string {
	t.Helper()
	originalPath := os.Getenv("PATH")
	fakeFrontendTools(t, 0)
	root := filepath.Join(t.TempDir(), "app")
	if err := cmdNew([]string{"example.com/app", root}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", originalPath)
	repo, _, err := moduleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	gomod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(gomod), "module github.com/FiretailHosting/pocketkit", "module example.com/app", 1)
	body += fmt.Sprintf("\nrequire github.com/FiretailHosting/pocketkit v0.0.0\nreplace github.com/FiretailHosting/pocketkit => %q\n", repo)
	writeTestFile(t, root, "go.mod", body)
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "go.sum", string(sum))
	return root
}

func TestCheckRunsParameterRouteTests(t *testing.T) {
	root := scaffoldFixture(t)
	writeTestFile(t, root, "api/things/_id/GET.go", `package thing
import "github.com/pocketbase/pocketbase/core"
func GET(e *core.RequestEvent) error {return e.NoContent(204)}
`)
	marker := filepath.Join(root, "parameter-test-ran")
	writeTestFile(t, root, "api/things/_id/GET_test.go", fmt.Sprintf(`package thing
import ("testing"; "os")
func TestRegression(t *testing.T) {
 if err:=os.WriteFile(%q,[]byte("ran"),0600);err!=nil {t.Fatal(err)}
 t.Fatal("intentional parameter-route regression")
}
`, marker))
	err := cmdCheck([]string{root})
	if err == nil || !strings.Contains(err.Error(), "go test") {
		t.Fatalf("expected failing route test, got %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("parameter-route test was skipped: %v", err)
	}
}

func TestScaffoldMigrationIsIndependentOfRuntimeConfig(t *testing.T) {
	root := scaffoldFixture(t)
	// The historical migration must still create sso_login_at after runtime config changes.
	writeTestFile(t, root, "main_test.go", `package main
import (
 "testing"
 "example.com/app/internal/auth"
 "github.com/pocketbase/pocketbase"
)
func TestMigrationSnapshot(t *testing.T) {
 auth.SSO.LoginField="replacement_login_at"
 app:=pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir:t.TempDir()})
 if err:=app.Bootstrap();err!=nil {t.Fatal(err)}
 defer app.ResetBootstrapState()
 if err:=app.RunAllMigrations();err!=nil {t.Fatal(err)}
 users,err:=app.FindCollectionByNameOrId("users");if err!=nil {t.Fatal(err)}
 if users.Fields.GetByName("sso_login_at")==nil || users.Fields.GetByName("replacement_login_at")!=nil {
  t.Fatal("historical migration followed mutable runtime config")
 }
 if users.PasswordAuth.Enabled || users.OTP.Enabled || !users.OAuth2.Enabled {t.Fatal("SSO collection settings were not applied")}
 // An existing database must converge with a fresh one when migrations run again.
 if err:=app.RunAllMigrations();err!=nil {t.Fatal(err)}
}
`)
	cmd := exec.Command("go", "test", "-race", ".")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scaffold migration: %v\n%s", err, output)
	}
}

package main

import (
	"strings"
	"testing"
)

// The fixture's test fails only when built with -race, which shows whether
// check enabled the race detector.
func TestCheckUsesRaceDetectorOnlyWithCGO(t *testing.T) {
	for _, testCase := range []struct {
		cgoEnabled string
		wantRace   bool
	}{
		{cgoEnabled: "0", wantRace: false},
		{cgoEnabled: "1", wantRace: true},
	} {
		t.Run("CGO_ENABLED="+testCase.cgoEnabled, func(t *testing.T) {
			t.Setenv("CGO_ENABLED", testCase.cgoEnabled)
			root := t.TempDir()
			writeTestFile(t, root, "go.mod", "module example.com/racecheck\n\ngo 1.21\n")
			writeTestFile(t, root, "racecheck.go", "package racecheck\n")
			writeTestFile(t, root, "race_test.go", `//go:build race

package racecheck

import "testing"

func TestRaceEnabled(t *testing.T) { t.Fatal("race detector enabled") }
`)
			err := checkPackages(root, []string{"./..."})
			if !testCase.wantRace && err != nil {
				t.Fatalf("check without CGO failed: %v", err)
			}
			if testCase.wantRace && (err == nil || !strings.Contains(err.Error(), "go test")) {
				t.Fatalf("expected the race-only test to fail, got %v", err)
			}
		})
	}
}

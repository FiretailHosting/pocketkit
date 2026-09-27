package pocketkit

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiagnoseAccessibleRepositoryDoesNotInferReleaseFailure(t *testing.T) {
	original := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = original })
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/repos/owner/app" {
			t.Errorf("unexpected repository request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
	})}
	got := diagnoseAccess(context.Background(), "owner/app", "")
	if !strings.Contains(got, "repository is reachable") || !strings.Contains(got, "original error") {
		t.Fatalf("diagnostic should preserve original failure: %q", got)
	}
	if strings.Contains(got, "release exists") || strings.Contains(got, "no asset") {
		t.Fatalf("diagnostic inferred an unverified release state: %q", got)
	}
}

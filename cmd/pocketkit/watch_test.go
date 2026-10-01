package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatchSourceTracksMovedDirectories(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "app")
	writeTestFile(t, root, "api/existing/GET.go", "package existing\n")
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := syncWatchDirs(w, root); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	changes := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- watchSource(ctx, w, root, func() {
			select {
			case changes <- struct{}{}:
			default:
			}
		})
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("watcher failed to stop")
		}
	}()
	awaitChange := func() {
		t.Helper()
		select {
		case <-changes:
		case err := <-done:
			t.Fatalf("watcher stopped: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("source change was not reported")
		}
	}
	// Moving the entire directory out need not emit an event for each Go file.
	if err := os.Rename(filepath.Join(root, "api/existing"), filepath.Join(parent, "removed")); err != nil {
		t.Fatal(err)
	}
	awaitChange()
	// A populated directory moved in must register its nested directories too.
	writeTestFile(t, parent, "incoming/_id/GET.go", "package item\n")
	if err := os.Rename(filepath.Join(parent, "incoming"), filepath.Join(root, "api/incoming")); err != nil {
		t.Fatal(err)
	}
	awaitChange()
	writeTestFile(t, root, "api/incoming/_id/GET.go", "package item\n// changed\n")
	awaitChange()
}

func TestSourceEventIgnoresGeneratedAndRuntimeFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{GenFile, "pb_data", "frontend", ".git/index"} {
		if sourceEvent(root, fsnotify.Event{Name: filepath.Join(root, name), Op: fsnotify.Create}) {
			t.Errorf("unexpected rebuild for %s", name)
		}
	}
}

func TestWatchRegistrationFailureIsReturned(t *testing.T) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if err := syncWatchDirs(w, t.TempDir()); err == nil {
		t.Fatal("closed watcher registration unexpectedly succeeded")
	}
}

func TestWatchErrorRecovery(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "api/moved/GET.go", "package moved\n")
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// Dropped events can hide new directories, so recovery must rescan.
	if err := recoverWatchError(w, root, fsnotify.ErrEventOverflow); err != nil {
		t.Fatalf("overflow should be recoverable: %v", err)
	}
	if !slices.Contains(w.WatchList(), filepath.Join(root, "api/moved")) {
		t.Errorf("overflow recovery did not rescan; watching %v", w.WatchList())
	}
	if err := recoverWatchError(w, root, errors.New("watch failed")); err == nil || !strings.Contains(err.Error(), "watch failed") {
		t.Fatalf("other watcher errors should stop dev, got %v", err)
	}
}

func TestDevBackendURL(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:9090": "http://127.0.0.1:9090",
		":9090":          "http://127.0.0.1:9090",
		"0.0.0.0:9090":   "http://127.0.0.1:9090",
		"[::]:9090":      "http://[::1]:9090",
		"[::1]:9090":     "http://[::1]:9090",
	} {
		got, err := devBackendURL(addr)
		if err != nil || got != want {
			t.Errorf("devBackendURL(%q) = %q, %v; want %q", addr, got, err, want)
		}
	}
	if _, err := devBackendURL("bad-address"); err == nil {
		t.Fatal("invalid address accepted")
	}
}

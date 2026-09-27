package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounce collapses editor save storms into a single rebuild.
const debounce = 150 * time.Millisecond

func cmdDev(args []string) error {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	addr := fs.String("http", "127.0.0.1:8090", "address to serve on")
	frontend := fs.Bool("frontend", true, "also run the frontend dev server if one exists")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, _, err := moduleRoot(".")
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	if *frontend {
		if cmd, ok := frontendDevCommand(root); ok {
			wg.Add(1)
			go func() {
				defer wg.Done()
				runFrontend(ctx, root, cmd)
			}()
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		runBackend(ctx, root, *addr)
	}()

	wg.Wait()
	return nil
}

// runBackend regenerates, starts the app, and restarts it whenever Go source changes.
func runBackend(ctx context.Context, root, addr string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pocketkit: watch: %v\n", err)
		return
	}
	defer watcher.Close()

	addWatchDirs(watcher, root)

	restart := make(chan struct{}, 1)
	kick := func() {
		select {
		case restart <- struct{}{}:
		default:
		}
	}
	kick()

	go func() {
		var timer *time.Timer
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !strings.HasSuffix(ev.Name, ".go") || filepath.Base(ev.Name) == GenFile {
					// New directories still need watching even though they are not .go files.
					if ev.Op&fsnotify.Create != 0 {
						if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
							_ = watcher.Add(ev.Name)
							kick()
						}
					}
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(debounce, kick)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Fprintf(os.Stderr, "pocketkit: watch: %v\n", err)
			}
		}
	}()

	var current *exec.Cmd
	for {
		select {
		case <-ctx.Done():
			stopProcess(current)
			return
		case <-restart:
			stopProcess(current)

			res, changed, err := generate(root)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\npocketkit: %v\n", err)
				continue
			}
			if changed {
				fmt.Printf("\npocketkit: wired %d route(s), %d hook(s)\n", len(res.Routes), len(res.Hooks))
			}
			addWatchDirs(watcher, root)

			cmd := exec.Command("go", "run", ".", "serve", "--http", addr)
			cmd.Dir = root
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "pocketkit: %v\n", err)
				continue
			}
			current = cmd
		}
	}
}

// stopProcess kills the app and the whole process group `go run` created, so the
// compiled child does not survive and hold the port.
func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

	done := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// addWatchDirs watches every directory that can hold app Go source.
func addWatchDirs(w *fsnotify.Watcher, root string) {
	_ = w.Add(root)
	for _, sub := range []string{"api", "hooks", "migrations", "internal"} {
		dir := filepath.Join(root, sub)
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				_ = w.Add(p)
			}
			return nil
		})
	}
}

// frontendDevCommand returns the package manager command for frontend/, if present.
func frontendDevCommand(root string) ([]string, bool) {
	if _, err := os.Stat(filepath.Join(root, "frontend", "package.json")); err != nil {
		return nil, false
	}
	if _, err := exec.LookPath("bun"); err == nil {
		return []string{"bun", "run", "dev"}, true
	}
	if _, err := exec.LookPath("npm"); err == nil {
		return []string{"npm", "run", "dev"}, true
	}
	return nil, false
}

func runFrontend(ctx context.Context, root string, argv []string) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = filepath.Join(root, "frontend")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "pocketkit: frontend: %v\n", err)
	}
}

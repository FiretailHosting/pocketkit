package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
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

	backendURL, err := devBackendURL(*addr)
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
				runFrontend(ctx, root, cmd, backendURL)
			}()
		}
	}

	backendErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		backendErr <- runBackend(ctx, root, *addr)
		stop()
	}()

	wg.Wait()
	return <-backendErr
}

// runBackend regenerates, starts the app, and restarts it whenever Go source changes.
func runBackend(ctx context.Context, root, addr string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch source: %w", err)
	}
	defer watcher.Close()

	if err := syncWatchDirs(watcher, root); err != nil {
		return err
	}

	restart := make(chan struct{}, 1)
	kick := func() {
		select {
		case restart <- struct{}{}:
		default:
		}
	}
	kick()

	watchErr := make(chan error, 1)
	go func() { watchErr <- watchSource(ctx, watcher, root, kick) }()

	var current *devProcess
	defer func() { stopProcess(current) }()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-watchErr:
			return err
		case <-restart:
			stopProcess(current)
			current = nil

			res, changed, err := generate(root)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\npocketkit: %v\n", err)
				continue
			}
			if changed {
				fmt.Printf("\npocketkit: wired %d route(s), %d hook(s)\n", len(res.Routes), len(res.Hooks))
			}

			cmd := exec.Command("go", "run", ".", "serve", "--http", addr)
			cmd.Dir = root
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			process, err := startProcess(cmd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "pocketkit: %v\n", err)
				continue
			}
			current = process
		}
	}
}

const shutdownTimeout = 3 * time.Second

// devProcess owns the one Wait call for a command. Closing done publishes err.
type devProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func startProcess(cmd *exec.Cmd) (*devProcess, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = shutdownTimeout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &devProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		// A launcher (go run, npm, bun) can exit before its children. Clean
		// them up even when the launcher exited naturally or failed to build.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		close(p.done)
	}()
	return p, nil
}

// stopProcess gives the command time to exit, then forces the whole group down.
// It is safe to call again after the command has already been reaped.
func stopProcess(p *devProcess) {
	if p == nil {
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()
	select {
	case <-p.done:
	case <-timer.C:
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
	}
}

// sourceDirs are the subtrees that can contain application Go source.
var sourceDirs = []string{"api", "hooks", "migrations", "internal"}

// syncWatchDirs adds populated directories moved into the tree and drops watches
// on directories moved out. Only missing paths are ignored during rename races.
func syncWatchDirs(w *fsnotify.Watcher, root string) error {
	wanted := map[string]bool{root: true}
	for _, sub := range sourceDirs {
		err := filepath.WalkDir(filepath.Join(root, sub), func(p string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				wanted[p] = true
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("discover source directories: %w", err)
		}
	}
	for _, watched := range w.WatchList() {
		if wanted[watched] {
			delete(wanted, watched)
			continue
		}
		if err := w.Remove(watched); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
			return fmt.Errorf("unwatch %s: %w", watched, err)
		}
	}
	for dir := range wanted {
		if err := w.Add(dir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("watch %s: %w", dir, err)
		}
	}
	return nil
}

func sourceEvent(root string, ev fsnotify.Event) bool {
	rel, err := filepath.Rel(root, ev.Name)
	if err != nil || filepath.Base(rel) == GenFile {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 1 && (strings.HasSuffix(rel, ".go") || rel == "go.mod" || rel == "go.sum") {
		return true
	}
	for _, dir := range sourceDirs {
		if parts[0] == dir {
			return strings.HasSuffix(rel, ".go") || ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0
		}
	}
	return false
}

func watchSource(ctx context.Context, w *fsnotify.Watcher, root string, kick func()) error {
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return fmt.Errorf("source watcher closed")
			}
			if !sourceEvent(root, ev) {
				continue
			}
			if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				if err := syncWatchDirs(w, root); err != nil {
					return err
				}
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, kick)
		case err, ok := <-w.Errors:
			if !ok {
				return fmt.Errorf("source watcher closed")
			}
			return fmt.Errorf("watch source: %w", err)
		}
	}
}

// devBackendURL converts a listening address into a destination the proxy can dial.
func devBackendURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid --http address %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
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

func runFrontend(ctx context.Context, root string, argv []string, backendURL string) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = filepath.Join(root, "frontend")
	cmd.Env = append(os.Environ(), "POCKETKIT_BACKEND_URL="+backendURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	p, err := startProcess(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pocketkit: frontend: %v\n", err)
		return
	}
	select {
	case <-ctx.Done():
		stopProcess(p)
	case <-p.done:
		if p.err != nil {
			fmt.Fprintf(os.Stderr, "pocketkit: frontend: %v\n", p.err)
		}
	}
}

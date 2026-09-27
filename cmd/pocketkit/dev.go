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
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
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

	var current *devProcess
	for {
		select {
		case <-ctx.Done():
			stopProcess(current)
			return
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
			addWatchDirs(watcher, root)

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
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = filepath.Join(root, "frontend")
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

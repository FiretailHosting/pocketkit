package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type processReady struct {
	PID        int
	Address    string
	BackendURL string
}

// The subprocess acts like go run/npm and a child server that ignores graceful
// stop requests: SIGTERM on Unix and Ctrl+Break on Windows.
func TestDevProcessHelper(t *testing.T) {
	mode := os.Getenv("POCKETKIT_TEST_PROCESS")
	if mode == "" {
		return
	}
	if mode == "launcher" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDevProcessHelper$")
		cmd.Env = append(os.Environ(), "POCKETKIT_TEST_PROCESS=server")
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if exitFile := os.Getenv("POCKETKIT_TEST_EXIT"); exitFile != "" {
			for {
				if _, err := os.Stat(exitFile); err == nil {
					os.Exit(0)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		_ = cmd.Wait()
		os.Exit(0)
	}
	// Catch and drop stop requests. signal.Ignore would let Windows' default
	// Ctrl+Break handler terminate the process.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt, syscall.SIGTERM)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ready, err := json.Marshal(processReady{PID: os.Getpid(), Address: listener.Addr().String(), BackendURL: os.Getenv("POCKETKIT_BACKEND_URL")})
	if err != nil {
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv("POCKETKIT_TEST_READY"), ready, 0o600); err != nil {
		os.Exit(1)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			os.Exit(1)
		}
		_ = conn.Close()
	}
}

func waitForServer(t *testing.T, file string) processReady {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(file)
		var ready processReady
		if err == nil && json.Unmarshal(data, &ready) == nil {
			t.Cleanup(func() {
				if process, err := os.FindProcess(ready.PID); err == nil {
					_ = process.Kill()
				}
			})
			return ready
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("helper server did not become ready")
	return processReady{}
}

func assertServerStopped(t *testing.T, ready processReady) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", ready.Address, 100*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child %d still holds %s", ready.PID, ready.Address)
}

func TestStopProcessCleansUpGroup(t *testing.T) {
	for _, mode := range []string{"launcher", "server"} {
		t.Run(mode, func(t *testing.T) {
			readyFile := filepath.Join(t.TempDir(), "ready")
			cmd := exec.Command(os.Args[0], "-test.run=^TestDevProcessHelper$")
			cmd.Env = append(os.Environ(), "POCKETKIT_TEST_PROCESS="+mode, "POCKETKIT_TEST_READY="+readyFile)
			p, err := startProcess(cmd)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(p.tree.kill)
			ready := waitForServer(t, readyFile)
			stopped := make(chan struct{})
			go func() { stopProcess(p); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(shutdownTimeout + 5*time.Second):
				t.Fatal("stopProcess did not finish within its grace period")
			}
			assertServerStopped(t, ready)
			stopProcess(p) // Already reaped: must not wait twice or signal again.
			if p.cmd.ProcessState == nil {
				t.Fatal("command was not reaped")
			}
		})
	}
}

func TestLauncherExitCleansUpChildren(t *testing.T) {
	dir := t.TempDir()
	readyFile, exitFile := filepath.Join(dir, "ready"), filepath.Join(dir, "exit")
	cmd := exec.Command(os.Args[0], "-test.run=^TestDevProcessHelper$")
	cmd.Env = append(os.Environ(), "POCKETKIT_TEST_PROCESS=launcher", "POCKETKIT_TEST_READY="+readyFile, "POCKETKIT_TEST_EXIT="+exitFile)
	p, err := startProcess(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.tree.kill)
	ready := waitForServer(t, readyFile)
	writeTestFile(t, dir, "exit", "exit")
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("launcher was not reaped")
	}
	assertServerStopped(t, ready)
}

func TestFrontendCancellationIsBounded(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	readyFile := filepath.Join(root, "ready")
	t.Setenv("POCKETKIT_TEST_PROCESS", "server")
	t.Setenv("POCKETKIT_TEST_READY", readyFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		runFrontend(ctx, root, []string{os.Args[0], "-test.run=^TestDevProcessHelper$"}, "http://127.0.0.1:9090")
		close(done)
	}()
	ready := waitForServer(t, readyFile)
	if ready.BackendURL != "http://127.0.0.1:9090" {
		t.Errorf("frontend backend URL = %q", ready.BackendURL)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(shutdownTimeout + 5*time.Second):
		t.Fatal("frontend cancellation hung")
	}
	assertServerStopped(t, ready)
}

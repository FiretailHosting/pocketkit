//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// processTree is a command and everything it started, as one process group.
type processTree struct {
	pid int
}

// prepareProcessTree makes the command lead a new process group, so stopping it
// also reaches the servers that go run, npm and bun start.
func prepareProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func trackProcessTree(cmd *exec.Cmd) (*processTree, error) {
	return &processTree{pid: cmd.Process.Pid}, nil
}

// interrupt asks every process in the tree to shut down.
func (tree *processTree) interrupt() {
	_ = syscall.Kill(-tree.pid, syscall.SIGTERM)
}

// kill forces every process in the tree down.
func (tree *processTree) kill() {
	_ = syscall.Kill(-tree.pid, syscall.SIGKILL)
}

func (tree *processTree) close() {}

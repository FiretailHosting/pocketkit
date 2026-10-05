//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	procGenerateConsoleCtrlEvent = kernel32.NewProc("GenerateConsoleCtrlEvent")
)

const (
	processSetQuota  = 0x0100
	processTerminate = 0x0001
	ctrlBreakEvent   = 1
)

// processTree is a command and everything it started. The console process
// group carries the graceful stop; the Job Object reaches every descendant,
// including those left behind when go run, npm or bun exits first.
type processTree struct {
	pid int

	mu  sync.Mutex
	job syscall.Handle // Zero once closed.
}

// prepareProcessTree starts the command in a new console process group, so
// Ctrl+Break reaches it and its children without reaching pocketkit.
func prepareProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// trackProcessTree places the started command in a new Job Object. Processes
// it starts afterwards join the job automatically. A child started before the
// assignment would escape, but launchers take far longer than that to spawn.
func trackProcessTree(cmd *exec.Cmd) (*processTree, error) {
	job, _, err := procCreateJobObjectW.Call(0, 0)
	if job == 0 {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	tree := &processTree{pid: cmd.Process.Pid, job: syscall.Handle(job)}
	process, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(cmd.Process.Pid))
	if err != nil {
		tree.close()
		return nil, fmt.Errorf("open process %d: %w", cmd.Process.Pid, err)
	}
	defer syscall.CloseHandle(process)
	if assigned, _, err := procAssignProcessToJobObject.Call(job, uintptr(process)); assigned == 0 {
		tree.close()
		return nil, fmt.Errorf("assign process %d to job object: %w", cmd.Process.Pid, err)
	}
	return tree, nil
}

// interrupt sends Ctrl+Break to the process group, which Go and Node report as
// an interrupt. Processes without a console are left to kill.
func (tree *processTree) interrupt() {
	_, _, _ = procGenerateConsoleCtrlEvent.Call(ctrlBreakEvent, uintptr(tree.pid))
}

// kill forces every process in the job down. It does nothing once closed.
func (tree *processTree) kill() {
	tree.mu.Lock()
	defer tree.mu.Unlock()
	if tree.job != 0 {
		_, _, _ = procTerminateJobObject.Call(uintptr(tree.job), 1)
	}
}

func (tree *processTree) close() {
	tree.mu.Lock()
	defer tree.mu.Unlock()
	if tree.job != 0 {
		_ = syscall.CloseHandle(tree.job)
		tree.job = 0
	}
}

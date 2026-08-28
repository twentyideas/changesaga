//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

// windowsDetachedProcess is the DETACHED_PROCESS creation flag. The syscall
// package exposes CREATE_NEW_PROCESS_GROUP but not this companion flag.
const windowsDetachedProcess = 0x00000008

func configureDetachedProcess(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP | windowsDetachedProcess
}

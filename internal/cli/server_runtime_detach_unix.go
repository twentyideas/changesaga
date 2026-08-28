//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// configureDetachedProcess isolates a managed server from the launcher's
// session. Process runners commonly clean up that entire session when the
// foreground command exits, even after Process.Release has discarded Go's
// handle to the child.
func configureDetachedProcess(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Setsid = true
}

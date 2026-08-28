//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestConfigureDetachedProcessCreatesSession(t *testing.T) {
	command := exec.Command("sh", "-c", "sleep 30")
	configureDetachedProcess(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})

	sessionID, err := syscall.Getsid(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != command.Process.Pid {
		t.Fatalf("detached child session = %d, want its PID %d", sessionID, command.Process.Pid)
	}
}

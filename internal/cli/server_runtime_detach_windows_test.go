//go:build windows

package cli

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestConfigureDetachedProcessCreatesProcessGroup(t *testing.T) {
	command := exec.Command("cmd.exe", "/c", "exit", "0")
	configureDetachedProcess(command)
	if command.SysProcAttr == nil {
		t.Fatal("detached child has no process attributes")
	}
	want := uint32(syscall.CREATE_NEW_PROCESS_GROUP | windowsDetachedProcess)
	if command.SysProcAttr.CreationFlags&want != want {
		t.Fatalf("detached child creation flags = %#x, want %#x", command.SysProcAttr.CreationFlags, want)
	}
}

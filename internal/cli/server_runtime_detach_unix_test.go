//go:build !windows

package cli

import (
	"os/exec"
	"testing"
)

func TestConfigureDetachedProcessCreatesSession(t *testing.T) {
	command := exec.Command("change-saga-test-child")
	configureDetachedProcess(command)
	if command.SysProcAttr == nil {
		t.Fatal("detached child has no process attributes")
	}
	if !command.SysProcAttr.Setsid {
		t.Fatal("detached child does not request a new session")
	}
}

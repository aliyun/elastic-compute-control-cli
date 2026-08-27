//go:build !windows

package aliyun

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func runExternalCredentialCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = externalCredentialWaitDelay
	cmd.Cancel = func() error { return terminateExternalCredentialProcessGroup(cmd) }
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		_ = terminateExternalCredentialProcessGroup(cmd)
	}
	return err
}

func terminateExternalCredentialProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

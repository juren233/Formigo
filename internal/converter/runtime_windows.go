//go:build windows

package converter

import (
	"os/exec"
	"syscall"
)

func configurePlatformCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
}

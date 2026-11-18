//go:build !windows

package converter

import "os/exec"

func configurePlatformCmd(_ *exec.Cmd) {}

//go:build windows

package executor

import (
	"os/exec"
	"strconv"
)

// setProcessGroup arranges for cmd.Cancel to kill the whole process tree
// via taskkill, since Windows has no pgid-style signal to a process group.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
}

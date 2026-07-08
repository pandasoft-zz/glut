//go:build !windows

package executor

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts cmd in its own process group and arranges for
// cmd.Cancel to signal the whole group (negative pgid) instead of only the
// direct child, so shell jobs and docker clients spawned by
// gitlab-ci-local are killed on timeout/cancellation too.
//
// Note: this detaches gitlab-ci-local from the terminal's foreground group, so
// a Ctrl-C no longer delivers SIGINT to it for a graceful shutdown; glut's
// signal handling cancels the context, which SIGKILLs the whole tree here.
// Containers started by the Docker daemon are not in this process tree and may
// outlive the kill.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		// The whole group can already be gone by the time the deadline fires;
		// report that as ErrProcessDone so os/exec does not surface a spurious
		// "canceling Cmd" error over the real timeout/cancel error.
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

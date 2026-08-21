//go:build unix

package fbhttp

import (
	"os/exec"
	"syscall"
	"time"
)

// udfKillGrace is how long make is given to tidy up after SIGTERM before the
// group is killed outright.
const udfKillGrace = 3 * time.Second

// udfSetProcAttr puts the build in a process group of its own. Without it a
// cancelled request would kill bash and leave cmake and cc compiling on.
func udfSetProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// udfKill stops the whole build.
//
// SIGTERM comes first because make traps it and deletes the target it was
// midway through writing. Straight to SIGKILL would leave a truncated object
// file with a timestamp newer than its source, which the next build would
// accept as up to date and link.
func udfKill(cmd *exec.Cmd) {
	if !udfSignal(cmd, syscall.SIGTERM) {
		return
	}
	time.Sleep(udfKillGrace)
	udfSignal(cmd, syscall.SIGKILL)
}

func udfSignal(cmd *exec.Cmd, sig syscall.Signal) bool {
	if cmd.Process == nil {
		return false
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil || pgid <= 0 {
		return cmd.Process.Signal(sig) == nil
	}
	return syscall.Kill(-pgid, sig) == nil
}

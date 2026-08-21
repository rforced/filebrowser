//go:build !unix

package fbhttp

import "os/exec"

// udfSetProcAttr has no portable equivalent off unix: there are no process
// groups to put the build in. Cancelling then reaches only the shell itself.
func udfSetProcAttr(_ *exec.Cmd) {}

func udfKill(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

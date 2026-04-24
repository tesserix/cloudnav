//go:build !windows

package tui

import "syscall"

// replaceProcess swaps the running cloudnav binary for the target
// binary using the raw exec(3) syscall. The kernel keeps the same
// PID, stdin / stdout / stderr fds, and working dir — the user sees a
// seamless handoff from old to new.
func replaceProcess(path string, args, env []string) error {
	return syscall.Exec(path, args, env)
}

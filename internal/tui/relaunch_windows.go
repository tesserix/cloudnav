//go:build windows

package tui

import (
	"os"
	"os/exec"
)

// replaceProcess on Windows spawns a fresh child with our stdio and
// exits the parent. Windows doesn't have a POSIX-style exec that keeps
// the PID, so this is the closest equivalent — visibly there's a brief
// flash as one console exits and another starts.
func replaceProcess(path string, args, env []string) error {
	cmd := exec.Command(path, args[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

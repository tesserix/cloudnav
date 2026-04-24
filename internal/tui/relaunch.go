package tui

import (
	"fmt"
	"os"
	"os/exec"
)

// execReplacement re-runs cloudnav in place so a successful upgrade
// takes effect without the user having to quit and retype the command.
// On Unix we replace the process image via syscall-level exec so the
// new binary inherits the tty cleanly. On Windows (where exec(3) isn't
// available) we spawn a child with the same tty handles and exit the
// parent.
//
// cloudnavPath prefers `cloudnav` on PATH (which follows the fresh brew
// / go-install symlink) over os.Executable (which resolves to the
// specific Cellar / GOPATH binary we were compiled from — i.e. the
// pre-upgrade copy).
func execReplacement() error {
	path, err := exec.LookPath("cloudnav")
	if err != nil {
		// Fall back to the original argv[0]. Worst case we relaunch
		// the same version — still gives the user a clean slate.
		path = os.Args[0]
	}
	args := os.Args
	if len(args) == 0 {
		args = []string{path}
	} else {
		args[0] = path
	}
	if err := replaceProcess(path, args, os.Environ()); err != nil {
		return fmt.Errorf("relaunch cloudnav: %w", err)
	}
	return nil
}

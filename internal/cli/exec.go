// Package cli is a thin subprocess runner used by every provider to shell out
// to its official CLI. Keeping the exec layer in one place makes auditing,
// timeouts, and error handling consistent across providers.
package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const DefaultTimeout = 30 * time.Second

type Runner struct {
	Bin     string
	Timeout time.Duration
	Env     []string
}

func New(bin string) *Runner {
	return &Runner{Bin: bin, Timeout: DefaultTimeout}
}

// Run executes the CLI with the given args and returns stdout. On failure the
// error includes a trimmed stderr so the caller can surface a useful message.
func (r *Runner) Run(ctx context.Context, args ...string) ([]byte, error) {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(ctx, r.Bin, args...)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if len(r.Env) > 0 {
		c.Env = append(c.Environ(), r.Env...)
	}
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), fmt.Errorf("%s %s: %s", r.Bin, strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

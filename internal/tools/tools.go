// Package tools handles installing and upgrading the third-party
// terminal tools cloudnav can integrate with — currently just Zellij,
// but the abstraction is here so the next dependency (atuin, ghq,
// gum, etc.) is one struct away.
//
// The pattern mirrors `provider.Installer` but stays decoupled from
// the cloud-CLI dispatch: tools are referenced by their PATH name,
// not by a Provider.
package tools

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Step is a single shell command in an install / upgrade plan.
type Step struct {
	Description string   // human-readable summary printed before exec
	Bin         string   // executable to run (looked up on PATH)
	Args        []string // arguments
}

// Tool is the metadata the install/upgrade flow needs for a third-
// party terminal binary. PlanFn is consulted at runtime so plans can
// pick the right package manager based on what's currently on PATH
// (brew vs cargo vs distro pkg).
type Tool struct {
	Name    string
	Bin     string
	PlanFn  func(goos string) ([]Step, bool)
	Upgrade func(goos string) ([]Step, bool)
}

// Available reports whether t.Bin is on PATH right now.
func (t Tool) Available() bool {
	if t.Bin == "" {
		return false
	}
	_, err := exec.LookPath(t.Bin)
	return err == nil
}

// InstallPlan returns the OS-specific install steps. Returns
// (nil, false) when the OS isn't supported or no package manager is
// available.
func (t Tool) InstallPlan(goos string) ([]Step, bool) {
	if t.PlanFn == nil {
		return nil, false
	}
	return t.PlanFn(goos)
}

// UpgradePlan returns the OS-specific upgrade steps. Falls back to
// the install plan when the tool doesn't ship a separate upgrade
// recipe — that's intentional, every package manager we use treats
// install on an existing tool as a no-op or as an upgrade.
func (t Tool) UpgradePlan(goos string) ([]Step, bool) {
	if t.Upgrade != nil {
		return t.Upgrade(goos)
	}
	return t.InstallPlan(goos)
}

// Ensure installs t when it isn't already on PATH. Idempotent —
// no-op when the tool is present. Stdout / stderr from each step is
// streamed to w so the caller (cloudnav workspace) can show the
// install progress inline instead of swallowing it.
func Ensure(t Tool, goos string, w io.Writer) error {
	if t.Available() {
		return nil
	}
	plan, ok := t.InstallPlan(goos)
	if !ok {
		return fmt.Errorf("%s: no automated installer available for %s", t.Name, goos)
	}
	return runSteps(t.Name, "installing", plan, w)
}

// Run executes a named plan (install or upgrade) for t. Used by
// `cloudnav install <tool>` / `cloudnav upgrade <tool>` where the
// caller already decided which path to take.
func Run(t Tool, action string, goos string, w io.Writer) error {
	var (
		plan []Step
		ok   bool
	)
	switch action {
	case "upgrade":
		plan, ok = t.UpgradePlan(goos)
	default:
		plan, ok = t.InstallPlan(goos)
	}
	if !ok {
		return fmt.Errorf("%s: no %s recipe for %s", t.Name, action, goos)
	}
	return runSteps(t.Name, action, plan, w)
}

func runSteps(name, action string, plan []Step, w io.Writer) error {
	_, _ = fmt.Fprintf(w, "→ %s %s via:\n", action, name)
	for _, s := range plan {
		_, _ = fmt.Fprintf(w, "    %s\n", s.Description)
	}
	_, _ = fmt.Fprintln(w)
	for _, s := range plan {
		if _, err := exec.LookPath(s.Bin); err != nil {
			return fmt.Errorf("required tool %q not found in PATH — can't run: %s", s.Bin, s.Description)
		}
		c := exec.Command(s.Bin, s.Args...)
		c.Stdin = os.Stdin
		c.Stdout = w
		c.Stderr = w
		if err := c.Run(); err != nil {
			return fmt.Errorf("%s: %w", s.Description, err)
		}
	}
	_, _ = fmt.Fprintf(w, "\n✓ %s %sd\n", name, action)
	return nil
}

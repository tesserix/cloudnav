package tools

import (
	"strings"
	"testing"
)

func TestZellijInstallPlanWindowsUnsupported(t *testing.T) {
	if _, ok := zellijInstall("windows"); ok {
		t.Error("zellijInstall should refuse on windows")
	}
	if _, ok := zellijUpgrade("windows"); ok {
		t.Error("zellijUpgrade should refuse on windows")
	}
}

// TestZellijInstallPlanShape pins the package-manager preference
// order — brew first, cargo second. The test inspects whichever
// plan the current host actually returns; if neither manager is
// installed, the host can't run a real install anyway, so we skip.
func TestZellijInstallPlanShape(t *testing.T) {
	plan, ok := zellijInstall("darwin")
	if !ok {
		t.Skip("no supported package manager on this host — skipping shape check")
	}
	if len(plan) != 1 {
		t.Fatalf("expected single-step plan, got %d", len(plan))
	}
	step := plan[0]
	if step.Bin != "brew" && step.Bin != "cargo" {
		t.Errorf("install plan uses %q, want brew or cargo", step.Bin)
	}
	if !strings.Contains(step.Description, "zellij") {
		t.Errorf("description %q should mention zellij", step.Description)
	}
}

func TestZellijUpgradePlanWrapsBrewUpdate(t *testing.T) {
	plan, ok := zellijUpgrade("darwin")
	if !ok {
		t.Skip("no supported package manager on this host")
	}
	step := plan[0]
	switch step.Bin {
	case "sh":
		// brew path — must wrap brew update so a stale formula
		// cache doesn't no-op the upgrade (matches the cloudnav
		// self-upgrade fix from 0.22.5).
		if len(step.Args) < 2 || !strings.Contains(step.Args[1], "brew update && brew upgrade") {
			t.Errorf("brew upgrade plan must wrap update+upgrade, got %v", step.Args)
		}
	case "cargo":
		// cargo path — must use --force so an existing-version
		// install actually rebuilds.
		if !contains(step.Args, "--force") {
			t.Errorf("cargo upgrade plan must pass --force, got %v", step.Args)
		}
	default:
		t.Errorf("unexpected upgrade bin %q", step.Bin)
	}
}

func TestZellijToolMetadata(t *testing.T) {
	if Zellij.Name != "zellij" || Zellij.Bin != "zellij" {
		t.Errorf("Zellij metadata: %+v", Zellij)
	}
	if Zellij.PlanFn == nil {
		t.Error("Zellij.PlanFn must not be nil")
	}
	if Zellij.Upgrade == nil {
		t.Error("Zellij.Upgrade must not be nil")
	}
}

func TestEnsureNoOpWhenAvailable(t *testing.T) {
	// `sh` is on every supported host; Ensure for it should be a
	// no-op without trying to install.
	fake := Tool{Name: "sh", Bin: "sh", PlanFn: func(string) ([]Step, bool) {
		t.Error("PlanFn should not run when binary is already on PATH")
		return nil, false
	}}
	var sink strings.Builder
	if err := Ensure(fake, "darwin", &sink); err != nil {
		t.Errorf("Ensure on present binary should be a no-op, got %v", err)
	}
}

func TestEnsureErrorsWhenNoPlan(t *testing.T) {
	fake := Tool{
		Name: "definitely-not-installed-anywhere-cloudnav",
		Bin:  "definitely-not-installed-anywhere-cloudnav",
		PlanFn: func(_ string) ([]Step, bool) {
			return nil, false // no plan available
		},
	}
	var sink strings.Builder
	err := Ensure(fake, "darwin", &sink)
	if err == nil {
		t.Error("Ensure should error when no install plan exists")
	}
}

func TestRunRoutesByAction(t *testing.T) {
	// Capture which plan got requested by recording into an outer
	// variable from the closures.
	var requested string
	t.Cleanup(func() { requested = "" })
	fake := Tool{
		Name: "x",
		Bin:  "x",
		PlanFn: func(_ string) ([]Step, bool) {
			requested = "install"
			return nil, false
		},
		Upgrade: func(_ string) ([]Step, bool) {
			requested = "upgrade"
			return nil, false
		},
	}
	var sink strings.Builder
	_ = Run(fake, "install", "darwin", &sink)
	if requested != "install" {
		t.Errorf("Run install routed to %q", requested)
	}
	_ = Run(fake, "upgrade", "darwin", &sink)
	if requested != "upgrade" {
		t.Errorf("Run upgrade routed to %q", requested)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

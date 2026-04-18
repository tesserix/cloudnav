package azure

import (
	"strings"
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestAzureLoginCommand(t *testing.T) {
	a := New()
	bin, args := a.LoginCommand()
	if bin != "az" {
		t.Errorf("bin = %q, want az", bin)
	}
	if len(args) != 1 || args[0] != "login" {
		t.Errorf("args = %v, want [login]", args)
	}
}

func TestAzureInstallHint(t *testing.T) {
	a := New()
	h := a.InstallHint()
	if !strings.Contains(h, "azure") {
		t.Errorf("install hint %q missing 'azure'", h)
	}
	if !strings.Contains(h, "https://") {
		t.Errorf("install hint %q missing a URL", h)
	}
}

func TestAzureInstallPlan(t *testing.T) {
	a := New()
	cases := []struct {
		goos       string
		wantOK     bool
		wantBinOne string // first step's bin when ok
	}{
		{"darwin", true, "brew"},
		{"windows", true, "winget"},
		{"linux", true, ""}, // either brew or sh — depends on host
		{"plan9", false, ""},
	}
	for _, c := range cases {
		steps, ok := a.InstallPlan(c.goos)
		if ok != c.wantOK {
			t.Errorf("%s: ok = %v, want %v", c.goos, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if len(steps) == 0 {
			t.Errorf("%s: no steps returned", c.goos)
			continue
		}
		if c.wantBinOne != "" && steps[0].Bin != c.wantBinOne {
			t.Errorf("%s: first step bin = %q, want %q", c.goos, steps[0].Bin, c.wantBinOne)
		}
		for _, s := range steps {
			if s.Description == "" {
				t.Errorf("%s: step has empty description", c.goos)
			}
			if s.Bin == "" {
				t.Errorf("%s: step has empty bin", c.goos)
			}
		}
	}
}

func TestAzureSatisfiesProviderInterfaces(t *testing.T) {
	// Compile-time assertions that Azure implements the optional capability
	// interfaces the TUI type-asserts against.
	var _ provider.Provider = (*Azure)(nil)
	var _ provider.Loginer = (*Azure)(nil)
	var _ provider.Installer = (*Azure)(nil)
	var _ provider.PIMer = (*Azure)(nil)
	var _ provider.Coster = (*Azure)(nil)
	var _ provider.VMOps = (*Azure)(nil)
	var _ provider.Billing = (*Azure)(nil)
}

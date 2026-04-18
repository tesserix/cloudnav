package aws

import (
	"strings"
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestAWSLoginCommand(t *testing.T) {
	a := New()
	bin, args := a.LoginCommand()
	if bin != "aws" {
		t.Errorf("bin = %q, want aws", bin)
	}
	if strings.Join(args, " ") != "sso login" {
		t.Errorf("args = %v, want [sso login]", args)
	}
}

func TestAWSInstallHint(t *testing.T) {
	if !strings.Contains(New().InstallHint(), "AWS") {
		t.Error("install hint should mention AWS")
	}
}

func TestAWSInstallPlan(t *testing.T) {
	a := New()
	for _, goos := range []string{"darwin", "linux", "windows"} {
		steps, ok := a.InstallPlan(goos)
		if !ok {
			t.Errorf("%s: no plan available", goos)
			continue
		}
		if len(steps) == 0 || steps[0].Bin == "" {
			t.Errorf("%s: empty plan", goos)
		}
	}
	if _, ok := a.InstallPlan("freebsd"); ok {
		t.Error("freebsd: unexpected install plan")
	}
}

func TestAWSSatisfiesProviderInterfaces(t *testing.T) {
	var _ provider.Provider = (*AWS)(nil)
	var _ provider.Loginer = (*AWS)(nil)
	var _ provider.Installer = (*AWS)(nil)
}

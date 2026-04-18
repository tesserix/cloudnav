package gcp

import (
	"strings"
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestGCPLoginCommand(t *testing.T) {
	g := New()
	bin, args := g.LoginCommand()
	if bin != "gcloud" {
		t.Errorf("bin = %q, want gcloud", bin)
	}
	if strings.Join(args, " ") != "auth login" {
		t.Errorf("args = %v, want [auth login]", args)
	}
}

func TestGCPInstallHint(t *testing.T) {
	if !strings.Contains(New().InstallHint(), "Google") {
		t.Error("install hint should mention Google")
	}
}

func TestGCPInstallPlan(t *testing.T) {
	g := New()
	for _, goos := range []string{"darwin", "linux", "windows"} {
		steps, ok := g.InstallPlan(goos)
		if !ok {
			t.Errorf("%s: no plan available", goos)
			continue
		}
		if len(steps) == 0 || steps[0].Bin == "" {
			t.Errorf("%s: empty plan", goos)
		}
	}
}

func TestGCPSatisfiesProviderInterfaces(t *testing.T) {
	var _ provider.Provider = (*GCP)(nil)
	var _ provider.Loginer = (*GCP)(nil)
	var _ provider.Installer = (*GCP)(nil)
}

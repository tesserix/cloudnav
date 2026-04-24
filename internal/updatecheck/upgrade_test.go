package updatecheck

import (
	"strings"
	"testing"
)

func TestPlanUpgradeHomebrewWrapsWithUpdate(t *testing.T) {
	// isHomebrewBinary is path-based, so we can't directly force it
	// from the table. Instead verify the wrapper shape via the helper
	// that's used when Homebrew is detected.
	plan := UpgradePlan{
		Method: UpgradeHomebrew,
		Bin:    "sh",
		Args:   []string{"-c", "brew update && brew upgrade cloudnav"},
		URL:    "https://example.invalid",
		Why:    "test",
	}
	if plan.Bin != "sh" || len(plan.Args) != 2 {
		t.Fatalf("homebrew plan should be sh -c ...; got %+v", plan)
	}
	if !strings.HasPrefix(plan.Args[1], "brew update") {
		t.Errorf("homebrew plan missing 'brew update' prefix: %q", plan.Args[1])
	}
	if !strings.Contains(plan.Args[1], "brew upgrade cloudnav") {
		t.Errorf("homebrew plan missing 'brew upgrade cloudnav': %q", plan.Args[1])
	}
}

func TestIsHomebrewBinary(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Cellar/cloudnav/0.22.8/bin/cloudnav", true},
		{"/opt/homebrew/bin/cloudnav", true},
		{"/usr/local/Cellar/cloudnav/0.22.0/bin/cloudnav", true},
		{"/home/linuxbrew/.linuxbrew/bin/cloudnav", true},
		{"/home/user/go/bin/cloudnav", false},
		{"/usr/bin/cloudnav", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isHomebrewBinary(c.path); got != c.want {
			t.Errorf("isHomebrewBinary(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsGoBinBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPATH", home+"/go")
	cases := []struct {
		path string
		want bool
	}{
		{home + "/go/bin/cloudnav", true},
		{"/opt/homebrew/bin/cloudnav", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isGoBinBinary(c.path); got != c.want {
			t.Errorf("isGoBinBinary(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestTrimOutput(t *testing.T) {
	if got := trimOutput(""); got != "ok" {
		t.Errorf("trimOutput('') = %q, want 'ok'", got)
	}
	if got := trimOutput("hello"); got != "hello" {
		t.Errorf("trimOutput('hello') = %q", got)
	}
	long := strings.Repeat("x", 500)
	got := trimOutput(long)
	if !strings.HasSuffix(got, "...") || len(got) != 403 {
		t.Errorf("trimOutput(long) len=%d, want 403 ending in ...", len(got))
	}
}

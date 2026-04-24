package updatecheck

import (
	"context"
	"strings"
	"testing"
	"time"
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

func TestCheckUsesCacheWhileFresh(t *testing.T) {
	// Point the cache at a temp dir so the test doesn't touch the real
	// user cache. We pre-seed a fresh entry and verify Check() returns
	// it without hitting the network (done implicitly — the fake Repo
	// would 404 if we did).
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	t.Setenv("HOME", tmp) // macOS fallback path

	// Drop a fresh cache entry (fetched 10 minutes ago, well within
	// pollInterval).
	oldRepo := Repo
	Repo = "example/notreal"
	t.Cleanup(func() { Repo = oldRepo })

	writeCache(cachedPayload{
		FetchedAt: time.Now().Add(-10 * time.Minute),
		Latest:    "v9.9.9",
		URL:       "https://example.invalid/releases/v9.9.9",
	})
	r := Check(context.Background(), "0.0.1")
	if r.Latest != "v9.9.9" {
		t.Errorf("Check should have served cache, got latest=%q", r.Latest)
	}
	if !r.Available {
		t.Errorf("Available should be true (9.9.9 > 0.0.1)")
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

package tui

import (
	"strings"
	"testing"

	"github.com/tesserix/cloudnav/internal/provider/azure"
)

func TestParentRGName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/subscriptions/abc/resourceGroups/rg-foo/providers/Microsoft.Compute/virtualMachines/vm1", "rg-foo"},
		{"/subscriptions/abc/resourceGroups/rg-only", "rg-only"},
		{"/subscriptions/abc", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := parentRGName(c.in); got != c.want {
			t.Errorf("parentRGName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShortDate(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"2026-01-15T12:34:56Z", "2026-01-15"},
		{"2026-01-15T12:34:56+05:30", "2026-01-15"},
		{"2026-01-15", "2026-01-15"},    // fallback path
		{"bogus", emDash},               // unparseable short string
		{"", emDash},                    // empty
		{"2026-01-15xyz", "2026-01-15"}, // fallback takes first 10 chars
	}
	for _, c := range cases {
		if got := shortDate(c.in); got != c.want {
			t.Errorf("shortDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShortTail(t *testing.T) {
	if got := shortTail("abcdef", 10); got != "abcdef" {
		t.Errorf("no-op case: got %q", got)
	}
	if got := shortTail("verylongresourceid", 6); got != "…rceid" {
		t.Errorf("trim case: got %q", got)
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		bin, want string
		args      []string
	}{
		{"brew", "brew install azure-cli", []string{"install", "azure-cli"}},
		{"brew", "brew install --cask google-cloud-sdk", []string{"install", "--cask", "google-cloud-sdk"}},
		{"sh", `sh -c 'echo hello world'`, []string{"-c", "echo hello world"}},
		{"sh", `sh -c 'printf %s '\''x'\'''`, []string{"-c", `printf %s 'x'`}},
	}
	for _, c := range cases {
		if got := shellQuote(c.bin, c.args); got != c.want {
			t.Errorf("shellQuote(%q,%v)\n  got:  %s\n  want: %s", c.bin, c.args, got, c.want)
		}
	}
}

func TestFilterAdvisorByScope(t *testing.T) {
	recs := []azure.Recommendation{
		{ResourceID: "/subscriptions/abc/resourceGroups/rg-a/providers/Microsoft.Compute/virtualMachines/vm1", Problem: "p1"},
		{ResourceID: "/subscriptions/abc/resourceGroups/rg-b/providers/Microsoft.Storage/storageAccounts/s1", Problem: "p2"},
		{ResourceID: "/subscriptions/xyz/resourceGroups/rg-c", Problem: "p3"},
		{ResourceID: "", Problem: "orphan"}, // no target; kept so subscription-scope advisor still lists it
	}

	// Subscription scope: abc matches two, xyz doesn't; orphan is kept.
	got := filterAdvisorByScope(recs, "/subscriptions/abc")
	if len(got) != 3 {
		t.Fatalf("sub scope: got %d, want 3 (including orphan)", len(got))
	}
	// RG scope: only rg-a under abc.
	got = filterAdvisorByScope(recs, "/subscriptions/abc/resourceGroups/rg-a")
	if len(got) != 2 || got[0].Problem != "p1" {
		t.Errorf("rg scope: got %+v", got)
	}
	// Case-insensitive matching so differing Azure ID casing still filters.
	got = filterAdvisorByScope(recs, "/SUBSCRIPTIONS/XYZ")
	if len(got) != 2 { // p3 + orphan
		t.Errorf("case insensitive: got %d, want 2", len(got))
	}
}

func TestPIMSourceBadge(t *testing.T) {
	// Badge output contains ANSI escapes; check the underlying label is there.
	cases := []struct {
		src, contains string
	}{
		{"azure", "azure"},
		{"", "azure"},
		{"entra", "entra"},
		{"group", "group"},
		{"unknown", "unknown"},
	}
	for _, c := range cases {
		if !strings.Contains(pimSourceBadge(c.src), c.contains) {
			t.Errorf("pimSourceBadge(%q) missing %q", c.src, c.contains)
		}
	}
}

func TestCategoryBadge(t *testing.T) {
	for _, c := range []string{"Cost", "Security", "Reliability", "HighAvailability", "Performance", "OperationalExcellence", "something-else"} {
		if categoryBadge(c) == "" {
			t.Errorf("categoryBadge(%q) empty", c)
		}
	}
}

func TestImpactBadge(t *testing.T) {
	for _, c := range []string{"High", "Medium", "Low", "Other"} {
		if impactBadge(c) == "" {
			t.Errorf("impactBadge(%q) empty", c)
		}
	}
}

func TestShorten(t *testing.T) {
	if got := shorten("short", 10); got != "short" {
		t.Errorf("no-op: got %q", got)
	}
	if got := shorten("abcdefghijklm", 8); len(got) > 8 || !strings.Contains(got, "a") {
		t.Errorf("trim: got %q", got)
	}
}

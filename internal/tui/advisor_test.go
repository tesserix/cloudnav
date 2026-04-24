package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestAdvisorInnerWidthClamps(t *testing.T) {
	cases := []struct {
		termW, want int
	}{
		{termW: 40, want: 48},   // tiny terminal → min clamp
		{termW: 62, want: 48},   // 62-14=48 at min boundary
		{termW: 80, want: 66},   // 80-14
		{termW: 156, want: 114}, // common laptop width → max clamp
		{termW: 300, want: 114}, // ultra-wide → still capped
	}
	for _, c := range cases {
		if got := advisorInnerWidth(c.termW); got != c.want {
			t.Errorf("advisorInnerWidth(%d) = %d, want %d", c.termW, got, c.want)
		}
	}
}

func TestAdvisorInnerHeightClamps(t *testing.T) {
	cases := []struct {
		termH, want int
	}{
		{termH: 10, want: 14}, // below min → min clamp
		{termH: 22, want: 14}, // 22-8=14 at min
		{termH: 38, want: 30}, // 38-8 (common)
		{termH: 60, want: 52}, // tall terminal
	}
	for _, c := range cases {
		if got := advisorInnerHeight(c.termH); got != c.want {
			t.Errorf("advisorInnerHeight(%d) = %d, want %d", c.termH, got, c.want)
		}
	}
}

func TestStableAdvisorBodyPadsShortContent(t *testing.T) {
	body := stableAdvisorBody([]string{"a", "bb"}, 156, 38)
	lines := strings.Split(body, "\n")
	wantH := advisorInnerHeight(38)
	wantW := advisorInnerWidth(156)
	if len(lines) != wantH {
		t.Fatalf("body has %d lines, want %d", len(lines), wantH)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != wantW {
			t.Errorf("line %d width = %d, want %d (line=%q)", i, w, wantW, line)
		}
	}
}

func TestStableAdvisorBodyTruncatesLongLines(t *testing.T) {
	long := strings.Repeat("x", 500)
	body := stableAdvisorBody([]string{long}, 156, 38)
	lines := strings.Split(body, "\n")
	wantW := advisorInnerWidth(156)
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != wantW {
			t.Errorf("line %d width = %d, want %d (truncation failed)", i, w, wantW)
		}
	}
}

func TestStableAdvisorBodyClipsExcessLines(t *testing.T) {
	in := make([]string, 0, 200)
	for range 200 {
		in = append(in, "padding line")
	}
	body := stableAdvisorBody(in, 156, 38)
	if h := lipgloss.Height(body); h != advisorInnerHeight(38) {
		t.Errorf("excess lines not clipped: height=%d, want %d", h, advisorInnerHeight(38))
	}
}

// TestAdvisorResourceCardStableAcrossScroll is the regression test for
// the "popup shrinks / grows as the user scrolls" bug. It builds an
// advisor popup with a mix of short- and long-problem recommendations
// and verifies the outer frame has the same width and height at every
// scroll offset.
func TestAdvisorResourceCardStableAcrossScroll(t *testing.T) {
	m := &model{width: 156, height: 38}
	m.advisorResource = provider.Node{
		Name:     "sqlsvr-alti-aue-np-dev",
		Location: "australiaeast",
		Cost:     "£7.29",
		Meta: map[string]string{
			"type": "Microsoft.Sql/servers",
		},
	}
	recs := []provider.Recommendation{
		{Impact: "high", Problem: "Short problem text 1", Solution: "Short solution 1"},
		{Impact: "high", Problem: "Minimal set of principals should be members of fixed high impact database roles in SQL databases"},
		{Impact: "medium", Problem: "SQL Threat Detection should be enabled at the SQL server level"},
		{Impact: "low", Problem: "Track all users with access to the database for SQL Databases"},
		{Impact: "low", Problem: "Track all users with access to the database for SQL Databases"},
		{Impact: "low", Problem: "Track all users with access to the database for SQL Databases", Solution: "Audit quarterly"},
		{Impact: "low", Problem: "Track all users with access to the database for SQL Databases"},
		{Impact: "low", Problem: "Track all users with access to the database for SQL Databases"},
		{Impact: "low", Problem: "Track all users with access to the database for SQL Databases"},
		{Impact: "low", Problem: "Track all users with access to the database for SQL Databases"},
		{Impact: "low", Problem: "Track all users with access to the database for SQL Databases"},
	}

	var baseW, baseH int
	for idx := 0; idx < len(recs); idx++ {
		m.advisorIdx = idx
		out := m.advisorResourceCard("Azure Advisor", recs)
		w := lipgloss.Width(out)
		h := lipgloss.Height(out)
		if idx == 0 {
			baseW, baseH = w, h
			continue
		}
		if w != baseW || h != baseH {
			t.Errorf("popup dims at scroll=%d: (%d × %d), want (%d × %d) — shrinkage regression",
				idx, w, h, baseW, baseH)
		}
	}
}

// TestAdvisorResourceCardStableWithEmpty covers the empty-state branch
// (no recommendations for the current scope) — its frame must match
// the loaded-state frame so toggling in/out of empty doesn't redraw.
func TestAdvisorResourceCardStableWithEmpty(t *testing.T) {
	m := &model{width: 156, height: 38}
	m.advisorResource = provider.Node{
		Name: "kv-alti-aue-np-dev",
		Meta: map[string]string{"type": "Microsoft.KeyVault/vaults"},
	}
	empty := m.advisorResourceCard("Azure Advisor", nil)
	loaded := m.advisorResourceCard("Azure Advisor", []provider.Recommendation{
		{Impact: "high", Problem: "p", Solution: "s"},
	})
	if lipgloss.Width(empty) != lipgloss.Width(loaded) || lipgloss.Height(empty) != lipgloss.Height(loaded) {
		t.Errorf("empty popup (%dx%d) differs from loaded (%dx%d)",
			lipgloss.Width(empty), lipgloss.Height(empty),
			lipgloss.Width(loaded), lipgloss.Height(loaded))
	}
}

func TestImpactBullet(t *testing.T) {
	cases := []struct {
		impact string
		want   string // rendered must contain this substring
	}{
		{impactHigh, "HIGH"},
		{"High", "HIGH"},
		{impactMedium, "MEDIUM"},
		{"MEDIUM", "MEDIUM"},
		{impactLow, "low"},
		{"", ""},
		{"critical", "critical"},
	}
	for _, c := range cases {
		got := impactBullet(c.impact)
		if c.want == "" && got != "" {
			t.Errorf("impactBullet(%q) = %q, want empty", c.impact, got)
		}
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("impactBullet(%q) = %q, want contains %q", c.impact, got, c.want)
		}
	}
}

func TestAdvisorSummary(t *testing.T) {
	recs := []provider.Recommendation{
		{Impact: "high"},
		{Impact: "HIGH"},
		{Impact: "medium"},
		{Impact: "low"},
		{Impact: "Low"},
		{Impact: "low"},
	}
	got := advisorSummary(recs)
	for _, want := range []string{"6 recommendations", "2 high", "1 medium", "3 low"} {
		if !strings.Contains(got, want) {
			t.Errorf("advisorSummary missing %q in %q", want, got)
		}
	}
}

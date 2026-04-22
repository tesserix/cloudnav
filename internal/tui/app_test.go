package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
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
	recs := []provider.Recommendation{
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

// newSearchModel is a focused fixture for exercising updateSearch(). It
// avoids the full newModel() ceremony (providers, config, etc.) — the search
// behavior is decoupled from everything other than the table, text input, and
// a minimal frame that refreshTable can operate on.
func newSearchModel(rows int) *model {
	tbl := table.New(
		table.WithColumns([]table.Column{{Title: "name", Width: 20}}),
		table.WithFocused(true),
		table.WithHeight(rows+2),
	)
	data := make([]table.Row, rows)
	nodes := make([]provider.Node, rows)
	for i := 0; i < rows; i++ {
		name := string(rune('A' + i))
		data[i] = table.Row{name}
		nodes[i] = provider.Node{ID: name, Name: name, Kind: provider.KindSubscription}
	}
	tbl.SetRows(data)

	ti := textinput.New()
	return &model{
		table:      tbl,
		search:     ti,
		searchMode: true,
		stack:      []frame{{title: "test", nodes: nodes}},
		costs:      map[string]map[string]string{},
		selected:   map[string]bool{},
		width:      120,
		height:     30,
	}
}

func TestUpdateSearchIgnoresNavigationKeys(t *testing.T) {
	// When the user is typing a / filter, up/down/pgup/pgdown must not move
	// the table cursor — otherwise committing the filter lands on a row the
	// user didn't visually select.
	m := newSearchModel(10)
	m.table.SetCursor(3)

	nav := []tea.KeyMsg{
		{Type: tea.KeyUp},
		{Type: tea.KeyDown},
		{Type: tea.KeyPgUp},
		{Type: tea.KeyPgDown},
	}
	for _, msg := range nav {
		if _, _ = m.updateSearch(msg); m.table.Cursor() != 3 {
			t.Errorf("key %s moved cursor to %d; expected it to stay at 3", msg.String(), m.table.Cursor())
			m.table.SetCursor(3) // reset for next iteration
		}
	}
}

func TestUpdateSearchEscClearsFilter(t *testing.T) {
	m := newSearchModel(3)
	m.filter = "foo"
	m.search.SetValue("foo")

	if _, _ = m.updateSearch(tea.KeyMsg{Type: tea.KeyEsc}); m.searchMode {
		t.Error("Esc should exit search mode")
	}
	if m.filter != "" {
		t.Errorf("Esc should clear filter, got %q", m.filter)
	}
	if m.search.Value() != "" {
		t.Errorf("Esc should clear the search input, got %q", m.search.Value())
	}
}

func TestUpdateSearchEnterKeepsFilter(t *testing.T) {
	m := newSearchModel(3)
	m.filter = "abc"
	m.search.SetValue("abc")

	if _, _ = m.updateSearch(tea.KeyMsg{Type: tea.KeyEnter}); m.searchMode {
		t.Error("Enter should exit search mode")
	}
	if m.filter != "abc" {
		t.Errorf("Enter should keep the filter, got %q", m.filter)
	}
}

func TestPIMSourceLabelIsPlain(t *testing.T) {
	// The plain label must not embed ANSI escape bytes; it's used on the
	// selected row so the outer lipgloss Selected background can span the
	// full row without being terminated by the badge's own reset code.
	for _, src := range []string{"azure", "", "entra", "group", "gcp", "custom-xyz"} {
		got := pimSourceLabel(src)
		if strings.ContainsRune(got, '\x1b') {
			t.Errorf("pimSourceLabel(%q) contains ANSI escape: %q", src, got)
		}
		if got == "" {
			t.Errorf("pimSourceLabel(%q) should not be empty", src)
		}
	}
}

func TestShortenTags(t *testing.T) {
	if got := shortenTags("", 10); got != emDash {
		t.Errorf("empty tags should render as emDash, got %q", got)
	}
	if got := shortenTags("env=prod", 20); got != "env=prod" {
		t.Errorf("short enough should pass through, got %q", got)
	}
	if got := shortenTags("env=prod, owner=platform, tier=gold", 15); got != "env=prod, owne…" {
		t.Errorf("truncation: got %q", got)
	}
	if got := shortenTags("anything", 1); got != "…" {
		t.Errorf("max=1 should render ellipsis only, got %q", got)
	}
}

func TestBudgetIndicator(t *testing.T) {
	cases := []struct {
		current, budget float64
		wantEmpty       bool
		wantEmoji       string
	}{
		{100, 0, true, ""},       // no budget set → blank
		{100, 1000, false, "🟢"},  // 10% of budget
		{760, 1000, false, "🟡"},  // 76% → warn
		{1000, 1000, false, "🔴"}, // exactly at budget → over
		{1500, 1000, false, "🔴"}, // over budget → red
	}
	for _, c := range cases {
		got := budgetIndicator(c.current, c.budget)
		if c.wantEmpty {
			if strings.TrimSpace(got) != "" {
				t.Errorf("budget %v / %v = %q, want blank", c.current, c.budget, got)
			}
			continue
		}
		if !strings.Contains(got, c.wantEmoji) {
			t.Errorf("budget %v / %v = %q, want emoji %q", c.current, c.budget, got, c.wantEmoji)
		}
	}
}

func TestForecastCell(t *testing.T) {
	if got := forecastCell(0, "USD"); got != emDash {
		t.Errorf("zero forecast should render as emDash, got %q", got)
	}
	if got := forecastCell(-5, "USD"); got != emDash {
		t.Errorf("negative forecast should render as emDash, got %q", got)
	}
	if got := forecastCell(1234.567, "USD"); got != "$1234.57" {
		t.Errorf("forecast render: got %q", got)
	}
	if got := forecastCell(42.5, "GBP"); got != "£42.50" {
		t.Errorf("GBP symbol: got %q", got)
	}
}

func TestBillingDeltaAnomalyPrefix(t *testing.T) {
	// Below threshold: no ⚠ prefix.
	if got := billingDelta(110, 100); strings.Contains(got, "⚠") {
		t.Errorf("10%% delta should not be flagged, got %q", got)
	}
	// Above +25%: ⚠ prefix on upward spike.
	if got := billingDelta(130, 100); !strings.Contains(got, "⚠") {
		t.Errorf("30%% up should be flagged, got %q", got)
	}
	// Below -25%: ⚠ prefix on downward drop.
	if got := billingDelta(70, 100); !strings.Contains(got, "⚠") {
		t.Errorf("-30%% should be flagged, got %q", got)
	}
	// Zero last-month: "new" marker, not ⚠.
	if got := billingDelta(100, 0); strings.Contains(got, "⚠") {
		t.Errorf("new spend should not be flagged, got %q", got)
	}
}

func TestSparklineFlatSeries(t *testing.T) {
	// A constant series should render the lowest block across the whole
	// width so the UI doesn't hide it — "everything at zero" and
	// "everything at peak" both look different from a real workload
	// and that's what we want.
	got := sparkline([]float64{5, 5, 5, 5})
	if got != "▁▁▁▁" {
		t.Errorf("flat = %q, want ▁▁▁▁", got)
	}
}

func TestSparklineScales(t *testing.T) {
	// Monotonically increasing series should map to ascending blocks.
	// Block runes are 3 bytes in UTF-8, so compare rune-count not byteLen.
	got := sparkline([]float64{0, 1, 2, 3, 4, 5, 6, 7})
	runes := []rune(got)
	if len(runes) != 8 {
		t.Fatalf("runes = %d, want 8", len(runes))
	}
	if runes[0] != '▁' {
		t.Errorf("first = %q, want lowest block", runes[0])
	}
	if runes[len(runes)-1] != '█' {
		t.Errorf("last = %q, want highest block", runes[len(runes)-1])
	}
}

func TestSparklineEmpty(t *testing.T) {
	if got := sparkline(nil); len(got) == 0 {
		t.Error("empty series should still produce a non-empty flat line so the column stays aligned")
	}
}

func TestSeriesStats(t *testing.T) {
	mn, mx, last := seriesStats([]float64{3, 1, 4, 1, 5, 9, 2, 6})
	if mn != 1 || mx != 9 || last != 6 {
		t.Errorf("stats = (%v, %v, %v), want (1, 9, 6)", mn, mx, last)
	}
	mn, mx, last = seriesStats(nil)
	if mn != 0 || mx != 0 || last != 0 {
		t.Errorf("empty = (%v, %v, %v), want zeroes", mn, mx, last)
	}
}

func TestAdvisorMatchesFilter(t *testing.T) {
	r := provider.Recommendation{
		Category:     "Cost",
		Impact:       "High",
		Problem:      "Unattached managed disk",
		Solution:     "Delete unattached disks",
		ImpactedType: "Microsoft.Compute/disks",
		ResourceID:   "/subscriptions/abc/resourceGroups/rg-foo/providers/Microsoft.Compute/disks/disk1",
	}
	cases := map[string]bool{
		"cost":       true,  // category
		"high":       true,  // impact
		"unattached": true,  // problem
		"delete":     true,  // solution
		"disks":      true,  // type
		"rg-foo":     true,  // resource id
		"security":   false, // not in any field
	}
	for q, want := range cases {
		if got := advisorMatchesFilter(r, q); got != want {
			t.Errorf("filter %q = %v, want %v", q, got, want)
		}
	}
}

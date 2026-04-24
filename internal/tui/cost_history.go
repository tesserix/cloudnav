package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/NimbleMarkets/ntcharts/linechart"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

// costHistPreset is one button in the window-selector strip above the
// chart. Key is the literal keystroke the user types; the provider
// receives Days and Bucket verbatim.
type costHistPreset struct {
	Key    string
	Label  string
	Days   int
	Bucket provider.CostBucket
}

// costHistoryPresets returns the window tabs in display order. The keys
// are all single lowercase characters so they're safe to intercept
// inside the overlay without trampling navigation (the overlay filters
// every keystroke anyway).
func costHistoryPresets() []costHistPreset {
	return []costHistPreset{
		{Key: "w", Label: "1W", Days: 7, Bucket: provider.BucketDay},
		{Key: "m", Label: "1M", Days: 30, Bucket: provider.BucketDay},
		{Key: "3", Label: "3M", Days: 90, Bucket: provider.BucketDay},
		{Key: "6", Label: "6M", Days: 180, Bucket: provider.BucketDay},
		{Key: "y", Label: "1Y", Days: 365, Bucket: provider.BucketMonth},
	}
}

// defaultCostWindow is the shape the `$` shortcut opens with.
func defaultCostWindow() provider.CostHistoryOptions {
	return provider.CostHistoryOptions{Days: 90, Bucket: provider.BucketDay}
}

// windowLabel gives a short human label for opts used in the status
// line and chart header.
func windowLabel(opts provider.CostHistoryOptions) string {
	switch {
	case opts.Days <= 7:
		return "last week"
	case opts.Days <= 31:
		return "last month"
	case opts.Days <= 95:
		return "last 3 months"
	case opts.Days <= 190:
		return "last 6 months"
	default:
		return "last year"
	}
}

// loadCostHistory is the entry point used by the `$` key when the
// overlay isn't yet open. It sets the full-frame loading state so the
// table screen shows a spinner until data arrives, and scopes the
// query to whichever subscription is currently in focus so the chart
// lines up with what the user is looking at.
func (m *model) loadCostHistory(opts provider.CostHistoryOptions) tea.Cmd {
	if _, ok := m.active.(provider.CostHistoryer); !ok {
		m.status = m.active.Name() + ": cost history not wired yet for this cloud"
		return nil
	}
	if opts.Days <= 0 {
		opts = defaultCostWindow()
	}
	// Inherit the drill-level scope when the caller didn't pass one.
	// Keeps $ contextual — press it on a subscription row and you get
	// that sub, press it at the cloud list and you get the fan-out.
	if opts.Scope == "" {
		opts.Scope, opts.ScopeLabel = m.currentCostScope()
	}
	// Capture the list of subs the user can cycle through with [ / ]
	// so they can compare without closing the overlay and re-drilling.
	m.costHistSubs, m.costHistSubIdx = m.collectCostHistSubs(opts.Scope)
	m.loading = true
	m.costHistLoading = true
	m.status = fmt.Sprintf("loading %s cost history for %s...", windowLabel(opts), m.active.Name())
	return tea.Batch(m.spinner.Tick, m.fetchCostHistory(opts))
}

// collectCostHistSubs returns the ordered sub list the [ / ] cycle
// should walk, plus the index of currentScope inside it. Prefers the
// currently-visible subs frame (so the user cycles through exactly
// what they were just looking at); falls back to the entity cache
// that the palette preloads at startup.
func (m *model) collectCostHistSubs(currentScope string) ([]provider.Node, int) {
	var subs []provider.Node
	for i := len(m.stack) - 1; i >= 0; i-- {
		f := m.stack[i]
		if len(f.nodes) > 0 && f.nodes[0].Kind == provider.KindSubscription {
			subs = append([]provider.Node(nil), f.nodes...)
			break
		}
	}
	if len(subs) == 0 {
		subs = append(subs, m.entities[pimSrcAzure]...)
	}
	idx := -1
	for i, s := range subs {
		if s.ID == currentScope {
			idx = i
			break
		}
	}
	return subs, idx
}

// currentCostScope returns the subscription ID (and human name) most
// relevant to the user's current position: the highlighted row when
// browsing the subscription list, the parent sub when drilled into an
// RG or resource, empty when at the cloud root.
func (m *model) currentCostScope() (string, string) {
	if m.active == nil || m.active.Name() != pimSrcAzure {
		return "", ""
	}
	// On the subscription list — use the highlighted row.
	if m.atSubscriptionLevel() {
		c := m.table.Cursor()
		if c >= 0 && c < len(m.visibleNodes) {
			n := m.visibleNodes[c]
			if n.Kind == provider.KindSubscription {
				return n.ID, n.Name
			}
		}
	}
	// Drilled into a sub — walk up the stack until we find one.
	for i := len(m.stack) - 1; i >= 0; i-- {
		if kindOf(&m.stack[i]) == provider.KindSubscription {
			if m.stack[i].parent != nil {
				return m.stack[i].parent.ID, m.stack[i].parent.Name
			}
		}
		if m.stack[i].parent != nil && m.stack[i].parent.Kind == provider.KindSubscription {
			return m.stack[i].parent.ID, m.stack[i].parent.Name
		}
	}
	return "", ""
}

// reloadCostHistory swaps to a different window while the overlay is
// already open. Keeps the old chart on screen so the user never stares
// at a blank box; flips costHistLoading so the header shows a spinner.
// Scope is carried over from the previously-loaded chart so switching
// windows doesn't silently re-fan-out across the whole tenant.
func (m *model) reloadCostHistory(opts provider.CostHistoryOptions) tea.Cmd {
	if _, ok := m.active.(provider.CostHistoryer); !ok {
		return nil
	}
	if opts.Scope == "" {
		opts.Scope = m.costHistOpts.Scope
		opts.ScopeLabel = m.costHistOpts.ScopeLabel
	}
	if opts == m.costHistOpts {
		return nil
	}
	m.costHistLoading = true
	m.costHistOpts = opts
	return tea.Batch(m.spinner.Tick, m.fetchCostHistory(opts))
}

// fetchCostHistory runs the provider call on a goroutine and emits the
// result as a costHistoryLoadedMsg. Shared by the first-open and
// in-place-reload paths.
func (m *model) fetchCostHistory(opts provider.CostHistoryOptions) tea.Cmd {
	ch, _ := m.active.(provider.CostHistoryer)
	ctx := m.ctx
	return func() tea.Msg {
		hist, err := ch.CostHistory(ctx, opts)
		if err != nil {
			return errMsg{err}
		}
		return costHistoryLoadedMsg{history: hist, opts: opts}
	}
}

// updateCostHistory handles keys while the cost-history overlay is
// visible. Esc / $ close; any preset key reloads in place; P opens
// the PIM overlay so the user can elevate when we detected a cost-read
// permission gap on this scope.
func (m *model) updateCostHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case keyEsc, "q", "$":
		m.costHistMode = false
		m.costHistLoading = false
		m.status = ""
		return m, nil
	case "P", "p":
		if m.costHistData.AccessDenied {
			m.costHistMode = false
			m.costHistLoading = false
			return m, m.loadPIM()
		}
	case "[":
		return m, m.cycleCostHistSub(-1)
	case "]":
		return m, m.cycleCostHistSub(+1)
	case "a", "A":
		// Drop the scope filter so the provider fans out across every
		// subscription the caller can see.
		opts := m.costHistOpts
		opts.Scope = ""
		opts.ScopeLabel = ""
		return m, m.reloadCostHistory(opts)
	}
	for _, p := range costHistoryPresets() {
		if key == p.Key {
			opts := provider.CostHistoryOptions{Days: p.Days, Bucket: p.Bucket}
			return m, m.reloadCostHistory(opts)
		}
	}
	return m, nil
}

// cycleCostHistSub moves the chart to the next or previous sub in the
// list collected at overlay-open time. Wraps at either end so the
// user never dead-ends. No-op when only one sub is available or the
// list is empty (e.g. fresh session with no subs frame loaded).
func (m *model) cycleCostHistSub(delta int) tea.Cmd {
	n := len(m.costHistSubs)
	if n == 0 {
		return nil
	}
	idx := m.costHistSubIdx + delta
	// Wrap rather than clamp so [ / ] always do something visible.
	idx = ((idx % n) + n) % n
	m.costHistSubIdx = idx
	sub := m.costHistSubs[idx]
	opts := m.costHistOpts
	opts.Scope = sub.ID
	opts.ScopeLabel = sub.Name
	return m.reloadCostHistory(opts)
}

// costHistoryView renders the full-screen overlay — a stock-ticker
// style line chart of daily spend with month-over-month deltas. Sizes
// itself to the terminal so the whole page is used rather than leaving
// a small box on an otherwise empty screen.
func (m *model) costHistoryView() string {
	h := m.costHistData
	w := m.width
	H := m.height
	if w <= 0 {
		w = 120
	}
	if H <= 0 {
		H = 32
	}
	// lipgloss Box border (2) + Padding(1,2) (4 horizontal, 2 vertical).
	innerW := w - 6
	if innerW < 60 {
		innerW = 60
	}
	innerH := H - 4
	if innerH < 18 {
		innerH = 18
	}

	scope := h.Series.Label
	if scope == "" {
		scope = m.active.Name()
	}
	title := styles.Title.Render("cost history")
	position := ""
	if len(m.costHistSubs) > 1 && m.costHistSubIdx >= 0 {
		position = fmt.Sprintf(" · %d/%d", m.costHistSubIdx+1, len(m.costHistSubs))
	}
	sub := styles.Help.Render(scope + position + " · " + windowLabel(m.costHistOpts) + " · " + currencyCode(h.Currency))
	loading := ""
	if m.costHistLoading {
		loading = "  " + styles.WarnS.Render(m.spinner.View()+" loading "+windowLabel(m.costHistOpts)+"...")
	}
	header := title + "  " + sub + loading

	tabs := renderCostHistoryTabs(m.costHistOpts)

	// Reserved chrome rows (the chart gets whatever's left).
	// header(1) + blank(1) + tabs(1) + blank(1) + monthStrip(1) + blank(1)
	// + chart(N) + blank(1) + note(0|1) + footer(1)
	reserved := 7
	if h.Note != "" {
		reserved++
	}
	chartH := innerH - reserved
	if chartH < 8 {
		chartH = 8
	}

	// Empty or all-zero series renders the same empty state — a chart
	// of straight-zero lines isn't information, it's noise. Distinguish
	// "no access" vs "access but zero spend" so the footer hints match.
	total := 0.0
	for _, p := range h.Series.Points {
		total += p.Amount
	}
	if len(h.Series.Points) == 0 || total == 0 {
		msg := "no cost data — check Cost Management Reader on your subscriptions"
		footer := styles.Help.Render("esc/$ close · [ / ] prev/next sub · a all subs · w 1W · m 1M · 3 3M · 6 6M · y 1Y")
		switch {
		case h.AccessDenied:
			msg = "you don't have cost-read access on " + scope
			footer = styles.WarnS.Render("P → jump to PIM to request access") + styles.Help.Render("   ·   esc/$ close")
		case len(h.Series.Points) > 0 && total == 0:
			// Genuine zero spend over the window — happens on fresh
			// sandbox subs or scopes where costs haven't rolled up yet.
			msg = "no spend recorded on " + scope + " over the selected window"
			if h.Note != "" {
				msg += "\n" + h.Note
			}
		}
		empty := strings.Join([]string{
			header,
			"",
			tabs,
			"",
			center(styles.Help.Render(msg), innerW),
			"",
			center(footer, innerW),
		}, "\n")
		return fullScreenBox(w, H).Render(empty)
	}

	chart := renderChart(h.Series.Points, innerW, chartH, h.Currency)
	months := renderMonthStrip(h.Months, h.Currency, innerW)
	footerBase := "  esc/$ close  ·  [ / ] prev/next sub · a all subs · w 1W · m 1M · 3 3M · 6 6M · y 1Y"
	if h.AccessDenied {
		footerBase = "  " + styles.WarnS.Render("P → jump to PIM to request cost-read access") +
			styles.Help.Render("   ·   esc/$ close")
	}
	footer := styles.Help.Render(footerBase)
	if h.AccessDenied {
		footer = footerBase
	}

	lines := []string{header, "", tabs, ""}
	if months != "" {
		lines = append(lines, months, "")
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, chart, "")
	if h.Note != "" {
		lines = append(lines, styles.Help.Render("  note · "+h.Note))
	}
	lines = append(lines, footer)
	return fullScreenBox(w, H).Render(strings.Join(lines, "\n"))
}

// renderCostHistoryTabs draws the [ 1W | 1M | 3M | 6M | 1Y ] selector.
func renderCostHistoryTabs(opts provider.CostHistoryOptions) string {
	parts := []string{}
	for _, p := range costHistoryPresets() {
		active := matchesPreset(opts, p)
		label := "  " + p.Label + "  "
		if active {
			parts = append(parts, styles.Selected.Render(label))
		} else {
			parts = append(parts, styles.Help.Render(label))
		}
	}
	sep := styles.Help.Render("│")
	return "  " + strings.Join(parts, sep)
}

func matchesPreset(opts provider.CostHistoryOptions, p costHistPreset) bool {
	return opts.Days == p.Days && opts.Bucket == p.Bucket
}

// renderMonthStrip builds a summary row — one block per month with
// its total and the month-over-month delta, colour-coded so spikes
// stand out without having to squint at the line.
func renderMonthStrip(months []provider.CostMonth, currency string, maxW int) string {
	if len(months) == 0 {
		return ""
	}
	sym := currencySym(currency)
	parts := []string{}
	for i, mo := range months {
		label := fmt.Sprintf("%s %d", shortMonth(mo.Month), mo.Year%100)
		amount := fmt.Sprintf("%s%s", sym, compactAmount(mo.Total))
		var delta string
		var style lipgloss.Style
		if i == 0 {
			delta = "baseline"
			style = styles.Help
		} else {
			prev := months[i-1].Total
			switch {
			case prev <= 0 && mo.Total <= 0:
				// Both months empty — don't pretend there's a change.
				delta = "—"
				style = styles.Help
			case prev <= 0 && mo.Total > 0:
				delta = "new"
				style = styles.AccentS
			default:
				pct := (mo.Total - prev) / prev * 100
				var arrow string
				switch {
				case pct >= 10:
					arrow = "▲"
					style = styles.Bad
				case pct >= 2:
					arrow = "↑"
					style = styles.WarnS
				case pct <= -10:
					arrow = "▼"
					style = styles.Good
				case pct <= -2:
					arrow = "↓"
					style = styles.Good
				default:
					arrow = "→"
					style = styles.Help
				}
				delta = fmt.Sprintf("%s %+.1f%%", arrow, pct)
			}
		}
		block := fmt.Sprintf("%s %s %s",
			styles.Key.Render(label),
			styles.Cost.Render(amount),
			style.Render(delta),
		)
		parts = append(parts, block)
	}
	sep := styles.Help.Render("   ·   ")
	joined := "  " + strings.Join(parts, sep)
	// If the strip exceeds the available width, drop the oldest entries
	// one by one until it fits. The baseline reference becomes less
	// important than the recent trend when space is tight.
	for lipgloss.Width(joined) > maxW && len(parts) > 1 {
		parts = parts[1:]
		joined = "  " + strings.Join(parts, sep)
	}
	return joined
}


// renderChart draws the cost time-series using ntcharts' Braille line
// chart. Much simpler and more robust than hand-rolled axis /
// slope-glyph rendering: ntcharts handles scaling, axis drawing,
// label placement, and Braille compositing in ~40 lines of caller
// code. Today's point is dropped because it is still accumulating and
// always drags the line down unrealistically.
func renderChart(points []provider.CostHistoryPoint, width, height int, currency string) string {
	if len(points) < 2 || width < 40 || height < 8 {
		return ""
	}
	// Drop the partial last point (today is still accumulating).
	data := points
	if len(data) > 1 {
		data = data[:len(data)-1]
	}

	maxY := 0.0
	for _, p := range data {
		if p.Amount > maxY {
			maxY = p.Amount
		}
	}
	// Floor at 1 so an all-zero series doesn't divide-by-zero in the
	// axis scaler and the chart still lays out sensibly.
	if maxY == 0 {
		maxY = 1
	}

	axisStyle := lipgloss.NewStyle().Foreground(styles.Muted)
	labelStyle := lipgloss.NewStyle().Foreground(styles.Muted)
	lineStyle := lipgloss.NewStyle().Foreground(styles.Accent)

	sym := currencySym(currency)
	chart := linechart.New(
		width, height,
		1, float64(len(data)),
		0, maxY*1.15,
		linechart.WithStyles(axisStyle, labelStyle, lineStyle),
		linechart.WithXYSteps(5, 4),
		linechart.WithXLabelFormatter(func(_ int, v float64) string {
			idx := int(v) - 1
			if idx < 0 || idx >= len(data) {
				return ""
			}
			return data[idx].Date.Format("Jan 2")
		}),
		linechart.WithYLabelFormatter(func(_ int, v float64) string {
			return sym + compactAmount(v)
		}),
	)
	chart.DrawXYAxisAndLabel()
	for i := 0; i < len(data)-1; i++ {
		chart.DrawBrailleLineWithStyle(
			canvas.Float64Point{X: float64(i + 1), Y: data[i].Amount},
			canvas.Float64Point{X: float64(i + 2), Y: data[i+1].Amount},
			lineStyle,
		)
	}
	return chart.View()
}

func shortMonth(m time.Month) string {
	return m.String()[:3]
}

// compactAmount renders a value with an adaptive suffix so the Y-axis
// labels and month-strip totals stay narrow.
func compactAmount(v float64) string {
	abs := math.Abs(v)
	switch {
	case abs >= 1_000_000:
		return fmt.Sprintf("%.1fM", v/1_000_000)
	case abs >= 1_000:
		return fmt.Sprintf("%.1fk", v/1_000)
	case abs >= 100:
		return fmt.Sprintf("%.0f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

func currencySym(code string) string {
	return currencyChar(code)
}

func currencyCode(code string) string {
	if code == "" {
		return currencyUSD
	}
	return strings.ToUpper(code)
}

// center pads s with spaces so it sits roughly mid-line inside width
// columns. Short-circuits on already-too-wide strings so we don't blow
// out the layout.
func center(s string, width int) string {
	wide := lipgloss.Width(s)
	if wide >= width {
		return s
	}
	pad := (width - wide) / 2
	return strings.Repeat(" ", pad) + s
}

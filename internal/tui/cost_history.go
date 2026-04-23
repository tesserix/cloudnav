package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

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
	m.loading = true
	m.costHistLoading = true
	m.status = fmt.Sprintf("loading %s cost history for %s...", windowLabel(opts), m.active.Name())
	return tea.Batch(m.spinner.Tick, m.fetchCostHistory(opts))
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
	}
	for _, p := range costHistoryPresets() {
		if key == p.Key {
			opts := provider.CostHistoryOptions{Days: p.Days, Bucket: p.Bucket}
			return m, m.reloadCostHistory(opts)
		}
	}
	return m, nil
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
	sub := styles.Help.Render(scope + " · " + windowLabel(m.costHistOpts) + " · " + currencyCode(h.Currency))
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

	if len(h.Series.Points) == 0 {
		msg := "no cost data — check Cost Management Reader on your subscriptions"
		footer := styles.Help.Render("esc/$ close · w 1W · m 1M · 3 3M · 6 6M · y 1Y")
		if h.AccessDenied {
			msg = "you don't have cost-read access on " + scope
			footer = styles.WarnS.Render("P → jump to PIM to request access") + styles.Help.Render("   ·   esc/$ close")
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

	chart := renderBrailleChart(h.Series.Points, h.Months, h.Bucket, innerW, chartH, h.Currency)
	months := renderMonthStrip(h.Months, h.Currency, innerW)
	footerBase := "  esc/$ close  ·  w 1W · m 1M · 3 3M · 6 6M · y 1Y"
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
			if prev <= 0 {
				delta = "new"
				style = styles.AccentS
			} else {
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

// renderBrailleChart draws the cost chart as an area series: each
// column gets one character — ╱ rising, ╲ falling, ─ flat — in the
// month's trend colour, with ░ shaded fill beneath the line. This
// reads the same way a stock-ticker area chart does: the line shows
// *direction*, the fill shows *magnitude*, and the colour shows
// *month-over-month* movement. Kept under the "braille" name to avoid
// churn in call sites; the visual is simply better than pure dots.
func renderBrailleChart(points []provider.CostHistoryPoint, months []provider.CostMonth, bucket provider.CostBucket, width, height int, currency string) string {
	if len(points) == 0 || width < 20 || height < 6 {
		return ""
	}
	const gutterW = 8
	plotW := width - gutterW - 1
	if plotW < 20 {
		plotW = 20
	}
	plotH := height - 2
	if plotH < 4 {
		plotH = 4
	}

	// Resample the series into plotW columns. Averaging across the
	// bucketed range gives a smoother line than nearest-neighbour and
	// keeps spikes visible on long windows without the chart looking
	// noisy on short ones.
	n := len(points)
	cols := make([]float64, plotW)
	colDate := make([]time.Time, plotW)
	for x := 0; x < plotW; x++ {
		start := x * n / plotW
		end := (x + 1) * n / plotW
		if start == end && start < n {
			end = start + 1
		}
		var sum float64
		var cnt int
		for i := start; i < end && i < n; i++ {
			sum += points[i].Amount
			cnt++
		}
		if cnt > 0 {
			cols[x] = sum / float64(cnt)
			colDate[x] = points[start].Date
		} else if x > 0 {
			cols[x] = cols[x-1]
			colDate[x] = colDate[x-1]
		}
	}

	// Y scaling with 5% headroom so peaks don't touch the top row.
	// Floor at 0 so the fill always anchors to the baseline — negative
	// cost values aren't a thing for our use case and the chart reads
	// backwards if they were.
	mn, mx := cols[0], cols[0]
	for _, v := range cols {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	if mn > 0 {
		mn = 0
	}
	if mx == mn {
		mx = mn + 1
	} else {
		mx += (mx - mn) * 0.05
	}

	rowFor := make([]int, plotW)
	colStyle := make([]lipgloss.Style, plotW)
	for x, v := range cols {
		frac := (v - mn) / (mx - mn)
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		rowFor[x] = int(math.Round(float64(plotH-1) * (1 - frac)))
		colStyle[x] = colourForPoint(colDate[x], months)
	}

	// Pre-compute the line character for each column based on the
	// slope between the prior column and this one.
	lineChar := make([]string, plotW)
	for x := 0; x < plotW; x++ {
		switch {
		case x == 0:
			lineChar[x] = "─"
		case rowFor[x-1] == rowFor[x]:
			lineChar[x] = "─"
		case rowFor[x-1] > rowFor[x]:
			// Previous point was lower on screen → line rises.
			lineChar[x] = "╱"
		default:
			lineChar[x] = "╲"
		}
	}

	// Compose the rows. For each terminal (row, col):
	//   - above the line: empty space
	//   - on the line: the slope character in the column's trend colour
	//   - below the line: ░ fill in a muted tint of the same colour so
	//     the chart body reads like a filled area
	fillStyle := styles.Help
	var lines []string
	for r := 0; r < plotH; r++ {
		var body strings.Builder
		for x := 0; x < plotW; x++ {
			switch {
			case r < rowFor[x]:
				body.WriteByte(' ')
			case r == rowFor[x]:
				body.WriteString(colStyle[x].Bold(true).Render(lineChar[x]))
			default:
				body.WriteString(fillStyle.Render("░"))
			}
		}

		// Y-axis label + │ + plotted row. Five labels spaced over the
		// chart height give enough context without crowding the gutter.
		showLabel := r == 0 || r == plotH-1 || r == plotH/2 || r == plotH/4 || r == 3*plotH/4
		gutter := strings.Repeat(" ", gutterW)
		if showLabel {
			sym := currencySym(currency)
			frac := 1 - float64(r)/float64(plotH-1)
			val := mn + frac*(mx-mn)
			lbl := fmt.Sprintf("%s%s", sym, compactAmount(val))
			if len(lbl) > gutterW-1 {
				lbl = lbl[:gutterW-1]
			}
			gutter = styles.Help.Render(fmt.Sprintf("%*s ", gutterW-1, lbl))
		}
		axis := styles.Help.Render("│")
		lines = append(lines, gutter+axis+body.String())
	}

	// X-axis baseline + tick labels.
	anchorX := make([]int, n)
	for i := range points {
		if n == 1 {
			anchorX[i] = plotW / 2
		} else {
			anchorX[i] = i * (plotW - 1) / (n - 1)
		}
	}
	gutter := strings.Repeat(" ", gutterW)
	baseline := gutter + styles.Help.Render("└"+strings.Repeat("─", plotW))
	lines = append(lines, baseline)
	lines = append(lines, gutter+" "+renderXAxis(points, anchorX, bucket, plotW))

	return strings.Join(lines, "\n")
}

// colourForPoint returns the style a point should render in based on
// its month's total compared to the prior month — red for a steep rise,
// warn for a modest one, green for a fall, accent otherwise.
func colourForPoint(date time.Time, months []provider.CostMonth) lipgloss.Style {
	mi := matchMonth(date, months)
	if mi <= 0 || mi >= len(months) {
		return styles.AccentS
	}
	prev := months[mi-1].Total
	cur := months[mi].Total
	switch {
	case prev <= 0:
		return styles.AccentS
	case cur > prev*1.10:
		return styles.Bad
	case cur > prev*1.02:
		return styles.WarnS
	case cur < prev*0.98:
		return styles.Good
	default:
		return styles.AccentS
	}
}

// renderXAxis places labels along the X axis. Strategy depends on the
// series bucket; in every case we advance by a "next-legal-position"
// cursor so two labels never overlap.
func renderXAxis(points []provider.CostHistoryPoint, anchorX []int, bucket provider.CostBucket, width int) string {
	row := make([]rune, width)
	for i := range row {
		row[i] = ' '
	}
	place := func(x int, label string) int {
		if x < 0 || x+len(label) >= width {
			return -1
		}
		for k, r := range label {
			row[x+k] = r
		}
		return x + len(label) + 1
	}

	nextOK := 0
	switch bucket {
	case provider.BucketMonth:
		// One label per data point is achievable on 1Y (~12 points).
		for i, p := range points {
			x := anchorX[i]
			if x < nextOK {
				continue
			}
			lbl := fmt.Sprintf("%s %d", shortMonth(p.Date.Month()), p.Date.Year()%100)
			n := place(x, lbl)
			if n > 0 {
				nextOK = n
			}
		}
	default:
		// Daily: label every Nth point with Mon DD, stride chosen so
		// labels never collide.
		sampleLabel := "Mon 31" // widest label shape; 6 chars
		stride := len(sampleLabel) + 2
		for i, p := range points {
			x := anchorX[i]
			if x < nextOK {
				continue
			}
			lbl := fmt.Sprintf("%s %d", p.Date.Format("Jan"), p.Date.Day())
			if i == 0 || isMonthBoundary(points, i) || anchorX[i] >= nextOK {
				n := place(x, lbl)
				if n > 0 {
					nextOK = n + stride - len(lbl) - 1
				}
			}
		}
	}
	return styles.Help.Render(string(row))
}

func isMonthBoundary(points []provider.CostHistoryPoint, i int) bool {
	if i == 0 {
		return true
	}
	return points[i].Date.Month() != points[i-1].Date.Month()
}

func matchMonth(d time.Time, months []provider.CostMonth) int {
	if d.IsZero() {
		return -1
	}
	for i, m := range months {
		if m.Year == d.Year() && m.Month == d.Month() {
			return i
		}
	}
	return -1
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

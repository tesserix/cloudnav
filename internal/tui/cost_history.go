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

// defaultCostWindow is the shape the `$` shortcut opens with when the
// user hasn't yet picked a window. Matches the old behaviour (last 3
// months, daily buckets) so people who learned the overlay before this
// change don't notice anything.
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

// loadCostHistory fetches a cost time-series for the requested window
// and opens (or refreshes) the overlay with the result. Re-running
// while the overlay is already visible is supported: the old view
// stays put until the new data arrives so the terminal doesn't flash.
func (m *model) loadCostHistory(opts provider.CostHistoryOptions) tea.Cmd {
	ch, ok := m.active.(provider.CostHistoryer)
	if !ok {
		m.status = m.active.Name() + ": cost history not wired yet for this cloud"
		return nil
	}
	if opts.Days <= 0 {
		opts = defaultCostWindow()
	}
	m.loading = true
	m.costHistLoading = true
	m.status = fmt.Sprintf("loading %s cost history for %s...", windowLabel(opts), m.active.Name())
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			hist, err := ch.CostHistory(ctx, opts)
			if err != nil {
				return errMsg{err}
			}
			return costHistoryLoadedMsg{history: hist, opts: opts}
		},
	)
}

// updateCostHistory handles keys while the cost-history overlay is
// visible. Esc/$ close; a preset key switches to that window.
func (m *model) updateCostHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case keyEsc, "q", "$":
		m.costHistMode = false
		m.status = ""
		return m, nil
	}
	for _, p := range costHistoryPresets() {
		if key == p.Key {
			m.costHistMode = false
			return m, m.loadCostHistory(provider.CostHistoryOptions{Days: p.Days, Bucket: p.Bucket})
		}
	}
	return m, nil
}

// costHistoryView renders the stock-ticker-style cost chart: a dotted
// line across the configured window, month boundaries marked on the X
// axis, and a summary strip above showing each month's total plus its
// percent change from the previous month (green for a decrease, warn
// for a modest rise, red for a steep one).
func (m *model) costHistoryView() string {
	h := m.costHistData
	w := m.width
	if w <= 0 {
		w = 120
	}
	innerW := w - 6
	if innerW < 40 {
		innerW = 40
	}
	chartH := m.height - 14
	if chartH < 8 {
		chartH = 8
	}
	if chartH > 22 {
		chartH = 22
	}

	scope := h.Series.Label
	if scope == "" {
		scope = m.active.Name()
	}
	header := styles.Title.Render("cost history") + "  " +
		styles.Help.Render(scope+" · "+windowLabel(m.costHistOpts)+" · "+currencyCode(h.Currency))

	tabs := renderCostHistoryTabs(m.costHistOpts)

	if len(h.Series.Points) == 0 {
		body := strings.Join([]string{
			header, "", tabs, "",
			styles.Help.Render("  no cost data returned — the caller may not have Cost Management reader on any sub"),
			"", styles.Help.Render("  esc/$ close · w 1W · m 1M · 3 3M · 6 6M · y 1Y"),
		}, "\n")
		return styles.Box.Render(body)
	}

	chart := renderDotChart(h.Series.Points, h.Months, h.Bucket, innerW, chartH, h.Currency)
	months := renderMonthStrip(h.Months, h.Currency)

	lines := []string{header, "", tabs, ""}
	if months != "" {
		lines = append(lines, months, "")
	}
	lines = append(lines, chart)
	if h.Note != "" {
		lines = append(lines, "", styles.Help.Render("  note · "+h.Note))
	}
	lines = append(lines, "", styles.Help.Render("  esc/$ close · w 1W · m 1M · 3 3M · 6 6M · y 1Y"))
	return styles.Box.Render(strings.Join(lines, "\n"))
}

// renderCostHistoryTabs draws the IBKR-style [ 1W | 1M | 3M | 6M | 1Y ]
// selector with the active preset highlighted.
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

// renderMonthStrip builds the IBKR-style summary row: one block per
// month showing its total and the MoM delta, colour-coded so the eye
// can find the spike months without scanning the chart itself.
func renderMonthStrip(months []provider.CostMonth, currency string) string {
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
		switch i {
		case 0:
			delta = "baseline"
			style = styles.Help
		default:
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
		block := fmt.Sprintf("%s  %s  %s",
			styles.Key.Render(label),
			styles.Cost.Render(amount),
			style.Render(delta),
		)
		parts = append(parts, block)
	}
	return "  " + strings.Join(parts, styles.Help.Render("   │   "))
}

// renderDotChart draws the line chart. Each terminal column represents
// one calendar day; dots are placed in the column at the Y-row matching
// the day's cost. Points inside the currently-drawn month render in
// green/red based on whether that month is cheaper or dearer than the
// prior one — mirroring the behaviour of equity charting tools that
// tint month segments by their return.
func renderDotChart(points []provider.CostHistoryPoint, months []provider.CostMonth, bucket provider.CostBucket, width, height int, currency string) string {
	if len(points) == 0 || width < 10 || height < 4 {
		return ""
	}
	const (
		gutterW = 8 // Y-axis label column width
		axisW   = 1
	)
	plotW := width - gutterW - axisW
	if plotW < 10 {
		plotW = width - 3
	}
	plotH := height - 2 // one row for X-axis baseline, one for X-axis labels

	// Size the plot width to the number of points when we have fewer
	// data points than screen columns. This matters for W / M presets
	// where a 7-point series stretched across 100 columns looks dotty
	// and sparse; giving each point its own column keeps the shape
	// readable and leaves predictable room for labels.
	if len(points) < plotW {
		plotW = len(points)
		if plotW < 10 {
			plotW = 10
		}
	}

	// Resample points into plotW columns using an averaging scheme so
	// chart width is independent of point-count.
	cols := make([]float64, plotW)
	colDate := make([]time.Time, plotW)
	n := len(points)
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

	mn, mx := cols[0], cols[0]
	for _, v := range cols {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	// Give the chart a bit of headroom so the max doesn't hug the top edge.
	if mx == mn {
		mx = mn + 1
	} else {
		mx += (mx - mn) * 0.05
	}

	// Pre-compute row Y for each column.
	rowFor := make([]int, plotW)
	for x, v := range cols {
		frac := (v - mn) / (mx - mn)
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		// Top row = 0, bottom row = plotH-1. Higher cost = smaller row.
		rowFor[x] = int(math.Round(float64(plotH-1) * (1 - frac)))
	}

	// Bucket columns by month so we can colour per-month.
	colStyle := make([]lipgloss.Style, plotW)
	monthIdx := make([]int, plotW)
	for x, d := range colDate {
		monthIdx[x] = matchMonth(d, months)
	}
	for x := 0; x < plotW; x++ {
		mi := monthIdx[x]
		switch {
		case mi <= 0 || mi >= len(months):
			colStyle[x] = styles.AccentS
		default:
			prev := months[mi-1].Total
			cur := months[mi].Total
			switch {
			case prev <= 0:
				colStyle[x] = styles.AccentS
			case cur > prev*1.10:
				colStyle[x] = styles.Bad
			case cur > prev*1.02:
				colStyle[x] = styles.WarnS
			case cur < prev*0.90:
				colStyle[x] = styles.Good
			case cur < prev*0.98:
				colStyle[x] = styles.Good
			default:
				colStyle[x] = styles.AccentS
			}
		}
	}

	// Render the grid.
	rows := make([][]string, plotH)
	for r := 0; r < plotH; r++ {
		rows[r] = make([]string, plotW)
		for c := 0; c < plotW; c++ {
			rows[r][c] = " "
		}
	}
	for x := 0; x < plotW; x++ {
		r := rowFor[x]
		if r < 0 {
			r = 0
		}
		if r >= plotH {
			r = plotH - 1
		}
		rows[r][x] = colStyle[x].Render(":")
	}

	// Y-axis tick labels — 5 evenly-spaced values.
	sym := currencySym(currency)
	labelFor := func(row int) string {
		frac := 1 - float64(row)/float64(plotH-1)
		val := mn + frac*(mx-mn)
		return fmt.Sprintf("%s%s", sym, compactAmount(val))
	}
	var lines []string
	for r := 0; r < plotH; r++ {
		showLabel := r == 0 || r == plotH-1 || r == plotH/2 || r == plotH/4 || r == 3*plotH/4
		gutter := strings.Repeat(" ", gutterW)
		if showLabel {
			lbl := labelFor(r)
			if len(lbl) > gutterW-1 {
				lbl = lbl[:gutterW-1]
			}
			gutter = fmt.Sprintf("%*s ", gutterW-1, lbl)
			gutter = styles.Help.Render(gutter)
		}
		axis := styles.Help.Render("│")
		lines = append(lines, gutter+axis+strings.Join(rows[r], ""))
	}

	// X-axis: plain baseline + tick labels. The label choice depends on
	// the series granularity — short windows need day-of-month labels,
	// long ones read better with month names or years.
	gutter := strings.Repeat(" ", gutterW)
	baseline := gutter + styles.Help.Render("└"+strings.Repeat("─", plotW))
	lines = append(lines, baseline)
	lines = append(lines, gutter+" "+renderXAxis(colDate, monthIdx, months, bucket, plotW))

	return strings.Join(lines, "\n")
}

// renderXAxis lays out X-axis labels suited to the series bucket. For
// monthly series (1Y preset) every column is its own month so we place
// a short month name at each column boundary. For short daily series
// (1W / 1M) we label the first column of each day. For longer daily
// series we fall back to month names at month boundaries.
func renderXAxis(colDate []time.Time, monthIdx []int, months []provider.CostMonth, bucket provider.CostBucket, width int) string {
	if bucket == provider.BucketMonth {
		return renderXMonthly(colDate, width)
	}
	// Daily buckets. When the overall span is short enough (~ <= 31 days
	// between first and last date) label individual days; otherwise fall
	// back to month boundaries.
	if isShortRange(colDate) {
		return renderXDaily(colDate, width)
	}
	return renderMonthAxis(monthIdx, months, width)
}

func isShortRange(dates []time.Time) bool {
	if len(dates) < 2 {
		return true
	}
	first, last := dates[0], dates[len(dates)-1]
	return last.Sub(first) <= 32*24*time.Hour
}

// renderXMonthly labels each column with its month abbreviation. Labels
// overlap so we only write when there's clear room — exactly like the
// daily path.
func renderXMonthly(colDate []time.Time, width int) string {
	row := make([]rune, width)
	for i := range row {
		row[i] = ' '
	}
	lastLabel := ""
	for x := 0; x < width; x++ {
		if colDate[x].IsZero() {
			continue
		}
		lbl := fmt.Sprintf("%s %d", shortMonth(colDate[x].Month()), colDate[x].Year()%100)
		if lbl == lastLabel {
			continue
		}
		if x+len(lbl) >= width {
			continue
		}
		collision := false
		for k := x; k < x+len(lbl); k++ {
			if row[k] != ' ' {
				collision = true
				break
			}
		}
		if collision {
			continue
		}
		for k, r := range lbl {
			row[x+k] = r
		}
		lastLabel = lbl
	}
	return styles.Help.Render(string(row))
}

// renderXDaily labels days-of-month on a short daily window. We pick a
// stride that fits the width — labelling every day on a 7-day window,
// every 3-5 days on a 30-day window.
func renderXDaily(colDate []time.Time, width int) string {
	row := make([]rune, width)
	for i := range row {
		row[i] = ' '
	}
	// Stride tuned so the labels don't collide. Each label is ~6 chars
	// ("Mon 15") so we need ~8 columns of spacing.
	stride := width / 8
	if stride < 1 {
		stride = 1
	}
	for x := 0; x < width; x += stride {
		if x >= len(colDate) || colDate[x].IsZero() {
			continue
		}
		lbl := fmt.Sprintf("%s %d", colDate[x].Format("Mon"), colDate[x].Day())
		if x+len(lbl) >= width {
			continue
		}
		for k, r := range lbl {
			if x+k >= len(row) {
				break
			}
			row[x+k] = r
		}
	}
	return styles.Help.Render(string(row))
}

// renderMonthAxis lays month labels along the X axis at the first
// column where each month appears. Labels are elided (empty spaces
// kept) when they'd overlap, so the axis reads cleanly even on narrow
// terminals.
func renderMonthAxis(monthIdx []int, months []provider.CostMonth, width int) string {
	row := make([]rune, width)
	for i := range row {
		row[i] = ' '
	}
	lastMonth := -2
	for x := 0; x < width; x++ {
		mi := monthIdx[x]
		if mi < 0 || mi >= len(months) {
			continue
		}
		if mi == lastMonth {
			continue
		}
		lastMonth = mi
		label := fmt.Sprintf("%s %d", shortMonth(months[mi].Month), months[mi].Year%100)
		// Skip if there's not enough room before the next month begins.
		if x+len(label) >= width {
			continue
		}
		// Skip if we'd collide with a previously-placed label.
		collision := false
		for k := x; k < x+len(label); k++ {
			if row[k] != ' ' {
				collision = true
				break
			}
		}
		if collision {
			continue
		}
		for k, r := range label {
			row[x+k] = r
		}
	}
	return styles.Help.Render(string(row))
}

// matchMonth returns the index of the month bucket containing d, or -1.
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
// labels and month-strip totals stay narrow: 1.3k for 1,300, 12.4k for
// 12,400, 1.1M for 1.1 million. For sub-thousand values we show the
// raw number with at most one decimal.
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

// currencySym returns the visual symbol for a currency code reusing the
// same mapping as the rest of the app. Empty input falls back to "$"
// so the chart never renders an unlabelled value.
func currencySym(code string) string {
	return currencyChar(code)
}

func currencyCode(code string) string {
	if code == "" {
		return currencyUSD
	}
	return strings.ToUpper(code)
}

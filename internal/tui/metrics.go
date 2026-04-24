package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

func (m *model) loadMetrics() tea.Cmd {
	met, ok := m.active.(provider.Metricser)
	if !ok {
		m.status = m.active.Name() + ": metrics overlay not wired yet for this cloud"
		return nil
	}
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visibleNodes) {
		return nil
	}
	cur := m.visibleNodes[c]
	if cur.Kind != provider.KindResource {
		m.status = "metrics needs a resource under the cursor"
		return nil
	}
	label := cur.Name
	if t := cur.Meta["type"]; t != "" {
		label += " · " + t
	}
	m.loading = true
	m.status = "loading metrics for " + cur.Name + "..."
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			data, err := met.Metrics(ctx, cur)
			if err != nil {
				return errMsg{err}
			}
			return metricsLoadedMsg{data: data, label: label}
		},
	)
}

// updateMetrics handles keys while the metrics overlay is visible.
// Read-only overlay — only dismiss.
func (m *model) updateMetrics(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc, "q", "M":
		m.metricsMode = false
		m.status = ""
		return m, nil
	}
	return m, nil
}

// metricsView renders each returned Metric as a Unicode-block sparkline
// with min / max / last labels so the reader can tell the shape of a
// series at a glance without needing a full chart.
func (m *model) metricsView() string {
	header := styles.Title.Render("metrics") + "  " + styles.Help.Render(m.metricsLabel)
	box := fullScreenBox(m.width, m.height)
	if len(m.metricsData) == 0 {
		return box.Render(strings.Join([]string{
			header,
			"",
			styles.Help.Render("  no default metrics for this resource type yet"),
			styles.Help.Render("  (cloudnav only whitelists a subset — VMs, App Service, SQL, Storage, AKS for now)"),
			"",
			styles.Help.Render("  esc/M close"),
		}, "\n"))
	}
	lines := []string{header, ""}
	for _, mm := range m.metricsData {
		mn, mx, last := seriesStats(mm.Points)
		unit := mm.Unit
		if unit == "" {
			unit = emDash
		}
		lines = append(lines, fmt.Sprintf("  %-30s  %s  min %8.2f  max %8.2f  last %8.2f %s",
			shorten(mm.Name, 30), sparkline(mm.Points), mn, mx, last, unit))
	}
	lines = append(lines, "", styles.Help.Render("  last 60 min · 5 min bins · esc/M close"))
	return box.Render(strings.Join(lines, "\n"))
}

// sparkline renders a series as a row of Unicode block runes. Zero-length
// or all-zero series render as a flat bottom line so the column stays
// aligned — better than collapsing to nothing and breaking the grid.
func sparkline(points []float64) string {
	if len(points) == 0 {
		return strings.Repeat("▁", 12)
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	mn, mx, _ := seriesStats(points)
	if mx == mn {
		return strings.Repeat(string(blocks[0]), len(points))
	}
	b := make([]rune, len(points))
	for i, v := range points {
		idx := int((v - mn) / (mx - mn) * float64(len(blocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		b[i] = blocks[idx]
	}
	return string(b)
}

// seriesStats returns (min, max, last) across a series. Min and max are
// set to 0 for empty slices so the caller can render without NaNs.
func seriesStats(points []float64) (float64, float64, float64) {
	if len(points) == 0 {
		return 0, 0, 0
	}
	mn, mx := points[0], points[0]
	for _, v := range points {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	return mn, mx, points[len(points)-1]
}

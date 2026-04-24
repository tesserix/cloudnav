package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

func (m *model) loadHealth() tea.Cmd {
	h, ok := m.active.(provider.HealthEventer)
	if !ok {
		m.status = m.active.Name() + ": service health not supported"
		return nil
	}
	m.loading = true
	m.status = "loading " + m.active.Name() + " service health..."
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			events, err := h.HealthEvents(ctx)
			if err != nil {
				return errMsg{err}
			}
			return healthLoadedMsg{events: events}
		},
	)
}
func (m *model) updateHealth(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc, "q", "H":
		m.healthMode = false
		m.status = ""
		return m, nil
	case keyUp, "k":
		if m.healthIdx > 0 {
			m.healthIdx--
		}
		return m, nil
	case keyDown, "j":
		if m.healthIdx < len(m.healthEvents)-1 {
			m.healthIdx++
		}
		return m, nil
	}
	return m, nil
}

// healthView renders the Service Health overlay — active incidents /
// planned maintenance / advisories across the caller's subscriptions.
// When nothing's active we show a clean "all clear" state so the user
// knows the lookup succeeded (rather than being confused about an empty
// pane).
func (m *model) healthView() string {
	header := styles.Title.Render("service health") + "  " +
		styles.Help.Render(fmt.Sprintf("%d active event(s)", len(m.healthEvents)))
	box := fullScreenBox(m.width, m.height)
	if len(m.healthEvents) == 0 {
		return box.Render(strings.Join([]string{
			header,
			"",
			styles.Good.Render("  🟢 all clear — no active incidents, maintenance, or advisories"),
			"",
			styles.Help.Render("  esc/H close"),
		}, "\n"))
	}

	window := 14
	if m.height > 12 {
		window = m.height - 12
	}
	if window < 5 {
		window = 5
	}
	start := 0
	if m.healthIdx >= window {
		start = m.healthIdx - window + 1
	}
	end := start + window
	if end > len(m.healthEvents) {
		end = len(m.healthEvents)
	}

	lines := []string{header, ""}
	if start > 0 {
		lines = append(lines, styles.Help.Render(fmt.Sprintf("  ↑ %d more above", start)))
	}
	for i := start; i < end; i++ {
		e := m.healthEvents[i]
		badge := healthEventBadge(e.Level)
		region := e.Region
		if region == "" {
			region = emDash
		}
		service := e.Service
		if service == "" {
			service = emDash
		}
		title := shorten(e.Title, 60)
		line := fmt.Sprintf("%s  %-22s  %-18s  %s", badge, shorten(service, 22), shorten(region, 18), title)
		if i == m.healthIdx {
			line = styles.Selected.Render("> " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	if end < len(m.healthEvents) {
		lines = append(lines, styles.Help.Render(fmt.Sprintf("  ↓ %d more below", len(m.healthEvents)-end)))
	}

	if m.healthIdx >= 0 && m.healthIdx < len(m.healthEvents) {
		e := m.healthEvents[m.healthIdx]
		lines = append(lines, "",
			styles.Header.Render("Details"),
			"Level:   "+healthEventBadge(e.Level),
			"Status:  "+e.Status,
			"Service: "+nonemptyOrDash(e.Service),
			"Region:  "+nonemptyOrDash(e.Region),
			"Scope:   "+nonemptyOrDash(e.Scope),
			"Since:   "+shortDate(e.StartTime),
			"Title:   "+e.Title,
		)
		if e.Summary != "" {
			lines = append(lines, "Summary: "+shorten(e.Summary, 120))
		}
	}
	lines = append(lines, "", styles.Help.Render("  ↑↓/jk move   esc/H close"))
	return box.Render(strings.Join(lines, "\n"))
}

// healthEventBadge colours a HealthEvent.Level for the overlay. Incidents
// get the red treatment, maintenance goes yellow, advisories blue-ish,
// anything else falls through to the muted style so we don't clobber a
// future level value.
func healthEventBadge(level string) string {
	switch level {
	case "incident":
		return styles.Bad.Render("🔴 incident")
	case "maintenance":
		return styles.WarnS.Render("🟡 maintenance")
	case "advisory":
		return styles.AccentS.Render("🔵 advisory")
	case "security":
		return styles.Bad.Render("🛡  security")
	default:
		return styles.Help.Render("• " + level)
	}
}

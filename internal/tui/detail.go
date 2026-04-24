package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
)

func (m *model) loadDetail() tea.Cmd {
	if m.active == nil {
		return nil
	}
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visibleNodes) {
		return nil
	}
	cur := m.visibleNodes[c]
	if cur.Kind == provider.KindCloud || cur.Kind == provider.KindCloudDisabled {
		return nil
	}
	m.loading = true
	m.status = "loading " + cur.Name + "..."
	prov := m.active
	ctx := m.ctx
	// Formatted summary built up-front from what the TUI already knows
	// (tags / health / location / cost / created time). Paired with the
	// raw provider JSON below the separator so anyone who scrolls past
	// the summary still gets the full payload — additive only.
	summary := formatDetailSummary(cur)
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			data, err := prov.Details(ctx, cur)
			if err != nil {
				return errMsg{err}
			}
			body := summary + "\n" + strings.Repeat("─", 60) + "\n" + string(data)
			return detailLoadedMsg{title: cur.Name, body: body}
		},
	)
}

// formatDetailSummary turns the cached Meta / Cost / state on a Node
// into a readable header for the detail overlay. The output is plain
// text (no ANSI) — the viewport renders it inside the styles.Box frame
// already, and styling it mid-body would fight with the raw-JSON
// section that follows.
func formatDetailSummary(n provider.Node) string {
	rows := []string{"  " + n.Name, ""}
	addRow := func(label, value string) {
		if value == "" {
			return
		}
		rows = append(rows, fmt.Sprintf("  %-12s %s", label+":", value))
	}
	addRow("Kind", string(n.Kind))
	addRow("ID", n.ID)
	addRow("Location", n.Location)
	addRow("State", n.State)
	// shortDate returns an em-dash for empty or malformed dates, so
	// only pass it through when the raw value is non-empty — otherwise
	// we'd get a "Created: —" row for resources that don't have a
	// creation timestamp (regions, accounts).
	if n.Meta["createdTime"] != "" {
		addRow("Created", shortDate(n.Meta["createdTime"]))
	}
	if n.Meta["changedTime"] != "" {
		addRow("Changed", shortDate(n.Meta["changedTime"]))
	}
	addRow("Type", n.Meta["type"])
	addRow("Sub", n.Meta["subscriptionId"])
	addRow("Tenant", n.Meta["tenantName"])
	addRow("Project", n.Meta["project"])
	addRow("Account", n.Meta["accountId"])
	addRow("Region", n.Meta["region"])
	addRow("Tags", n.Meta["tags"])
	addRow("Health", n.Meta["health"])
	addRow("Cost (MTD)", n.Cost)
	return strings.Join(rows, "\n")
}

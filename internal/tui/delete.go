package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

func (m *model) promptDelete() {
	if !m.atRGLevel() {
		m.status = "D works on the resource-groups view"
		return
	}
	if _, ok := m.active.(*azure.Azure); !ok {
		m.status = "delete is Azure-only"
		return
	}
	targets := []provider.Node{}
	for _, n := range m.visibleNodes {
		if m.selected[n.ID] {
			targets = append(targets, n)
		}
	}
	if len(targets) == 0 {
		m.status = "nothing selected — use space to select rows, [ to select all, D to delete"
		return
	}
	for _, t := range targets {
		if lv := m.rgLockLevel(t.Name); lv != "" {
			m.status = fmt.Sprintf("refused — %s has a %s lock; press L to remove it first", t.Name, lv)
			return
		}
	}
	m.deleteMode = true
	m.deleteTargets = targets
	m.deleteInput.SetValue("")
	m.deleteInput.Focus()
	m.status = ""
}

// executeDelete fires the actual async deletion after the user has typed the
// confirmation word. Runs one request per target and reports a single summary
// message — Azure handles the multi-hour async teardown.
func (m *model) executeDelete() tea.Cmd {
	az, ok := m.active.(*azure.Azure)
	if !ok || len(m.deleteTargets) == 0 {
		m.deleteMode = false
		return nil
	}
	targets := m.deleteTargets
	subID := m.currentSubID()
	ctx := m.ctx
	m.deleteMode = false
	m.deleteInput.Blur()
	m.deleteTargets = nil
	m.selected = map[string]bool{}
	m.loading = true
	m.status = fmt.Sprintf("deleting %d resource group(s) asynchronously...", len(targets))
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			fails := 0
			for _, t := range targets {
				if err := az.DeleteResourceGroup(ctx, subID, t.Name); err != nil {
					fails++
				}
			}
			if fails > 0 {
				return deletedMsg{
					msg: fmt.Sprintf("%d of %d deletions failed", fails, len(targets)),
					err: fmt.Errorf("%d failures", fails),
				}
			}
			return deletedMsg{msg: fmt.Sprintf("requested deletion of %d RG(s) — Azure is processing", len(targets))}
		},
	)
}

// updateDeleteConfirm handles keys inside the delete confirmation overlay.
// Enter with "DELETE" typed fires; anything else (including esc or Enter with
// a wrong word) cancels. Matching is case-insensitive for user comfort but
// an empty input never proceeds — that's the safety floor.
func (m *model) updateDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		m.deleteMode = false
		m.deleteTargets = nil
		m.deleteInput.Blur()
		m.status = "deletion cancelled"
		return m, nil
	case keyEnter:
		if strings.EqualFold(strings.TrimSpace(m.deleteInput.Value()), "DELETE") {
			return m, m.executeDelete()
		}
		m.deleteMode = false
		m.deleteTargets = nil
		m.deleteInput.Blur()
		m.status = "deletion cancelled — confirmation word did not match 'DELETE'"
		return m, nil
	}
	var cmd tea.Cmd
	m.deleteInput, cmd = m.deleteInput.Update(msg)
	return m, cmd
}

// deleteConfirmView renders the destructive-action modal. The disclaimer is
// blunt on purpose: the user is about to tell Azure to tear down resource
// groups that may hold production data, and cloudnav does not have the state
// or authority to undo that. Making the wording visible makes it harder to
// wave away.
func (m *model) deleteConfirmView() string {
	lines := []string{
		styles.Bad.Render("⚠  DELETE RESOURCE GROUPS"),
		"",
		styles.Header.Render("This will permanently delete:"),
	}
	for i, t := range m.deleteTargets {
		lines = append(lines, fmt.Sprintf("  %2d. %s   %s", i+1, t.Name, styles.Help.Render(t.Location)))
	}
	lines = append(lines,
		"",
		styles.Bad.Render("Everything inside each resource group — VMs, databases, storage,"),
		styles.Bad.Render("keys, backups — goes with it. The operation cannot be undone."),
		"",
		m.deleteInput.View(),
		"",
		styles.ModalHint.Render("enter  proceed     esc  cancel"),
	)
	return m.overlay(strings.Join(lines, "\n"))
}

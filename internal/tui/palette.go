package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/config"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

func (m *model) openPalette() tea.Cmd {
	m.paletteMode = true
	m.paletteInput.SetValue("")
	m.paletteInput.Focus()
	m.paletteIdx = 0
	m.rebuildPalette()
	return m.preloadEntities()
}

func (m *model) preloadEntities() tea.Cmd {
	cmds := []tea.Cmd{}
	for _, p := range m.providers {
		if _, ok := m.entities[p.Name()]; ok {
			continue
		}
		prov := p
		ctx := m.ctx
		cmds = append(cmds, func() tea.Msg {
			nodes, err := prov.Root(ctx)
			if err != nil {
				return entitiesLoadedMsg{provider: prov.Name(), nodes: nil}
			}
			return entitiesLoadedMsg{provider: prov.Name(), nodes: nodes}
		})
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *model) rebuildPalette() {
	q := strings.ToLower(m.paletteInput.Value())
	all := make([]paletteItem, 0, 32)

	for _, p := range m.providers {
		all = append(all, paletteItem{
			label:  "☁  switch to " + p.Name(),
			action: "switch-cloud",
			arg:    p.Name(),
		})
	}
	for _, bm := range m.cfg.Bookmarks {
		all = append(all, paletteItem{
			label:  "★ " + bm.Label,
			action: "open-bookmark",
			arg:    bm.Label,
		})
	}
	for _, p := range m.providers {
		for _, n := range m.entities[p.Name()] {
			all = append(all, paletteItem{
				label:    "▸ " + p.Name() + "  " + n.Name + "  " + shortID(n.ID),
				action:   "jump-entity",
				provider: p.Name(),
				node:     n,
			})
		}
	}

	if q == "" {
		m.paletteItems = all
	} else {
		filtered := make([]paletteItem, 0, len(all))
		for _, it := range all {
			if containsFold(it.label, q) || containsFold(it.arg, q) || containsFold(it.node.ID, q) {
				filtered = append(filtered, it)
			}
		}
		m.paletteItems = filtered
	}
	if m.paletteIdx >= len(m.paletteItems) {
		m.paletteIdx = 0
	}
}

func containsFold(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), needle)
}

func (m *model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		m.paletteMode = false
		m.paletteInput.Blur()
		return m, nil
	case keyUp:
		if m.paletteIdx > 0 {
			m.paletteIdx--
		}
		return m, nil
	case keyDown:
		if m.paletteIdx < len(m.paletteItems)-1 {
			m.paletteIdx++
		}
		return m, nil
	case keyEnter:
		if m.paletteIdx < len(m.paletteItems) {
			cmd := m.runPaletteItem(m.paletteItems[m.paletteIdx])
			m.paletteMode = false
			m.paletteInput.Blur()
			return m, cmd
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	m.rebuildPalette()
	return m, cmd
}

func (m *model) runPaletteItem(it paletteItem) tea.Cmd {
	switch it.action {
	case "switch-cloud":
		for _, p := range m.providers {
			if p.Name() == it.arg {
				m.active = p
				m.resetView()
				m.stack = m.stack[:1]
				return m.load(p.Name(), nil)
			}
		}
	case "open-bookmark":
		for _, bm := range m.cfg.Bookmarks {
			if bm.Label == it.arg {
				return m.openBookmark(bm)
			}
		}
	case "jump-entity":
		for _, p := range m.providers {
			if p.Name() != it.provider {
				continue
			}
			m.active = p
			m.resetView()
			m.stack = m.stack[:1]
			m.restorePath = []config.Crumb{{
				Kind: string(it.node.Kind),
				ID:   it.node.ID,
				Name: it.node.Name,
			}}
			m.restoreLabel = p.Name() + " / " + it.node.Name
			m.status = "jumping to " + m.restoreLabel + "..."
			return m.load(p.Name(), nil)
		}
	}
	return nil
}

func (m *model) openBookmark(bm config.Bookmark) tea.Cmd {
	for _, p := range m.providers {
		if p.Name() == bm.Provider {
			m.active = p
			m.resetView()
			m.stack = m.stack[:1]
			// Skip the first crumb — it's the cloud level we just set as active.
			if len(bm.Path) > 1 {
				m.restorePath = append(m.restorePath[:0], bm.Path[1:]...)
				m.restoreLabel = bm.Label
				m.status = "restoring ★ " + bm.Label + "..."
			} else {
				m.restorePath = nil
				m.restoreLabel = ""
				m.status = "★ " + bm.Label
			}
			return m.load(p.Name(), nil)
		}
	}
	m.status = "bookmark refers to unavailable provider " + bm.Provider
	return nil
}

// advanceRestore drills one level deeper along m.restorePath, if any.
func (m *model) advanceRestore() tea.Cmd {
	if len(m.restorePath) == 0 {
		if m.restoreLabel != "" {
			m.status = "★ " + m.restoreLabel
			m.restoreLabel = ""
		}
		return nil
	}
	next := m.restorePath[0]
	for i, n := range m.visibleNodes {
		if (next.ID != "" && n.ID == next.ID) || (next.ID == "" && n.Name == next.Name) {
			m.table.SetCursor(i)
			m.restorePath = m.restorePath[1:]
			return m.drillDown()
		}
	}
	m.status = fmt.Sprintf("restore stopped at %q (not found)", next.Name)
	m.restorePath = nil
	m.restoreLabel = ""
	return nil
}

func (m *model) paletteView() string {
	window := 10
	if m.height > 14 {
		window = m.height - 12
	}
	if window < 4 {
		window = 4
	}
	start := 0
	if len(m.paletteItems) > window {
		start = m.paletteIdx - window/2
		if start < 0 {
			start = 0
		}
		if start+window > len(m.paletteItems) {
			start = len(m.paletteItems) - window
		}
	}
	end := start + window
	if end > len(m.paletteItems) {
		end = len(m.paletteItems)
	}

	counter := fmt.Sprintf("%d items", len(m.paletteItems))
	if len(m.paletteItems) > window {
		counter = fmt.Sprintf("%d–%d of %d", start+1, end, len(m.paletteItems))
	}
	lines := []string{
		styles.Title.Render("palette") + "  " + styles.Help.Render(counter),
		"",
		m.paletteInput.View(),
		"",
	}
	if start > 0 {
		lines = append(lines, styles.Help.Render("  ↑ "+fmt.Sprintf("%d more above", start)))
	}
	for i := start; i < end; i++ {
		it := m.paletteItems[i]
		line := "  " + it.label
		if i == m.paletteIdx {
			line = styles.Selected.Render("> " + it.label)
		}
		lines = append(lines, line)
	}
	if end < len(m.paletteItems) {
		lines = append(lines, styles.Help.Render("  ↓ "+fmt.Sprintf("%d more below", len(m.paletteItems)-end)))
	}
	if len(m.paletteItems) == 0 {
		lines = append(lines, styles.Help.Render("  no matches"))
	}
	lines = append(lines,
		"",
		styles.ModalHint.Render("↑↓ nav  ↵ select  esc close  (type to filter)"),
	)
	return m.overlay(strings.Join(lines, "\n"))
}

package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		m.searchMode = false
		m.search.Blur()
		m.filter = ""
		m.search.SetValue("")
		m.refreshTable()
		return m, nil
	case keyEnter:
		m.searchMode = false
		m.search.Blur()
		return m, nil
	case keyUp, keyDown, "pgup", "pgdown":
		// Swallow list-navigation keys while typing a filter: moving the
		// table cursor mid-search makes it unclear which row will be acted
		// on when the filter is committed. Esc clears, Enter commits.
		return m, nil
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.filter = m.search.Value()
	m.refreshTable()
	return m, cmd
}

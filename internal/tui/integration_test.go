package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tesserix/cloudnav/internal/tui/components"
)

// Integration-style tests that drive the Bubble Tea model via its own
// Update / View methods. They don't need network access or an /os/exec
// cloud CLI — every test asserts pure TUI state transitions.

// sendKey pushes a tea.KeyMsg built from a rune or named key into Update
// and returns the updated model plus any returned command.
func sendKey(t *testing.T, m *model, key string) *model {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	updated, _ := m.Update(msg)
	return updated.(*model)
}

func TestShellFillsTerminal(t *testing.T) {
	// Shell must produce exactly height lines regardless of body size.
	out := components.Shell(80, 24, "HDR\nbar", "X", "F")
	h := lipgloss.Height(out)
	if h != 24 {
		t.Errorf("Shell height = %d, want 24", h)
	}
}

func TestShellHandlesZeroDims(t *testing.T) {
	out := components.Shell(0, 0, "H", "B", "F")
	if !strings.Contains(out, "H") || !strings.Contains(out, "B") || !strings.Contains(out, "F") {
		t.Errorf("Shell should degrade gracefully at zero dims, got %q", out)
	}
}

func TestKeybarWrapsNarrow(t *testing.T) {
	parts := []string{"<a> one", "<b> two", "<c> three", "<d> four"}
	// Budget must be below total single-line width to force a wrap.
	// Parts joined with "  " separator ~37 cells, minus 2-cell indent.
	out := components.Keybar(30, parts)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Errorf("Keybar at width 30 should wrap to multiple lines, got 1: %q", out)
	}
}

func TestKeybarSinglesWide(t *testing.T) {
	parts := []string{"<a> one", "<b> two"}
	out := components.Keybar(200, parts)
	if strings.Count(out, "\n") != 0 {
		t.Errorf("Keybar at width 200 should fit on one line, got:\n%s", out)
	}
}

func TestBreadcrumbRendersSeparators(t *testing.T) {
	out := components.Breadcrumb("app", []string{"a", "b"})
	// › separator is in styles.CrumbSep
	if strings.Count(out, "›") < 2 {
		t.Errorf("Breadcrumb should have at least two › separators for trail len 2, got %q", out)
	}
}

func TestHelpOpenClose(t *testing.T) {
	m := newModel()
	m.width, m.height = 100, 30
	m.pushHome()
	// Press ?
	m = sendKey(t, m, "?")
	if !m.showHelp {
		t.Fatal("? should open help overlay")
	}
	view := m.View()
	if !strings.Contains(view, "keybindings") {
		t.Error("help view should mention keybindings")
	}
	// Any key closes help — match the Update() logic which resets on any msg.
	m = sendKey(t, m, "esc")
	if m.showHelp {
		t.Error("esc should close help overlay")
	}
}

func TestSearchModeOpenClose(t *testing.T) {
	m := newModel()
	m.width, m.height = 100, 30
	m.pushHome()
	m = sendKey(t, m, "/")
	if !m.searchMode {
		t.Fatal("/ should enter search mode")
	}
	m = sendKey(t, m, "esc")
	if m.searchMode {
		t.Error("esc should exit search mode")
	}
}

func TestPaletteOpenClose(t *testing.T) {
	m := newModel()
	m.width, m.height = 100, 30
	m.pushHome()
	m = sendKey(t, m, ":")
	if !m.paletteMode {
		t.Fatal(": should open palette")
	}
	m = sendKey(t, m, "esc")
	if m.paletteMode {
		t.Error("esc should close palette")
	}
}

func TestWindowResizeUpdatesChrome(t *testing.T) {
	m := newModel()
	m.pushHome()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(*model)
	if m.width != 120 || m.height != 40 {
		t.Errorf("WindowSizeMsg not applied: width=%d height=%d", m.width, m.height)
	}
	// View must render exactly height lines after shell wrap.
	view := m.View()
	if h := lipgloss.Height(view); h != 40 {
		t.Errorf("full view height = %d, want 40", h)
	}
}

func TestViewNeverPanicsAtSmallSizes(t *testing.T) {
	m := newModel()
	m.pushHome()
	for _, wh := range []struct{ w, h int }{
		{10, 5}, {40, 10}, {80, 24}, {200, 60},
	} {
		m.width, m.height = wh.w, wh.h
		_ = m.View() // just ensure no panic
	}
}

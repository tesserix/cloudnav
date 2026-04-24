// Package components hosts reusable TUI widgets: the layout shell,
// breadcrumb bar, keybind strip, and modal helpers.
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tesserix/cloudnav/internal/tui/styles"
)

// ansiReset is emitted between Shell sections so a styled background
// (e.g. a selected table row with Background(Purple)) doesn't bleed
// onto the pad lines that follow. Without this the terminal keeps
// applying the last style to every subsequent cell until it hits a
// reset, which shows up as a purple splash-screen below the list.
const ansiReset = "\x1b[0m"

// Shell renders header + body + footer so the output is exactly
// width × height cells. Body is padded or truncated to fit.
func Shell(width, height int, header, body, footer string) string {
	if width <= 0 || height <= 0 {
		return strings.Join([]string{header, body, footer}, "\n")
	}
	bH := height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bH < 1 {
		bH = 1
	}
	return header + ansiReset + "\n" + fitHeight(body, bH) + ansiReset + "\n" + footer
}

func fitHeight(content string, n int) string {
	lines := strings.Split(content, "\n")
	if len(lines) == n {
		return content
	}
	if len(lines) > n {
		return strings.Join(lines[:n], "\n")
	}
	// Each pad line starts with a reset so the previous row's styling
	// can't bleed past. Empty string alone wouldn't trigger that reset.
	pad := make([]string, n-len(lines))
	for i := range pad {
		pad[i] = ansiReset
	}
	return strings.Join(append(lines, pad...), "\n")
}

// FitWidth right-pads or truncates s to exactly width cells.
func FitWidth(s string, width int) string {
	w := lipgloss.Width(s)
	if w == width {
		return s
	}
	if w > width {
		if len(s) > width {
			return s[:width]
		}
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// TwoCol separates left and right by enough spaces to fill width.
func TwoCol(width int, left, right string) string {
	if width == 0 {
		return left + "   " + right
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// Breadcrumb joins the app title and trail segments with the themed
// separator.
func Breadcrumb(app string, trail []string) string {
	parts := make([]string, 0, 1+len(trail))
	parts = append(parts, styles.Title.Render(app))
	for _, t := range trail {
		parts = append(parts, styles.Crumb.Render(t))
	}
	return strings.Join(parts, styles.CrumbSep)
}

// KeyPair renders a "<key> action" hint.
func KeyPair(k, action string) string {
	return styles.Key.Render("<"+k+">") + " " + styles.Help.Render(action)
}

// Keybar packs pairs across as many lines as needed to fit width.
func Keybar(width int, parts []string) string {
	const indent = "  "
	const sep = "  "
	if width <= 20 {
		return indent + strings.Join(parts, sep)
	}
	budget := width - len(indent)
	widths := make([]int, len(parts))
	for i, p := range parts {
		widths[i] = lipgloss.Width(p)
	}
	var lines []string
	cur := make([]string, 0, len(parts))
	curW := 0
	for i, s := range parts {
		need := widths[i]
		if len(cur) > 0 {
			need += len(sep)
		}
		if len(cur) > 0 && curW+need > budget {
			lines = append(lines, indent+strings.Join(cur, sep))
			cur = cur[:0]
			curW = 0
			need = widths[i]
		}
		cur = append(cur, s)
		curW += need
	}
	if len(cur) > 0 {
		lines = append(lines, indent+strings.Join(cur, sep))
	}
	return strings.Join(lines, "\n")
}

// Modal centers a bordered popup inside a width × height rectangle.
func Modal(width, height int, body string) string {
	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		styles.Modal.Render(body),
	)
}

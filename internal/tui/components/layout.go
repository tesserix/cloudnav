// Package components hosts reusable TUI widgets: the layout shell, the
// breadcrumb bar, the keybind strip. Everything cloud-agnostic and
// free of model state.
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tesserix/cloudnav/internal/tui/styles"
)

// Shell renders a 3-band layout (header, body, footer) that exactly fills
// width × height. Body is padded or truncated vertically so the output is
// always `height` lines — no shorter, no taller. This is what gives the
// TUI its full-terminal feel.
//
// header and footer are rendered as-is (their own height is trusted). Body
// gets whatever vertical space is left.
func Shell(width, height int, header, body, footer string) string {
	if width <= 0 || height <= 0 {
		return strings.Join([]string{header, body, footer}, "\n")
	}
	hH := lipgloss.Height(header)
	fH := lipgloss.Height(footer)
	bH := height - hH - fH
	if bH < 1 {
		bH = 1
	}
	body = fitHeight(body, bH)
	return header + "\n" + body + "\n" + footer
}

// fitHeight pads with blank lines or truncates so the content is exactly
// n lines tall. Assumes content is already width-sized by the caller.
func fitHeight(content string, n int) string {
	lines := strings.Split(content, "\n")
	if len(lines) == n {
		return content
	}
	if len(lines) > n {
		return strings.Join(lines[:n], "\n")
	}
	pad := make([]string, n-len(lines))
	return strings.Join(append(lines, pad...), "\n")
}

// FitWidth right-pads (or truncates) s so it's exactly width cells wide,
// measured with lipgloss so ANSI doesn't throw off counts.
func FitWidth(s string, width int) string {
	w := lipgloss.Width(s)
	if w == width {
		return s
	}
	if w > width {
		// lipgloss doesn't have a clean truncate that respects ANSI, so
		// fall back to byte truncation — callers rarely hit this path.
		if len(s) > width {
			return s[:width]
		}
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// TwoCol renders left + right separated by enough spaces to fill width.
// When the terminal width is unknown (0) it falls back to a small gap.
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

// Breadcrumb joins path segments with the themed separator. First segment
// is the app title (cyan bold), remaining are muted.
func Breadcrumb(app string, trail []string) string {
	parts := []string{styles.Title.Render(app)}
	for _, t := range trail {
		parts = append(parts, styles.Crumb.Render(t))
	}
	return strings.Join(parts, styles.CrumbSep)
}

// KeyPair renders a single "<key> action" pair in the keybar format.
func KeyPair(k, action string) string {
	return styles.Key.Render("<"+k+">") + " " + styles.Help.Render(action)
}

// Keybar packs pairs onto as many lines as needed for width. Pairs are
// "<key> action" strings (typically from KeyPair). Returns a multi-line
// string with two-space indent and two-space separator.
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

// Modal centers a bordered popup inside a (width × height) rectangle.
// Caller is responsible for having already sized its body to the modal's
// inner width. Returns a rendered string exactly `width × height` cells.
func Modal(width, height int, body string) string {
	framed := styles.Modal.Render(body)
	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		framed,
	)
}

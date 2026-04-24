package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tesserix/cloudnav/internal/tui/styles"
)

// Composite places fg over bg at (x, y), returning the stitched string.
// Both bg and fg are multi-line ANSI-colored strings (output of other
// lipgloss renders). For each line that fg covers, the corresponding bg
// line is cut into [0,x) + fg + [x+fgWidth, bgWidth) so the overlay
// doesn't push content off-screen. ANSI escapes are preserved by using
// ansi.Cut — a naive byte-slice would tear color codes and corrupt the
// rendering.
//
// This gives the aztimator-style "table visible behind the modal" effect:
// the modal is drawn in its own z-layer, not by replacing the body.
func Composite(bg, fg string, x, y int) string {
	if bg == "" {
		return fg
	}
	if fg == "" {
		return bg
	}
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	for i, fgLine := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bgLine := bgLines[row]
		bgW := ansi.StringWidth(bgLine)
		fgW := ansi.StringWidth(fgLine)

		switch {
		case x <= 0 && fgW >= bgW:
			// fg fully covers the row
			bgLines[row] = fgLine
		case x <= 0:
			// fg starts at column 0, bg tail survives to the right
			bgLines[row] = fgLine + ansi.Cut(bgLine, fgW, bgW)
		case x+fgW >= bgW:
			// fg reaches past the right edge — keep only the left of bg
			bgLines[row] = ansi.Cut(bgLine, 0, x) + fgLine
		default:
			bgLines[row] = ansi.Cut(bgLine, 0, x) + fgLine + ansi.Cut(bgLine, x+fgW, bgW)
		}
	}
	return strings.Join(bgLines, "\n")
}

// CenterOverlay composites a modal at the center of a (width × height)
// background. Returns the merged view with the same line count and width
// as the background. Use this in place of components.Modal when you want
// the table to stay visible behind the popup.
func CenterOverlay(bgWidth, bgHeight int, bg, body string) string {
	framed := styles.Modal.Render(body)
	fgW := lipgloss.Width(framed)
	fgH := lipgloss.Height(framed)
	x := (bgWidth - fgW) / 2
	y := (bgHeight - fgH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return Composite(bg, framed, x, y)
}

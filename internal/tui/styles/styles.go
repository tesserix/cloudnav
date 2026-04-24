// Package styles centralises all lipgloss styling so the visual identity
// (colors, spacing, separators) stays consistent across pages.
//
// Rule of thumb: never call lipgloss.NewStyle() outside this file. If a new
// style is needed, add it here so the whole UI can be re-themed in one place.
package styles

import "github.com/charmbracelet/lipgloss"

// Palette. Keep the list short — every new color is a visual tax on the user.
var (
	Purple = lipgloss.Color("#7c3aed")
	Cyan   = lipgloss.Color("#22d3ee")
	Muted  = lipgloss.Color("#6b7280")
	Subtle = lipgloss.Color("#374151")
	Fg     = lipgloss.Color("#e5e7eb")
	White  = lipgloss.Color("#ffffff")
	Accent = lipgloss.Color("#a78bfa")
	Green  = lipgloss.Color("#22c55e")
	Warn   = lipgloss.Color("#f59e0b")
	Err    = lipgloss.Color("#ef4444")
)

// Inline text styles — cheap, build once at package init, reuse everywhere.
var (
	Title    = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
	Crumb    = lipgloss.NewStyle().Foreground(Muted)
	CrumbSep = lipgloss.NewStyle().Foreground(Subtle).Render(" › ")
	Key      = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
	Help     = lipgloss.NewStyle().Foreground(Muted)
	Header   = lipgloss.NewStyle().Bold(true).Foreground(Fg).Underline(true)
	Cost     = lipgloss.NewStyle().Foreground(Accent)
	Bad      = lipgloss.NewStyle().Foreground(Err)
	Good     = lipgloss.NewStyle().Foreground(Green)
	WarnS    = lipgloss.NewStyle().Foreground(Warn)
	AccentS  = lipgloss.NewStyle().Foreground(Accent)
	Spinner  = lipgloss.NewStyle().Foreground(Cyan)
	Loading  = lipgloss.NewStyle().Bold(true).Foreground(Cyan)

	// Prompt is the cyan "/ " / ": " lead-in used by all textinputs.
	Prompt = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
	// PromptErr is the red prompt used by destructive confirms (DELETE).
	PromptErr = lipgloss.NewStyle().Bold(true).Foreground(Err)
)

// Block styles — these own their own layout, don't compose them into text.
var (
	StatusBar = lipgloss.NewStyle().Background(Subtle).Foreground(Fg).Padding(0, 1)
	Box       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Subtle).Padding(1, 2)
	Selected  = lipgloss.NewStyle().Background(Purple).Foreground(White).Bold(true)

	// Modal is the centered popup container. Brighter border than Box so
	// it reads clearly as a z-ordered overlay over the table body.
	Modal      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Accent).Padding(1, 2)
	ModalTitle = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
	ModalLabel = lipgloss.NewStyle().Foreground(Muted)
	ModalValue = lipgloss.NewStyle().Foreground(Fg)
	ModalHint  = lipgloss.NewStyle().Foreground(Muted).Italic(true)

	// Tab renders category filter chips (resource type tabs). Active tab
	// uses the accent background, others stay muted.
	Tab       = lipgloss.NewStyle().Foreground(Muted).Padding(0, 1)
	TabActive = lipgloss.NewStyle().Bold(true).Foreground(White).Background(Purple).Padding(0, 1)
)

// Table styles — applied once to the bubbles/table via TableStyles().
func TableStyles() (header, selected, cell lipgloss.Style) {
	header = lipgloss.NewStyle().BorderStyle(lipgloss.Border{}).Bold(true).Foreground(Fg).Padding(0, 1)
	selected = lipgloss.NewStyle().Background(Purple).Foreground(White).Bold(true)
	cell = lipgloss.NewStyle().Padding(0, 1)
	return
}

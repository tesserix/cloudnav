// Package styles centralises all lipgloss styling so the visual identity
// (colors, spacing, separators) stays consistent across pages.
package styles

import "github.com/charmbracelet/lipgloss"

var (
	Purple = lipgloss.Color("#7c3aed")
	Cyan   = lipgloss.Color("#22d3ee")
	Muted  = lipgloss.Color("#6b7280")
	Subtle = lipgloss.Color("#374151")
	Fg     = lipgloss.Color("#e5e7eb")
	Accent = lipgloss.Color("#a78bfa")
	Green  = lipgloss.Color("#22c55e")
	Warn   = lipgloss.Color("#f59e0b")
	Err    = lipgloss.Color("#ef4444")
)

var (
	Title     = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
	Crumb     = lipgloss.NewStyle().Foreground(Muted)
	CrumbSep  = lipgloss.NewStyle().Foreground(Subtle).Render(" › ")
	Key       = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
	Help      = lipgloss.NewStyle().Foreground(Muted)
	Header    = lipgloss.NewStyle().Bold(true).Foreground(Fg).Underline(true)
	Cost      = lipgloss.NewStyle().Foreground(Accent)
	Bad       = lipgloss.NewStyle().Foreground(Err)
	Good      = lipgloss.NewStyle().Foreground(Green)
	StatusBar = lipgloss.NewStyle().Background(Subtle).Foreground(Fg).Padding(0, 1)
	Box       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Subtle).Padding(1, 2)
	Selected  = lipgloss.NewStyle().Background(Purple).Foreground(lipgloss.Color("#ffffff")).Bold(true)
)

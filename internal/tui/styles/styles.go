// Package styles centralises lipgloss styling so the visual identity
// stays consistent across pages. Never call lipgloss.NewStyle outside
// this file — add it here so the whole UI can be re-themed from one
// place.
//
// Palette uses ANSI-256 codes rather than hex. Two reasons:
//  1. ANSI colours shift with the user's terminal theme (light vs
//     dark mode), so cloudnav doesn't look wrong on a light theme.
//  2. They render correctly on 256-colour terminals without the
//     guessing game hex codes do through termenv's colour-profile
//     quantiser.
package styles

import "github.com/charmbracelet/lipgloss"

// Palette — dark-scheme by default, ANSI 256 so light themes get a
// reasonable automatic mapping.
var (
	// Chrome tones — shades of dark grey used as zone backgrounds to
	// separate header / body / footer visually.
	HeaderBg = lipgloss.Color("235") // breadcrumb + keybar strip
	HintBg   = lipgloss.Color("233") // status bar + hint line
	SelBg    = lipgloss.Color("57")  // selected / cursor row background
	SelFg    = lipgloss.Color("229") // selected / cursor row text

	// Text tones.
	Muted  = lipgloss.Color("240") // dividers, dim text
	Subtle = lipgloss.Color("245") // secondary body copy
	Fg     = lipgloss.Color("255") // primary body copy
	White  = lipgloss.Color("255")

	// Accents — used sparingly.
	Accent = lipgloss.Color("86")  // app title, active hints (teal)
	Purple = lipgloss.Color("63")  // popup borders, info accents
	Green  = lipgloss.Color("114") // good / up
	Warn   = lipgloss.Color("214") // update pill, rising cost
	Err    = lipgloss.Color("196") // destructive / denied
	Cyan   = lipgloss.Color("86")  // alias for Accent (back-compat)
)

// Inline text styles — cheap to build once at init, reused everywhere.
var (
	Title    = lipgloss.NewStyle().Bold(true).Foreground(Accent).Background(HeaderBg)
	Crumb    = lipgloss.NewStyle().Foreground(Fg).Background(HeaderBg)
	CrumbSep = lipgloss.NewStyle().Foreground(Muted).Background(HeaderBg).Render(" › ")
	Key      = lipgloss.NewStyle().Bold(true).Foreground(Accent).Background(HeaderBg)
	Help     = lipgloss.NewStyle().Foreground(Subtle).Background(HeaderBg)
	Header   = lipgloss.NewStyle().Bold(true).Foreground(Fg)
	Cost     = lipgloss.NewStyle().Foreground(Accent)
	Bad      = lipgloss.NewStyle().Foreground(Err)
	Good     = lipgloss.NewStyle().Foreground(Green)
	WarnS    = lipgloss.NewStyle().Foreground(Warn)
	AccentS  = lipgloss.NewStyle().Foreground(Accent)
	Spinner  = lipgloss.NewStyle().Foreground(Accent)
	Loading  = lipgloss.NewStyle().Bold(true).Foreground(Accent)

	// Prompt / PromptErr lead the textinputs.
	Prompt    = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	PromptErr = lipgloss.NewStyle().Bold(true).Foreground(Err)
)

// Block styles — each owns its layout, don't compose them into text.
var (
	// StatusBar is the thin strip at the bottom of the screen. Distinct
	// background so it reads as a separate zone from the body.
	StatusBar = lipgloss.NewStyle().Background(HintBg).Foreground(Subtle).Padding(0, 1)

	// HeaderBar is the breadcrumb + keybar band. Same bg as the Crumb /
	// Key styles so every element inside reads as one zone.
	HeaderBar = lipgloss.NewStyle().Background(HeaderBg).Padding(0, 1)

	// Box is the default bordered container (shared by full-screen
	// views like advisor / pim that need a framed canvas).
	Box = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Muted).Padding(1, 2)

	// Selected is the cursor-row highlight. Muted dark violet with
	// cream text — deliberately less loud than the old bright purple.
	Selected = lipgloss.NewStyle().Background(SelBg).Foreground(SelFg).Bold(true)

	// Modal is the centered popup container. Purple border so the
	// overlay reads as elevated over the table body.
	Modal      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Purple).Padding(1, 2)
	ModalTitle = lipgloss.NewStyle().Bold(true).Foreground(Purple)
	ModalLabel = lipgloss.NewStyle().Foreground(Muted)
	ModalValue = lipgloss.NewStyle().Foreground(Fg)
	ModalHint  = lipgloss.NewStyle().Foreground(Muted).Italic(true)

	// Tab / TabActive render category filter chips.
	Tab       = lipgloss.NewStyle().Foreground(Subtle).Padding(0, 1)
	TabActive = lipgloss.NewStyle().Bold(true).Foreground(SelFg).Background(SelBg).Padding(0, 1)
)

// TableStyles is what the bubbles/table Model uses for header / row /
// cursor rendering. Returned as a struct so callers can apply in one
// call: ts.Header, ts.Selected, ts.Cell = styles.TableStyles().
func TableStyles() (header, selected, cell lipgloss.Style) {
	header = lipgloss.NewStyle().BorderStyle(lipgloss.Border{}).Bold(true).Foreground(Subtle).Padding(0, 1)
	selected = lipgloss.NewStyle().Background(SelBg).Foreground(SelFg).Bold(true)
	cell = lipgloss.NewStyle().Padding(0, 1)
	return
}

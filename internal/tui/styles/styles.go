// Package styles centralises lipgloss styling so the visual identity
// stays consistent across pages. Never call lipgloss.NewStyle outside
// this file — add it here so the whole UI can be re-themed from one
// place.
//
// The exported Color and Style vars are mutable: Apply(t UITheme)
// rebuilds every value so a runtime palette pick (`:dracula`,
// `:nord`, etc.) flips the chrome without restarting cloudnav.
// Callers must read these vars on every render — caching the value
// from package init would freeze the pre-Apply default theme.
//
// Per-cloud terminal accents (GCP / AWS / Azure brand palettes) live
// in themes.go and stay independent from this UI theme since they
// encode information ("which cloud am I in") rather than aesthetics.
package styles

import "github.com/charmbracelet/lipgloss"

// Palette colours — populated by Apply(). Defaults match the original
// dark-scheme ANSI 256 palette so cloudnav looks identical pre-Apply.
var (
	HeaderBg lipgloss.Color
	HintBg   lipgloss.Color
	SelBg    lipgloss.Color
	SelFg    lipgloss.Color

	Muted  lipgloss.Color
	Subtle lipgloss.Color
	Fg     lipgloss.Color
	White  lipgloss.Color

	Accent lipgloss.Color
	Purple lipgloss.Color
	Green  lipgloss.Color
	Warn   lipgloss.Color
	Err    lipgloss.Color
	Cyan   lipgloss.Color
)

// Inline text styles — rebuilt by Apply() so a theme switch propagates
// to every rendered surface on the next View() pass.
var (
	Title    lipgloss.Style
	Crumb    lipgloss.Style
	CrumbSep string
	Key      lipgloss.Style
	Help     lipgloss.Style
	Header   lipgloss.Style
	Cost     lipgloss.Style
	Bad      lipgloss.Style
	Good     lipgloss.Style
	WarnS    lipgloss.Style
	AccentS  lipgloss.Style
	Spinner  lipgloss.Style
	Loading  lipgloss.Style

	Prompt    lipgloss.Style
	PromptErr lipgloss.Style
)

// Block styles — each owns its layout, don't compose them into text.
var (
	StatusBar  lipgloss.Style
	HeaderBar  lipgloss.Style
	Box        lipgloss.Style
	Selected   lipgloss.Style
	Modal      lipgloss.Style
	ModalTitle lipgloss.Style
	ModalLabel lipgloss.Style
	ModalValue lipgloss.Style
	ModalHint  lipgloss.Style
	Tab        lipgloss.Style
	TabActive  lipgloss.Style
)

// active is the currently-applied UI theme. Public read access via
// Active(); writes go through Apply() so derived Styles stay in sync.
var active UITheme

// init applies the default theme so every package-level Style var has
// a usable value before main() runs. Keeps tests + early bootstrap
// code working identically to the pre-theming version of this file.
func init() {
	Apply(UIDefault)
}

// Active returns the currently-applied theme.
func Active() UITheme { return active }

// Apply reassigns every package-level Color and Style var to match
// theme t. Safe to call any number of times — the bubbletea render
// loop reads these vars on every View(), so the next paint will
// reflect the new palette. Not thread-safe; callers should drive
// this from the bubbletea Update goroutine.
func Apply(t UITheme) {
	active = t

	HeaderBg = t.HeaderBg
	HintBg = t.HintBg
	SelBg = t.SelBg
	SelFg = t.SelFg

	Muted = t.Muted
	Subtle = t.Subtle
	Fg = t.Fg
	White = t.Fg // alias kept for back-compat with old call sites

	Accent = t.Accent
	Purple = t.Purple
	Green = t.Green
	Warn = t.Warn
	Err = t.Err
	Cyan = t.Accent // alias kept for back-compat

	Title = lipgloss.NewStyle().Bold(true).Foreground(Accent).Background(HeaderBg)
	Crumb = lipgloss.NewStyle().Foreground(Fg).Background(HeaderBg)
	CrumbSep = lipgloss.NewStyle().Foreground(Muted).Background(HeaderBg).Render(" › ")
	Key = lipgloss.NewStyle().Bold(true).Foreground(Accent).Background(HeaderBg)
	Help = lipgloss.NewStyle().Foreground(Subtle).Background(HeaderBg)
	Header = lipgloss.NewStyle().Bold(true).Foreground(Fg)
	Cost = lipgloss.NewStyle().Foreground(Accent)
	Bad = lipgloss.NewStyle().Foreground(Err)
	Good = lipgloss.NewStyle().Foreground(Green)
	WarnS = lipgloss.NewStyle().Foreground(Warn)
	AccentS = lipgloss.NewStyle().Foreground(Accent)
	Spinner = lipgloss.NewStyle().Foreground(Accent)
	Loading = lipgloss.NewStyle().Bold(true).Foreground(Accent)

	Prompt = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	PromptErr = lipgloss.NewStyle().Bold(true).Foreground(Err)

	StatusBar = lipgloss.NewStyle().Background(HintBg).Foreground(Subtle).Padding(0, 1)
	HeaderBar = lipgloss.NewStyle().Background(HeaderBg).Padding(0, 1)
	Box = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Muted).Padding(1, 2)
	Selected = lipgloss.NewStyle().Background(SelBg).Foreground(SelFg).Bold(true)
	Modal = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Purple).Padding(1, 2)
	ModalTitle = lipgloss.NewStyle().Bold(true).Foreground(Purple)
	ModalLabel = lipgloss.NewStyle().Foreground(Muted)
	ModalValue = lipgloss.NewStyle().Foreground(Fg)
	ModalHint = lipgloss.NewStyle().Foreground(Muted).Italic(true)
	Tab = lipgloss.NewStyle().Foreground(Subtle).Padding(0, 1)
	TabActive = lipgloss.NewStyle().Bold(true).Foreground(SelFg).Background(SelBg).Padding(0, 1)
}

// TableStyles is what the bubbles/table Model uses for header / row /
// cursor rendering. Built fresh on every call so a theme switch
// applies to tables without an explicit re-style step from the model.
func TableStyles() (header, selected, cell lipgloss.Style) {
	header = lipgloss.NewStyle().BorderStyle(lipgloss.Border{}).Bold(true).Foreground(Subtle).Padding(0, 1)
	selected = lipgloss.NewStyle().Background(SelBg).Foreground(SelFg).Bold(true)
	cell = lipgloss.NewStyle().Padding(0, 1)
	return
}

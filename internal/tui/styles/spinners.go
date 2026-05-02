package styles

import "github.com/charmbracelet/bubbles/spinner"

// SpinnerOption pairs a user-facing name with a bubbles spinner
// definition. The palette picker iterates over Spinners() to render
// the entries; the model keeps the chosen name in config so the
// preference persists across runs.
type SpinnerOption struct {
	Name    string
	Spinner spinner.Spinner
}

// Spinners returns the curated list of bubbles spinner styles
// exposed to the user. We omit the joke spinners (Monkey) and the
// noisy emoji ones that don't render well in a status bar (Globe and
// Moon are kept because they're popular requests).
func Spinners() []SpinnerOption {
	return []SpinnerOption{
		{"dot", spinner.Dot},
		{"line", spinner.Line},
		{"minidot", spinner.MiniDot},
		{"jump", spinner.Jump},
		{"pulse", spinner.Pulse},
		{"points", spinner.Points},
		{"globe", spinner.Globe},
		{"moon", spinner.Moon},
		{"meter", spinner.Meter},
		{"hamburger", spinner.Hamburger},
		{"ellipsis", spinner.Ellipsis},
	}
}

// SpinnerByName resolves a name to its spinner.Spinner. Unknown
// names fall back to spinner.Dot so a stale config can't break the
// loading footer.
func SpinnerByName(name string) (spinner.Spinner, bool) {
	for _, opt := range Spinners() {
		if opt.Name == name {
			return opt.Spinner, true
		}
	}
	return spinner.Dot, false
}

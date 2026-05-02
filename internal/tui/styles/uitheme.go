package styles

import "github.com/charmbracelet/lipgloss"

// UITheme is the full set of palette colours the navigator chrome
// reads. A theme is just data — Apply() projects it onto the
// package-level Style vars in styles.go.
//
// Keep in mind: per-cloud terminal accents (themes.go::Theme) are a
// separate concept. Those encode brand identity for the embedded PTY
// page and intentionally don't follow the user's UI theme — switching
// to Dracula shouldn't repaint the GCP terminal in purple.
type UITheme struct {
	Name string

	HeaderBg lipgloss.Color
	HintBg   lipgloss.Color
	SelBg    lipgloss.Color
	SelFg    lipgloss.Color

	Muted  lipgloss.Color
	Subtle lipgloss.Color
	Fg     lipgloss.Color

	Accent lipgloss.Color
	Purple lipgloss.Color
	Green  lipgloss.Color
	Warn   lipgloss.Color
	Err    lipgloss.Color
}

// Built-in UI themes. Each is dark-friendly except UISolarizedLight,
// which is the canonical light counterpart for users on bright
// terminal themes.
var (
	// UIDefault preserves the original cloudnav palette — ANSI 256
	// codes that adapt to the user's terminal theme. The other named
	// themes use truecolor hex so the look is consistent everywhere.
	UIDefault = UITheme{
		Name:     "default",
		HeaderBg: lipgloss.Color("235"),
		HintBg:   lipgloss.Color("233"),
		SelBg:    lipgloss.Color("57"),
		SelFg:    lipgloss.Color("229"),
		Muted:    lipgloss.Color("240"),
		Subtle:   lipgloss.Color("245"),
		Fg:       lipgloss.Color("255"),
		Accent:   lipgloss.Color("86"),
		Purple:   lipgloss.Color("63"),
		Green:    lipgloss.Color("114"),
		Warn:     lipgloss.Color("214"),
		Err:      lipgloss.Color("196"),
	}

	// UIDracula — the popular dark palette by Zeno Rocha. Soft pinks
	// and purples on a deep blue-grey background.
	UIDracula = UITheme{
		Name:     "dracula",
		HeaderBg: lipgloss.Color("#282a36"),
		HintBg:   lipgloss.Color("#1e1f29"),
		SelBg:    lipgloss.Color("#44475a"),
		SelFg:    lipgloss.Color("#f8f8f2"),
		Muted:    lipgloss.Color("#6272a4"),
		Subtle:   lipgloss.Color("#bd93f9"),
		Fg:       lipgloss.Color("#f8f8f2"),
		Accent:   lipgloss.Color("#8be9fd"),
		Purple:   lipgloss.Color("#bd93f9"),
		Green:    lipgloss.Color("#50fa7b"),
		Warn:     lipgloss.Color("#ffb86c"),
		Err:      lipgloss.Color("#ff5555"),
	}

	// UINord — the cool, frost-blue palette by Arctic Ice Studio.
	UINord = UITheme{
		Name:     "nord",
		HeaderBg: lipgloss.Color("#2e3440"),
		HintBg:   lipgloss.Color("#272c36"),
		SelBg:    lipgloss.Color("#434c5e"),
		SelFg:    lipgloss.Color("#eceff4"),
		Muted:    lipgloss.Color("#4c566a"),
		Subtle:   lipgloss.Color("#81a1c1"),
		Fg:       lipgloss.Color("#eceff4"),
		Accent:   lipgloss.Color("#88c0d0"),
		Purple:   lipgloss.Color("#b48ead"),
		Green:    lipgloss.Color("#a3be8c"),
		Warn:     lipgloss.Color("#ebcb8b"),
		Err:      lipgloss.Color("#bf616a"),
	}

	// UISolarizedDark — Ethan Schoonover's classic. Warm tones on a
	// dark teal-grey base.
	UISolarizedDark = UITheme{
		Name:     "solarized-dark",
		HeaderBg: lipgloss.Color("#073642"),
		HintBg:   lipgloss.Color("#002b36"),
		SelBg:    lipgloss.Color("#586e75"),
		SelFg:    lipgloss.Color("#fdf6e3"),
		Muted:    lipgloss.Color("#586e75"),
		Subtle:   lipgloss.Color("#93a1a1"),
		Fg:       lipgloss.Color("#eee8d5"),
		Accent:   lipgloss.Color("#2aa198"),
		Purple:   lipgloss.Color("#6c71c4"),
		Green:    lipgloss.Color("#859900"),
		Warn:     lipgloss.Color("#b58900"),
		Err:      lipgloss.Color("#dc322f"),
	}

	// UISolarizedLight — same Solarized accent set on a cream
	// background. The only built-in theme tuned for light terminals.
	UISolarizedLight = UITheme{
		Name:     "solarized-light",
		HeaderBg: lipgloss.Color("#eee8d5"),
		HintBg:   lipgloss.Color("#fdf6e3"),
		SelBg:    lipgloss.Color("#93a1a1"),
		SelFg:    lipgloss.Color("#002b36"),
		Muted:    lipgloss.Color("#93a1a1"),
		Subtle:   lipgloss.Color("#586e75"),
		Fg:       lipgloss.Color("#073642"),
		Accent:   lipgloss.Color("#2aa198"),
		Purple:   lipgloss.Color("#6c71c4"),
		Green:    lipgloss.Color("#859900"),
		Warn:     lipgloss.Color("#b58900"),
		Err:      lipgloss.Color("#dc322f"),
	}

	// UIMonochrome — neutral greys for users who want zero colour
	// distraction (or render on monochrome terminals / screen
	// recordings). Errors and warnings stay tinted because they
	// carry semantic weight.
	UIMonochrome = UITheme{
		Name:     "monochrome",
		HeaderBg: lipgloss.Color("#1c1c1c"),
		HintBg:   lipgloss.Color("#0a0a0a"),
		SelBg:    lipgloss.Color("#3a3a3a"),
		SelFg:    lipgloss.Color("#ffffff"),
		Muted:    lipgloss.Color("#5f5f5f"),
		Subtle:   lipgloss.Color("#a8a8a8"),
		Fg:       lipgloss.Color("#dcdcdc"),
		Accent:   lipgloss.Color("#e4e4e4"),
		Purple:   lipgloss.Color("#9e9e9e"),
		Green:    lipgloss.Color("#b8bb26"),
		Warn:     lipgloss.Color("#fabd2f"),
		Err:      lipgloss.Color("#fb4934"),
	}
)

// UIThemes is the ordered registry exposed to the palette. Order
// drives the picker's display order — most-popular dark themes first,
// light variant in the middle, monochrome last.
var UIThemes = []UITheme{
	UIDefault,
	UIDracula,
	UINord,
	UISolarizedDark,
	UISolarizedLight,
	UIMonochrome,
}

// UIThemeByName looks up a theme by its lowercase name. Falls back to
// UIDefault on miss so a stale config.Theme doesn't break startup.
func UIThemeByName(name string) (UITheme, bool) {
	for _, t := range UIThemes {
		if t.Name == name {
			return t, true
		}
	}
	return UIDefault, false
}

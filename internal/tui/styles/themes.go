package styles

import "github.com/charmbracelet/lipgloss"

// Theme is a per-cloud visual identity used by chrome that wants to
// match the active provider — currently the embedded PTY terminal,
// which paints its frame, prompt, and status bar in the cloud's brand
// colours so the user always knows which cloud they're shelling into.
//
// Hex colours work here (not ANSI 256) because the chrome around the
// terminal isn't the table palette — these are deliberate brand
// accents and we want them to render the same on every modern
// terminal that supports truecolor.
type Theme struct {
	Name      string         // human-readable "GCP" / "AWS" / "Azure" / "cloudnav"
	Primary   lipgloss.Color // dominant brand colour — frame border, header bg accent
	Secondary lipgloss.Color // secondary brand colour — prompt, key hints
	Accent    lipgloss.Color // tertiary highlight — bell / alerts inside the term
	Surface   lipgloss.Color // header / footer background inside the terminal frame
	Text      lipgloss.Color // primary text on the surface
	Subtle    lipgloss.Color // dim text on the surface (paths, hints)
}

// Render-ready styles derived from a Theme. Built once per theme switch.
type ThemeStyles struct {
	Title       lipgloss.Style // app title pill in the terminal frame
	Frame       lipgloss.Style // bordered container around the PTY screen
	HeaderBar   lipgloss.Style // top strip inside the frame (cloud + context)
	StatusBar   lipgloss.Style // bottom strip inside the frame (hint + state)
	Cloud       lipgloss.Style // cloud-name pill (bold on Primary)
	Context     lipgloss.Style // context label "sub=… rg=…"
	Prompt      lipgloss.Style // bold leading prompt colour
	KeyHint     lipgloss.Style // <ctrl-d> close, <ctrl-l> clear, etc.
	HintText    lipgloss.Style // muted descriptor next to a key hint
	Cursor      lipgloss.Style // block cursor inside the terminal screen
	Welcome     lipgloss.Style // intro banner shown before the first PTY byte
	Description lipgloss.Style // sub-header line under the welcome banner
}

// Themes — keep brand palettes in one place. ThemeFor() resolves these
// by provider name; unknown / nil providers fall back to ThemeDefault.
var (
	// GCP — Google brand palette (search-bar style): blue / red / yellow / green.
	ThemeGCP = Theme{
		Name:      "GCP",
		Primary:   lipgloss.Color("#4285F4"), // Google blue
		Secondary: lipgloss.Color("#EA4335"), // Google red
		Accent:    lipgloss.Color("#FBBC05"), // Google yellow
		Surface:   lipgloss.Color("#0B1F3A"), // deep blue-black header bg
		Text:      lipgloss.Color("#FFFFFF"),
		Subtle:    lipgloss.Color("#9AA0A6"),
	}

	// AWS — orange + slate, matching the console's signature accents.
	ThemeAWS = Theme{
		Name:      "AWS",
		Primary:   lipgloss.Color("#FF9900"), // Amazon orange
		Secondary: lipgloss.Color("#FFAC31"), // lighter orange for hover
		Accent:    lipgloss.Color("#1A476F"), // dark cobalt accent
		Surface:   lipgloss.Color("#232F3E"), // AWS slate
		Text:      lipgloss.Color("#FFFFFF"),
		Subtle:    lipgloss.Color("#A8B5C6"),
	}

	// Azure — cyan + blue, mirroring the portal's primary chrome.
	ThemeAzure = Theme{
		Name:      "Azure",
		Primary:   lipgloss.Color("#0078D4"), // Azure blue
		Secondary: lipgloss.Color("#00BCF2"), // Azure cyan
		Accent:    lipgloss.Color("#50E6FF"), // electric cyan
		Surface:   lipgloss.Color("#0E1726"), // dark navy header bg
		Text:      lipgloss.Color("#FFFFFF"),
		Subtle:    lipgloss.Color("#A3C2E8"),
	}

	// Default — used at the cloud-list level (no provider chosen yet)
	// and as the fallback for any unknown provider name. Reuses the
	// app's accent teal so the chrome doesn't suddenly change identity
	// just because no cloud is active.
	ThemeDefault = Theme{
		Name:      "cloudnav",
		Primary:   lipgloss.Color("#7C3AED"), // app violet (matches release badge)
		Secondary: lipgloss.Color("#5EEAD4"), // teal accent (matches Title)
		Accent:    lipgloss.Color("#FBBF24"),
		Surface:   lipgloss.Color("#111827"),
		Text:      lipgloss.Color("#FFFFFF"),
		Subtle:    lipgloss.Color("#9CA3AF"),
	}
)

// ThemeFor selects a theme by provider name. The match is
// case-insensitive against the canonical provider names we register
// in tui.buildProviders ("Azure", "GCP", "AWS"). Empty / unknown
// names fall back to ThemeDefault.
func ThemeFor(provider string) Theme {
	switch normalize(provider) {
	case "gcp", "google", "google cloud":
		return ThemeGCP
	case "aws", "amazon", "amazon web services":
		return ThemeAWS
	case "azure", "microsoft azure":
		return ThemeAzure
	default:
		return ThemeDefault
	}
}

func normalize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

// Build derives the render-ready lipgloss styles from a Theme.
// Cheap (just a handful of NewStyle().Foreground/Background calls) so
// callers can rebuild on every theme change without caching.
func (t Theme) Build() ThemeStyles {
	return ThemeStyles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Text).
			Background(t.Primary).
			Padding(0, 1),
		Frame: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Primary),
		HeaderBar: lipgloss.NewStyle().
			Background(t.Surface).
			Foreground(t.Text).
			Padding(0, 1),
		StatusBar: lipgloss.NewStyle().
			Background(t.Surface).
			Foreground(t.Subtle).
			Padding(0, 1),
		Cloud: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Primary),
		Context: lipgloss.NewStyle().
			Foreground(t.Secondary),
		Prompt: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Secondary),
		KeyHint: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Accent),
		HintText: lipgloss.NewStyle().
			Foreground(t.Subtle),
		Cursor: lipgloss.NewStyle().
			Background(t.Secondary).
			Foreground(t.Surface),
		Welcome: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Primary),
		Description: lipgloss.NewStyle().
			Foreground(t.Subtle).
			Italic(true),
	}
}

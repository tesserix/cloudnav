package styles

import "testing"

func TestThemeForResolvesEachCloud(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Azure", "Azure"},
		{"azure", "Azure"},
		{"Microsoft Azure", "Azure"},
		{"GCP", "GCP"},
		{"gcp", "GCP"},
		{"Google Cloud", "GCP"},
		{"AWS", "AWS"},
		{"aws", "AWS"},
		{"Amazon Web Services", "AWS"},
		{"", "cloudnav"},
		{"OracleCloud", "cloudnav"},
	}
	for _, c := range cases {
		got := ThemeFor(c.input).Name
		if got != c.want {
			t.Errorf("ThemeFor(%q).Name = %q, want %q", c.input, got, c.want)
		}
	}
}

// Each cloud theme must populate every render-critical colour;
// a zero value would render as terminal-default and ruin the
// brand identity the user expects when they press `x`.
func TestThemeBuildHasFullPalette(t *testing.T) {
	for _, theme := range []Theme{ThemeGCP, ThemeAWS, ThemeAzure, ThemeDefault} {
		if theme.Primary == "" {
			t.Errorf("%s: Primary is empty", theme.Name)
		}
		if theme.Secondary == "" {
			t.Errorf("%s: Secondary is empty", theme.Name)
		}
		if theme.Surface == "" {
			t.Errorf("%s: Surface is empty", theme.Name)
		}
		if theme.Text == "" {
			t.Errorf("%s: Text is empty", theme.Name)
		}
		if theme.Subtle == "" {
			t.Errorf("%s: Subtle is empty", theme.Name)
		}
	}
}

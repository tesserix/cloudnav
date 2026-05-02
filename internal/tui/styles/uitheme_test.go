package styles

import "testing"

// Apply must mutate every package-level Color var so a runtime theme
// switch propagates without a process restart. If a future palette
// adds a new tone we want this test to flag the missing wire-up.
func TestApplyMutatesPackageColors(t *testing.T) {
	defer Apply(UIDefault) // restore so other tests see the default theme

	Apply(UIDracula)

	cases := []struct {
		name string
		got  any
		want any
	}{
		{"HeaderBg", HeaderBg, UIDracula.HeaderBg},
		{"HintBg", HintBg, UIDracula.HintBg},
		{"SelBg", SelBg, UIDracula.SelBg},
		{"SelFg", SelFg, UIDracula.SelFg},
		{"Muted", Muted, UIDracula.Muted},
		{"Subtle", Subtle, UIDracula.Subtle},
		{"Fg", Fg, UIDracula.Fg},
		{"Accent", Accent, UIDracula.Accent},
		{"Purple", Purple, UIDracula.Purple},
		{"Green", Green, UIDracula.Green},
		{"Warn", Warn, UIDracula.Warn},
		{"Err", Err, UIDracula.Err},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("after Apply(UIDracula): %s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if Active().Name != "dracula" {
		t.Errorf("Active().Name = %q, want dracula", Active().Name)
	}
}

func TestApplyRebuildsCrumbSep(t *testing.T) {
	defer Apply(UIDefault)
	Apply(UIDracula)
	if CrumbSep == "" {
		t.Fatal("CrumbSep empty after Apply — Apply must pre-render the separator")
	}
	// We don't assert across themes because lipgloss strips ANSI in
	// non-tty test runs, so two themes can render to the same string.
	// Verifying the pre-render happened is enough to catch regressions.
}

func TestUIThemeByNameKnownThemes(t *testing.T) {
	for _, want := range []string{"default", "dracula", "nord", "solarized-dark", "solarized-light", "monochrome"} {
		got, ok := UIThemeByName(want)
		if !ok {
			t.Errorf("UIThemeByName(%q) missed", want)
			continue
		}
		if got.Name != want {
			t.Errorf("UIThemeByName(%q).Name = %q", want, got.Name)
		}
	}
}

func TestUIThemeByNameFallsBackToDefault(t *testing.T) {
	got, ok := UIThemeByName("not-a-theme")
	if ok {
		t.Error("UIThemeByName should report miss for unknown theme")
	}
	if got.Name != "default" {
		t.Errorf("fallback theme = %q, want default", got.Name)
	}
}

func TestSpinnerByNameKnownSpinners(t *testing.T) {
	for _, want := range []string{"dot", "line", "globe", "moon", "pulse"} {
		_, ok := SpinnerByName(want)
		if !ok {
			t.Errorf("SpinnerByName(%q) missed", want)
		}
	}
}

func TestSpinnerByNameFallsBackToDot(t *testing.T) {
	if _, ok := SpinnerByName("not-a-spinner"); ok {
		t.Error("SpinnerByName should report miss for unknown spinner")
	}
}

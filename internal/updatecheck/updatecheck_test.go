package updatecheck

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.3", "v1.2.2", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.3.0", false},
		{"v2.0.0", "v1.9.9", true},
		{"v1.2.3", "dev", true},     // dev is always older
		{"v1.2.3", "unknown", true}, // unreleased local build
		{"", "v1.2.3", false},
		{"v1.2.3", "", false},
		{"1.2.3", "1.2.2", true},                       // no v prefix
		{"v1.2.3-rc.1", "v1.2.3-rc.2", false},          // ignore suffix => equal
		{"v1.2.3+meta", "v1.2.2", true},                // ignore build metadata
		{"v1.10.0", "v1.2.0", true},                    // numeric (not lex) compare
		{"v1.2", "v1.1", true},                         // two-component form
		{"zzzz", "v1.2.3", true}, // lexicographic fallback when semver fails
	}
	for _, tc := range cases {
		got := IsNewer(tc.latest, tc.current)
		if got != tc.want {
			t.Errorf("IsNewer(%q,%q)=%v want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	cases := map[string][]int{
		"v1.2.3":        {1, 2, 3},
		"1.2.3":         {1, 2, 3},
		"v1.2":          {1, 2, 0},
		"v1.2.3-rc.1":   {1, 2, 3},
		"v1.2.3+abc":    {1, 2, 3},
	}
	for in, want := range cases {
		got := parseSemver(in)
		if len(got) != 3 {
			t.Errorf("parseSemver(%q) len=%d want 3", in, len(got))
			continue
		}
		for i := 0; i < 3; i++ {
			if got[i] != want[i] {
				t.Errorf("parseSemver(%q)[%d]=%d want %d", in, i, got[i], want[i])
			}
		}
	}
}

func TestParseSemverInvalid(t *testing.T) {
	// Four components or non-numeric pieces should return nil so the
	// caller falls back to a string compare rather than pretending.
	if got := parseSemver("1.2.3.4"); got != nil {
		t.Errorf("parseSemver 4-component=%v want nil", got)
	}
	if got := parseSemver("v1.x.3"); got != nil {
		t.Errorf("parseSemver non-numeric=%v want nil", got)
	}
}

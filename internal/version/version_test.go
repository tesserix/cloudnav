package version

import (
	"strings"
	"testing"
)

func TestStringDefaults(t *testing.T) {
	got := String()
	for _, sub := range []string{"dev", "none", "unknown"} {
		if !strings.Contains(got, sub) {
			t.Errorf("String() = %q, want it to contain %q", got, sub)
		}
	}
}

func TestStringOverridden(t *testing.T) {
	prev := Version
	Version = "v1.2.3"
	defer func() { Version = prev }()
	if !strings.Contains(String(), "v1.2.3") {
		t.Errorf("String() = %q, want to contain v1.2.3", String())
	}
}

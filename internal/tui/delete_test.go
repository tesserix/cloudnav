package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestDeleteNoun(t *testing.T) {
	cases := []struct {
		scope deleteScope
		n     int
		want  string
	}{
		{deleteScopeRG, 1, "resource group"},
		{deleteScopeRG, 3, "resource groups"},
		{deleteScopeResource, 1, "resource"},
		{deleteScopeResource, 2, "resources"},
	}
	for _, c := range cases {
		if got := deleteNoun(c.scope, c.n); got != c.want {
			t.Errorf("deleteNoun(%v,%d) = %q, want %q", c.scope, c.n, got, c.want)
		}
	}
}

func TestFailuresToErrJoinsPerTarget(t *testing.T) {
	fails := []deleteFailure{
		{Name: "rg-a", Err: errors.New("AuthorizationFailed: not allowed")},
		{Name: "rg-b", Err: errors.New("LockedResource: CanNotDelete lock present")},
	}
	got := failuresToErr(fails)
	if got == nil {
		t.Fatal("expected non-nil error")
	}
	msg := got.Error()
	if !strings.Contains(msg, "rg-a:") || !strings.Contains(msg, "AuthorizationFailed") {
		t.Errorf("error missing rg-a context: %q", msg)
	}
	if !strings.Contains(msg, "rg-b:") || !strings.Contains(msg, "CanNotDelete") {
		t.Errorf("error missing rg-b context: %q", msg)
	}
	// Newline between the two so detail overlay renders them readably.
	if !strings.Contains(msg, "\n") {
		t.Errorf("expected multi-line output, got single-line: %q", msg)
	}
}

func TestFailuresToErrEmptyDoesNotPanic(t *testing.T) {
	// Callers avoid this path, but a defensive check is cheap.
	_ = failuresToErr(nil)
}

func TestStateBadgeColouring(t *testing.T) {
	cases := []struct {
		in      string
		wantRaw string // visible content after the ANSI prefix
	}{
		{"Succeeded", "Succeeded"},
		{"Deleting", "Deleting"},
		{"deleting", "Deleting"},
		{"Failed", "Failed"},
		{"Canceled", "Canceled"},
	}
	for _, c := range cases {
		got := stateBadge(c.in)
		if !strings.Contains(got, c.wantRaw) {
			t.Errorf("stateBadge(%q) = %q, expected to contain %q", c.in, got, c.wantRaw)
		}
	}
}

func TestStateBadgePassThroughUnknown(t *testing.T) {
	if got := stateBadge("Provisioning"); got != "Provisioning" {
		t.Errorf("unknown state should pass through unchanged, got %q", got)
	}
}

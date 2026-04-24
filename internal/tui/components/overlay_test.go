package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCompositePreservesBgAround(t *testing.T) {
	bg := "aaaaaaaa\nbbbbbbbb\ncccccccc\ndddddddd"
	fg := "XX\nYY"
	got := Composite(bg, fg, 3, 1)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 lines, got %d", len(lines))
	}
	if lines[0] != "aaaaaaaa" {
		t.Errorf("row 0 should be untouched, got %q", lines[0])
	}
	if lines[1] != "bbbXXbbb" {
		t.Errorf("row 1 composite wrong: got %q, want bbbXXbbb", lines[1])
	}
	if lines[2] != "cccYYccc" {
		t.Errorf("row 2 composite wrong: got %q, want cccYYccc", lines[2])
	}
	if lines[3] != "dddddddd" {
		t.Errorf("row 3 should be untouched, got %q", lines[3])
	}
}

func TestCompositeHandlesAnsiColorsInBg(t *testing.T) {
	// Red "aaaa" background. Raw ANSI: ESC [ 31 m aaaa ESC [ 0 m
	bg := "\x1b[31maaaaaaaa\x1b[0m"
	fg := "XX"
	got := Composite(bg, fg, 3, 0)
	// Visible width should still be 8
	if w := ansi.StringWidth(got); w != 8 {
		t.Errorf("composited width = %d, want 8 (ANSI may be torn)", w)
	}
	if !strings.Contains(got, "XX") {
		t.Errorf("fg missing from composite: %q", got)
	}
}

func TestCompositeOverflowingRight(t *testing.T) {
	bg := "aaaa"
	fg := "XXXXXX" // wider than remaining space
	got := Composite(bg, fg, 2, 0)
	// Expected: aa + XXXXXX
	if got != "aaXXXXXX" {
		t.Errorf("got %q, want aaXXXXXX", got)
	}
}

func TestCompositeOutOfBoundsRows(t *testing.T) {
	bg := "aa\nbb\ncc"
	fg := "ZZ"
	// y = 10 beyond bg → bg unchanged
	got := Composite(bg, fg, 0, 10)
	if got != bg {
		t.Errorf("out-of-bounds y should be no-op, got %q", got)
	}
}

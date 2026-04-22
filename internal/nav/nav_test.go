package nav

import "testing"

func TestStackPushPopTop(t *testing.T) {
	var s Stack
	if s.Top() != nil {
		t.Fatal("Top on empty stack should be nil")
	}
	s.Push(Frame{Title: "a"})
	s.Push(Frame{Title: "b"})
	if s.Depth() != 2 {
		t.Fatalf("Depth = %d, want 2", s.Depth())
	}
	if s.Top().Title != "b" {
		t.Fatalf("Top = %q, want b", s.Top().Title)
	}
	top, ok := s.Pop()
	if !ok || top.Title != "b" {
		t.Fatalf("Pop = (%q, %v), want (b, true)", top.Title, ok)
	}
	if s.Depth() != 1 {
		t.Fatalf("Depth after pop = %d, want 1", s.Depth())
	}
}

func TestStackBreadcrumbs(t *testing.T) {
	var s Stack
	s.Push(Frame{Title: "clouds"})
	s.Push(Frame{Title: "azure"})
	s.Push(Frame{Title: "acme-prod"})
	crumbs := s.Breadcrumbs()
	want := []string{"clouds", "azure", "acme-prod"}
	if len(crumbs) != len(want) {
		t.Fatalf("len = %d, want %d", len(crumbs), len(want))
	}
	for i, c := range crumbs {
		if c != want[i] {
			t.Errorf("[%d] = %q, want %q", i, c, want[i])
		}
	}
}

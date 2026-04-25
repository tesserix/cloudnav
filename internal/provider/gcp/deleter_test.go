package gcp

import (
	"context"
	"errors"
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestZoneFromInstanceID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"//compute.googleapis.com/projects/p/zones/us-central1-a/instances/foo", "us-central1-a"},
		{"projects/p/zones/europe-west2-c/instances/bar", "europe-west2-c"},
		{"no-zone-here", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := zoneFromInstanceID(c.in); got != c.want {
			t.Errorf("zoneFromInstanceID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNodeProjectFromID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"//storage.googleapis.com/projects/my-proj/buckets/foo", "my-proj"},
		{"projects/data-warehouse/zones/us-central1-a/instances/x", "data-warehouse"},
		{"no-project", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := nodeProjectFromID(c.in); got != c.want {
			t.Errorf("nodeProjectFromID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDeleteUnsupportedKind(t *testing.T) {
	g := New()
	err := g.Delete(context.Background(), provider.Node{
		Kind: provider.KindCloud,
	})
	if !errors.Is(err, provider.ErrNotSupported) {
		t.Errorf("Delete on KindCloud should return ErrNotSupported, got %v", err)
	}
}

func TestDeleteUnsupportedResourceType(t *testing.T) {
	g := New()
	err := g.Delete(context.Background(), provider.Node{
		Kind: provider.KindResource,
		Meta: map[string]string{
			"type": "pubsub.googleapis.com/Topic",
		},
	})
	if !errors.Is(err, provider.ErrNotSupported) {
		t.Errorf("Delete on unsupported asset type should return ErrNotSupported, got %v", err)
	}
}

func TestParseLiensCLI(t *testing.T) {
	in := []byte(`[
        {"name":"liens/p1234","reason":"production project, do not delete","origin":"sre-team","restrictions":["resourcemanager.projects.delete"]},
        {"name":"liens/p5678","reason":"compliance hold","origin":"compliance"}
    ]`)
	out, err := parseLiensCLI(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 liens, got %d", len(out))
	}
	if out[0].Name != "liens/p1234" || out[0].Reason != "production project, do not delete" {
		t.Errorf("first lien shape unexpected: %+v", out[0])
	}
	if out[0].Level != "Lien" {
		t.Errorf("level should be 'Lien', got %q", out[0].Level)
	}
}

func TestLocksOnNonProjectKind(t *testing.T) {
	g := New()
	out, err := g.Locks(context.Background(), provider.Node{Kind: provider.KindResource})
	if err != nil {
		t.Errorf("Locks on non-project should return (nil, nil), got err=%v", err)
	}
	if out != nil {
		t.Errorf("Locks on non-project should return nil slice, got %v", out)
	}
}

func TestCreateLockOnNonProjectKind(t *testing.T) {
	g := New()
	err := g.CreateLock(context.Background(), provider.Node{Kind: provider.KindResource}, "test")
	if !errors.Is(err, provider.ErrNotSupported) {
		t.Errorf("CreateLock on non-project should return ErrNotSupported, got %v", err)
	}
}

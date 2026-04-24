package cache

import (
	"os"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore[map[string]string](dir, "costs", time.Minute)
	payload := map[string]string{"a": "1", "b": "2"}
	if err := s.Set("scope1", payload); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := s.Get("scope1")
	if !ok {
		t.Fatal("Get: miss after Set")
	}
	if got["a"] != "1" || got["b"] != "2" {
		t.Errorf("Get returned wrong payload: %v", got)
	}
}

func TestStoreMissOnUnknownKey(t *testing.T) {
	s := NewStore[string](t.TempDir(), "b", time.Minute)
	if _, ok := s.Get("nope"); ok {
		t.Error("Get should miss on an unknown key")
	}
}

func TestStoreTTLExpiry(t *testing.T) {
	dir := t.TempDir()
	// 1ms TTL so the entry is stale before the next tick.
	s := NewStore[int](dir, "short", time.Millisecond)
	if err := s.Set("x", 42); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, ok := s.Get("x"); ok {
		t.Error("Get should treat entries past TTL as misses")
	}
}

func TestStoreDelete(t *testing.T) {
	s := NewStore[int](t.TempDir(), "b", time.Minute)
	_ = s.Set("x", 1)
	if err := s.Delete("x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("x"); ok {
		t.Error("Get should miss after Delete")
	}
	// Second Delete on a missing key should be a no-op.
	if err := s.Delete("x"); err != nil {
		t.Errorf("Delete on missing key should be no-op, got %v", err)
	}
}

func TestStoreClear(t *testing.T) {
	s := NewStore[int](t.TempDir(), "b", time.Minute)
	_ = s.Set("a", 1)
	_ = s.Set("b", 2)
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("a"); ok {
		t.Error("Clear should drop all entries")
	}
}

func TestSanitise(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"simple", "simple"},
		{"sub:abc/rg-foo", "sub_abc_rg-foo"},
		{"a\\b*c?", "a_b_c_"},
		{"", "empty"},
	}
	for _, c := range cases {
		if got := sanitise(c.in); got != c.want {
			t.Errorf("sanitise(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPathHonoursEnv(t *testing.T) {
	t.Setenv("CLOUDNAV_CACHE", "/tmp/cloudnav-test-override")
	if got := Path(); got != "/tmp/cloudnav-test-override" {
		t.Errorf("Path should honour CLOUDNAV_CACHE, got %q", got)
	}
	_ = os.Unsetenv("CLOUDNAV_CACHE")
}

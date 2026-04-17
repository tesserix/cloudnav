package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLOUDNAV_CONFIG", filepath.Join(dir, "config.json"))
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Bookmarks) != 0 {
		t.Errorf("expected empty bookmarks, got %d", len(c.Bookmarks))
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("CLOUDNAV_CONFIG", path)

	c := &Config{
		DefaultProvider: "azure",
		Theme:           "dark",
		Bookmarks: []Bookmark{
			{
				Label:    "azure / Platform-Prod",
				Provider: "azure",
				Path: []Crumb{
					{Kind: "cloud", ID: "", Name: "azure"},
					{Kind: "subscription", ID: "fcb999d2", Name: "Platform-Prod"},
				},
			},
		},
	}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DefaultProvider != "azure" {
		t.Errorf("DefaultProvider=%q", loaded.DefaultProvider)
	}
	if len(loaded.Bookmarks) != 1 {
		t.Fatalf("bookmarks len=%d", len(loaded.Bookmarks))
	}
	if loaded.Bookmarks[0].Path[1].Name != "Platform-Prod" {
		t.Errorf("path[1].Name=%q", loaded.Bookmarks[0].Path[1].Name)
	}
}

func TestAddAndRemoveBookmark(t *testing.T) {
	c := &Config{}
	c.AddBookmark(Bookmark{Label: "a"})
	c.AddBookmark(Bookmark{Label: "b"})
	c.AddBookmark(Bookmark{Label: "a"}) // duplicate — ignored
	if len(c.Bookmarks) != 2 {
		t.Fatalf("len=%d want 2", len(c.Bookmarks))
	}
	c.RemoveBookmark("a")
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len after remove=%d want 1", len(c.Bookmarks))
	}
	if c.Bookmarks[0].Label != "b" {
		t.Errorf("remaining label=%q", c.Bookmarks[0].Label)
	}
}

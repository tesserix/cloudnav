// Package config loads and saves the user-scoped preferences that persist
// between cloudnav sessions — bookmarks, default provider, theme. Everything
// is optional; callers get safe zero-values on error.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type Bookmark struct {
	Label    string  `json:"label"`
	Provider string  `json:"provider"`
	Path     []Crumb `json:"path"`
	Created  string  `json:"created,omitempty"`
}

type Crumb struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Config struct {
	DefaultProvider string `json:"default_provider,omitempty"`
	// Theme picks the navigator UI palette. One of "default" (the
	// original ANSI-256 dark scheme), "dracula", "nord",
	// "solarized-dark", "solarized-light", "monochrome". Empty falls
	// back to "default". Set via the `:` palette ("theme: …") and
	// persisted automatically.
	Theme string `json:"theme,omitempty"`
	// Spinner picks the loading-footer animation. One of the names in
	// styles.Spinners() — "dot", "line", "globe", etc. Empty falls
	// back to "dot".
	Spinner   string     `json:"spinner,omitempty"`
	Bookmarks []Bookmark `json:"bookmarks,omitempty"`
	GCP       GCPConfig  `json:"gcp,omitempty"`
	// AutoUpgrade, when true, lets cloudnav upgrade itself silently at startup
	// whenever a newer release is detected on GitHub. Off by default — users
	// opt in explicitly so a background TUI never surprises them with a
	// binary swap mid-session.
	AutoUpgrade bool `json:"auto_upgrade,omitempty"`

	// DisplayCurrency renders cost amounts in this currency regardless of
	// the cloud's native currency. Empty (default) means each cloud
	// renders in its own native currency. ISO 4217 code, e.g. "GBP".
	// Rates come from frankfurter.app (free, ECB-backed, daily) and are
	// cached in SQLite for 24 hours. The value is upper-cased on use, so
	// "gbp" / "GBP" / " gbp " all resolve the same.
	DisplayCurrency string `json:"display_currency,omitempty"`
}

// GCPConfig holds GCP-specific preferences. Optional; environment variables
// of the same meaning still win so CI / scripts don't have to rewrite files.
type GCPConfig struct {
	// BillingTable is the BigQuery billing-export table in "project.dataset.table"
	// form. Month-to-date cost in the projects view pulls from here; if not
	// set, cost fails open and shows a hint instead.
	BillingTable string `json:"billing_table,omitempty"`
}

// Path returns the resolved config file path.
func Path() string {
	if v := os.Getenv("CLOUDNAV_CONFIG"); v != "" {
		return v
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "cloudnav", "config.json")
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "cloudnav", "config.json")
}

// Load reads the config file. A missing file is not an error — it returns an
// empty Config.
func Load() (*Config, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes the config atomically.
func Save(c *Config) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// AddBookmark appends a bookmark if no existing entry has the same Label.
func (c *Config) AddBookmark(b Bookmark) {
	for _, existing := range c.Bookmarks {
		if existing.Label == b.Label {
			return
		}
	}
	c.Bookmarks = append(c.Bookmarks, b)
}

// RemoveBookmark deletes a bookmark by Label. Silent if not found.
func (c *Config) RemoveBookmark(label string) {
	out := c.Bookmarks[:0]
	for _, b := range c.Bookmarks {
		if b.Label != label {
			out = append(out, b)
		}
	}
	c.Bookmarks = out
}

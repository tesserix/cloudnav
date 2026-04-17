// Package version exposes build metadata injected via -ldflags at build time.
package version

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func String() string {
	return Version + " (" + Commit + " · " + Date + ")"
}

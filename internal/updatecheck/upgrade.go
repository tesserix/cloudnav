package updatecheck

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// UpgradeMethod is the install flavour we detected for the running
// binary. The TUI surfaces it so the user can see what's about to run
// before they confirm.
type UpgradeMethod string

const (
	UpgradeGoInstall UpgradeMethod = "go install"
	UpgradeHomebrew  UpgradeMethod = "homebrew"
	UpgradeManual    UpgradeMethod = "manual"
)

// UpgradePlan is the resolved plan for applying an upgrade on this
// machine. For automatic methods Bin+Args is populated; for Manual the
// caller should open URL in a browser instead.
type UpgradePlan struct {
	Method UpgradeMethod
	Bin    string
	Args   []string
	// URL is the release page to open when no automatic path is
	// available (or when the user prefers to upgrade manually).
	URL string
	// Why is a short human label shown above the confirm prompt so the
	// user understands how we picked the method ("brew detected",
	// "running from GOPATH/bin", etc.).
	Why string
}

// PlanUpgrade picks an upgrade method for the current binary. Detection
// order: Homebrew (cellar path), Go install (GOPATH/bin or GOBIN), then
// manual (open browser). We never fall through to a silent no-op — the
// user always gets a concrete next action.
func PlanUpgrade(latestTag, releaseURL string) UpgradePlan {
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)

	if isHomebrewBinary(exe) {
		// 'brew upgrade cloudnav' on its own only consults the local
		// formula cache. If the tap hasn't been refreshed since the new
		// tag was published, brew reports "already installed" and
		// nothing actually upgrades. Run update first so the upgrade
		// sees the newest formula.
		return UpgradePlan{
			Method: UpgradeHomebrew,
			Bin:    "sh",
			Args:   []string{"-c", "brew update && brew upgrade cloudnav"},
			URL:    releaseURL,
			Why:    "binary lives under a Homebrew prefix",
		}
	}
	if isGoBinBinary(exe) {
		target := "github.com/tesserix/cloudnav/cmd/cloudnav@latest"
		if latestTag != "" {
			target = "github.com/tesserix/cloudnav/cmd/cloudnav@" + latestTag
		}
		return UpgradePlan{
			Method: UpgradeGoInstall,
			Bin:    "go",
			Args:   []string{"install", target},
			URL:    releaseURL,
			Why:    "binary lives in a Go bin directory",
		}
	}
	return UpgradePlan{
		Method: UpgradeManual,
		URL:    releaseURL,
		Why:    "no automatic upgrade path detected — open release page",
	}
}

func isHomebrewBinary(path string) bool {
	if path == "" {
		return false
	}
	if strings.Contains(path, "/Cellar/") || strings.Contains(path, "/homebrew/") {
		return true
	}
	// macOS default locations
	prefixes := []string{"/opt/homebrew/", "/usr/local/Cellar/", "/home/linuxbrew/"}
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func isGoBinBinary(path string) bool {
	if path == "" {
		return false
	}
	candidates := []string{os.Getenv("GOBIN")}
	if gp := os.Getenv("GOPATH"); gp != "" {
		for _, p := range filepath.SplitList(gp) {
			candidates = append(candidates, filepath.Join(p, "bin"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "go", "bin"))
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if strings.HasPrefix(path, c+string(os.PathSeparator)) || strings.HasPrefix(path, c+"/") {
			return true
		}
	}
	// Fall back to "is `go` on PATH at all" — useful on fresh setups
	// where the binary might live anywhere but re-running go install
	// will drop the new one in the same place.
	if _, err := exec.LookPath("go"); err == nil {
		return false
	}
	return false
}

// Run executes the upgrade plan and returns a short user-facing status
// string on success, or an error. After a successful exit we verify
// the binary on disk actually moved to targetTag — brew has a history
// of exit-zero no-ops when its formula cache is stale, and users
// deserve to know when that happens instead of a false success.
// targetTag can be empty to skip the post-upgrade check.
func Run(ctx context.Context, plan UpgradePlan, targetTag string) (string, error) {
	switch plan.Method {
	case UpgradeGoInstall, UpgradeHomebrew:
		cmd := exec.CommandContext(ctx, plan.Bin, plan.Args...)
		cmd.Env = append(os.Environ(), "GO111MODULE=on")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("%s %s: %w", plan.Bin, strings.Join(plan.Args, " "), err)
		}
		ClearCache()
		summary := trimOutput(string(out))
		if installed, ok := installedVersion(ctx); ok && targetTag != "" && !IsNewer(installed, targetTag) && installed != targetTag && "v"+installed != targetTag {
			return summary, fmt.Errorf(
				"upgrade command exited ok but cloudnav is still reporting %s (expected %s) — "+
					"try 'brew update && brew upgrade cloudnav' from a shell",
				installed, targetTag)
		}
		return summary, nil
	case UpgradeManual:
		return "", openBrowser(plan.URL)
	default:
		return "", fmt.Errorf("unknown upgrade method %q", plan.Method)
	}
}

// installedVersion invokes cloudnav on PATH and parses the version line
// it prints, returning "0.22.7" (without any surrounding text). ok=false
// means we couldn't find / run the binary — callers should treat that
// as "skip the check" rather than as a failure.
func installedVersion(ctx context.Context) (string, bool) {
	path, err := exec.LookPath("cloudnav")
	if err != nil {
		return "", false
	}
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return "", false
	}
	// Expected format: "cloudnav 0.22.7 (commit · date)"
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return "", false
	}
	return fields[1], true
}

func openBrowser(url string) error {
	if url == "" {
		return fmt.Errorf("no release URL to open")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform %q", runtime.GOOS)
	}
	return cmd.Start()
}

func trimOutput(s string) string {
	s = strings.TrimSpace(s)
	const max = 400
	if len(s) > max {
		return s[:max] + "..."
	}
	if s == "" {
		return "ok"
	}
	return s
}

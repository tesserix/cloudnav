package tools

import "os/exec"

// Zellij is the Tool descriptor for the Zellij terminal multiplexer.
// `cloudnav workspace` ensures it's installed; `cloudnav install
// zellij` is the explicit imperative form.
var Zellij = Tool{
	Name:    "zellij",
	Bin:     "zellij",
	PlanFn:  zellijInstall,
	Upgrade: zellijUpgrade,
}

func zellijInstall(goos string) ([]Step, bool) {
	if goos == "windows" {
		return nil, false
	}
	// Homebrew is the preferred path on both macOS and linuxbrew
	// installs — same recipe, no version drift between platforms.
	if hasBin("brew") {
		return []Step{{
			Description: "brew install zellij",
			Bin:         "brew",
			Args:        []string{"install", "zellij"},
		}}, true
	}
	// Cargo fallback for Linux systems without brew. Most distro
	// repos don't carry zellij, so this is the realistic second
	// option. --locked pins the dep tree from the upstream Cargo.lock.
	if hasBin("cargo") {
		return []Step{{
			Description: "cargo install --locked zellij",
			Bin:         "cargo",
			Args:        []string{"install", "--locked", "zellij"},
		}}, true
	}
	return nil, false
}

func zellijUpgrade(goos string) ([]Step, bool) {
	if goos == "windows" {
		return nil, false
	}
	if hasBin("brew") {
		// `brew update` first — without it brew silently no-ops on
		// stale formula caches; same fix as the cloudnav self-upgrade
		// uses (CHANGELOG 0.22.5).
		return []Step{{
			Description: "brew update && brew upgrade zellij",
			Bin:         "sh",
			Args:        []string{"-c", "brew update && brew upgrade zellij"},
		}}, true
	}
	if hasBin("cargo") {
		// `cargo install --force` rebuilds even if the same version
		// is already installed; combined with --locked it reproduces
		// the upstream-pinned build.
		return []Step{{
			Description: "cargo install --locked --force zellij",
			Bin:         "cargo",
			Args:        []string{"install", "--locked", "--force", "zellij"},
		}}, true
	}
	return nil, false
}

func hasBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

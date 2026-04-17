# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] — 2026-04-17

All three clouds active and a persistence layer landed.

### Added
- **GCP provider** wrapping `gcloud`: projects → resources via Cloud Asset search. Portal links, details, unit-tested parsers.
- **AWS provider** wrapping `aws`: caller account → regions → resources via Resource Groups Tagging API. ARN→name/service/type derivation. Regional console portal handoffs.
- **Persistent bookmarks (`f` key)** written to `~/Library/Application Support/cloudnav/config.json` (macOS) / `~/.config/cloudnav/config.json` (Linux). Atomic save; deduped by label.
- **Command palette (`:` key)** — full-screen fuzzy switcher that merges cloud-switchers and saved bookmarks into one list. `↑↓ Enter esc`.
- **Azure cost column (`c` toggle)** — month-to-date spend per resource group via a single grouped Cost Management query. Correct currency symbol (£ / $ / € / ₹ / ¥ / A$ / C$). Cached per subscription.
- **PIM activation** — real `cloudnav pim list` + `cloudnav pim activate <index> --reason "..." --duration <hours>` against the Azure roleAssignmentScheduleRequests API. Generates the required request GUID inline.
- `ls` non-interactive command learned GCP and AWS: `ls gcp projects|resources`, `ls aws account|regions|resources`.

### Fixed
- Home page stops sorting alphabetically — preserves provider registration order so `azure` stays first.

[Unreleased]: https://github.com/tesserix/cloudnav/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/tesserix/cloudnav/releases/tag/v0.2.0

## [0.1.0] — 2026-04-17

First public release.

### Added
- Cobra CLI: `cloudnav` (TUI), `doctor`, `version`, `ls`, `completion`.
- Bubbletea TUI with full Azure navigation: clouds → subscriptions → resource groups → resources, cursor, breadcrumbs, help overlay, empty-state and filtered-count indicators.
- Fuzzy search (`/`), three sort modes (`s`: name / state / location), portal handoff (`o`), scrollable JSON detail (`i`), exec-in-context shell (`x`), PIM eligible-roles listing (`p`), refresh (`r`).
- Tenant and subscription display-name resolution via `az rest /tenants`, caching, and meta enrichment.
- Non-interactive mode: stdin/stdout TTY detection falls back to a guided error with `doctor` / `ls --json` hints.
- Apache-2.0 licensed OSS layout: README, CONTRIBUTING, SECURITY, CODE_OF_CONDUCT, ROADMAP, issue/PR templates.
- CI (lint + test matrix) with golangci-lint v2.1.6, CodeQL, dependabot.
- GoReleaser multi-arch release pipeline (darwin/linux amd64/arm64, windows amd64) with SBOM and Homebrew formula auto-published to `tesserix/homebrew-tap`.

[0.1.0]: https://github.com/tesserix/cloudnav/releases/tag/v0.1.0

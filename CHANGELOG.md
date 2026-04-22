# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] — 2026-04-18

### Added
- **Azure resource-group lock visibility and management**. On the RG view cloudnav fetches `az lock list` once per subscription and adds a LOCK column (🔒 CanNotDelete / 🔒 ReadOnly / —). Press `<L>` on a locked RG to remove its first lock; press `<L>` on an unlocked RG to create a `cloudnav-protect` CanNotDelete lock. Changes are reflected instantly after re-fetch.
- **Multi-select + bulk delete of resource groups**. On the RG view `space` toggles a ● marker on the cursor row, `[` selects all currently visible rows, `]` clears the selection. `<D>` asks Azure to delete the selected RGs (async, `--no-wait`) — refuses with an explanatory status if any selected RG still has a lock, so you have to `L` to unlock first. The keybar shows `<D> delete N` only when a selection exists.
- Per-row delta arrows on the cost column are now colour-coded (green ↓, red bold ↑, grey →) in both Azure RG and AWS region views.
- Subscription-level cost on the Azure subs view: press `<c>` on the subs list to get an MTD column with MoM arrows across every visible sub. Subs where the caller lacks Cost Management Reader are labelled "no cost read access" instead of a silent £0.
- Empty states are now context-specific — an empty RG says "no resources inside 'rg-foo'"; an empty sub-list suggests checking `az login`; etc.

### Fixed
- Table cell-count panic when navigating between views with different column counts — `refreshTable` now normalises every row to exactly `len(cols)` cells before calling `SetRows`.

[Unreleased]: https://github.com/tesserix/cloudnav/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/tesserix/cloudnav/releases/tag/v0.6.0

## [0.5.2] — 2026-04-17

### Added
- **Tenant filter on the Azure subs view** — press `<t>` to cycle through the tenants represented in the current list (`all → tenant A → tenant B → all`). The keybar shows the active tenant inline (`<t> tenant: Acme Production`), the footer combines it with any `/` filter (`tenant: Acme Production  filter: platform  3/72`).
- `/` search now also matches on the tenant name, so `/acme production` narrows to just that tenant's subs.

[0.5.2]: https://github.com/tesserix/cloudnav/releases/tag/v0.5.2

## [0.5.1] — 2026-04-17

### Changed
- TUI layout redesigned to match the cleaner aznav-style chrome:
  - Line 1: `cloudnav › clouds › azure › <sub>` breadcrumb on the left, compact `^_^` marker on the right.
  - Line 2: discoverable keybar — `<↵> drill  </> search  <:> palette  <f> flag  <p> PIM  <i> info  <o> portal  <c> costs  <s> sort name  <r> refresh  <esc> back  <q> quit`.
  - Table body is now borderless with padded cells, so rows feel spacious and the purple cursor row stands out.
  - Footer is a single quiet line that surfaces only what's contextual: search input while typing, `filter: X  n/total` when filtered, spinner while loading, or item count when idle.
- Sort mode now surfaces inline on the `<s>` key in the keybar (`<s> sort name / state / location`) instead of being tucked in the corner.

[0.5.1]: https://github.com/tesserix/cloudnav/releases/tag/v0.5.1

## [0.5.0] — 2026-04-17

Advisor reports, multi-cloud VM control, richer cost tables, and a shell-based e2e harness.

### Added
- **`cloudnav advisor --subscription <id>`** — Azure Advisor recommendations in a table, sortable by impact (High/Medium/Low), filterable by `--category Cost|Security|HighAvailability|Performance|OperationalExcellence`.
- **`cloudnav vm list|show|start|stop`** — multi-cloud VM control:
  - `list` across Azure (sub/RG scope), GCP (project scope), AWS (region scope) with `--state` filter.
  - `show` dumps the full cloud-native describe JSON.
  - `start`/`stop` accept multiple IDs and **require `--yes`** to proceed. Pre-flight refuses the operation otherwise.
- **`cloudnav cost subs|rgs|regions|services`** — read-only cost reports with MoM delta, sorted desc by spend, tabwriter-aligned columns, `--json` everywhere. Azure sub-level query runs 8-way parallel and flags subs where you lack Cost Management Reader.
- **`test/e2e/`** — tmux-driven shell harness covering every CLI verb + TUI drill flows (67 assertions). `make test-e2e`.

### Fixed
- Palette overflow with >150 entities: view now picks a scroll window around the cursor and shows "N more above/below" breadcrumbs so cloud switchers stay visible.
- Provider CLI timeouts lifted from 30s: Azure 2m, AWS 2m, GCP 3m — `gcloud asset search-all-resources` was being killed on large projects.

[0.5.0]: https://github.com/tesserix/cloudnav/releases/tag/v0.5.0

## [0.4.0] — 2026-04-17

JIT / elevation story rounded out for all three clouds, plus multi-cloud `pim` CLI.

### Added
- **AWS SSO** as a PIM-equivalent: cloudnav now parses `~/.aws/config`, lists every profile that has `sso_role_name`, and activation runs `aws sso login --profile <name>` inline (supports browser auth). Works from both the TUI (`p` key) and CLI (`cloudnav pim list --cloud aws`, `cloudnav pim activate N --cloud aws`).
- **GCP JIT** surface: `p` on GCP and `cloudnav pim list --cloud gcp` now print the exact `gcloud projects add-iam-policy-binding` template with a time-bound condition expression — you paste it, you're elevated. No silent failure.
- **GCP per-project cost via BigQuery export**: if `CLOUDNAV_GCP_BILLING_TABLE=project.dataset.table` is set, `c` on the projects view runs a `bq query` against the export and renders MTD cost per project. Absent env var shows a clear pointer to the setup docs.
- **`cloudnav pim`** grew a `--cloud azure|aws|gcp` flag so the CLI is symmetric with the TUI. Defaults to Azure.

### Fixed
- Nothing regressed — all earlier keybindings / CLI verbs run green on the full smoke suite.

[0.4.0]: https://github.com/tesserix/cloudnav/releases/tag/v0.4.0

## [0.3.0] — 2026-04-17

Multi-cloud cost, PIM activation in the TUI, and the palette that searches every sub/project/account.

### Added
- **Global quick-search** inside the `:` palette. On open we load every provider's top-level entities (subs / projects / accounts) in parallel; typing filters across name and id. Picking an entity jumps straight to it in one keystroke.
- **Deep-restore of bookmarks** — saved breadcrumbs are now walked level-by-level with the cursor landing on the exact target.
- **Cost column** on all three clouds: Azure RGs, AWS regions, GCP projects. Each row includes month-over-month delta (↑/↓/→) when last-period data is available. GCP surfaces a "BigQuery billing export needed" message cleanly.
- **PIM activation inside the TUI**: `p` opens a selectable list, `j/k` move cursor, `+/-` change duration, `a` asks for a justification and submits — all without leaving cloudnav.
- Non-Azure `p` / `c` presses now surface concrete guidance instead of silent no-ops.

### Fixed
- Table cursor underflow after `SetRows(nil)` that silently swallowed the Enter key on the home page when certain operations cleared rows.

[0.3.0]: https://github.com/tesserix/cloudnav/releases/tag/v0.3.0

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

# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.22.41] — 2026-04-25

### Added — currency conversion via frankfurter.app

- **Display every cost in the currency of your choice.** New
  `internal/currency` package wraps the public frankfurter.app
  feed (free, ECB-backed, no API key) and re-denominates cost
  amounts at format time. ISO 4217 currency codes; falls back to
  the cloud's native currency on FX failures so cost rendering
  never blocks on the network.
- **Three ways to set the display currency:**
  - `display_currency` field in `~/.config/cloudnav/config.json`
    (e.g. `"display_currency": "GBP"`).
  - `--currency` flag on `cloudnav cost` (and every subcommand).
  - Process-wide override via `currency.SetDefault` (used by the
    TUI bootstrap; future runtime hotkey hooks here).
- **SQLite-cached rate tables.** New `fx-rates` bucket in
  `cloudnav.db`; 24-hour TTL because ECB updates daily. The same
  rate table is reused across providers in one process, and the
  same rates persist across cloudnav launches inside the TTL
  window.
- **Coalesced concurrent fetches.** Two cost overlays opening at
  the same time don't fire two HTTP requests — the second waits
  on the first via a per-base inflight channel.
- **Compile-time-safe wiring.** Each provider has a `fx.go`
  wrapper that calls `currency.ConvertDefault`, so the
  formatters' dependency surface is one short call. No global
  type assertions or import cycles.
- 8 new tests in `internal/currency/converter_test.go` cover
  same-currency no-op, empty-display passthrough, nil-converter
  safety, round-trip via stub server, unknown-target fallback,
  runtime `SetDisplay`, cache hit count (5 calls → 1 fetch), and
  the `LatestRates` base-as-1.0 normalisation.

## [0.22.40] — 2026-04-25

### Added — AWS SDK migration phases 3, 4, 5, 7, 8 (final batch)

- **Phase 3 — Cost Explorer + Budgets SDK.** `cloud.aws.com/.../
  costexplorer` powers `Costs()`, `fetchCostGroupedBy()`,
  `fetchCostForecast()`. `budgets` powers `fetchAccountBudget()`.
  Single client connection, typed row scanning, no subprocess.
- **Phase 4 — CloudWatch SDK.** `cloudwatch.GetMetricData` typed
  end-to-end. Reducer choice (`Average` over 5-min windows)
  matches the CLI verbatim so sparkline shape doesn't shift.
- **Phase 5 — Compute Optimizer SDK.** Enrollment status probe +
  EC2 instance recommendations + Cost anomalies all routed
  through the SDK with manual paginator (Compute Optimizer's API
  shape doesn't ship a generated paginator). Trusted Advisor
  stays on the CLI for now — it's a paid-support-plan feature
  that few users hit.
- **Phase 7 — `provider.CostHistoryer` for AWS.** Wires the `$`
  overlay to AWS at parity with Azure / GCP. Daily / monthly
  granularity via `GetCostAndUsage`; weekly bucket rolled up in
  process because Cost Explorer doesn't expose a weekly
  granularity natively. Single-account scope via the
  `LINKED_ACCOUNT` dimension filter.
- **Phase 8 — `HealthEventer` + `Deleter` for AWS.**
  - `HealthEventer` via `health.DescribeEvents`. Lights up the
    `H` overlay on AWS at parity with Azure / GCP. Free-tier
    accounts get a quiet empty list (Health is paid-support).
  - `Deleter` dispatcher routes EC2 instance terminations
    through `ec2.TerminateInstances` and S3 bucket deletions
    through `s3.DeleteBucket`. Other resource types return
    `ErrNotSupported` with a portal hand-off hint — same shape
    as the GCP per-asset-type dispatcher.
  - Locker explicitly deferred — AWS has no built-in lock
    primitive. The closest analog is IAM service-control
    policies, which are policy-shaped not resource-shaped.
    Documented as out-of-scope for this migration.

### Cross-cloud capability matrix — final state

| Capability | Azure | GCP | AWS |
|---|---|---|---|
| Provider / nav | ✅ | ✅ | ✅ |
| Coster / Billing | ✅ | ✅ | ✅ |
| Advisor | ✅ | ✅ | ✅ |
| PIMer | ✅ | ✅ | ✅ |
| Metricser | ✅ | ✅ | ✅ |
| VMOps | ✅ | ✅ | ✅ |
| CostHistoryer (`$`) | ✅ | ✅ | ✅ |
| HealthEventer (`H`) | ✅ | ✅ | ✅ |
| Deleter (`D`) | ✅ | ✅ | ✅ |
| Locker (`L`) | ✅ | ✅ (liens) | ⚠️ deferred |
| BillingScoped | ❌ | ✅ | ✅ |
| Installer / Loginer | ✅ | ✅ | ✅ |

Every cross-cloud capability except Locker is now at parity. The
TUI's `D`, `H`, `$`, `B` overlays light up identically across all
three clouds.

## [0.22.39] — 2026-04-25

### Added — GCP cost chart + AWS SDK migration begins

- **GCP `CostHistory` / `$` overlay parity.** Implements
  `provider.CostHistoryer` for GCP via BigQuery daily-grouped
  query against the billing-export table. Brings the `$` cost
  chart to GCP at parity with Azure. Day / week / month buckets;
  scope filter for single-project drilling; gap-fills missing
  days with zero so the line reads continuously.
- **AWS SDK migration phases 1, 2, 6.**
  - **Phase 1 — Foundation.** `aws-sdk-go-v2/config` + `sts` +
    `organizations` + `ec2` + `resourcegroupstaggingapi`. `Root()`,
    `LoggedIn()`, `regions()`, and `resources()` all routed
    through the typed SDK with CLI fallback on auth failure.
  - **Phase 2 — EC2 VM ops.** `ListVMs` / `ShowVM` / `StartVM` /
    `StopVM` via `ec2.Client`. Pagination handled natively.
  - **Phase 6 — AWS SQLite cache layer.** Two new buckets:
    `aws-root` (account list, 10-min TTL) and `aws-resources`
    (per-region drill, 5-min TTL). Mirrors `azure-root` and
    `gcp-root`. Cache key includes a fingerprint of
    `~/.aws/config` + `~/.aws/credentials` + `AWS_PROFILE` /
    `AWS_REGION` env vars so `aws sso login` /
    `export AWS_PROFILE=other` auto-invalidate the cache.
  - `InvalidateRootCache()` method on `*AWS` mirrors the Azure
    and GCP same-named methods.

### Roadmap (`docs/aws-sdk-migration.md` to come; tracked in tasks)

- ✅ Phase 1 — Foundation
- ✅ Phase 2 — EC2 VM ops
- ✅ Phase 6 — Cache layer
- 🚧 Phase 3 — Cost Explorer + Budgets SDK
- 🚧 Phase 4 — CloudWatch SDK
- 🚧 Phase 5 — Advisor SDK (compute-optimizer + support)
- 🚧 Phase 7 — `CostHistoryer` for AWS
- 🚧 Phase 8 — `HealthEventer` (AWS Health) + `Deleter` (best-effort EC2 / S3)

## [0.22.38] — 2026-04-25

### Added — GCP cache parity with Azure (everything in SQLite)

- **`gcp-root` SQLite bucket** caches the GCP `Root()` enumeration
  (projects, or folders when `CLOUDNAV_GCP_ORG` is set). Mirrors
  the `azure-root` bucket; key is `(org, gcloud-cred-fingerprint)`
  so a switch to a different gcloud account or a different org
  invalidates the cache automatically. 10-min TTL — same as the
  Azure side. Override via `CLOUDNAV_GCP_CACHE_TTL`; opt-out with
  `CLOUDNAV_GCP_NO_CACHE=1`.
- **`gcp-assets` SQLite bucket** caches per-project Asset Inventory
  drills. Key is `(projectID, sorted asset types)` so the same
  drill in any order is one cache hit. 5-min TTL because resources
  churn faster than projects.
- `InvalidateRootCache()` method on `*GCP` mirrors the Azure
  same-named method so the TUI's `r` refresh path works
  identically across clouds.
- `gcloudCredFingerprint()` derives a 16-char SHA-256 prefix from
  `~/.config/gcloud/active_config` + ADC JSON + `credentials.db`,
  so `gcloud auth login` / `gcloud config set account ...` /
  `gcloud config configurations activate <other>` all auto-bust
  the cache.
- 9 new tests in `internal/provider/gcp/cache_test.go`: round-trip,
  org isolation, TTL expiry, opt-out flag, manual invalidation,
  asset-cache key order-invariance, project isolation, asset
  round-trip, and a sanity-check that `cache.Shared()` resolves to
  `*SQLiteBackend` by default (the headline "everything in SQLite"
  assertion).
- E2E (`test/e2e/gcp_parity_test.sh`) now asserts
  `<cache>/cloudnav.db` exists after the GCP CLI commands run, so
  a future regression that silently fell back to JSON would fail
  the build.

### What this means in practice
After this release, every GCP feature path that was hitting a wire
or a subprocess on every launch — project list, folder traversal,
asset drill, cost lookup, PIM list — caches into the same
`cloudnav.db` file as the Azure caches. Second launches inside
the TTL window are <10 ms cold-start to "table populated".

## [0.22.37] — 2026-04-25

### Added — GCP SDK migration phases 9-12 + Service Health + Budgets

- **Phase 9 — Folders SDK.** Org-level folder traversal via
  `resourcemanager.FoldersClient.ListFolders`; sub-projects under
  a folder via `SearchProjects` with `parent:folders/<id>` filter.
  Replaces three `gcloud resource-manager folders ...` shells.
- **Phase 10 — Privileged Access Manager SDK.** PAM entitlements
  + grants via `cloud.google.com/go/privilegedaccessmanager/apiv1`.
  The `p` PIM overlay now talks to PAM through the typed SDK; the
  conditional-IAM fallback path stays on gcloud since it's a
  diagnostic, not a hot path.
- **Phase 11 — ADC-based `LoggedIn` check.** Drops the
  `gcloud auth list` subprocess from cloudnav startup. Uses
  `golang.org/x/oauth2/google.FindDefaultCredentials` and mints
  one token to verify the creds work end-to-end. ~250ms shaved
  from every cold start.
- **Phase 12 — Cloud Billing SDK.** `GetProjectBillingInfo` via
  `cloudbilling.v1` for the cost auto-detect path. Used by both
  `Costs()` and `BillingSummary()`.
- **Service Health for GCP.** New
  `cloud.google.com/go/servicehealth/apiv1` integration brings
  GCP to parity with Azure / AWS on the `H` overlay. `HealthEvents`
  now satisfies `provider.HealthEventer` across all three clouds.
- **Cloud Billing Budgets.** `B` overlay now reads real budget caps
  via `cloud.google.com/go/billing/budgets/apiv1.ListBudgets`
  instead of parsing CLI JSON. Currency comes from the SDK Money
  type — no more silent currency-mismatch bugs.

### Cross-cloud navigation reference
Documented in `docs/gcp-sdk-migration.md`:

| Cloud | Hierarchy |
|---|---|
| Azure | Tenant → Subscription → ResourceGroup → Resource |
| GCP   | Organization → Folder → Project → Resource |
| AWS   | Organization → OU → Account → Region → Resource |

cloudnav normalises these into the same Kind enum + drill path so
users press the same keys for the same operations everywhere.

## [0.22.36] — 2026-04-25

### Added — GCP SDK migration, phases 5 + 6 + 7 + 8 (final batch)

- **Phase 5 — BigQuery cost SDK.** `cloud.google.com/go/bigquery`
  now backs the per-project cost query both for the `c` column on
  the projects view and the new `cloudnav cost projects` CLI.
  Typed row scanning, single client connection, no subprocess.
  Host project resolved automatically from
  `<project>.<dataset>.<table>`. Falls back to `gcloud bq query`
  when ADC isn't usable.
- **Phase 6 — `provider.Deleter` cross-cloud interface.** Formal
  shape so the TUI's `D` (delete) overlay no longer
  type-asserts to `*azure.Azure`:
  - Azure adapter wraps `DeleteResource` / `DeleteResourceGroup`,
    extracting the subscription id from the Node's parent or
    Meta. Returns `provider.ErrNotSupported` for kinds without
    an Azure-side delete.
  - GCP per-asset-type dispatcher: compute instances (via
    `Instances.Delete` SDK + LRO `op.Wait`), storage buckets
    (via `bucket.Delete` SDK), projects (via Resource Manager
    v3 `DeleteProject`, which respects liens). Anything else
    returns `ErrNotSupported` with a portal hand-off hint.
- **Phase 7 — `provider.Locker` + GCP liens.** Liens are GCP's
  closest analog to Azure management locks; project-scoped,
  block any RPC carrying the named restricted permission.
  cloudnav now implements:
  - `Locks(node)` — list active liens.
  - `CreateLock(node, reason)` — block
    `cloudresourcemanager.projects.delete`.
  - `RemoveLock(node, name)` — drop a lien by resource name.
  Resources inside a project return an empty Lock list (not an
  error) so the L overlay renders cleanly. Implemented via
  `gcloud alpha resource-manager liens` because the v3 Go SDK
  doesn't expose Liens (only v1 REST does, which would mean a
  second auth pool).
- **Phase 8 — Cloud Monitoring SDK.** `cloud.google.com/go/
  monitoring/apiv3/v2` powers the `m` Metricser overlay.
  Reducer / aligner choices match the gcloud CLI path verbatim
  (`AlignmentPeriod=300s`, `ALIGN_MEAN` for gauges,
  `ALIGN_RATE` for byte counters) so sparkline shape doesn't
  shift between the two paths. Same lazy + cached-error
  pattern; gcloud fallback on auth failure.
- New compile-time assertions: `var _ provider.Deleter =
  (*Azure)(nil)`, `(*GCP)(nil)`; same for `Locker`. A future
  refactor that drops a method now fails the build instead of
  silently breaking the TUI.

### Roadmap progress (`docs/gcp-sdk-migration.md`)
All 8 phases are now ✅. The migration is complete; future GCP
work lands as feature additions on top of the SDK foundation.

## [0.22.35] — 2026-04-25

### Added — GCP SDK migration, phase 3 + 4 (Compute + Recommender)

- **`cloud.google.com/go/compute/apiv1` SDK fast path** for VM
  operations. `ListVMs` now uses Compute Engine's
  `AggregatedList` RPC — one call to enumerate every zone's
  instances instead of N per-zone fan-outs. `StartVM` / `StopVM` /
  `ShowVM` route through the SDK with full LRO `op.Wait` so the
  caller's status is accurate. Falls back to `gcloud compute
  instances ...` on any SDK failure.
- **`cloud.google.com/go/recommender/apiv1` SDK fast path** for
  the Advisor overlay. `ListRecommendations` for each entry in
  the per-project recommender catalog (10 IDs across cost /
  performance / security / reliability), running in parallel
  with the same 6-way semaphore the gcloud path uses. Priority
  enum (`P1`/`P2`/`P3`/`P4`) maps to the high/medium/low impact
  labels the TUI advisor card already renders.
- Per-file lazy + cached-error pattern means a failed auth probe
  doesn't keep re-paying latency on subsequent calls.
- `g.Close()` now releases the Compute and Recommender clients
  alongside Resource Manager / Asset Inventory.

### Roadmap progress (`docs/gcp-sdk-migration.md`)
- ✅ Phase 1 — Foundation (Resource Manager + project listing)
- ✅ Phase 2 — Asset Inventory (resource enumeration)
- ✅ Phase 3 — Compute SDK (VM ops)
- ✅ Phase 4 — Recommender SDK (Advisor)
- 🚧 Phase 5 — Billing + BigQuery SDK (deferred — `bq query`
  works fine and BQ auth patterns aren't reused elsewhere yet)
- 🚧 Phase 6 — Delete dispatcher (needs cross-cloud `Deleter`
  interface refactor in `provider.go`)
- 🚧 Phase 7 — Project liens (lock equivalent — depends on
  Phase 6's refactor for the UI hook-up)
- 🚧 Phase 8 — Monitoring SDK

## [0.22.34] — 2026-04-25

### Added — GCP SDK migration, phase 1 + 2 (foundation + Asset Inventory)

- **`cloud.google.com/go/resourcemanager/apiv3` SDK fast path** for
  `Root()` / project listing. Authenticated via Application Default
  Credentials (the same source `gcloud` reads from), reuses one
  HTTP/2 connection per process, returns typed errors. Falls back
  to `gcloud projects list` when ADC isn't available so cloudnav
  stays usable on hosts without service-account creds.
- **`cloud.google.com/go/asset/apiv1` SDK fast path** for the
  resource enumeration drill. Asset Inventory `SearchAllResources`
  via the SDK is ~3× faster than spawning `gcloud asset
  search-all-resources` per drill on a 5k-asset project; same
  whitelist + page-limit semantics. Falls through to the gcloud
  CLI on any SDK failure (no ADC, API not enabled, transient
  network).
- Lazy-initialised SDK clients under `g.sdk` with one-shot error
  caching so a failed auth probe doesn't keep re-paying latency.
- `Close()` now releases both SDK client connections cleanly.
- Tests for `splitCSV`, `lastSegment`, `parseProjectNumber` and
  the SDK-not-NPE contract.

### Roadmap progress (`docs/gcp-sdk-migration.md`)
- ✅ Phase 1 — Foundation (Resource Manager + project listing)
- ✅ Phase 2 — Asset Inventory (resource enumeration)
- 🚧 Phase 3 — Compute SDK (VM ops)
- 🚧 Phase 4 — Recommender SDK (Advisor)
- 🚧 Phase 5 — Billing + BigQuery SDK
- 🚧 Phase 6 — Delete dispatcher
- 🚧 Phase 7 — Project liens (lock equivalent)

## [0.22.33] — 2026-04-25

### Added
- **`cloudnav cost projects`** — per-project month-to-date cost for
  GCP, sourced from the BigQuery billing-export table. Honours the
  same `--json` / `--match <substring>` / `--limit N` flags as
  `cost subs`. Falls back to the setup deeplink when no billing
  export is configured. Closes the GCP gap in the cost CLI matrix
  (Azure: `subs` / `rgs`; AWS: `regions` / `services`; GCP: now
  `projects`).
- `docs/gcp-sdk-migration.md` — concrete roadmap for replacing the
  remaining `gcloud.Run` shells with the official Go SDKs
  (`resourcemanager`, `asset`, `compute`, `recommender`, `billing`,
  `bigquery`, `iam`, `monitoring`), plus design notes for the
  Azure-equivalent **delete dispatcher** (per-resource-type SDK
  calls instead of a single delete RPC) and **lock model** (project
  liens via `Liens.Create`, scoped at the project boundary). Lays
  out a 7-step shippable PR sequence so each session picks up at
  the next unchecked step.

### Tests
- `internal/cmd/cost_test.go` pins the `cost projects` subcommand
  registration, flag declarations, and help-text references to
  BigQuery and `CLOUDNAV_GCP_BILLING_TABLE` so a future refactor
  can't silently drop them.
- `test/e2e/gcp_parity_test.sh` adds an e2e block for `cost
  projects` — passes either when BigQuery export is configured
  (table format) or when it isn't (setup-deeplink hint).

## [0.22.32] — 2026-04-25

### Changed
- **Every cache now flows through SQLite.** Two stragglers were
  still writing raw JSON next to `cloudnav.db`:
    * `~/Library/Caches/cloudnav/azure-root.json` — the cross-tenant
      subscription enumeration cache (~200 KB on a 671-sub user).
    * `~/Library/Caches/cloudnav/update-check.json` — the GitHub
      release-poll cache.
  Both packages now use `cache.Store[T]` against the new
  `cache.Shared()` singleton, so the cross-tenant Root snapshot and
  the update-check payload land in the same `cloudnav.db` as the
  cost / pim / rgraph rows. One open SQLite handle, one set of WAL
  files, one place to look when debugging.
- New `cache.Shared()` returns the process-wide backend so all
  subsystems agree on the same `*sql.DB` pool. Lazy-initialised on
  first call.

### Removed
- The bespoke read/write/atomic-rename code in
  `internal/provider/azure/root_cache.go` and
  `internal/updatecheck/updatecheck.go`. Both files lost ~80 lines
  apiece and gained nothing in behaviour — the SQLite-backed
  `Store[T]` already handles atomicity, TTL, and corrupted-payload
  misses.

### Migration
- Existing `azure-root.json` and `update-check.json` files become
  orphans; safe to `rm` after upgrade. cloudnav repopulates the
  SQLite rows on first use.

## [0.22.31] — 2026-04-25

### Added
- **Auto-install Zellij when missing.** `cloudnav workspace` no
  longer just prints an install hint when the `zellij` binary
  isn't on PATH — it runs the right install command for the host
  (Homebrew → `brew install zellij`, falling back to
  `cargo install --locked zellij` on Linux without brew). The
  workspace launches as soon as the install completes.
- New explicit subcommands for the same paths:
  - `cloudnav install zellij` — first-time install via the host's
    package manager.
  - `cloudnav upgrade zellij` — wraps `brew update && brew upgrade`
    or `cargo install --locked --force` so a stale formula cache
    can't no-op the upgrade (same fix shape as the cloudnav self-
    upgrade in 0.22.5).
- `internal/tools/` package factors the install / upgrade plan
  shape into a small `Tool` struct with per-OS `PlanFn` /
  `Upgrade` callbacks, so the next dependency (atuin / gum / etc.)
  is one `var` away.
- 11 new tests across `internal/tools` and `internal/cmd` cover
  the brew-vs-cargo preference order, the Windows refusal, idempotent
  `Ensure`, action routing, and the install dispatch's separation
  between cloud names (provider path) and tool names (tools path).

### Changed
- `cloudnav install` now accepts `zellij` alongside `azure` /
  `aws` / `gcp`. Cloud names still flow through
  `provider.Installer`; tool names dispatch through the new
  `internal/tools` package.

## [0.22.30] — 2026-04-25

### Added
- **`cloudnav workspace` — opt-in Zellij integration.** Launches the
  TUI inside an isolated Zellij session whose theme mirrors the
  cloudnav palette (purple modal accent, sky network category, cyan
  app title, etc.). Layout + theme + Zellij config are written to
  `<UserConfigDir>/cloudnav/zellij/` and selected via
  `zellij --config-dir`, so the user's own `~/.config/zellij` stays
  completely untouched. Standalone `cloudnav` is unchanged — the
  workspace command is purely additive.
- Detects when invoked from inside an existing Zellij session
  (`$ZELLIJ`) and refuses to nest, pointing the user at plain
  `cloudnav` instead.
- Refuses on Windows with a clear error (Zellij isn't supported
  there); macOS / Linux print install hints (`brew install zellij`
  / `cargo install --locked zellij`) when the binary is missing.
- `internal/cmd/workspace_test.go` covers the config-dir override,
  default-path suffix safety (so we never write into the user's
  ~/.config/zellij), idempotent file write, and asserts the
  embedded layout + theme stay in sync with the cloudnav palette.

## [0.22.29] — 2026-04-25

### Added
- **Persistent Resource Graph snapshot cache.** Drilling into a
  multi-RG selection used to fire a fresh KQL query against Azure
  Resource Graph every time — 2–5 seconds for 100+ RGs in a busy
  subscription. Snapshots now land in a new `rgraph` cache bucket
  keyed by `(subID, sorted RG names)` so the same selection in any
  order is one cache hit. Repeat drills inside the 10-min TTL load
  in <10 ms; the table flips back instead of showing the spinner.
- The `r` refresh key now clears the rgraph bucket so users still
  have a one-keystroke way to force a re-fetch when resources have
  changed.
- `internal/tui/nav_test.go` covers the cache key (order-invariant,
  varies by sub, varies by RG set), round-trip through the SQLite
  backend, and the clear-on-refresh contract.

## [0.22.28] — 2026-04-25

### Changed
- **SQLite is now the default cache backend.** Cost lookups, PIM
  role snapshots, update-check polls, and any other persisted
  payloads land in a single `<CLOUDNAV_CACHE>/cloudnav.db` (WAL
  journaling, `(bucket, key) WITHOUT ROWID`, indexed by bucket).
  The previous JSON-per-key cache directory remains on disk and is
  safe to delete; cloudnav will repopulate the SQLite store on next
  use.
- Opt out with `CLOUDNAV_CACHE_BACKEND=json` (or `files` / `off`)
  if you need a `cat`-able cache or you're on a read-only filesystem
  where SQLite can't open WAL.

## [0.22.27] — 2026-04-24

### Added
- **Pluggable cache backend with optional SQLite store** (now the
  default in 0.22.28). Refactored the cache layer around a
  `Backend` interface; the existing JSON-per-key file store is now
  `*JSONBackend` and a new `*SQLiteBackend` ships alongside it.
  SQLite uses WAL journaling, a single
  `<CLOUDNAV_CACHE>/cloudnav.db` file, and a
  `(bucket, key) WITHOUT ROWID` schema with `idx_cache_bucket` for
  per-bucket clears. Driver: `modernc.org/sqlite` — pure Go, no CGO.
- Parity test matrix (`TestBackendParity`) asserts both backends
  agree on `Get` / `Set` / `Delete` / `Clear`. SQLite-specific
  tests cover upsert behaviour, TTL, bucket-isolated `Clear`,
  concurrent writes, and persistence across reopen.

### Changed
- `Store[T]` now wraps a `Backend` instead of writing files directly.
  `cache.NewStore` keeps the JSON-rooted-at-`baseDir` shape for
  back-compat; new callers use `NewStoreWithBackend` to share one
  open backend across multiple buckets.

## [0.22.26] — 2026-04-24

### Fixed
- **Advisor popup frame is now pinned to a stable inner size**
  regardless of the current scroll position or the length of any
  individual card's problem / solution text. Previously the popup
  still visually shrank and grew because `styles.Modal` auto-sized
  to the longest rendered line and the tallest card — scrolling
  through a mix of short and long recommendations redrew the frame
  on every arrow key. The advisor body is now padded / truncated
  to a fixed `(innerW × innerH)` before it reaches the modal
  border, so the user sees a single constant frame.

### Added
- `internal/tui/advisor_test.go` covers the new `stableAdvisorBody`
  helper and `advisorInnerWidth` / `advisorInnerHeight` clamps, plus
  a regression test that scrolls through an 11-card advisor popup
  and asserts the outer frame dimensions stay constant at every
  scroll offset.
- E2E script now pins the `--category` / `--impact` flag help and
  the `--json` output contract.

## [0.22.25] — 2026-04-24

### Fixed
- **Advisor popup no longer shrinks / grows while scrolling.** Every
  recommendation card is now normalised to exactly four lines, the
  top and bottom scroll indicators each reserve a line whether or
  not they have text, and unused card slots are padded with blank
  four-line blocks. Total popup height is constant regardless of
  where the cursor is in the list.

### Changed
- **Centered section rule between resource context and cards.** The
  blank separator has been replaced with a centered
  `━━━ Recommendations (N) ━━━` divider so the cost / resource
  block and the card list read as two distinct sections.

## [0.22.10] — 2026-04-24

### Changed
- **Update check is now cache-first.** `Check()` serves from the disk
  cache when the last poll was within `pollInterval` (1 hour), and
  only hits GitHub past that. Stale cache (> 24 h) is still used as
  an offline fallback. Previously every cloudnav launch hit
  `/releases/latest`, which burned the anonymous 60/hour quota during
  active development and turned the update pill off while a new
  release was in fact live.
- **`U` now forces a fresh re-check.** `CheckForce()` bypasses the
  cache and always hits GitHub, so pressing `U` right after a new
  tag ships surfaces it immediately instead of waiting up to an hour.

## [0.22.9] — 2026-04-24

### Added
- `docs/config.md` documents every field in `config.json` plus the
  cache paths and environment-variable overrides. `auto_upgrade`
  finally has a published spec.
- Unit tests pin the homebrew upgrade plan shape (`sh -c "brew update
  && brew upgrade cloudnav"`), `isHomebrewBinary` / `isGoBinBinary`
  detection, `trimOutput` length cap, and the delete helpers
  (`deleteNoun`, `failuresToErr`, `stateBadge`).
- `test/e2e/upgrade_test.sh` asserts `cloudnav version` prints a
  parseable first line — `installedVersion()` depends on that shape.

## [0.22.8] — 2026-04-24

### Fixed
- **Post-upgrade version verification.** After the upgrade command
  exits 0, cloudnav invokes the binary on `PATH` and parses its
  version output. If the version didn't actually move to the target
  tag (e.g. a silent brew no-op), surface a clear failure instead of
  a misleading "upgrade complete" banner, with a remediation hint
  pointing to `brew update && brew upgrade cloudnav`.

## [0.22.7] — 2026-04-24

### Fixed
- **Silent no-op on the Homebrew upgrade path.** `brew upgrade
  cloudnav` alone consults the local formula cache — without a prior
  `brew update` brew reports "already installed" even when a newer
  formula exists. The upgrade command exited 0, cloudnav marked it
  successful, but the binary on disk was unchanged. Wrap the plan as
  `sh -c "brew update && brew upgrade cloudnav"` so the formula cache
  is refreshed first. The confirmation overlay unwraps `sh -c` for
  display so users still see a clean command line.

## [0.22.6] — 2026-04-24

### Added
- **Autonomous auto-upgrade path.** When `config.AutoUpgrade` is true
  AND a newer release is detected on startup, cloudnav now: runs the
  plan silently, and on success automatically re-execs the fresh
  binary in place. User sees one "auto-upgrading to vX.Y.Z…" flash in
  the footer, then cloudnav reopens on the new version — no
  keystrokes required. Manual path (`U` → `y` → `R`) unchanged.

## [0.22.5] — 2026-04-24

### Added
- **One-key self-relaunch after upgrade.** Press `R` (or `↵`) on the
  post-success overlay and cloudnav re-execs the freshly-installed
  binary in place of the current process. POSIX: `syscall.Exec`
  preserves the PID / stdio / cwd. Windows: spawns a fresh child with
  the same stdio, exits the parent.

## [0.22.4] — 2026-04-24

### Changed
- **Loud top-right update pill.** When a newer release is available
  the header renders a reversed-video pill — yellow bg, dark fg, bold
  — instead of the old muted warning text: `[ ↑ v0.22.x available —
  press U ]`. Impossible to miss against the breadcrumb row.
- **`U` is always visible in the keybar.** Promoted to the front
  ("upgrade now") when an update is detected, at the tail ("check
  updates") otherwise. Pressing `U` when no update is known triggers
  a fresh GitHub lookup.

## [0.22.3] — 2026-04-24

### Fixed
- **Purple splash-screen below the table after a delete.** `Shell`
  appended sections without emitting an ANSI reset, so the last row's
  selected-cursor background leaked onto every pad line. Now emits
  `\x1b[0m` between header / body / footer and each pad line.
- **`…──0m` artefacts in the STATE cell.** `bubbles/table` truncates
  via `runewidth.Truncate` which walks escape codes as runes and cuts
  mid-sequence. Shorten `stateBadge` to plain "Deleting" (no `⟳`
  glyph) so the 12-cell STATE column never needs to truncate.
- **Table flash-replaced by the full-screen loading panel during the
  post-delete reload.** Split `load()` into `loadInto(drill bool)` —
  `reload()` now passes `drill=false` so the table stays visible and
  only the footer spinner moves.

## [0.22.2] — 2026-04-24

### Added
- **Cross-tenant subscription + PIM discovery.** The portal shows
  every tenant a user is a member of because it mints per-tenant
  tokens on the fly. `DefaultAzureCredential` only covers the
  currently-selected `az` tenant, so cross-tenant guest memberships
  were silently dropped. `allTenants()` merges three discovery
  sources (`az account tenant list`, ARM `/tenants`, per-sub
  tenantIds) and `Root()` fans out a tenant-scoped `/subscriptions`
  call in parallel, dedupes by sub id, returns the union. PIM uses
  the same discovery set so roles in cross-tenant directories are
  enumerated or surface as diagnostic rows.

## [0.22.1] — 2026-04-24

### Fixed
- **Blank TENANT column.** `armsubscription.Subscription` omits
  `TenantID` from its typed model, so after the SDK migration every
  `Meta["tenantId"]` came back empty and the tenant-name lookup
  missed. Hit the raw ARM `/subscriptions?api-version=2022-12-01`
  endpoint with an SDK-minted token instead — same URL `az account
  list` uses underneath, returns every field including `tenantId`,
  no process spawn. Dropped the `armsubscription` import entirely.

## [0.22.0] — 2026-04-24

### Changed
- **Azure SDK migration complete.** The remaining `az` CLI call sites
  — tenant discovery, `LoggedIn` probe, `Details` (resource / sub /
  resource group), VM list / show / start / stop, and tenant-for-sub
  lookup — now use the Azure SDK or direct ARM REST with SDK-minted
  tokens. `az.Run` remains only as the fallback inside each SDK-first
  path, plus the intentional `az login` handoff for interactive auth
  and one `account list` in PIM that needs `TenantID` per sub (omitted
  from the SDK `Subscription` model). Adds `armcompute/v5` (~2 MB) for
  VM operations.

### Fixed
- **PIM scope names resolve for sub IDs you don't own directly.**
  Previously a PIM eligibility for a subscription not in your
  `az account list` rendered as `/subscriptions/<guid>...` in the scope
  column. One Resource Graph query against `resourcecontainers` now
  hydrates those names the first time you open the PIM view; resolved
  names are cached in-process for the session.

## [0.21.1] — 2026-04-24

### Fixed
- Visible delete feedback: sticky green confirmation banner in the
  footer (survives the auto-refresh) and a coloured `⟳ Deleting`
  badge in the STATE column so you can see that a delete request
  actually went through. `esc` dismisses the banner.

## [0.21.0] — 2026-04-24

### Added
- **Azure SDK migration — phases 1–3.** `azcore` + `azidentity` +
  `armsubscription` + `armresources` + `armlocks` replace the hot
  `az`-shell paths across auth, subscriptions, resource groups,
  resources, locks, and resource-group deletion.
  `DefaultAzureCredential` reads the `az login` cache in-process so
  token acquisition no longer spawns a subprocess per tenant. Every
  SDK path keeps a CLI fallback for when the credential chain can't
  resolve (no cached login, az not installed). Cost Management queries
  stay on hand-rolled REST calls because they already flow through the
  shared retrying `http.Client` — the `armcostmanagement` SDK would
  add binary weight without a perf change.
- **Azure Resource Graph fast path** for multi-RG drills. When `D`-ing
  or drilling into several selected RGs, one KQL POST returns every
  resource across them instead of N sequential `az resource list`
  calls. 10-RG drills drop from ~15 s to ~1–2 s. Falls back to the
  per-RG walk when the caller lacks sub-level reader.
- **Resource-level multi-select delete.** `D` now works on the
  resources view as well as resource groups. Each target is removed via
  a direct ARM `DELETE`, up to 8 in parallel. Errors are collected
  per-target with the resource name, so the status says
  `foo: cannot delete while attached` instead of `2 failures`. The
  confirmation overlay adapts heading / disclaimer to match the scope.
- **Persistent cost cache** at `~/.cache/cloudnav/costs/`. A restart
  serves the cost column from disk instead of repeating Cost Management
  queries. 15-minute TTL; JSON-per-key files with atomic tmp-and-rename
  writes.
- **Opt-in auto-upgrade.** Set `auto_upgrade: true` in config.json to
  have cloudnav run the detected `go install` or `brew upgrade` plan
  silently at startup when a newer release ships on GitHub. The manual
  `U` flow is unchanged.
- **ANSI-aware overlay compositor.** Modals (help / delete / upgrade /
  palette) now render over the list view with the table still visible
  behind them, instead of replacing the whole screen.
- **Reusable layout components** under `internal/tui/components`:
  `Shell` guarantees full-terminal fill, plus `Breadcrumb`, `Keybar`,
  `Modal`, `Composite`. Styles consolidated in `internal/tui/styles`.
- **TUI integration tests** (`internal/tui/integration_test.go`) cover
  shell height math, keybar wrapping, overlay open/close, window
  resize — no cloud access required.
- **`cloudnav find` subcommand** for discovery-first lookups
  (scopes / resources / pim) when you know part of a name but not the
  exact path. Short aliases added: `list` → `ls`, `costs` → `cost`,
  `jit` → `pim`.

### Changed
- **TUI package carved into feature files.** The 4700-line `app.go`
  split into `advisor.go`, `billing.go`, `costs.go`, `delete.go`,
  `detail.go`, `health.go`, `help.go`, `locks.go`, `login.go`,
  `metrics.go`, `nav.go`, `palette.go`, `pim.go`, `search.go`. `app.go`
  now hosts only the model, Update dispatch, main View, and table
  rendering.
- **PIM tokens cached in-memory** per (tenant, audience) until ~2 min
  before expiry. Second PIM list in the same session no longer re-hits
  the credential chain.
- **All Azure REST paths share a single HTTP client** with keep-alive,
  HTTP/2, and a per-host connection pool. Requests run through
  `doWithRetry` which honours `Retry-After` on 429 / 503 / 5xx up to 3
  attempts, respects the request context.
- **API errors show the real reason first.** `trimAPIErr` unwraps the
  `{"error":{"code","message"}}` envelope that Azure and Graph use, so
  the status bar renders `AuthorizationFailed: ...` instead of a
  truncated HTTP URL. Applied to the Azure REST, PIM activation, Graph
  POST, Cost Management, and `cli.Runner` paths.
- **Subscription cost fetch parallelised.** Within one subscription,
  the current / last-month / forecast / budget queries now run
  concurrently. Cost column loads ~3× faster on tenants with many
  visible subs.
- **Adaptive resource column widths.** On narrow terminals `COST (MTD)`
  no longer clips — `TAGS` absorbs the slack, `HEALTH` is dropped on
  <80-cell budgets, `TYPE` / `LOCATION` / `CREATED` step down in tiers.

### Fixed
- `cli.Runner` error format flipped so the stderr reason is line one
  and the command that ran goes on line two. The TUI status bar
  (which truncates by terminal width) now shows something useful
  instead of the command with the reason chopped off.
- Atomic `updatecheck` cache write (tmp + rename) fixes torn JSON when
  two `Check()` calls race.
- TUI context threaded through `Run()` with `context.WithCancel` so
  quit cancels in-flight provider calls instead of leaking goroutines.

## [0.6.0] — 2026-04-18

### Added
- **Azure resource-group lock visibility and management**. On the RG view cloudnav fetches `az lock list` once per subscription and adds a LOCK column (🔒 CanNotDelete / 🔒 ReadOnly / —). Press `<L>` on a locked RG to remove its first lock; press `<L>` on an unlocked RG to create a `cloudnav-protect` CanNotDelete lock. Changes are reflected instantly after re-fetch.
- **Multi-select + bulk delete of resource groups**. On the RG view `space` toggles a ● marker on the cursor row, `[` selects all currently visible rows, `]` clears the selection. `<D>` asks Azure to delete the selected RGs (async, `--no-wait`) — refuses with an explanatory status if any selected RG still has a lock, so you have to `L` to unlock first. The keybar shows `<D> delete N` only when a selection exists.
- Per-row delta arrows on the cost column are now colour-coded (green ↓, red bold ↑, grey →) in both Azure RG and AWS region views.
- Subscription-level cost on the Azure subs view: press `<c>` on the subs list to get an MTD column with MoM arrows across every visible sub. Subs where the caller lacks Cost Management Reader are labelled "no cost read access" instead of a silent £0.
- Empty states are now context-specific — an empty RG says "no resources inside 'rg-foo'"; an empty sub-list suggests checking `az login`; etc.

### Fixed
- Table cell-count panic when navigating between views with different column counts — `refreshTable` now normalises every row to exactly `len(cols)` cells before calling `SetRows`.

[Unreleased]: https://github.com/tesserix/cloudnav/compare/v0.22.41...HEAD
[0.22.41]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.41
[0.22.40]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.40
[0.22.39]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.39
[0.22.38]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.38
[0.22.37]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.37
[0.22.36]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.36
[0.22.35]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.35
[0.22.34]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.34
[0.22.33]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.33
[0.22.32]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.32
[0.22.31]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.31
[0.22.30]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.30
[0.22.29]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.29
[0.22.28]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.28
[0.22.27]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.27
[0.22.26]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.26
[0.22.25]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.25
[0.22.10]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.10
[0.22.9]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.9
[0.22.8]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.8
[0.22.7]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.7
[0.22.6]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.6
[0.22.5]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.5
[0.22.4]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.4
[0.22.3]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.3
[0.22.2]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.2
[0.22.1]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.1
[0.22.0]: https://github.com/tesserix/cloudnav/releases/tag/v0.22.0
[0.21.1]: https://github.com/tesserix/cloudnav/releases/tag/v0.21.1
[0.21.0]: https://github.com/tesserix/cloudnav/releases/tag/v0.21.0
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

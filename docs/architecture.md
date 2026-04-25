# Architecture

cloudnav is three layers wired together by a navigation stack.

```
┌────────────────────────────────────────────┐
│                Bubbletea TUI               │  internal/tui
│  app · components · styles · keys · pages  │
└───────────────────┬────────────────────────┘
                    │ provider.Node values
┌───────────────────▼────────────────────────┐
│             provider.Provider              │  internal/provider
│   Azure · GCP · AWS implementations        │
└───────────────────┬────────────────────────┘
                    │ Azure SDK + ARM REST + cli.Runner
┌───────────────────▼────────────────────────┐
│    Azure SDK (azcore + azidentity)         │  primary path
│    cli.Runner (az / gcloud / aws)          │  fallback / non-Azure
│    cache.Store                             │  persistent cost cache
└────────────────────────────────────────────┘
```

## Invariants

- **Cloud concepts stay out of the TUI.** The TUI works against the
  generic `provider.Node` and `provider.Provider`. Anything
  cloud-specific lives in `internal/provider/<cloud>/`.
- **No credentials in our process.** cloudnav never asks for tokens or
  passwords. Auth flows through each cloud's standard SDK credential
  chain — `azidentity.NewDefaultAzureCredential`,
  `google.FindDefaultCredentials`, `config.LoadDefaultConfig` — so
  whatever the host already supports (CLI cached tokens, Service
  Principal env vars, federated workload identity, Managed Identity,
  IRSA / OIDC, IAM role from instance metadata) just works. See
  [`auth.md`](auth.md) for the full per-cloud method matrix.
- **Auth method is observable.** Every provider implements
  `provider.Identifier` so `cloudnav doctor` shows both the
  authenticated principal and the credential source that resolved
  (`Azure CLI cached token`, `Service Account JSON`, `Web Identity /
  OIDC`, etc.).

## TUI layer

`internal/tui` is organised per feature. Each overlay owns its file:

```
app.go        model struct · Update dispatch · View root · table render
advisor.go    advisor overlay
billing.go    portfolio cost view
cost_history  cost chart
costs.go      cost column load paths
delete.go     RG + resource delete confirm
detail.go     detail viewport
health.go     service-health overlay
help.go       keybindings reference
locks.go      RG lock badges
login.go      I-key login flow
metrics.go    sparkline overlay
nav.go        drill / back / reload
palette.go    : palette (cross-cloud jump)
pim.go        PIM list / activate
search.go     / filter
upgrade.go    self-upgrade flow
```

`internal/tui/components` holds reusable widgets:
`Shell` (3-band layout that fills the terminal), `Breadcrumb`, `Keybar`,
`Modal`, `Composite` (ANSI-aware z-ordered compositing so modals render
over the table body instead of replacing it).

`internal/tui/styles` is the single source of lipgloss styles —
`lipgloss.NewStyle()` should not appear outside that package.

## Navigation

`internal/nav.Stack` is a LIFO of `Frame` objects. Each frame is "where
the user currently is" — title (for the breadcrumb), parent node (for
reload), and the child nodes being rendered. Drill-down pushes; `esc`
pops.

## Provider contract

```go
type Provider interface {
    Name() string
    LoggedIn(ctx) error
    Root(ctx) ([]Node, error)             // top level
    Children(ctx, Node) ([]Node, error)   // drill down
    PortalURL(Node) string                // for the `o` key
    Details(ctx, Node) ([]byte, error)    // for the `i` key
}
```

Optional capabilities (`Coster`, `PIMer`, `Advisor`, `Billing`,
`HealthEventer`, `Metricser`, `CostHistoryer`, `BillingSummarer`,
`Deleter`, `Locker`, `Identifier`) are type-asserted at call sites,
so providers opt in per feature. Adding a cloud means implementing
the base + whichever optionals make sense. Compile-time
`var _ provider.X = (*Y)(nil)` assertions catch interface
regressions at build time.

## Azure — SDK-first, CLI-fallback

cloudnav was originally a pure CLI wrapper. It now uses the Azure SDK
for Go (`azcore`, `azidentity`, `armsubscription`) for the hottest paths
and falls back to the `az` CLI only when the SDK credential chain can't
resolve (no cached login, az not installed).

- **Auth** — `DefaultAzureCredential` chain. Resolves Service
  Principal env vars / federated workload identity / Managed
  Identity / Azure CLI cached token in that order, all transparent
  to the rest of the codebase. See [`auth.md`](auth.md) for the
  full method matrix.
- **Tokens** — cached in-process per (tenant, audience) until ~2 min
  before expiry. A PIM list across N tenants acquires N tokens once per
  session instead of 2N processes per refresh.
- **Subscriptions** — `armsubscription.NewSubscriptionsClient` pager.
- **Resource groups** — `armresources.NewResourceGroupsClient`.
- **Resources in a single RG** — `armresources.NewClient` with
  `$expand=createdTime,changedTime` for audit timestamps.
- **Resources across RGs** — one Resource Graph (KQL) POST covers all
  selected RGs in a single request. Replaces the N-sequential
  `az resource list` fanout that made 10-RG drills take 10-30 s.
- **Locks** — `armlocks.NewManagementLocksClient` for list / create /
  delete.
- **Resource group delete** — `armresources.NewResourceGroupsClient.BeginDelete`.
- **ARM REST** — a single package-level `http.Client` with keep-alive,
  HTTP/2, and connection pooling. All requests flow through
  `doWithRetry` which honours `Retry-After` on 429/503 up to 3
  attempts.
- **Error surface** — `trimAPIErr` unwraps
  `{"error":{"code","message"}}` envelopes so the TUI status bar shows
  the actual reason (`AuthorizationFailed: ...`) instead of a truncated
  HTTP URL.

### Still on the CLI

`az` stays in two places:
1. As the login bootstrap — cloudnav never runs `az login` for you.
2. As the fallback whenever the SDK credential chain can't resolve
   (e.g. the user has a custom `AZURE_CONFIG_DIR`, or they removed
   `az` cache files but the binary itself still works).

VM start / stop / show and resource detail (`az resource show`) remain
on `cli.Runner` — lower-traffic paths that are fine shelling out.

## GCP — SDK-first, CLI-fallback

GCP went through the same migration as Azure. All 12 phases are
documented in [`gcp-sdk-migration.md`](gcp-sdk-migration.md). Resolves
auth via `google.FindDefaultCredentials` (ADC), supporting Service
Account JSON, Workload Identity Federation, Impersonated SA,
metadata server, and gcloud user creds.

Liens (the GCP analog to Azure management locks) intentionally stay
on `gcloud alpha resource-manager liens` — Liens v1 has no Go SDK
in `cloud.google.com/go`, and pulling
`google.golang.org/api/cloudresourcemanager/v1` just for this would
mean a second auth pool. Liens are infrequently accessed; CLI
fallback is fine.

## AWS — SDK-first, CLI-fallback

Mirrors the GCP migration. Routes through the v2 SDK
(`config.LoadDefaultConfig`) which resolves IRSA / OIDC, static
keys, temporary creds, profiles (with AssumeRole), SSO, ECS task
role, and EC2 IMDS — every method `aws-cli` supports works in
cloudnav by default.

Trusted Advisor stays on the CLI fallback because it's a paid
support-plan feature few users hit. Locker is intentionally
unimplemented — AWS has no native lock primitive; the closest
analog is SCPs which are policy-shaped not resource-shaped.

## Caching

Every disk cache lives in a single SQLite file —
`$XDG_CACHE_HOME/cloudnav/cloudnav.db` (or
`~/.cache/cloudnav/cloudnav.db` / `%LOCALAPPDATA%\cloudnav\cloudnav.db`)
— with one table-style bucket per data class. Default is SQLite as
of 0.22.28; opt out with `CLOUDNAV_CACHE_BACKEND=json` for the
older per-key file layout.

| Bucket | Cloud | TTL | Purpose |
|---|---|---|---|
| `costs` | all | 15 min | warm the cost column on restart |
| `pim` | all | 5 min | persist PIM eligibilities across sessions |
| `azure-root` | Azure | 10 min | cross-tenant subscription enumeration |
| `rgraph` | Azure | 10 min | multi-RG drill (Resource Graph KQL snapshots) |
| `gcp-root` | GCP | 10 min | project + folder enumeration |
| `gcp-assets` | GCP | 5 min | per-project Asset Inventory drill |
| `aws-root` | AWS | 10 min | account list (Organizations / STS) |
| `aws-resources` | AWS | 5 min | per-region tagging API drill |
| `fx-rates` | currency | 24 h | frankfurter.app rate tables for `display_currency` |
| `update-check` | self | 1 h poll / 24 h stale fallback | cheap startup |

Cache key fingerprints include the active credential file (Azure az
profile, gcloud config, AWS `~/.aws/credentials` + profile env vars)
so a re-login or profile switch auto-invalidates the relevant rows.
Override `CLOUDNAV_CACHE` to relocate the directory.

## Upgrade

`internal/updatecheck` polls GitHub Releases **at most once per hour**
(cache-first `Check()`); past that it hits the API, refreshes the
cache, and on failure falls back to the stale entry so the UI doesn't
go quiet when the user is offline. Anonymous quota (60/hour) is never
touched at this cadence — no GitHub token required.

When a newer tag is detected, the header renders a loud reversed-video
pill (`[ ↑ vX.Y.Z available — press U ]`). Pressing `U`:

- opens the confirmation overlay when a newer release is already known;
- forces a fresh GitHub lookup (bypassing the 1-hour cache) when no
  update is known — useful right after a release is cut.

The confirmation overlay shows the exact plan that will run
(`brew update && brew upgrade cloudnav`, `go install …@latest`, or a
browser handoff for non-automatic installs). `y` / `↵` runs the plan;
on success cloudnav invokes the freshly-installed binary and parses its
version to verify the disk actually moved — if not (e.g. a silent brew
no-op), the banner reports a failure instead of a misleading
"complete".

### Self-relaunch

On the post-success overlay, `R` (or `↵`) re-execs the freshly-
installed binary in place. On POSIX we `syscall.Exec` — same PID,
same stdio, same working dir — so the handoff is invisible. On
Windows we spawn a fresh child and exit the parent.

### Autonomous path

Set `"auto_upgrade": true` in `~/.config/cloudnav/config.json`. When
an update is detected on startup, cloudnav runs the plan silently and
re-execs into the new binary without asking. Browser plans (manual
releases) are never auto-launched.

## Testing

- **Unit tests** live next to the code (`*_test.go`). `go test -race
  ./...` should be clean on every commit.
- **Integration tests** for the TUI live in
  `internal/tui/integration_test.go` and drive the Bubble Tea model via
  its own `Update` / `View` methods — no network, no CLI, just state
  transitions.
- **End-to-end tests** live in `test/e2e/*.sh`. They run the binary
  against a live cloud login. Opt-in; not part of CI.

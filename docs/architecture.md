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
  passwords. Auth is whatever `az login` / `gcloud auth` / `aws sso
  login` already configured.

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
`HealthEventer`, `Metricser`, `CostHistoryer`, `BillingSummarer`) are
type-asserted at call sites, so providers opt in per feature. Adding a
cloud means implementing the base + whichever optionals make sense.

## Azure — SDK-first, CLI-fallback

cloudnav was originally a pure CLI wrapper. It now uses the Azure SDK
for Go (`azcore`, `azidentity`, `armsubscription`) for the hottest paths
and falls back to the `az` CLI only when the SDK credential chain can't
resolve (no cached login, az not installed).

- **Auth** — `DefaultAzureCredential` / `AzureCLICredential`. Reads the
  `az login` cache directly, no process spawn per call.
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

## Caching

| Layer     | Where                                   | TTL    | Purpose |
|-----------|-----------------------------------------|--------|---------|
| Token     | in-memory (pim_tokens.go)               | 58 min | avoid per-call az spawn |
| Root      | in-memory + disk                        | 90 s / session | sub list across back/forward |
| Cost      | in-memory + disk (`cache.Store`)        | 15 min | warm the cost column on restart |
| Resource Health | in-memory per sub                 | 60 s   | avoid per-resource lookups |
| Update check | disk (updatecheck)                   | 24 h   | quieter startup |

Disk caches live under `$XDG_CACHE_HOME/cloudnav` (or
`~/.cache/cloudnav` / `%LOCALAPPDATA%\cloudnav`). Override with
`CLOUDNAV_CACHE`.

## Upgrade

`internal/updatecheck` checks GitHub Releases on startup. The header
shows an `↑ update available` badge when a newer tag exists; `U` opens
a confirmation overlay that runs the resolved install plan (`go
install` / `brew upgrade` / browser for manual releases).

Opt-in auto-upgrade: set `"auto_upgrade": true` in
`~/.config/cloudnav/config.json` to have cloudnav silently run the
non-interactive plans at startup when a newer release ships.

## Testing

- **Unit tests** live next to the code (`*_test.go`). `go test -race
  ./...` should be clean on every commit.
- **Integration tests** for the TUI live in
  `internal/tui/integration_test.go` and drive the Bubble Tea model via
  its own `Update` / `View` methods — no network, no CLI, just state
  transitions.
- **End-to-end tests** live in `test/e2e/*.sh`. They run the binary
  against a live cloud login. Opt-in; not part of CI.

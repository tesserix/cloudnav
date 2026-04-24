# cloudnav

A fast, keyboard-driven multi-cloud navigator. One TUI for **Azure**, **GCP**, and **AWS** — drill through tenants, subscriptions, projects, accounts, resource groups, resources, costs, and IAM without leaving the terminal.

[![Release](https://img.shields.io/github/v/release/tesserix/cloudnav?color=7c3aed)](https://github.com/tesserix/cloudnav/releases)
[![CI](https://github.com/tesserix/cloudnav/actions/workflows/ci.yml/badge.svg)](https://github.com/tesserix/cloudnav/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/tesserix/cloudnav.svg)](https://pkg.go.dev/github.com/tesserix/cloudnav)
[![Go Report Card](https://goreportcard.com/badge/github.com/tesserix/cloudnav)](https://goreportcard.com/report/github.com/tesserix/cloudnav)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

```
┌─ cloudnav ───────────────────────────────── azure • acme-prod ─┐
│ azure › acme-prod › resource groups                 47 items   │
├────────────────────────────────────────────────────────────────┤
│  NAME                               LOCATION    STATE   COST   │
│  web-api-prod-rg                    uksouth     OK      £2,355 │
│  analytics-prod-rg                  uksouth     OK      £869   │
│  ...                                                           │
├────────────────────────────────────────────────────────────────┤
│ ↵ open  / search  c costs  o portal  p PIM  r refresh  ? help  │
└────────────────────────────────────────────────────────────────┘
```

## Read-only by default

cloudnav is a **navigator**, not an orchestrator. Every command is read-only
unless it's explicitly documented as mutating and requires `--yes`:

- `vm start` / `vm stop` — start/stop VMs (opt-in mutation, `--yes` required).
- `pim activate` — requests time-bound role elevation via the cloud's own PIM/SSO/JIT surface. This *changes IAM state* but doesn't create resources.

Nothing else writes — not `ls`, `cost`, `advisor`, `doctor`, the TUI, or
anything in the palette.

## Why

Jumping between `az`, `gcloud`, `aws`, the three web portals, and half a dozen cost dashboards wastes minutes every time. `cloudnav` puts it all behind one keyboard-first TUI:

- **Unified hierarchy** — Azure tenants/subs/RGs, GCP orgs/projects, AWS orgs/accounts/regions all rendered the same way.
- **Real auth** — no new credentials. Uses whatever `az`/`gcloud`/`aws` already have logged in (SSO, federated, SP, workload identity).
- **PIM-first on Azure** — list and activate eligible roles from inside the TUI.
- **Costs inline** — 30-day spend as a sortable column per resource group / project / account.
- **Portal handoff** — one keystroke opens the current row in the cloud's web console.
- **CLI escape hatch** — `x` runs any provider CLI command inside the current context (subscription / project / account already selected).

## Install

### Homebrew

```bash
brew tap tesserix/tap
brew install cloudnav
```

### Go

```bash
go install github.com/tesserix/cloudnav/cmd/cloudnav@latest
```

### Binary

Grab the latest from [Releases](https://github.com/tesserix/cloudnav/releases) — `darwin`/`linux`/`windows` on `amd64` and `arm64`.

## Prerequisites

`cloudnav` wraps the cloud providers' own CLIs. Install whichever you need:

| Provider | CLI | Auth |
|---------|-----|------|
| Azure | [`az`](https://learn.microsoft.com/cli/azure/install-azure-cli) | `az login` |
| GCP | [`gcloud`](https://cloud.google.com/sdk/docs/install) | `gcloud auth login` + `gcloud auth application-default login` |
| AWS | [`aws`](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) | `aws configure sso` or `aws configure` |

Run `cloudnav doctor` to verify everything is wired up.

## Quickstart — step by step

1. **Install the tool** (pick one of the options above).
2. **Log in to the cloud you care about** using its own CLI:
   ```bash
   az login                           # Azure
   gcloud auth login                  # GCP
   aws configure sso                  # AWS (recommended)
   ```
3. **Verify everything is wired up:**
   ```bash
   cloudnav doctor
   ```
   Expected output:
   ```
   ✓ azure  you@example.com
   ✓ gcp    you@example.com
   ✓ aws    arn:aws:iam::123456789012:user/you
   ```
4. **Launch the TUI:**
   ```bash
   cloudnav
   ```
   Use `↑`/`↓` (or `j`/`k`) to move, `↵` to drill down, `esc` to go back, `?` for help, `q` to quit.
5. **Open the current selection in the cloud portal** with `o`.
6. **Run a CLI command in the current scope** with `x` — cloudnav will pre-fill the right `--subscription` / `--project` / `--profile`.
7. **(Azure only) List and activate PIM roles** with `p`.

### Non-interactive / scripting

```bash
cloudnav find scopes prod
cloudnav find resources web --cloud azure --subscription <id>
cloudnav find pim admin --cloud azure

cloudnav ls azure subs --json | jq '.[].name'
cloudnav ls azure rgs --subscription <id>
cloudnav ls azure resources --subscription <id> --resource-group my-rg --json
```

`cloudnav find` is the discovery-first layer.
Use it when you know part of a name or scope but not the exact path yet.
`cloudnav ls` is still there as the lower-level, script-friendly primitive.

## Keybindings

| Key | Action |
|-----|--------|
| `↵` / `l` | Drill down |
| `esc` / `h` | Back up one level |
| `j` `k` / `↑` `↓` | Move selection |
| `/` | Fuzzy search current view |
| `:` | Command palette — switch cloud, tenant, subscription |
| `c` | Toggle cost column |
| `s` | Cycle sort (name → cost → state) |
| `o` | Open selected resource in cloud portal |
| `i` | Show full JSON detail |
| `p` | PIM — list/activate eligible roles (Azure) |
| `x` | Exec provider CLI in current context |
| `r` | Refresh |
| `f` | Bookmark current view |
| `?` | Help |
| `q` / `ctrl+c` | Quit |

## Configuration

`cloudnav` reads `~/.config/cloudnav/config.json` (macOS/Linux) or `%APPDATA%\cloudnav\config.json` (Windows). Every field is optional; sensible defaults apply.

```json
{
  "default_provider": "azure",
  "auto_upgrade": false,
  "gcp": {
    "billing_table": "my-project.billing.gcp_billing_export_v1"
  }
}
```

Highlights:

- `auto_upgrade` — when `true`, cloudnav detects a newer GitHub release on startup, runs the upgrade plan silently (`brew update && brew upgrade cloudnav` on Homebrew, `go install …@latest` on Go), then re-execs into the new binary. Browser plans (manual releases) are never auto-launched.
- `gcp.billing_table` — BigQuery billing-export table backing the GCP cost column.
- `bookmarks` — populated by `f` inside the TUI.

See [`docs/config.md`](docs/config.md) for the full reference (cache paths, env var overrides, all fields).

### cloudnav never stores your credentials

- cloudnav does **not** read, write, or cache tokens, keys, passwords, or refresh tokens.
- All authentication is delegated to the wrapped CLIs (`az`, `gcloud`, `aws`). When you run `cloudnav`, it inherits their logged-in session for the duration of the subprocess call.
- The optional config file holds preferences only (theme, bookmarks, sort order). You can delete it at any time with no loss of access.
- Logs go to `~/.local/state/cloudnav/cloudnav.log` (Linux) / `~/Library/Logs/cloudnav/cloudnav.log` (macOS) and contain only the CLI commands we executed plus any stderr — never tokens.

## Non-interactive / headless use

cloudnav is a TUI by default, but every navigation step is also exposed as a scriptable command:

```bash
cloudnav search scopes prod
cloudnav find resources vm-01 --cloud azure --subscription <id> --details
cloudnav jit list --cloud azure
cloudnav costs services --json

cloudnav ls azure subs --json | jq '.[].name'
cloudnav ls azure rgs --subscription <id> --json
cloudnav ls azure resources --subscription <id> --resource-group my-rg --json
```

When stdout is not a terminal (pipe, CI, Docker without `-t`), `cloudnav ls` will emit plain output by default and `--json` switches to machine-readable. The TUI binary itself requires a terminal; on headless machines use `cloudnav ls`, `cloudnav doctor`, and `cloudnav version` only.

## Architecture

```
┌──────────────────┐   ┌───────────────┐   ┌────────────────────────────┐
│   Bubbletea TUI  │◀─▶│ provider API  │◀─▶│  Azure SDK + ARM REST       │
│ components+styles│   │  (normalized) │   │  cli.Runner (gcloud / aws)  │
└──────────────────┘   └───────────────┘   └────────────────────────────┘
```

- `cmd/cloudnav` — entrypoint.
- `internal/cmd` — Cobra commands (`tui`, `doctor`, `version`, `ls`, `find`, `completion`).
- `internal/provider` — `Provider` interface + Azure / GCP / AWS implementations.
- `internal/provider/azure` — SDK-first (`azcore` / `azidentity` / `armsubscription`) with `cli.Runner` fallback. Direct ARM REST via a shared `http.Client` with HTTP/2, connection pooling, and `Retry-After` backoff. Resource Graph (KQL) for multi-RG and multi-sub enumeration.
- `internal/cli` — generic subprocess runner with timeout + context, used for `gcloud` / `aws` and the Azure fallback path.
- `internal/cache` — on-disk key-value store (JSON per key, atomic writes) powering the persistent cost cache.
- `internal/nav` — navigation stack (breadcrumbs, back, context).
- `internal/tui` — Bubbletea model, per-feature files (advisor / billing / costs / delete / detail / health / palette / pim …).
- `internal/tui/components` — reusable layout widgets (`Shell`, `Breadcrumb`, `Keybar`, `Modal`, `Composite`).
- `internal/tui/styles` — single-source lipgloss theme.
- `internal/iam` — provisioning of scoped SP / SA / IAM Role with least-privilege presets.

See [`docs/architecture.md`](docs/architecture.md) for the full design,
[`docs/design-system.md`](docs/design-system.md) for the UI style rules
and palette that every view shares,
[`docs/config.md`](docs/config.md) for every config knob, and
[`CHANGELOG.md`](CHANGELOG.md) for recent work — including the SDK
migration, cross-tenant discovery, Resource Graph fast path, persistent
cost cache, overlay compositor + adaptive column widths, PIM hardening,
the rate-limit-safe update check (1-hour poll), and the one-keystroke
self-relaunch on upgrade.

## Roadmap

See [`ROADMAP.md`](ROADMAP.md). Current phase: **1 — Azure navigation + PIM**.

## Development

```bash
git clone https://github.com/tesserix/cloudnav.git
cd cloudnav
make dev          # runs against your currently-logged-in az session
make test
make lint
make build
```

Contributions welcome — read [`CONTRIBUTING.md`](CONTRIBUTING.md) first.

## Security

Found a vulnerability? Please follow the process in [`SECURITY.md`](SECURITY.md) — do not open a public issue.

## License

Apache License 2.0 — see [`LICENSE`](LICENSE).

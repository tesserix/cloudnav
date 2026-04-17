# cloudnav

A fast, keyboard-driven multi-cloud navigator. One TUI for **Azure**, **GCP**, and **AWS** — drill through tenants, subscriptions, projects, accounts, resource groups, resources, costs, and IAM without leaving the terminal.

[![Release](https://img.shields.io/github/v/release/tesserix/cloudnav?color=7c3aed)](https://github.com/tesserix/cloudnav/releases)
[![CI](https://github.com/tesserix/cloudnav/actions/workflows/ci.yml/badge.svg)](https://github.com/tesserix/cloudnav/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/tesserix/cloudnav.svg)](https://pkg.go.dev/github.com/tesserix/cloudnav)
[![Go Report Card](https://goreportcard.com/badge/github.com/tesserix/cloudnav)](https://goreportcard.com/report/github.com/tesserix/cloudnav)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

```
┌─ cloudnav ────────────────────────────── azure • Platform-Prod ─┐
│ azure › Platform-Prod › resource groups              47 items   │
├─────────────────────────────────────────────────────────────────┤
│  NAME                                LOCATION    STATE   COST   │
│  Yellowfin-container-testing         uksouth     OK      £2,355 │
│  nonprod-uksouth-baseline-rg         uksouth     OK      £869   │
│  ...                                                            │
├─────────────────────────────────────────────────────────────────┤
│ ↵ open  / search  c costs  o portal  p PIM  r refresh  ? help  │
└─────────────────────────────────────────────────────────────────┘
```

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

## Quickstart

```bash
# launch the TUI
cloudnav

# or start already scoped to a cloud
cloudnav azure
cloudnav gcp
cloudnav aws

# non-interactive (pipeable) output
cloudnav ls azure subs --json
cloudnav ls azure rgs --subscription <id>
```

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

`cloudnav` reads `~/.config/cloudnav/config.yml` (macOS/Linux) or `%APPDATA%\cloudnav\config.yml` (Windows). Everything is optional; sensible defaults apply.

```yaml
default_provider: azure
show_cost: true
theme: dark            # dark | light | auto
bookmarks:
  - provider: azure
    path: subs/fcb999d2-0d48-42ae-a29a-42bbd6cd5106/rgs
cache_ttl: 10m
```

Override per-invocation with env vars — `CLOUDNAV_THEME`, `CLOUDNAV_NO_COLOR`, `CLOUDNAV_LOG_LEVEL`.

## Architecture

```
┌──────────────────┐   ┌───────────────┐   ┌────────────────────────┐
│   Bubbletea TUI  │◀─▶│ provider API  │◀─▶│  exec az / gcloud / aws │
│  (pages + keys)  │   │  (normalized) │   │   (JSON → structs)      │
└──────────────────┘   └───────────────┘   └────────────────────────┘
```

- `cmd/cloudnav` — entrypoint.
- `internal/cmd` — Cobra commands (`tui`, `doctor`, `version`, `ls`, `completion`).
- `internal/provider` — `Provider` interface + Azure/GCP/AWS implementations. Each provider owns its CLI adapter and JSON unmarshaling.
- `internal/cli` — generic subprocess runner with timeout + context.
- `internal/nav` — navigation stack (breadcrumbs, back, context).
- `internal/tui` — Bubbletea model, pages (home/list/detail), keymap, styles.
- `internal/iam` — provisioning of scoped SP / SA / IAM Role with least-privilege presets.

See [`docs/architecture.md`](docs/architecture.md) for the full design.

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

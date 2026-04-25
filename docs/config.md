# Configuration

cloudnav reads a JSON config file on startup. All fields are optional;
omit them to get the defaults.

**Default location:**

| OS      | Path |
|---------|------|
| macOS / Linux | `$XDG_CONFIG_HOME/cloudnav/config.json`, falling back to `~/.config/cloudnav/config.json` |
| Windows | `%APPDATA%\cloudnav\config.json` |

Override with `CLOUDNAV_CONFIG=/path/to/config.json`.

## Shape

```json
{
  "default_provider": "azure",
  "theme": "default",
  "auto_upgrade": false,
  "display_currency": "GBP",
  "gcp": {
    "billing_table": "my-project.billing.gcp_billing_export_v1"
  },
  "bookmarks": []
}
```

> **Cloud authentication is not in this file.** cloudnav never reads
> credentials; it relies on each cloud's standard SDK auth chain
> (Azure CLI cached token / Service Principal / Managed Identity;
> GCP ADC; AWS profile / IRSA / static keys / SSO). See
> [`auth.md`](auth.md) for the full method matrix and the env vars
> for every flow.

## Fields

### `default_provider`

Which cloud to highlight on the home screen. Supported values: `azure`,
`gcp`, `aws`. Leave empty to start on the generic clouds list.

### `theme`

Reserved for future use. Currently ignored.

### `auto_upgrade`

Opt-in silent self-upgrade. When `true` and cloudnav detects a newer
release on startup, it:

1. Runs the detected upgrade plan (`brew update && brew upgrade cloudnav`
   on Homebrew machines, `go install …@latest` on Go machines) silently.
2. On success, automatically re-execs the freshly-installed binary in
   place of the current process.

You see one "auto-upgrading to vX.Y.Z…" flash in the footer, then
cloudnav reopens on the new version with the update pill gone. Skips
the interactive `release page` path so cloudnav never auto-launches a
browser.

Default: `false`. When unset, cloudnav shows the update pill and waits
for you to press `U`.

### `display_currency`

ISO 4217 currency code (e.g. `"GBP"`, `"EUR"`, `"INR"`). When set,
every cost rendering in cloudnav — sub / RG / resource columns, the
`B` billing overlay, the `$` cost chart, and `cloudnav cost`
subcommands — re-denominates from the cloud's native currency to the
chosen one.

Rates come from [frankfurter.app](https://www.frankfurter.app/)
(free, ECB-backed, no API key) and are cached in the SQLite cache
under the `fx-rates` bucket with a 24-hour TTL. On FX failures the
formatters silently fall back to the cloud's native currency — cost
rendering never blocks on the network.

Override per-invocation with `cloudnav cost --currency GBP`.
Override at runtime via the `currency.SetDefault` API (used by the
TUI bootstrap; future hotkey hooks here).

Unset (default) → each cloud renders in its own native currency.

### `gcp.billing_table`

BigQuery billing-export table in `project.dataset.table` form. Backs
the GCP cost column. Unset → cost column stays blank on GCP projects.
`CLOUDNAV_GCP_BILLING_TABLE` environment variable overrides this per
invocation.

### `bookmarks`

Generated entries — cloudnav writes to this list when you press `f`
inside the TUI. No reason to hand-edit.

## Cache

cloudnav stores small caches (costs, GitHub release check) under:

| OS      | Path |
|---------|------|
| macOS   | `~/Library/Caches/cloudnav` |
| Linux   | `$XDG_CACHE_HOME/cloudnav` (else `~/.cache/cloudnav`) |
| Windows | `%LOCALAPPDATA%\cloudnav` |

Override with `CLOUDNAV_CACHE`. Safe to delete at any time — cloudnav
will repopulate on next use.

### Cache backend

By default cloudnav stores its cache in a single SQLite file at
`<CLOUDNAV_CACHE>/cloudnav.db` — WAL journaling, indexed lookups by
`(bucket, key)`, atomic upserts. Driver: `modernc.org/sqlite`
(pure Go, no CGO, no extra install step).

If you'd rather have one JSON file per cache entry (easier to inspect
with `cat` / `ls`, or required on a read-only filesystem where SQLite
can't open WAL), opt out:

```bash
export CLOUDNAV_CACHE_BACKEND=json
```

That switches both the cost cache and the PIM cache back to
`<CLOUDNAV_CACHE>/<bucket>/<key>.json`. Both backends pass the same
parity test matrix, so swapping is observation-free — you'll just
re-fetch from the cloud once because the two stores don't share data.

## Environment variables

| Variable | Purpose |
|---|---|
| `CLOUDNAV_CONFIG` | path to config.json |
| `CLOUDNAV_CACHE` | base directory for on-disk caches |
| `CLOUDNAV_CACHE_BACKEND` | `json` to switch back to per-key JSON files; default is the SQLite single-file DB |
| `CLOUDNAV_GCP_BILLING_TABLE` | GCP cost source override |
| `CLOUDNAV_THEME` | reserved |
| `CLOUDNAV_NO_COLOR` | disable styling |

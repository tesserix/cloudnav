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
  "gcp": {
    "billing_table": "my-project.billing.gcp_billing_export_v1"
  },
  "bookmarks": []
}
```

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

## Environment variables

| Variable | Purpose |
|---|---|
| `CLOUDNAV_CONFIG` | path to config.json |
| `CLOUDNAV_CACHE` | base directory for on-disk caches |
| `CLOUDNAV_GCP_BILLING_TABLE` | GCP cost source override |
| `CLOUDNAV_THEME` | reserved |
| `CLOUDNAV_NO_COLOR` | disable styling |

# End-to-end test suite

Lives in `test/e2e`. Runs against your live cloud credentials — treat it as a smoke test, not a unit-test suite. These tests never mutate cloud state; they exercise `cloudnav`'s read paths (list, doctor, ls, cost, pim list).

## Why shell, not Playwright

cloudnav is a terminal UI. Playwright drives browsers. For a Bubbletea TUI we use `tmux` to spawn a real PTY, send keystrokes, and capture the pane.

## Run it

```bash
make build                    # produce ./bin/cloudnav first
make test-e2e                 # or: ./test/e2e/run.sh
```

Prereqs on your machine:

- `az login`, `gcloud auth login`, `aws configure` (or SSO) — whichever clouds you want to cover
- `tmux` for the TUI half (silently skipped if missing)
- `python3` for lightweight JSON assertions

### Operator-specific env vars

The tests drill into a specific Azure subscription by name. Defaults are
neutral placeholders so nothing operator-specific lives in the public
repo; set these before running the suite against your real tenant:

| Env var | Default | What it does |
|---|---|---|
| `CLOUDNAV_E2E_AZURE_SUB` | `acme-prod` | Subscription name typed into the `/` filter when drilling into Azure |
| `CLOUDNAV_E2E_AZURE_SUB_ID` | `00000000-0000-0000-0000-000000000000` | Subscription GUID used by the `cost rgs --subscription` assertion |
| `CLOUDNAV_E2E_LOCKED_RG` | `locked-rg` | A resource-group name known to carry a 🔒 lock — used to assert the lock badge renders |
| `CLOUDNAV_E2E_TENANT_PATTERN` | `.` (any tenant) | Substring the azure tenant column must match |

## What it covers

- **`cli_test.sh`** — version, help, doctor, `ls` for every cloud, `pim` list for each cloud, `cost --help`, `cost services`, out-of-range activation, non-TTY fallback.
- **`tui_test.sh`** — home renders all three clouds, `?` help overlay, `:` palette preload, azure drill with tenant name resolution, aws drill to regions.

## Adding a test

Drop a `something_test.sh` file in this directory. The runner sources every file matching `*_test.sh` in the shared env that exposes:

- `$BIN` — absolute path to the binary under test
- `pass "name"` / `fail "name" "detail"` — helpers that update the counters
- `assert_contains "name" "needle" "haystack"`
- `assert_regex "name" "regex" "haystack"`
- `assert_exit "name" want got`

Keep tests idempotent and read-only — anything that creates or modifies cloud resources does not belong here.

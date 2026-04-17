# Contributing to cloudnav

Thanks for your interest. cloudnav is small, opinionated, and designed to stay that way. Contributions that keep it focused are very welcome.

## Ground rules

- Be kind. Read [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).
- Discuss before building anything large — open an issue first.
- Small, reviewable PRs beat large ones.
- Tests accompany behavior changes.

## Development setup

Requirements: Go 1.25+, `az` / `gcloud` / `aws` (whichever you work on).

```bash
git clone https://github.com/tesserix/cloudnav.git
cd cloudnav
make dev          # runs the TUI against your logged-in session
make test
make lint
make build        # produces ./bin/cloudnav
```

Useful loops:

```bash
go run ./cmd/cloudnav doctor
go run ./cmd/cloudnav ls azure subs --json
```

## Project layout

See [`docs/architecture.md`](docs/architecture.md). In short:

- `internal/provider/<cloud>` — anything cloud-specific lives here.
- `internal/tui` — Bubbletea code; must stay cloud-agnostic.
- `internal/cli` — the only place that shells out.

If a PR adds a cloud concept into `internal/tui`, that's almost always wrong.

## Commit conventions

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(azure): list resource groups with cost column
fix(tui): truncate breadcrumbs correctly on narrow terminals
docs(readme): clarify PIM flow
chore(ci): bump actions/setup-go to v5
```

Allowed types: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`.

## Pull requests

- Branch off `main`.
- Run `make test lint` before pushing.
- Fill out the PR template.
- CI must be green.

## Adding a provider

1. Implement `provider.Provider` in `internal/provider/<name>/`.
2. Wrap the provider's CLI in a typed adapter — no raw `map[string]any` escaping the package.
3. Register in `internal/provider/registry.go`.
4. Add recorded CLI fixtures in `test/fixtures/<name>/` so tests run offline.
5. Document any extra auth / permission requirements in `docs/providers.md`.

## Reporting security issues

Don't open a public issue. See [`SECURITY.md`](SECURITY.md).

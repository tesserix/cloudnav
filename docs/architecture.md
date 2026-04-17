# Architecture

cloudnav is three layers wired together by a navigation stack.

```
┌────────────────────────────────────────────┐
│                Bubbletea TUI               │  internal/tui
│    pages · keys · styles · nav stack       │
└───────────────────┬────────────────────────┘
                    │ provider.Node values
┌───────────────────▼────────────────────────┐
│             provider.Provider              │  internal/provider
│   Azure · GCP · AWS implementations        │
└───────────────────┬────────────────────────┘
                    │ exec az / gcloud / aws
┌───────────────────▼────────────────────────┐
│                 cli.Runner                 │  internal/cli
│     subprocess with timeout + stderr       │
└────────────────────────────────────────────┘
```

## Invariants

- **Cloud concepts stay out of the TUI.** The TUI layer works against the
  generic `provider.Node` and `provider.Provider`. Anything cloud-specific
  must live in `internal/provider/<cloud>/`.
- **Only `internal/cli` shells out.** Providers depend on `cli.Runner`, not on
  `os/exec` directly. This gives one place to add timeouts, metrics, or
  recording for tests.
- **No credentials in our process.** cloudnav never asks the user for tokens,
  keys, or passwords. Authentication is whatever the wrapped CLI is already
  configured with (`az login`, `gcloud auth`, `aws sso login`).

## Navigation

`internal/nav.Stack` is a LIFO of `Frame` objects. Each frame is "where the
user currently is" — its title (for the breadcrumb), parent node (for reload),
and the list of child nodes being shown. Drill-down pushes a frame; `esc` pops.

## Provider contract

```go
type Provider interface {
    Name() string
    LoggedIn(ctx) error
    Root(ctx) ([]Node, error)                     // top level (subs / projects / accounts)
    Children(ctx, Node) ([]Node, error)           // drill down
    PortalURL(Node) string                        // for the `o` keybinding
    Details(ctx, Node) ([]byte, error)            // raw JSON for the `i` keybinding
}
```

Adding a cloud = implementing this interface, registering it in the TUI, and
adding fixtures in `test/fixtures/<cloud>/`.

## Auth model

| Phase | Source of credentials |
|-------|-----------------------|
| 1–3   | Whatever `az` / `gcloud` / `aws` are logged in as — user or SSO session |
| 4     | Optional scoped Service Principal / Service Account / IAM Role provisioned via `cloudnav iam bootstrap` and consumed by the wrapped CLI |

In Phase 4, cloudnav only *provisions* the identity and prints the exact
command the user (or their CI) should run to configure their CLI. cloudnav
does not store secrets.

## Why wrap CLIs instead of using SDKs?

- Every user's cloud auth is already configured the right way for their org
  (SSO, federated, MFA, conditional access). Re-implementing that is a
  support nightmare.
- The official CLIs are the canonical reference for endpoints, API versions,
  and retry behavior.
- The binary stays small and dependency-light.

Downside: subprocess latency. We accept it because navigation is bursty and
each call is ~200ms; cache and async loads cover the rest.

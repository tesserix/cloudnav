# cloudnav design system

The TUI treats every screen as a stack of three horizontal zones — a
**header band**, a **body**, and a **footer band**. Each zone has a
distinct role, a distinct background tone, and a fixed set of style
rules. New views (for GCP and AWS, or any future cloud) should reuse
the existing components rather than introduce new primitives.

All styles live in a single source-of-truth package:
[`internal/tui/styles/styles.go`](../internal/tui/styles/styles.go).
`lipgloss.NewStyle` is not called anywhere else in the codebase — if a
new style is needed, add it to the styles package first.

---

## Palette

cloudnav ships one dark-scheme palette expressed in **ANSI-256 colour
codes**. The codes remap automatically on a light-theme terminal so
the UI stays legible without a custom light mode.

### Chrome tones (zone backgrounds)

| Role | ANSI | Sits behind |
|------|------|-------------|
| HeaderBg | 235 | Breadcrumb + keybar band |
| HintBg | 233 | Status-bar footer + banner hints |
| SelBg | 57 | Selected table row + active tab |
| (body) | terminal default | Table and content |

Three distinct shades keep each zone visually anchored to the edge of
the window — content never "bleeds" from one zone into another.

### Text tones

| Name | ANSI | Use |
|------|------|-----|
| Fg | 255 | Primary text on any background |
| Subtle | 245 | Secondary body copy, inactive tab labels |
| Muted | 240 | Dividers, dim hints, column headers |
| SelFg | 229 | Text on the selected-row background |

### Accents — used sparingly

| Name | ANSI | Use |
|------|------|-----|
| Accent | 86 | App title, active keybind labels, spinner |
| Purple | 63 | Popup / modal borders + titles |
| Green | 114 | Success, ↓ falling cost |
| Warn | 214 | Update pill, ↑ rising cost, background rate-limit |
| Err | 196 | Destructive confirms, access-denied |

Rule of thumb: **one accent per screen**. More than that and the user
can't tell what's actionable.

---

## Layout zones

### Header band

Two lines, both painted on `HeaderBg`:

```
cloudnav › clouds › azure › Platform-DevTest          0.22.13 ^_^
<↵> drill </> search <:> palette <f> flag <p> PIM <i> info <o> portal …
```

- **Line 1** — breadcrumb on the left, version / update pill on the
  right. App name in `Accent`, intermediate crumbs in `Fg`, separators
  (`›`) in `Muted`.
- **Line 2** — keybar: `<KEY>` in `Accent` bold, action label in
  `Subtle`. Keys wrap across lines when the terminal is narrow rather
  than truncate.

Both lines fill the terminal width so the dark-grey chrome reaches
edge-to-edge.

### Body

Whatever the current mode renders — a `bubbles/table`, a `viewport`,
or a centered modal composited over the list. The body inherits the
terminal's default background.

Tables use `TableStyles()` from the styles package:
- Header: bold on `Subtle`, no border.
- Cell: `Padding(0, 1)`.
- Selected row: `SelFg` on `SelBg`, bold.

### Footer band

One line, painted on `HintBg` via `StatusBar`. Priority order (first
match wins):

1. An error: red `error:` prefix + the provider's message.
2. A sticky deletion banner (green): survives auto-refresh.
3. The active search input (`/` prompt).
4. A filter chip (tenant / search / category) with `N/total`.
5. A loading spinner + status text.
6. Item count (`40 items`) or idle state.

Nothing else competes for footer real estate — status bars that flash
multiple messages in a second teach users to ignore the whole strip.

---

## Modals / overlays

Two flavours:

**Compact overlay** — `m.overlay(body)` in the TUI. Composes the
modal as a centered bordered panel on top of the current list view,
using an ANSI-aware line-by-line compositor so the table stays
visible behind. Used for help, delete-confirm, palette, upgrade.

**Full-screen panel** — `fullScreenBox(w, h).Render(body)`. Replaces
the body with a framed container. Used for the information-dense
views (advisor, PIM list, billing, cost chart, service health,
metrics).

Rules:
- Purple `RoundedBorder` around both flavours.
- Title at the top in `ModalTitle` style (purple, bold).
- Hints at the bottom in `ModalHint` (muted, italic).
- Never blink. Never animate beyond the existing spinner.

---

## Keybar conventions

Keys are single characters, unstyled in descriptions, wrapped as
`<KEY>` in the strip. Grouping rules:

- **Navigation first** — drill, search, palette, flag.
- **Scope operations next** — per-surface actions (PIM, info, portal,
  costs).
- **Global / danger last** — refresh, back, quit; destructive keys
  (delete, lock) only appear when the current context makes them
  valid.

Capital letters indicate a "broader" scope than their lowercase
counterparts: `<i>` info on cursor, `<I>` login to cloud; `<l>`
drill (row), `<L>` lock.

---

## States and affordances

| State | Treatment |
|-------|-----------|
| Idle | Muted text, no pill |
| Loading | `Spinner` + `Loading` (bold accent) short message in footer |
| Error | `Bad` red prefix `error:` + trimmed first line |
| Update available | Reversed-video pill top-right: `[ ↑ vX.Y.Z available — press U ]` |
| Deletion in flight | Sticky green banner (10 min TTL) + `Deleting` coloured STATE cell |
| Access denied | `Bad`-coloured row with action hint, or `Err` badge |

---

## Adding a new cloud (GCP / AWS / future)

1. Implement `provider.Provider` with whatever optional capability
   interfaces make sense (`Coster`, `PIMer`, `Advisor`, `Billing`,
   `HealthEventer`, `Metricser`).
2. Reuse the existing views — `list.go`, `detail.go`, `costs.go`,
   etc. — they work off the generic `provider.Node` abstraction.
   Don't introduce cloud-specific views unless the cloud has a
   capability no other cloud shares.
3. Cloud-specific labels live in the provider's `Name()` plus the
   `Kind` enum (`KindSubscription`, `KindProject`, `KindAccount`).
   The TUI reads those and picks the right breadcrumb / column set.
4. If a cloud needs a new column, add it to `columnsFor()` and the
   matching row builder in `rowsFromNodes()` — no styling changes
   should be needed.
5. Diagnostics: failed providers / tenants surface as a row in the
   list with `⚠` prefix and a hint string. Avoid modal error dialogs
   — the user is navigating; pop-ups break flow.

If you catch yourself adding a new colour or a new border style,
stop. The style package has what you need, or the system is wrong
and should be fixed at the palette level rather than per-view.

---

## What this system deliberately rejects

- **Multiple accents on one screen.** Pick one.
- **Animated decoration** (blinking, marching ants, rainbow gradients).
  Spinners only, and only when something is actually running.
- **Nested borders.** A modal inside a box inside a panel is a sign
  the layout is wrong.
- **Colour-as-information without text.** Every red row has a word
  that says why. Colour is a hint, not the signal.
- **Cloud-specific colour coding** (Azure blue vs AWS orange etc.).
  cloudnav is a unified navigator, not three tools bolted together.

---

## Reference implementation

The Azure surface (`internal/tui/*.go` plus `internal/provider/azure/`)
is the canonical application of this system. When implementing GCP or
AWS features:

- Copy the Azure file of the equivalent feature (e.g. `advisor.go`,
  `pim.go`) as a starting point.
- Mirror its `load*` / `update*` / `view*` trio.
- Register the provider in `buildProviders` in `app.go`.
- Add tests to the existing `*_test.go` patterns — no new test
  frameworks.

The goal is that a user switching from Azure to AWS in the palette
doesn't notice a style change, only a data change.

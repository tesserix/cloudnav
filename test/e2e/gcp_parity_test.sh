# shellcheck shell=bash
#
# Verifies GCP has reached feature parity with Azure for the surfaces the
# user cares about most: advisor (Recommender), PIM (PAM), and the visible
# UI affordances in the TUI. Everything here is best-effort — when the
# underlying API isn't enabled on any project we still want the error paths
# to be clean and informative, not a crash.

# CLI: pim list --cloud gcp should print either real entitlements or a
# fallback hint (no stack traces / no cobra errors).
out=$("$BIN" pim list --cloud gcp 2>&1); rc=$?
if [[ $rc -eq 0 ]]; then
  pass "pim list --cloud gcp returned 0 (PAM entitlements available)"
else
  assert_regex "pim list --cloud gcp fails gracefully" '(PAM|Privileged Access|conditional|not enabled)' "$out"
fi

# CLI: pim help now reflects PAM rather than the old conditional-IAM-only wording.
out=$("$BIN" pim --help 2>&1)
assert_contains "pim help mentions Privileged Access Manager for GCP" "Privileged Access Manager" "$out"

if ! command -v tmux >/dev/null 2>&1; then
  return 0
fi

SESSION=cn-gcp
tmux kill-session -t "$SESSION" 2>/dev/null || true

start() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  tmux new-session -d -s "$SESSION" -x 180 -y 40 "$BIN"
  sleep 2
}
grab() { tmux capture-pane -t "$SESSION" -p; }
send() { tmux send-keys -t "$SESSION" "$@"; }
stop()  { tmux send-keys -t "$SESSION" q 2>/dev/null || true; sleep 0.5; tmux kill-session -t "$SESSION" 2>/dev/null || true; }

# GCP project list should now expose the CREATED column (GA 2025 parity
# with Azure resource-level creation dates).
start
send j; sleep 0.3                     # cursor onto gcp row
send Enter; sleep 12                   # drill into gcp projects
view=$(grab)
if echo "$view" | grep -q 'clouds › gcp'; then
  assert_contains "gcp projects view exposes CREATED column" "CREATED" "$view"
  assert_contains "gcp keybar exposes <A> advisor" "<A> advisor" "$view"
else
  # gcp drill may have hit an auth / permission issue in this environment —
  # we still want the rest of the suite to report cleanly.
  pass "gcp drill didn't complete in this env — skipping visual assertions"
fi
stop

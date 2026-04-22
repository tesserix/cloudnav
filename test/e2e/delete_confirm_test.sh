# shellcheck shell=bash
#
# Verifies the destructive-action confirmation flow added in v0.9.2.
# Pattern: drill into a sub, select one RG, press D, check the modal appears
# with the DELETE prompt and disclaimer, then type a deliberately WRONG word
# and press Enter to confirm the cancel path. Actual delete never fires.

if ! command -v tmux >/dev/null 2>&1; then
  return 0
fi

SESSION=cn-del
tmux kill-session -t "$SESSION" 2>/dev/null || true

start() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  tmux new-session -d -s "$SESSION" -x 190 -y 40 "$BIN"
  sleep 2
}
grab() { tmux capture-pane -t "$SESSION" -p; }
send() { tmux send-keys -t "$SESSION" "$@"; }
stop()  { tmux send-keys -t "$SESSION" q 2>/dev/null || true; sleep 0.5; tmux kill-session -t "$SESSION" 2>/dev/null || true; }

start
send Enter; sleep 10                 # drill into azure → subs
send "/"; sleep 0.3
send "$CLOUDNAV_E2E_AZURE_SUB"; sleep 1
send Enter; sleep 0.5                # pick the first match
send Enter; sleep 15                 # drill into sub → RGs

# D on empty selection should just hint, not open the modal.
send "D"; sleep 0.4
nosel=$(grab)
assert_contains "D with no selection shows hint, does not open modal" "nothing selected" "$nosel"

# Select one row and try again — modal should open.
send " "; sleep 0.4
send "D"; sleep 0.5
modal=$(grab)
assert_contains "D opens DELETE confirmation modal" "DELETE RESOURCE GROUPS" "$modal"
assert_contains "modal warns that the op is irreversible" "cannot be undone" "$modal"
assert_contains "modal disclaims cloudnav responsibility" "responsible" "$modal"
assert_contains "modal asks for typed confirmation" "type DELETE" "$modal"

# Type the wrong word — Enter must cancel, not delete.
send "nope"; sleep 0.2
send Enter; sleep 0.4
after=$(grab)
assert_contains "wrong word cancels with status message" "cancelled" "$after"
# We should be back on the RG list; the modal text shouldn't still be there.
if ! echo "$after" | grep -q "DELETE RESOURCE GROUPS"; then
  pass "confirmation modal closes after cancel"
else
  fail "confirmation modal closes after cancel" "modal still visible"
fi

stop

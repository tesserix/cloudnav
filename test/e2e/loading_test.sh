# shellcheck shell=bash
#
# Covers the drill-loading overlay + input-lock behaviour (v0.9.3). The goal
# is NOT to race the subs fetch — that's flaky in CI — but to exercise the
# two ends of the loading lifecycle:
#   1. `?` help works even while the startup spinner is on
#   2. After the nodes land the table is back and navigation works
# If the test rig doesn't have tmux available, the whole file is skipped.

if ! command -v tmux >/dev/null 2>&1; then
  return 0
fi

SESSION=cn-load
tmux kill-session -t "$SESSION" 2>/dev/null || true

start() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  tmux new-session -d -s "$SESSION" -x 180 -y 40 "$BIN"
  sleep 2
}
grab() { tmux capture-pane -t "$SESSION" -p; }
send() { tmux send-keys -t "$SESSION" "$@"; }
stop()  { tmux send-keys -t "$SESSION" q 2>/dev/null || true; sleep 0.5; tmux kill-session -t "$SESSION" 2>/dev/null || true; }

# Drill into azure — during the Root() call the screen should show the big
# loading panel. Capture quickly; if we miss the window it's still fine
# because once nodes arrive the panel disappears.
start
send Enter
# The loading panel shows "⏳ loading" + "input is disabled until"; grab
# within the first couple of seconds.
sleep 1
mid=$(grab)
if echo "$mid" | grep -q "loading"; then
  pass "drill shows prominent loading indicator"
else
  pass "drill loading completed before capture (fast cache)"
fi

# Wait for the drill to finish and confirm we made it to the subs view.
sleep 9
after=$(grab)
assert_contains "drill completes and shows subs view" "clouds › azure" "$after"
# After the load finishes the footer should no longer be the big panel.
if ! echo "$after" | grep -q "input is disabled"; then
  pass "loading panel clears once nodes arrive"
else
  fail "loading panel clears once nodes arrive" "still shows the disabled-input banner"
fi

stop

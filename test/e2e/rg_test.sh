# shellcheck shell=bash

if ! command -v tmux >/dev/null 2>&1; then
  return 0
fi

SESSION=cn-rg
tmux kill-session -t "$SESSION" 2>/dev/null || true

start() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  tmux new-session -d -s "$SESSION" -x 190 -y 35 "$BIN"
  sleep 2
}
grab() { tmux capture-pane -t "$SESSION" -p; }
send() { tmux send-keys -t "$SESSION" "$@"; }
stop()  { tmux send-keys -t "$SESSION" q 2>/dev/null || true; sleep 0.5; tmux kill-session -t "$SESSION" 2>/dev/null || true; }

start
send Enter; sleep 10
send "/"; sleep 0.3
send "Platform-Prod"; sleep 1
send Enter; sleep 0.5
send Enter; sleep 15
view=$(grab)
assert_contains "RG view shows SEL column" " " "$view"
assert_contains "RG view shows LOCK column" "LOCK" "$view"
assert_contains "RG view keybar exposes <L> lock" "<L> lock" "$view"
assert_contains "RG view keybar exposes <␣> select" "<␣> select" "$view"

send " "; sleep 0.3
view=$(grab)
assert_contains "space toggles selection marker (●)" "●" "$view"
assert_regex   "keybar updates to show delete count" '<D>[[:space:]]*delete[[:space:]]+[0-9]+' "$view"

send "]"; sleep 0.5
view=$(grab)
if echo "$view" | grep -Fq '●'; then
  fail "] clears selection" "still shows ● marker after ]"
else
  pass "] clears the selection"
fi

send "["; sleep 0.3
view=$(grab)
assert_regex "[ selects all visible rows" '<D>[[:space:]]*delete[[:space:]]+[0-9]+' "$view"

# filter for an RG known to have a lock
send "/"; sleep 0.3
send "rg-bis-yfin-aue-prod-prod"; sleep 1.2
view=$(grab)
assert_regex "locked RG shows 🔒 badge" '🔒.*(CanNotDelete|ReadOnly)' "$view"
stop

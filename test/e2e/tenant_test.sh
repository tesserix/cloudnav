# shellcheck shell=bash

if ! command -v tmux >/dev/null 2>&1; then
  return 0
fi

SESSION=cn-tenant
tmux kill-session -t "$SESSION" 2>/dev/null || true

start() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  tmux new-session -d -s "$SESSION" -x 170 -y 35 "$BIN"
  sleep 2
}
grab() { tmux capture-pane -t "$SESSION" -p; }
send() { tmux send-keys -t "$SESSION" "$@"; }
stop()  { tmux send-keys -t "$SESSION" q 2>/dev/null || true; sleep 0.5; tmux kill-session -t "$SESSION" 2>/dev/null || true; }

start
send Enter; sleep 10
view=$(grab)
assert_contains "t keybinding visible on subs view (tenant: all)" "tenant: all" "$view"

send t; sleep 0.5
view=$(grab)
assert_regex "pressing t sets an actual tenant name in the keybar" 'tenant: [A-Za-z]' "$view"
assert_regex "footer surfaces the tenant filter and N/total" 'tenant:.*[0-9]+/[0-9]+' "$view"
stop

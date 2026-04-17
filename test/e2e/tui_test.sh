# shellcheck shell=bash

if ! command -v tmux >/dev/null 2>&1; then
  log "  ${DIM}tmux not installed — skipping TUI tests${NC}"
  return 0
fi

SESSION=cn-e2e
tmux kill-session -t "$SESSION" 2>/dev/null || true

start_cn() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  tmux new-session -d -s "$SESSION" -x 160 -y 35 "$BIN"
  sleep 2
}

grab() { tmux capture-pane -t "$SESSION" -p; }
send() { tmux send-keys -t "$SESSION" "$@"; }

stop_cn() { tmux send-keys -t "$SESSION" q 2>/dev/null || true; sleep 0.5; tmux kill-session -t "$SESSION" 2>/dev/null || true; }

# T1: home renders all 3 clouds
start_cn
home=$(grab)
assert_contains "TUI home lists azure" "azure" "$home"
assert_contains "TUI home lists gcp" "gcp" "$home"
assert_contains "TUI home lists aws" "aws" "$home"
assert_contains "TUI home shows sort keybinding" "sort name" "$home"

# T2: help overlay opens
send "?"; sleep 0.5
help=$(grab)
assert_contains "TUI help overlay opens on ?" "keybindings" "$help"
assert_contains "TUI help lists palette hint" "palette" "$help"
send " "; sleep 0.3   # close help

# T3: quit works
stop_cn

# T4: palette opens and preloads entities from all clouds
start_cn
send ":"; sleep 12
pal=$(grab)
assert_contains "palette opens on :" "palette" "$pal"
assert_contains "palette shows cloud switcher for azure" "switch to azure" "$pal"
assert_contains "palette shows cloud switcher for gcp" "switch to gcp" "$pal"
assert_contains "palette shows cloud switcher for aws" "switch to aws" "$pal"
send Escape; sleep 0.3
stop_cn

# T5: drill azure and verify tenant column resolves
start_cn
send Enter; sleep 10
azure=$(grab)
assert_contains "azure drill breadcrumbs updated" "clouds › azure" "$azure"
if echo "$azure" | grep -q 'Civica'; then
  pass "azure subs show resolved tenant display name"
else
  fail "azure tenant column" "expected a 'Civica ...' tenant label in subs view"
fi
stop_cn

# T6: drill aws to regions
start_cn
send j; sleep 0.3
send j; sleep 0.3
send Enter; sleep 6
send Enter; sleep 8
aws=$(grab)
assert_contains "aws drill reaches regions" "clouds › aws" "$aws"
assert_regex "aws regions list contains us-east-1 or similar" 'us-east-[12]|eu-west-|ap-south-' "$aws"
stop_cn

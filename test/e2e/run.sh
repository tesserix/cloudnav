#!/usr/bin/env bash
set -u
IFS=$'\n\t'

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BIN="$ROOT/bin/cloudnav"

RED=$'\033[0;31m'; GREEN=$'\033[0;32m'; CYAN=$'\033[0;36m'; DIM=$'\033[2m'; NC=$'\033[0m'

PASS=0
FAIL=0
FAIL_NAMES=()

log() { printf "%s\n" "$*" >&2; }

pass() { PASS=$((PASS+1)); printf "  %s✓%s %s\n" "$GREEN" "$NC" "$1"; }
fail() { FAIL=$((FAIL+1)); FAIL_NAMES+=("$1"); printf "  %s✗ %s%s\n%s%s%s\n" "$RED" "$1" "$NC" "$DIM" "$2" "$NC"; }

assert_contains() {
  local name="$1" needle="$2" haystack="$3"
  if printf "%s" "$haystack" | grep -q -F "$needle"; then
    pass "$name"
  else
    fail "$name" "expected to find '$needle' in output; got:\n$haystack"
  fi
}

assert_regex() {
  local name="$1" pattern="$2" haystack="$3"
  if printf "%s" "$haystack" | grep -Eq "$pattern"; then
    pass "$name"
  else
    fail "$name" "expected regex '$pattern' to match; got:\n$haystack"
  fi
}

assert_exit() {
  local name="$1" want="$2" got="$3"
  if [[ "$got" == "$want" ]]; then
    pass "$name"
  else
    fail "$name" "expected exit $want, got $got"
  fi
}

if [[ ! -x "$BIN" ]]; then
  log "$RED""error:$NC $BIN not found — run make build first"
  exit 1
fi

log "${CYAN}cloudnav e2e suite${NC}"
log "binary: $BIN"
log ""

for f in "$HERE"/*_test.sh; do
  [[ -f "$f" ]] || continue
  log "${CYAN}→ $(basename "$f" _test.sh)${NC}"
  # shellcheck disable=SC1090
  source "$f"
  log ""
done

log ""
log "${CYAN}summary:${NC} $GREEN$PASS passed$NC, $RED$FAIL failed$NC"
if (( FAIL > 0 )); then
  for n in "${FAIL_NAMES[@]}"; do log "  - $n"; done
  exit 1
fi

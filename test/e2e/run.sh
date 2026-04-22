#!/usr/bin/env bash
set -u
IFS=$'\n\t'

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BIN="$ROOT/bin/cloudnav"

# Real e2e runs hit the operator's live cloud, so the tests need a
# specific subscription name to drill into. Kept as an env var override
# with a neutral default so nothing operator-specific lands in the
# public repo. Set CLOUDNAV_E2E_AZURE_SUB=<your-sub-name> locally when
# running against a real tenant.
export CLOUDNAV_E2E_AZURE_SUB="${CLOUDNAV_E2E_AZURE_SUB:-acme-prod}"
# Subscription GUID for the cost-rgs drill. Defaults to an all-zero
# placeholder that the cost API will reject, which the test tolerates as
# a clean-error case. Operators set CLOUDNAV_E2E_AZURE_SUB_ID to the
# actual GUID when validating against a real tenant.
export CLOUDNAV_E2E_AZURE_SUB_ID="${CLOUDNAV_E2E_AZURE_SUB_ID:-00000000-0000-0000-0000-000000000000}"
# Some assertions look for any tenant-display-name pattern in the
# subscription list. A generic substring that matches any ARM-resolved
# tenant name keeps the e2e passing on a fresh account — operators who
# want to assert against their specific org set CLOUDNAV_E2E_TENANT_PATTERN.
export CLOUDNAV_E2E_TENANT_PATTERN="${CLOUDNAV_E2E_TENANT_PATTERN:-.}"
# Name of a resource group known to carry an Azure lock, used by
# rg_test.sh to assert the 🔒 badge renders. Default is a neutral name
# that won't exist in a fresh tenant; the test tolerates a miss.
export CLOUDNAV_E2E_LOCKED_RG="${CLOUDNAV_E2E_LOCKED_RG:-locked-rg}"

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

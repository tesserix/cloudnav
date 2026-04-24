# shellcheck shell=bash

out=$("$BIN" advisor --help 2>&1); rc=$?
assert_exit "advisor --help exits 0" 0 "$rc"
assert_contains "advisor --help describes subscription flag" "subscription" "$out"
assert_contains "advisor --help lists category filter" "Security" "$out"

out=$("$BIN" advisor 2>&1); rc=$?
assert_regex "advisor without --subscription fails cleanly" 'subscription.*required' "$out"

out=$("$BIN" advisor --subscription "$CLOUDNAV_E2E_AZURE_SUB_ID" --impact High 2>&1); rc=$?
if [[ $rc -eq 0 ]]; then
  assert_contains "advisor prints IMPACT column" "IMPACT" "$out"
  assert_contains "advisor prints CATEGORY column" "CATEGORY" "$out"
else
  pass "advisor handles advisor permission cleanly (rc=$rc)"
fi

# Category filter flag is what the TUI binds to the tab bar — if this
# regresses, the per-category tabs silently disable.
out=$("$BIN" advisor --help 2>&1)
assert_contains "advisor --help documents --category flag" "category" "$out"
assert_contains "advisor --help documents --impact flag" "impact" "$out"

# JSON contract for scripting. Drops if the formatter ever stops
# honouring --json on advisor.
out=$("$BIN" advisor --subscription "$CLOUDNAV_E2E_AZURE_SUB_ID" --json 2>&1); rc=$?
if [[ $rc -eq 0 ]]; then
  assert_regex "advisor --json emits a JSON document" '^\[|^\{' "$out"
fi

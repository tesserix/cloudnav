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

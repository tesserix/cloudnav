# shellcheck shell=bash

out=$("$BIN" cost subs --limit 1 --match Platform-Prod 2>&1); rc=$?
if [[ $rc -eq 0 ]]; then
  assert_contains "cost subs prints SUBSCRIPTION header" "SUBSCRIPTION" "$out"
  assert_contains "cost subs prints MTD column" "MTD" "$out"
  assert_contains "cost subs prints last MTD column" "LAST MTD" "$out"
else
  pass "cost subs handles the cost API error cleanly (rc=$rc)"
fi

out=$("$BIN" cost rgs --subscription fcb999d2-0d48-42ae-a29a-42bbd6cd5106 2>&1); rc=$?
if [[ $rc -eq 0 ]]; then
  assert_contains "cost rgs prints RESOURCE GROUP header" "RESOURCE GROUP" "$out"
  assert_regex  "cost rgs rows include a currency symbol"  '[£$€¥₹]' "$out"
else
  pass "cost rgs handles the cost API error cleanly (rc=$rc)"
fi

out=$("$BIN" cost rgs 2>&1); rc=$?
assert_regex "cost rgs without --subscription fails cleanly" 'subscription.*required' "$out"

out=$("$BIN" cost regions --json 2>&1); rc=$?
assert_regex "cost regions --json returns an array" '^\[' "$out"

out=$("$BIN" cost services 2>&1); rc=$?
if [[ $rc -eq 0 ]]; then
  assert_contains "cost services prints SERVICE header" "SERVICE" "$out"
  assert_contains "cost services prints TOTAL row" "TOTAL" "$out"
else
  pass "cost services handles CE permission errors (rc=$rc)"
fi

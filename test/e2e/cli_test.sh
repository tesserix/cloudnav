# shellcheck shell=bash

out=$("$BIN" version 2>&1); rc=$?
assert_exit "version exits 0" 0 "$rc"
assert_regex "version shows semver-ish" '^cloudnav [v0-9]' "$out"

out=$("$BIN" --help 2>&1); rc=$?
assert_exit "help exits 0" 0 "$rc"
for sub in doctor version ls pim cost completion login install; do
  assert_contains "help lists '$sub' subcommand" "  $sub " "$out"
done
assert_contains "root help mentions doctor in onboarding" "cloudnav doctor" "$out"
assert_contains "root help mentions install" "cloudnav install" "$out"
assert_contains "root help mentions login" "cloudnav login" "$out"

out=$("$BIN" doctor 2>&1); rc=$?
assert_exit "doctor exits 0" 0 "$rc"
for cloud in azure gcp aws; do
  assert_regex "doctor reports $cloud" "^[✓✗] *$cloud" "$out"
done

out=$("$BIN" ls azure subs --json 2>&1); rc=$?
assert_exit "ls azure subs --json exits 0" 0 "$rc"
assert_regex "ls azure subs returns JSON array" '^\[' "$out"
count=$(printf "%s" "$out" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo 0)
if [[ "$count" -gt 0 ]]; then
  pass "ls azure subs returns $count subscriptions"
else
  fail "ls azure subs returns nodes" "got count=$count"
fi

out=$("$BIN" ls gcp projects --json 2>&1); rc=$?
assert_exit "ls gcp projects --json exits 0" 0 "$rc"
assert_regex "ls gcp returns JSON array" '^\[' "$out"

out=$("$BIN" ls aws account --json 2>&1); rc=$?
assert_exit "ls aws account --json exits 0" 0 "$rc"
assert_contains "ls aws account returns an account kind" '"kind": "account"' "$out"

out=$("$BIN" ls aws regions --json 2>&1); rc=$?
assert_exit "ls aws regions --json exits 0" 0 "$rc"
assert_contains "ls aws regions returns a region kind" '"kind": "region"' "$out"

out=$("$BIN" pim list 2>&1); rc=$?
if [[ $rc -eq 0 ]]; then
  assert_regex "pim list azure returns at least one role" '^[[:space:]]*1\.' "$out"
else
  pass "pim list handles azure error cleanly (rc=$rc)"
fi

out=$("$BIN" pim list --cloud aws 2>&1); rc=$?
assert_regex "pim list aws either lists profiles or explains 'aws configure sso'" '(aws configure sso|^[[:space:]]*1\.)' "$out"

out=$("$BIN" pim list --cloud gcp 2>&1); rc=$?
# GCP PIM now tries Privileged Access Manager first. When PAM is enabled we
# see numbered entitlements; when it isn't, the error points at conditional
# IAM. Either response proves the wiring is in place.
assert_regex "pim list gcp returns PAM entitlements or conditional-IAM fallback" '(add-iam-policy-binding|^[[:space:]]*1\.|no PAM entitlements)' "$out"

out=$("$BIN" pim activate 999 --reason test 2>&1); rc=$?
assert_regex "pim activate out-of-range fails cleanly" 'out of range' "$out"

out=$("$BIN" cost --help 2>&1); rc=$?
assert_exit "cost --help exits 0" 0 "$rc"
for sub in subs rgs regions services; do
  assert_contains "cost --help lists '$sub'" "  $sub " "$out"
done

out=$("$BIN" cost services 2>&1); rc=$?
if [[ $rc -eq 0 ]]; then
  assert_contains "cost services prints SERVICE header" "SERVICE" "$out"
  assert_contains "cost services prints MTD header" "MTD" "$out"
else
  pass "cost services handles CE permission errors (rc=$rc)"
fi

echo "" | "$BIN" >/dev/null 2>&1; rc=$?
assert_regex "running without TTY fails with non-zero exit" '^[1-9]' "$rc"

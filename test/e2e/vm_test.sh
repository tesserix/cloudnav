# shellcheck shell=bash

out=$("$BIN" vm --help 2>&1); rc=$?
assert_exit "vm --help exits 0" 0 "$rc"
for sub in list show start stop; do
  assert_contains "vm --help lists '$sub'" "  $sub " "$out"
done
assert_contains "vm help mentions --yes guard" "yes" "$out"

out=$("$BIN" vm list --cloud aws --region us-east-1 --json 2>&1); rc=$?
assert_exit "vm list aws us-east-1 --json exits 0" 0 "$rc"
assert_regex "vm list aws --json returns an array" '^\[' "$out"

out=$("$BIN" vm start i-fake-id --cloud aws --region us-east-1 2>&1); rc=$?
assert_regex "vm start without --yes is refused" 'requires --yes' "$out"

out=$("$BIN" vm stop i-fake-id --cloud aws --region us-east-1 2>&1); rc=$?
assert_regex "vm stop without --yes is refused" 'requires --yes' "$out"

out=$("$BIN" vm list --cloud aws 2>&1); rc=$?
assert_regex "vm list aws without --region fails cleanly" 'region.*required' "$out"

out=$("$BIN" vm list --cloud gcp 2>&1); rc=$?
assert_regex "vm list gcp without --project fails cleanly" 'project.*required' "$out"

out=$("$BIN" vm list --cloud azure 2>&1); rc=$?
assert_regex "vm list azure without scope fails cleanly" 'subscription.*required' "$out"

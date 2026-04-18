# shellcheck shell=bash
#
# Covers the first-run onboarding surface added in v0.9.x:
#   - `cloudnav login <cloud>` wraps az / gcloud / aws
#   - `cloudnav install <cloud>` dispatches per-OS install recipes
#   - `cloudnav doctor` prints actionable next steps
# The assertions here don't actually install or log in — they just exercise
# the arg parsing and help strings so regressions in the UX-facing copy get
# caught.

# login
out=$("$BIN" login --help 2>&1); rc=$?
assert_exit "login --help exits 0" 0 "$rc"
assert_contains "login help mentions the cloud arg" "login [cloud]" "$out"
for c in azure aws gcp; do
  assert_contains "login help accepts '$c'" "$c" "$out"
done

out=$("$BIN" login 2>&1); rc=$?
assert_regex "login without arg fails cleanly" 'accepts 1 arg' "$out"

out=$("$BIN" login bogus 2>&1); rc=$?
assert_regex "login rejects unknown cloud" '(invalid argument|unknown cloud)' "$out"

# install
out=$("$BIN" install --help 2>&1); rc=$?
assert_exit "install --help exits 0" 0 "$rc"
assert_contains "install help mentions brew / winget dispatch" "Homebrew" "$out"
assert_contains "install help mentions credentials storage" "~/.azure" "$out"

out=$("$BIN" install 2>&1); rc=$?
assert_regex "install without arg fails cleanly" 'accepts 1 arg' "$out"

out=$("$BIN" install bogus 2>&1); rc=$?
assert_regex "install rejects unknown cloud" '(invalid argument|unknown cloud)' "$out"

# doctor — output format changed to suggest `cloudnav login` / `cloudnav install`
out=$("$BIN" doctor 2>&1); rc=$?
assert_exit "doctor exits 0 regardless of login state" 0 "$rc"
# If any cloud is not logged in, doctor should point at `cloudnav login`
if echo "$out" | grep -q "✗"; then
  if echo "$out" | grep -q "not logged in"; then
    assert_contains "doctor tells the user to run 'cloudnav login'" "cloudnav login" "$out"
  fi
  if echo "$out" | grep -q "not installed"; then
    assert_contains "doctor tells the user to run 'cloudnav install'" "cloudnav install" "$out"
  fi
else
  pass "doctor output is all ✓ — no actionable hints to test"
fi

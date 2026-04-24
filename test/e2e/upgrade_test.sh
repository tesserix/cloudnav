#!/usr/bin/env bash
# e2e: verify the `cloudnav version` output stays parseable so the
# post-upgrade verification in internal/updatecheck/upgrade.go
# (installedVersion) can detect whether a brew/go-install actually
# moved the binary. Breaking the format of the version line would
# re-introduce the silent-no-op bug we fixed in 0.22.7 / 0.22.8.
set -u
IFS=$'\n\t'

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BIN="${BIN:-$ROOT/bin/cloudnav}"

if [[ ! -x "$BIN" ]]; then
  echo "SKIP: build binary first (make build or go build -o bin/cloudnav ./cmd/cloudnav)"
  exit 0
fi

out="$("$BIN" version 2>&1)"
first_line="$(head -n1 <<<"$out")"

# Expected shape: "cloudnav <version> (<commit> · <date>)"
#   field 1  = "cloudnav"
#   field 2  = semver-ish version (what installedVersion parses)
# The default IFS is overridden at the top of run.sh; use whitespace
# here so `read -r` splits the line on spaces.
IFS=' ' read -r name ver rest <<<"$first_line"

fail() {
  echo "FAIL: $1"
  echo "  got: $first_line"
  exit 1
}

[[ "$name" == "cloudnav" ]]        || fail "first field should be 'cloudnav'"
[[ -n "$ver" ]]                    || fail "version token missing"
# Version is either a semver (0.22.8) or 'dev' on local builds. Both
# are OK — installedVersion treats dev as older than any real tag so
# a follow-up release will still be detected.
[[ "$ver" =~ ^((v)?[0-9]+\.[0-9]+|dev|unknown)$ ]] || fail "version token not semver or dev: $ver"

echo "PASS: cloudnav version prints parseable '$name $ver ...'"

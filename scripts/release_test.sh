#!/usr/bin/env bash
set -euo pipefail
config="$PWD/cliff.toml"
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
cd "$fixture"
git init -q
git config user.name Test
git config user.email test@example.com
git config commit.gpgsign false
git config tag.gpgsign false
git commit -q --allow-empty -m 'feat: initial release'
git tag v0.1.2

check_bump() {
  local message=$1 expected=$2
  git checkout -q --detach v0.1.2
  git commit -q --allow-empty -m "$message"
  actual=$(git cliff --config "$config" --offline --unreleased --bumped-version)
  [ "$actual" = "$expected" ] || { echo "$message: expected $expected, got $actual" >&2; exit 1; }
}
check_bump 'fix: repair lookup' v0.1.3
check_bump 'perf: reduce lookup work' v0.1.3
check_bump 'feat: add lookup mode' v0.2.0
check_bump 'docs: explain lookup' v0.1.3
check_bump 'build: simplify packaging' v0.1.3
check_bump $'feat!: change lookup\n\nBREAKING CHANGE: Use the new lookup flag.' v1.0.0
check_bump $'build!: change installation\n\nBREAKING CHANGE: Reinstall the plugin.' v1.0.0
notes=$(git cliff --config "$config" --offline --unreleased)
[[ "$notes" == *'Reinstall the plugin.'* ]]
[[ "$notes" == *'https://github.com/azohra/op-agent/commit/'* ]]
check_bump $'Legacy maintenance summary\n\nLong historical discussion.' v0.1.2
notes=$(git cliff --config "$config" --offline --unreleased)
[[ "$notes" != *'Legacy maintenance summary'* ]]
[[ "$notes" != *'Long historical discussion.'* ]]
echo 'Release version and notes checks passed'

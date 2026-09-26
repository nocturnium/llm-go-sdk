#!/usr/bin/env bash
# release-decide_test.sh: dry-runs release-decide.sh against a scratch repository.
#
# Covers the cases the two release lanes must get right:
#   a) a feat(llmadk) commit touching only llmadk/ cuts no root release
#   b) a commit touching both trees cuts a release in both lanes
#   c) the first llmadk release, with no llmadk tag yet, is llmadk/v0.1.0
#   d) root and llmadk tags on the same commit each resolve to their own lane
#   e) llmadk's own workflow files count for the llmadk lane only
# plus the plain root paths (docs-only skips, fix bumps patch, feat bumps minor).
#
# Usage: GIT_CLIFF=/path/to/git-cliff scripts/release-decide_test.sh
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
decide="${here}/release-decide.sh"
cliff_config="${here}/../cliff.toml"
repo="$(mktemp -d)"
trap 'rm -rf "$repo"' EXIT
export CLIFF_CONFIG="$cliff_config"
unset GITHUB_OUTPUT

cd "$repo"
git init -q
git config user.email test@example.com
git config user.name test
git config commit.gpgsign false
git config tag.gpgsign false

failures=0
commit() { # commit <message> <file>...
	local msg="$1"
	shift
	for f in "$@"; do
		mkdir -p "$(dirname "$f")"
		echo "$RANDOM" >>"$f"
	done
	git add -A
	git commit -qm "$msg"
}
expect() { # expect <lane> <want release> <want version or ->
	local out release version
	out="$("$decide" "$1")"
	release="$(sed -n 's/^release=//p' <<<"$out")"
	version="$(sed -n 's/^version=//p' <<<"$out")"
	if [ "$release" != "$2" ] || { [ "$3" != - ] && [ "$version" != "$3" ]; }; then
		echo "FAIL ${CASE} lane=$1: want release=$2 version=$3, got release=$release version=$version"
		printf '    %s\n' "$out"
		failures=$((failures + 1))
	else
		echo "ok   ${CASE} lane=$1 release=$release ${version}"
	fi
}

commit "chore: init" README.md
git tag -a v6.9.5 -m v6.9.5

CASE=docs-only
commit "docs: readme" README.md
expect root false -

CASE=a
commit "feat(llmadk): scaffold" llmadk/go.mod
expect root false -
CASE=c
expect llmadk true llmadk/v0.1.0

CASE=b
commit "feat: touch both trees" llms.go llmadk/model.go
expect root true v6.10.0
expect llmadk true llmadk/v0.1.0

CASE=d
git tag -a v6.10.0 -m v6.10.0
git tag -a llmadk/v0.1.0 -m llmadk/v0.1.0
expect root false -
expect llmadk false -
if [ "$(git describe --tags --abbrev=0 --match 'v[0-9]*')" != v6.10.0 ]; then
	echo "FAIL d: root describe did not resolve v6.10.0"
	failures=$((failures + 1))
fi

CASE=d-after
commit "fix(llmadk): adapter fix" llmadk/model.go
expect root false -
expect llmadk true llmadk/v0.1.1
commit "fix: root fix" llms.go
expect root true v6.10.1

CASE=llmadk-workflow
git tag -a v6.10.1 -m v6.10.1
commit "feat(llmadk): run CI on the module" llmadk/model.go .github/workflows/llmadk.yml
expect root false -

CASE=v0-breaking
commit "feat(llmadk)!: break adapter" llmadk/model.go
expect llmadk true llmadk/v0.2.0

if [ "$failures" -gt 0 ]; then
	echo "${failures} failure(s)"
	exit 1
fi
echo "all release-lane cases pass"

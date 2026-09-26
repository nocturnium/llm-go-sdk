#!/usr/bin/env bash
# release-decide.sh LANE
#
# Decides whether a release lane has releasable commits since its last tag and, if
# so, which version to cut. The repository holds two Go modules that release
# independently:
#
#   root    module github.com/nocturnium/llm-go-sdk/v6, tags vX.Y.Z, every path
#           except llmadk/
#   llmadk  module github.com/nocturnium/llm-go-sdk/llmadk, tags llmadk/vX.Y.Z,
#           only llmadk/
#
# Each lane matches only its own tags (a bare `git describe` would return
# whichever tag is newest, so an llmadk tag would poison the root lane) and counts
# only commits that touch its own paths, so a feat(llmadk) commit cannot cut a
# root release. A commit touching both trees counts for both lanes.
#
# Output (appended to $GITHUB_OUTPUT when set, always echoed):
#   release=true|false  version=<tag>  previous=<tag or none>
#
# Requires git-cliff (for commit classification with cliff.toml) and jq.
set -euo pipefail

lane="${1:?usage: release-decide.sh root|llmadk}"
cliff="${GIT_CLIFF:-git-cliff}"
config="${CLIFF_CONFIG:-cliff.toml}"

case "$lane" in
root)
	match='v[0-9]*'
	pattern='^v[0-9]+\.'
	pathflag=(--exclude-path 'llmadk/**')
	prefix='v'
	;;
llmadk)
	match='llmadk/v[0-9]*'
	pattern='^llmadk/v[0-9]+\.'
	pathflag=(--include-path 'llmadk/**')
	prefix='llmadk/v'
	;;
*)
	echo "unknown lane: $lane" >&2
	exit 2
	;;
esac

emit() {
	echo "$1"
	if [ -n "${GITHUB_OUTPUT:-}" ]; then
		echo "$1" >>"$GITHUB_OUTPUT"
	fi
}

previous="$(git describe --tags --abbrev=0 --match "$match" 2>/dev/null || echo none)"
emit "previous=${previous}"

# The context JSON lists the commits git-cliff classified, restricted to this
# lane's paths. The range is passed explicitly rather than left to --unreleased:
# when a root tag and an llmadk tag sit on the same commit, git-cliff attributes the
# commit to one of them and drops the other lane's boundary. Only feat/fix/perf/
# breaking commits are releasable; `--bumped-version` alone would bump a patch for
# a docs-only commit.
range=()
if [ "$previous" != none ]; then
	range=("${previous}..HEAD")
fi
context="$("$cliff" --config "$config" --tag-pattern "$pattern" "${pathflag[@]}" --context "${range[@]}" 2>/dev/null || echo '[]')"
count() {
	jq "[.[].commits[]? | select($1)] | length" <<<"$context" 2>/dev/null || echo 0
}
breaking="$(count '.breaking==true')"
features="$(count '.group=="Features"')"
fixes="$(count '.group=="Bug Fixes" or .group=="Performance"')"
for n in breaking features fixes; do
	case "${!n}" in '' | *[!0-9]*) printf -v "$n" 0 ;; esac
done
releasable=$((breaking + features + fixes))
echo "lane=${lane} previous=${previous} breaking=${breaking} features=${features} fixes=${fixes}"

if [ "$releasable" -eq 0 ]; then
	emit "release=false"
	exit 0
fi

if [ "$previous" = none ]; then
	if [ "$lane" = root ]; then
		echo "::error::root lane has no previous tag; refusing to invent a version" >&2
		emit "release=false"
		exit 0
	fi
	next="${prefix}0.1.0"
else
	ver="${previous#"$prefix"}"
	ver="${ver%%-*}"
	IFS=. read -r major minor patch <<<"$ver"
	for n in major minor patch; do
		case "${!n}" in '' | *[!0-9]*)
			echo "::error::unparseable previous tag ${previous}; not releasing" >&2
			emit "release=false"
			exit 0
			;;
		esac
	done
	if [ "$breaking" -gt 0 ] && [ "$major" -gt 0 ]; then
		major=$((major + 1)) minor=0 patch=0
	elif [ "$breaking" -gt 0 ] || [ "$features" -gt 0 ]; then
		# Below v1 a breaking change bumps the minor, per semver's 0.y rule.
		minor=$((minor + 1)) patch=0
	else
		patch=$((patch + 1))
	fi
	next="${prefix}${major}.${minor}.${patch}"
fi

if git rev-parse -q --verify "refs/tags/${next}" >/dev/null; then
	echo "::warning::computed ${next} already exists; not releasing" >&2
	emit "release=false"
	exit 0
fi
emit "release=true"
emit "version=${next}"

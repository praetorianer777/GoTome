#!/usr/bin/env bash
# Cuts a release: ./release.sh <version> [--dry-run]
#
# Moves what CHANGELOG.md lists under "Unreleased" into a section for the
# version, writes VERSION, commits both on main and tags the commit. Nothing is
# built here and nothing is pushed: pushing the tag is what makes the release
# workflow build the images and publish the release, and the last line printed
# is the command that does it.
set -euo pipefail
cd "$(dirname "$0")"

die() { echo "release: $*" >&2; exit 1; }

VERSION_ARG=""
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -*) die "unknown option $arg" ;;
    *) [[ -z "$VERSION_ARG" ]] || die "one version, please"; VERSION_ARG="$arg" ;;
  esac
done
[[ -n "$VERSION_ARG" ]] || die "usage: ./release.sh <version> [--dry-run]   (the current version is $(cat VERSION))"

NEW="${VERSION_ARG#v}"
[[ "$NEW" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "'$VERSION_ARG' is not a version like 1.2.3"
TAG="v$NEW"
CURRENT="$(cat VERSION)"
# The date is overridable so the tests produce the same changelog every day.
DATE="${RELEASE_DATE:-$(date +%Y-%m-%d)}"

# ── The checkout ─────────────────────────────────────────
# A release cut from a stale or dirty checkout tags code that is not what main
# holds: the tag pushes, the branch is rejected, and the images are built from
# a commit nobody can see on main.
branch="$(git symbolic-ref --quiet --short HEAD || true)"
[[ "$branch" == "main" ]] || die "releases are cut on main, and this is '${branch:-a detached HEAD}'"
[[ -z "$(git status --porcelain)" ]] || die "the working tree has changes; commit or stash them first"
git fetch --quiet origin main
[[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" ]] \
  || die "main is not level with origin/main; pull (or push) first"
git rev-parse --quiet --verify "refs/tags/$TAG" >/dev/null && die "the tag $TAG exists already"

# ── The version ──────────────────────────────────────────
newer() { [[ "$1" != "$2" && "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n1)" == "$1" ]]; }
# The first release may be of the version the file already carries.
if git describe --tags --abbrev=0 >/dev/null 2>&1 || [[ "$NEW" != "$CURRENT" ]]; then
  newer "$NEW" "$CURRENT" || die "$NEW is not above the current version $CURRENT"
fi

# ── The notes ────────────────────────────────────────────
# Everything between "## [Unreleased]" and the next "## [" heading.
NOTES="$(awk '
  /^## \[Unreleased\]/ { on = 1; next }
  /^## \[/             { on = 0 }
  on                   { print }
' CHANGELOG.md | sed -e '/./,$!d' | sed -e ':a' -e '/^\n*$/{$d;N;ba' -e '}')"
[[ -n "$NOTES" ]] || die "CHANGELOG.md lists nothing under Unreleased; a release needs notes"

echo "Release $TAG ($DATE), from $CURRENT"
echo
echo "$NOTES"
echo
if (( DRY_RUN )); then
  echo "Dry run: nothing was changed."
  exit 0
fi

# ── Write, commit, tag ───────────────────────────────────
echo "$NEW" > VERSION
awk -v heading="## [$NEW] - $DATE" '
  /^## \[Unreleased\]/ { print; print ""; print heading; next }
  { print }
' CHANGELOG.md > CHANGELOG.md.new
mv CHANGELOG.md.new CHANGELOG.md

# Only the two files this release rewrote.
git add VERSION CHANGELOG.md
git commit --quiet -m "Release $TAG"
# Annotated with the notes: the release workflow reads them from the tag.
git tag -a "$TAG" -m "GOtome $TAG" -m "$NOTES"

echo "Committed and tagged $TAG. Publish it with:"
echo "  git push origin main $TAG"

#!/usr/bin/env bash
# Cuts a release: ./release.sh [<version>|major|minor|patch] [--dry-run]
#
# Without a version it works one out from what was merged since the last tag:
# the branch types of its pull requests (asked of gh) and the sections of the
# changelog's Unreleased entries.
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
CURRENT="$(cat VERSION)"
# The date is overridable so the tests produce the same changelog every day.
DATE="${RELEASE_DATE:-$(date +%Y-%m-%d)}"
# The tests hand in a stand-in.
GH="${RELEASE_GH:-gh}"

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
# ── The notes ────────────────────────────────────────────
# Everything between "## [Unreleased]" and the next "## [" heading.
NOTES="$(awk '
  /^## \[Unreleased\]/ { on = 1; next }
  /^## \[/             { on = 0 }
  on                   { print }
' CHANGELOG.md | sed -e '/./,$!d' | sed -e ':a' -e '/^\n*$/{$d;N;ba' -e '}')"
[[ -n "$NOTES" ]] || die "CHANGELOG.md lists nothing under Unreleased; a release needs notes"

# ── The version ──────────────────────────────────────────
LAST_TAG="$(git describe --tags --abbrev=0 2>/dev/null || true)"

# bump_of_changes: major, minor or patch for what was merged since the last
# tag, with the reason on stderr. A feature is minor and anything else patch;
# a break is major, but minor before 1.0, as semver leaves 0.x to change.
bump_of_changes() {
  local sections types="" counts="" kind=patch why reason="" n
  sections="$(grep -oE '^### [A-Za-z]+' <<< "$NOTES" | cut -c5- | sort -u | tr '\n' ' ')"
  if [[ -n "$LAST_TAG" ]]; then
    local prs; prs="$(git log --format=%s "$LAST_TAG..HEAD" | sed -nE 's/.*\(#([0-9]+)\)$/\1/p')"
    for n in $prs; do
      local branch; branch="$("$GH" pr view "$n" --json headRefName -q .headRefName 2>/dev/null)" || { types=""; reason="gh could not be asked, so the changelog decides"; break; }
      types+="${branch%%/*}"$'\n'
    done
    [[ -n "$types" ]] && counts="$(sort <<< "${types%$'\n'}" | uniq -c | awk '{printf "%s%d %s", (NR>1 ? ", " : ""), $1, $2}')"
  fi
  if git log --format=%B "${LAST_TAG:+$LAST_TAG..}HEAD" | grep -q 'BREAKING CHANGE' || [[ " $sections" == *" Removed "* ]]; then
    kind=major; why="a breaking change"
  elif grep -qx feat <<< "$types" || [[ " $sections" =~ \ (Added|Changed|Deprecated)\  ]]; then
    kind=minor; why="new or changed features"
  else
    why="fixes only"
  fi
  if [[ "$kind" == major && "${CURRENT%%.*}" == 0 ]]; then kind=minor; why="$why, before 1.0"; fi
  {
    echo "Since ${LAST_TAG:-the start}: ${counts:-no pull requests found}; changelog: ${sections:-nothing}"
    [[ -n "$reason" ]] && echo "($reason)"
    echo "That is $why: a $kind release."
  } >&2
  echo "$kind"
}

bump() {
  local major minor patch; IFS=. read -r major minor patch <<< "$CURRENT"
  case "$1" in
    major) echo "$((major + 1)).0.0" ;;
    minor) echo "$major.$((minor + 1)).0" ;;
    patch) echo "$major.$minor.$((patch + 1))" ;;
  esac
}

case "${VERSION_ARG#v}" in
  ""|auto)
    # The first release is of the version the file carries.
    if [[ -z "$LAST_TAG" ]]; then NEW="$CURRENT"; else NEW="$(bump "$(bump_of_changes)")"; fi ;;
  major|minor|patch) NEW="$(bump "$VERSION_ARG")" ;;
  *) NEW="${VERSION_ARG#v}" ;;
esac
[[ "$NEW" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "'$VERSION_ARG' is not a version like 1.2.3, nor major, minor or patch"
TAG="v$NEW"
git rev-parse --quiet --verify "refs/tags/$TAG" >/dev/null && die "the tag $TAG exists already"

newer() { [[ "$1" != "$2" && "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n1)" == "$1" ]]; }
# The first release may be of the version the file already carries.
if [[ -n "$LAST_TAG" || "$NEW" != "$CURRENT" ]]; then
  newer "$NEW" "$CURRENT" || die "$NEW is not above the current version $CURRENT"
fi


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

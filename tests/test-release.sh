#!/usr/bin/env bash
# Tests for release.sh. Each case runs the real script in a throwaway
# repository with a throwaway remote, so nothing is committed, tagged or pushed
# for real. Offline; needs only git: gh is a stand-in.
set -uo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

export GIT_AUTHOR_NAME=Tester GIT_AUTHOR_EMAIL=tester@example.com
export GIT_COMMITTER_NAME=Tester GIT_COMMITTER_EMAIL=tester@example.com
export RELEASE_DATE=2026-02-03

# The stand-in for gh answers a pull request's branch from FAKE_BRANCHES
# ("12=feat/12-x 13=fix/13-y"), and fails for any it does not know.
mkdir -p "$WORK/bin"
cat > "$WORK/bin/gh" <<'EOF2'
#!/usr/bin/env bash
for pair in $FAKE_BRANCHES; do
  [[ "${pair%%=*}" == "$3" ]] && { echo "${pair#*=}"; exit 0; }
done
exit 1
EOF2
chmod +x "$WORK/bin/gh"
export RELEASE_GH="$WORK/bin/gh" FAKE_BRANCHES=""

FAILED=0
pass() { echo "   ✅ $1"; }
fail() { echo "   ❌ $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

# sandbox <name>: a repository on main at version 0.1.0 with two unreleased
# entries, level with its remote. Leaves the shell inside it.
sandbox() {
  local dir="$WORK/$1"
  git init -q --bare "$dir-origin.git"
  git init -q -b main "$dir"
  cd "$dir" || exit 1
  cp "$REPO/release.sh" .
  echo "0.1.0" > VERSION
  cat > CHANGELOG.md <<'EOF'
# Changelog

## [Unreleased]

### Added

- Books can be uploaded.

### Fixed

- The scanner no longer misses renamed files.

## [0.1.0] - 2026-01-01

### Added

- The first release.
EOF
  git add -A && git commit -q -m "fixture"
  git tag -a v0.1.0 -m "GOtome v0.1.0"
  git remote add origin "$dir-origin.git"
  git push -q origin main v0.1.0
}

# refused <label> <args…>: the script must fail and change nothing.
refused() {
  local label="$1"; shift
  local before; before="$(git rev-parse HEAD)$(cat VERSION)$(git tag | sort | tr '\n' ' ')"
  if ./release.sh "$@" >/dev/null 2>&1; then
    fail "$label: was accepted"
  elif [[ "$(git rev-parse HEAD)$(cat VERSION)$(git tag | sort | tr '\n' ' ')" != "$before" ]]; then
    fail "$label: was refused, but something changed"
  else
    pass "$label"
  fi
}

echo "== a release"
sandbox release
out="$(./release.sh 0.2.0 2>&1)"; status=$?
check "exits cleanly" "$status" "0"
check "VERSION is written" "$(cat VERSION)" "0.2.0"
check "the commit is the release" "$(git log -1 --format=%s)" "Release v0.2.0"
check "only VERSION and the changelog are in it" "$(git show --name-only --format= HEAD | sort | tr '\n' ' ')" "CHANGELOG.md VERSION "
check "the tag points at it" "$(git rev-parse 'v0.2.0^{commit}')" "$(git rev-parse HEAD)"
check "the tag is annotated" "$(git cat-file -t v0.2.0)" "tag"
check "the tag carries the notes" "$(git tag -l --format='%(contents)' v0.2.0 | grep -c 'Books can be uploaded\|renamed files')" "2"
check "the changelog has the dated section" "$(grep -c '^## \[0.2.0\] - 2026-02-03$' CHANGELOG.md)" "1"
check "Unreleased is kept and empty" "$(awk '/^## \[Unreleased\]/{on=1;next} /^## \[/{on=0} on && NF' CHANGELOG.md | wc -l | tr -d ' ')" "0"
check "the entries moved under the version" "$(awk '/^## \[0.2.0\]/{on=1;next} /^## \[/{on=0} on && /^- /' CHANGELOG.md | wc -l | tr -d ' ')" "2"
check "the older section is untouched" "$(grep -c '^## \[0.1.0\] - 2026-01-01$' CHANGELOG.md)" "1"
check "nothing was pushed" "$(git ls-remote --tags origin | grep -c 'v0.2.0')" "0"
check "it says how to publish" "$(grep -c 'git push origin main v0.2.0' <<< "$out")" "1"

echo "== a dry run"
sandbox dry
out="$(./release.sh v0.2.0 --dry-run 2>&1)"; status=$?
check "exits cleanly" "$status" "0"
check "prints the notes" "$(grep -c 'Books can be uploaded' <<< "$out")" "1"
check "changes nothing" "$(cat VERSION)$(git tag | tr '\n' ' ')$(git status --porcelain)" "0.1.0v0.1.0 "

echo "== refusals"
sandbox refusals
refused "not a version" "next"
refused "an unknown part" "micro"
refused "two parts" "0.2"
refused "the current version again" "0.1.0"
refused "a lower version" "0.0.9"
refused "an unknown option" "0.2.0" "--force"

git tag v0.3.0
refused "a tag that exists" "0.3.0"
git tag -d v0.3.0 >/dev/null

echo "stray" > stray.txt
refused "a dirty tree" "0.2.0"
rm stray.txt

git switch -q -c feat/1-something
refused "another branch" "0.2.0"
git switch -q main

git commit -q --allow-empty -m "not pushed"
refused "main ahead of the remote" "0.2.0"
git reset -q --hard origin/main

awk '/^## \[Unreleased\]/{print; print ""; skip=1; next} /^## \[/{skip=0} !skip' CHANGELOG.md > c && mv c CHANGELOG.md
git commit -q -am "empty unreleased" && git push -q origin main
refused "nothing unreleased" "0.2.0"

# merged <subject> <section> <line>: a pull request's squash commit with its
# changelog entry, pushed. The sandbox's own entries are taken out first.
merged() {
  awk -v section="$2" -v line="$3" '
    /^## \[Unreleased\]/ { print; print ""; print "### " section; print ""; print "- " line; print ""; skip = 1; next }
    /^## \[/             { skip = 0 }
    !skip
  ' CHANGELOG.md > c && mv c CHANGELOG.md
  git commit -q -am "$1" && git push -q origin main
}

echo "== the version worked out"
sandbox auto-fix
merged "Fix the scanner (#12)" Fixed "The scanner works."
FAKE_BRANCHES="12=fix/12-scanner" out="$(./release.sh 2>&1)"
check "fixes only are a patch" "$(cat VERSION)" "0.1.1"
check "it says why" "$(grep -c '1 fix.*Fixed' <<< "$out")" "1"

sandbox auto-feat
merged "Add uploads (#13)" Fixed "Uploads."
FAKE_BRANCHES="13=feat/13-uploads" ./release.sh >/dev/null 2>&1
check "a feature branch is a minor release" "$(cat VERSION)" "0.2.0"

sandbox auto-added
merged "Add uploads (#14)" Added "Uploads."
FAKE_BRANCHES="" out="$(./release.sh auto 2>&1)"
check "without gh the changelog decides" "$(cat VERSION)" "0.2.0"
check "and it says so" "$(grep -c 'changelog decides' <<< "$out")" "1"

sandbox auto-break
merged "Drop the old API (#15)" Removed "The old API."
FAKE_BRANCHES="15=feat/15-drop" ./release.sh >/dev/null 2>&1
check "a break before 1.0 is a minor release" "$(cat VERSION)" "0.2.0"

sandbox auto-break-1
echo "1.4.2" > VERSION && git commit -q -am "1.4.2" && git tag -a v1.4.2 -m v1.4.2 && git push -q origin main v1.4.2
merged "Change the API (#16)" Fixed "The API." && git commit -q --allow-empty -m "Follow-up" -m "BREAKING CHANGE: the API moved" && git push -q origin main
FAKE_BRANCHES="16=fix/16-api" ./release.sh >/dev/null 2>&1
check "a break from 1.0 on is a major release" "$(cat VERSION)" "2.0.0"

sandbox auto-mention
merged "Explain breaks (#17)" Fixed "Docs." && git commit -q --allow-empty -m "Note" -m "A BREAKING CHANGE footer makes a major release." && git push -q origin main
FAKE_BRANCHES="17=fix/17-docs" ./release.sh >/dev/null 2>&1
check "a mention of a break is no break" "$(cat VERSION)" "0.1.1"

sandbox parts
./release.sh minor --dry-run >/dev/null 2>&1
check "a dry run with a part changes nothing" "$(cat VERSION)" "0.1.0"
./release.sh patch >/dev/null 2>&1
check "a part names the bump" "$(cat VERSION)" "0.1.1"

echo "== the first release of a repository"
dir="$WORK/first"
git init -q --bare "$dir-origin.git" && git init -q -b main "$dir" && cd "$dir" || exit 1
cp "$REPO/release.sh" . && echo "0.1.0" > VERSION
printf '# Changelog\n\n## [Unreleased]\n\n### Added\n\n- Everything.\n' > CHANGELOG.md
git add -A && git commit -q -m fixture && git remote add origin "$dir-origin.git" && git push -q origin main
./release.sh 0.1.0 >/dev/null 2>&1
check "may carry the version the file already has" "$(git tag)" "v0.1.0"

if (( FAILED )); then
  echo "❌ release tests failed"
  exit 1
fi
echo "✅ release tests passed"

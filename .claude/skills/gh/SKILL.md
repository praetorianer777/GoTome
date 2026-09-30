---
name: gh
description: Use the GitHub CLI (gh) for this repo — find or create issues, create issue branches, open and update PRs, check CI runs and releases. Use before starting any code change (every change needs an issue and an issue branch) and whenever GitHub issues, PRs, Actions or releases are involved.
---

# GitHub CLI (`gh`) for GOtome

Repo: `praetorianer777/GoTome`, default branch `main`.

## Rules

- Every change starts from a GitHub issue and lives on a branch `<type>/<issue>-<slug>`
  (`feat|fix|chore|docs|refactor|test|perf|ci|build|revert`, slug lowercase with dashes).
  `.claude/hooks/branch-guard.sh` blocks edits, commits and pushes anywhere else.
- Never push to `main`. Merge your own pull request once its CI is green (the owner's
  standing decision); leave anyone else's, and any with failing or missing checks,
  to the owner.
- `gh` runs non-interactively here: always pass `--title`/`--body` (or `--body-file`),
  never rely on prompts or an editor. Prefer `--json … --jq …` for reading.
- Creating issues, PRs or comments is outward-facing: confirm with the user first
  unless they already asked for it.

## Preflight

```bash
gh auth status          # not logged in → ask the user to run: ! gh auth login
```

## 1. Find or create the issue

```bash
gh issue list --state open --json number,title,labels --jq '.[] | "#\(.number) \(.title)"'
gh issue list --search "scanner in:title,body" --state all
gh issue list --milestone "M0 Foundation" --state open
gh issue view 42 --json number,title,body,state,comments,milestone
gh issue create --title "Scanner misses renamed files" --body "$(cat <<'EOF'
## Problem
…
## Expected
…
EOF
)"
```

`gh issue create` prints the issue URL; the number is its last path segment.

The backlog is filed as issues grouped into milestones `M0`–`M10`. Each issue lists
the issues it depends on under "Dependencies"; do not start one whose dependencies
are not merged into `main`.

## 2. Create the issue branch

```bash
git fetch origin
gh issue develop 42 --name fix/42-scanner-renamed-files --base main --checkout
```

This links the branch to the issue on GitHub. If the branch already exists:
`gh issue develop --list 42`, then `git switch <branch>`.

## 3. Commit and push

Commit via `/commit`. Run `./run-tests.sh` (the push gate: hook tests, plus the Go,
web and e2e stages as they exist) and fix failures before pushing — the branch guard
runs it on every `git push` and blocks the push if it fails. Push only the issue branch:

```bash
./run-tests.sh
git push -u origin HEAD
```

## 4. Pull request

```bash
gh pr create --base main --title "fix: detect renamed files in the scanner" --body "$(cat <<'EOF'
Closes #42

## Summary
…

## Testing
…
EOF
)"
gh pr view --json number,url,state,reviewDecision,statusCheckRollup
gh pr checks --watch
gh pr edit --add-label bug
gh pr comment --body "…"
```

Always put `Closes #<issue>` in the PR body.

## 5. Merge and close

```bash
gh pr checks 17 --watch
gh pr merge 17 --squash --delete-branch
gh issue close 42 --reason completed --comment "Done in #17."
git switch main && git pull --ff-only
```

Close the issue yourself: in this repository a merged pull request does not close
the issue its body names.

## Reading feedback and CI

```bash
gh pr view 17 --comments
gh api repos/{owner}/{repo}/pulls/17/comments --jq '.[] | "\(.path):\(.line) \(.body)"'
gh run list --branch "$(git branch --show-current)" --limit 5
gh run view <run-id> --log-failed
```

## Releases

Cutting a release is the user's job, not part of issue work. `./release.sh <version>`
moves the changelog's Unreleased entries under the version, commits and tags on `main`
and prints the push command; pushing the `v*` tag makes `release.yml` build the amd64
and arm64 images, publish them on GHCR and create the GitHub release. Read-only:

```bash
gh release list --limit 5
gh release view v0.1.0
```

## Pitfalls

- `gh api` placeholders `{owner}`/`{repo}` resolve from the current repo; quote the path in fish.
- Output is paged when attached to a TTY; set `GH_PAGER=cat` if a command hangs.
- `gh pr create` fails if the branch is not pushed yet — push first.

# GoTome

Self-hosted ebook, PDF and audiobook library manager. Go backend, React + Tailwind
frontend, PostgreSQL. A deployment is exactly two containers: the app and Postgres.

## Workflow

- Every change starts from a GitHub issue and lives on a branch
  `<type>/<issue>-<slug>` (`feat|fix|chore|docs|refactor|test|perf|ci|build|revert`).
  `.claude/hooks/branch-guard.sh` blocks edits, commits and pushes anywhere else.
  Use the `gh` skill for issues, branches and pull requests.
- One issue, one pull request, with `Closes #<issue>` in the body. Merging is the
  owner's decision.
- The backlog is filed as issues in milestones `M0`–`M10`. An issue names its
  dependencies; do not start it before they are merged.
- Issues #14 (search engine) and #15 (embedding runtime) end in decision records
  under `docs/decisions/`. No search or embedding feature work starts before them.

## Testing

`./run-tests.sh` is the push gate: the branch guard runs it before every `git push`,
and CI runs the same script. It must stay fast (target under five minutes) and must
work in parallel git worktrees, so it may not rely on fixed ports or fixed compose
project names. An issue that adds something testable adds its stage to the script.

The hook has its own tests in `.claude/hooks/tests/`; run them after changing a hook.

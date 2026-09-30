# GOtome

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

## Layout and toolchain

- `backend/` is the Go module; `cmd/gotome` is the one binary (`serve`,
  `healthcheck`, `version`). Configuration is `GOTOME_*` environment variables only.
- `make` runs the Go toolchain in a container (`mk/go.mk`), with caches under
  `.cache/`. `make help` lists the targets; `make check-go` is the backend gate.
- Go is pinned to 1.27 (`golang:1.27-bookworm`). A sibling project pins 1.26 because
  1.27 killed its test binaries on this host; that was checked here on 2026-09-30
  with a five-second test, plain and with `-race`, on the host and in the container,
  and did not reproduce. If it shows up, `make check-go GO_IMAGE=golang:1.26-bookworm`
  is the way to tell the toolchain apart from the code.

## Testing

`./run-tests.sh` is the push gate: the branch guard runs it before every `git push`,
and CI runs the same script. It must stay fast (target under five minutes) and must
work in parallel git worktrees, so it may not rely on fixed ports or fixed compose
project names. An issue that adds something testable adds its stage to the script.

The hook has its own tests in `.claude/hooks/tests/`; run them after changing a hook.

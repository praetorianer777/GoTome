#!/usr/bin/env bash
# The push gate: .claude/hooks/branch-guard.sh runs this before every git push,
# and CI runs the same script. Exits non-zero on the first failing stage.
#
# ./run-tests.sh --full runs everything: the browser suite in every browser
# rather than the smoke tests in one. CI does that; a push does not, so that a
# push stays quick.
#
# --only <part>[,<part>...] runs some of the stages, in the same order: shell,
# engine, go, koplugin, fuzz, web, android (with --full), integration, scale
# (with --full) and e2e. CI gives each part its own job; every stage is in
# exactly one part, so the parts together are the whole gate. Parts that need
# the stack start one of their own. E2E_PROJECT=<name> narrows a --full browser
# run to one Playwright project.
#
# Every stage is timed: the table at the end (and in the job summary of GitHub
# Actions, and in .cache/gate-timings.md) says where the minutes went.
set -euo pipefail
cd "$(dirname "$0")"

FULL=
ONLY=
while [[ $# -gt 0 ]]; do
  case "$1" in
    --full) FULL=1 ;;
    --only)
      [[ $# -ge 2 ]] || { echo "usage: $0 [--full] [--only part,part]" >&2; exit 2; }
      ONLY=",$2,"; shift ;;
    *) echo "usage: $0 [--full] [--only part,part]" >&2; exit 2 ;;
  esac
  shift
done
export FULL

PARTS="shell engine go koplugin fuzz web android integration scale e2e"
for part in $(tr ',' ' ' <<<"$ONLY"); do
  [[ " $PARTS " == *" $part "* ]] || { echo "unknown part '$part'; parts: $PARTS" >&2; exit 2; }
done

want() { [[ -z $ONLY || $ONLY == *",$1,"* ]]; }

TIMINGS=()
GATE_START=$SECONDS

# timed <stage> <command...>: runs the command and records how long it took.
timed() {
  local name=$1 start=$SECONDS rc=0
  shift
  "$@" || rc=$?
  local took=$((SECONDS - start))
  TIMINGS+=("$name|$took|$rc")
  printf '⏱  %s: %dm%02ds%s\n' "$name" $((took / 60)) $((took % 60)) "$([[ $rc -ne 0 ]] && echo ' (failed)')"
  return $rc
}

report_timings() {
  [[ ${#TIMINGS[@]} -gt 0 ]] || return 0
  local total=$((SECONDS - GATE_START)) table row name took rc
  table="| Stage | Time | Result |"$'\n'"|---|---:|---|"$'\n'
  for row in "${TIMINGS[@]}"; do
    IFS='|' read -r name took rc <<<"$row"
    table+=$(printf '| %s | %dm%02ds | %s |' "$name" $((took / 60)) $((took % 60)) "$([[ $rc -eq 0 ]] && echo ok || echo failed)")$'\n'
  done
  table+=$(printf '| **Total** | **%dm%02ds** | |' $((total / 60)) $((total % 60)))$'\n'
  local title="Timings: ${GATE_LABEL:-${GITHUB_JOB:-run-tests.sh}}${FULL:+ (full)}"
  echo "$title"
  printf '%s' "$table"
  mkdir -p .cache
  { printf '### %s\n\n' "$title"; printf '%s\n' "$table"; } >.cache/gate-timings.md
  if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
    cat .cache/gate-timings.md >>"$GITHUB_STEP_SUMMARY"
  fi
}

STACK_STARTED=
finish() {
  local rc=$?
  trap - EXIT
  if [[ -n $STACK_STARTED ]]; then
    # A failure keeps the stack's logs in .cache/stack-logs, which CI uploads:
    # what the app and Postgres did while a test waited is gone with the stack.
    if [[ $rc -ne 0 ]]; then
      mkdir -p .cache/stack-logs
      make -s stack-logs >.cache/stack-logs/stack.log 2>&1 || true
    fi
    make stack-down >/dev/null 2>&1 || true
  fi
  report_timings || true
  exit $rc
}
trap finish EXIT

if want shell; then
  echo "🐚 Shell script tests"
  timed "shell tests" .claude/hooks/tests/branch-guard-test.sh
  timed "release tests" tests/test-release.sh
fi

# Each layer joins the gate in the change that introduces it, through a
# Makefile target of the same name, so this file only decides the order.
has_target() { make -n "$1" >/dev/null 2>&1; }

# The part a make target belongs to.
part_of() {
  case "$1" in
    check-engine) echo engine ;;
    check-go) echo go ;;
    test-koplugin) echo koplugin ;;
    fuzz-go) echo fuzz ;;
    check-web) echo web ;;
    check-android) echo android ;;
    stack-check | embed-check | test-integration) echo integration ;;
    scale-test) echo scale ;;
    sso-up | test-e2e) echo e2e ;;
  esac
}

# The Tolino app builds with an Android SDK and Gradle of some gigabytes,
# which a push does not wait for.
for layer in check-engine check-go test-koplugin fuzz-go check-web ${FULL:+check-android}; do
  if want "$(part_of "$layer")" && has_target "$layer"; then
    echo "🔍 $layer"
    timed "$layer" make "$layer"
  fi
done

layers="stack-check test-integration test-e2e"
# The real embedding model is downloaded once per cache, which a push does
# not wait for.
# Keycloak, for the single sign-on browser tests, takes half a minute
# to start, and the views at 50,000 books a minute and a half, which a
# push does not wait for either.
[[ -n $FULL ]] && layers="stack-check embed-check sso-up test-integration scale-test test-e2e"
stack_layers=
for layer in $layers; do
  want "$(part_of "$layer")" && stack_layers+=" $layer"
done

if [[ -n $stack_layers ]] && has_target stack-up; then
  # One compose project and port per checkout, so gates running in parallel
  # worktrees never share a database or a port.
  STACK_STARTED=1
  echo "🐳 Stack"
  timed "stack-up" make stack-up
  for layer in $stack_layers; do
    if has_target "$layer"; then
      echo "🧪 $layer"
      timed "$layer" make "$layer"
    fi
  done
fi

echo "✅ All tests passed"

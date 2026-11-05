#!/usr/bin/env bash
# Tests for .claude/hooks/branch-guard.sh: the workflow rules it enforces
# (issue branches, no pushes to main, tests before a push) and the cases that
# have bypassed it before — quoted text, heredocs, command wrappers, worktrees,
# subshells, `bash -c` and the other shapes of a push (#280).
#
# Runs against a throwaway clone with a stubbed run-tests.sh; nothing is built,
# pushed or tested for real.
set -u

SRC="$(cd "$(dirname "$0")/../../.." && pwd)"

# The fixtures commit, and a CI runner has no git identity of its own.
export GIT_AUTHOR_NAME=Tester GIT_AUTHOR_EMAIL=tester@example.com
export GIT_COMMITTER_NAME=Tester GIT_COMMITTER_EMAIL=tester@example.com

# A CI checkout is a detached HEAD, so a clone of it has no branch at all and no
# origin/main to track. Both fixtures need main and its upstream to exist, or
# every case in this file is judged against a branch that is not there.
fixture_main() {
  git checkout -q -B main
  git update-ref refs/remotes/origin/main HEAD
  git branch -q --set-upstream-to=origin/main main
}

W=$(mktemp -d)
trap 'rm -rf "$W"' EXIT
git clone -q "$SRC" "$W/r" && cd "$W/r"
fixture_main && cp -r "$SRC/.claude" . && cp "$SRC/run-tests.sh" . && git add -A && git commit -qm fixture
export CLAUDE_PROJECT_DIR="$W/r"
G="git"; C="commit"
fail=0

check() { # expected tool arg label
  local exp="$1" tool="$2" arg="$3"
  local got
  got=$(jq -nc --arg t "$tool" --arg a "$arg" --arg cwd "$W/r" \
    'if $t=="Bash" then {tool_name:$t,cwd:$cwd,tool_input:{command:$a}} else {tool_name:$t,cwd:$cwd,tool_input:{file_path:$a}} end' \
    | .claude/hooks/branch-guard.sh | jq -r '.hookSpecificOutput.permissionDecision // empty')
  got=${got:-allow}
  if [[ "$got" == "$exp" ]]; then echo "ok    $exp  ${arg//$'\n'/⏎}"; else echo "FAIL  want $exp got $got  ${arg//$'\n'/⏎}"; fail=1; fi
}

echo "== on main"
check deny  Write "$W/r/README.md"
check allow Write "/tmp/x"
check allow Bash  "$G status"
check deny  Bash  "$G $C -m x"
check deny  Bash  "$G switch -c foo"
check deny  Bash  "$G switch -qc foo"
check deny  Bash  "$G checkout -qb foo"
check allow Bash  "$G merge --ff-only origin/main"
check allow Bash  "$G reset --hard origin/main"
check deny  Bash  "$G merge origin/main"
check deny  Bash  "$G merge --ff-only some-other-branch"
check deny  Bash  "$G reset --hard HEAD~1"
check allow Bash  "$G merge --ff-only origin/main 2>&1 | tail -1"
check allow Bash  "$G merge --ff-only origin/main > /tmp/out.txt"
check deny  Bash  "$G merge origin/main 2>&1"
check allow Bash  "$G switch -qc fix/42-audio && $G $C -m x"
check allow Bash  "$G switch -c fix/42-audio && $G add -A && $G $C -m x"
check deny  Bash  "$G push origin main"
check deny  Bash  "$G push origin fix/42-audio:main"
check ask   Bash  "gh pr merge 3"
check ask   Bash  "GH_PAGER=cat gh pr merge 3"
check allow Bash  "echo $G $C"
# A shell's -c string is run, not data (#280).
check deny  Bash  "bash -c \"echo x && $G add -A\""
check allow Bash  "echo \"echo x && $G add -A\""
check allow Bash  "cat > f <<'EOF'
text
$G add -A
EOF
echo done"
check deny  Bash  "cat > f <<'EOF'
text
EOF
$G add -A"
check deny  Bash  "echo a
$G $C -m x"
check allow Bash  "$G -C /tmp/other $C -m x"
check deny  Bash  "$G -C . $C -m x"
check deny  Bash  "time $G $C -m x"
check deny  Bash  "env FOO=1 $G push origin main"
check deny  Bash  "sudo $G $C -m x"
check ask   Bash  "time gh pr merge 3"

git switch -qc fix/1-test
echo "== on fix/1-test"
check allow Write "$W/r/README.md"
check allow Bash  "$G checkout README.md && $G $C -m x"
check deny  Bash  "$G checkout main && $G $C -m x"
check deny  Bash  "$G push origin main"

# ── Detached HEAD during a rebase (#97) ─────────────────────────
# A conflicted rebase is judged by the branch being rebased, not by the
# detached HEAD it leaves behind, or it could never be finished or aborted.
conflict() { # branch-to-rebase onto
  git switch -q main && printf 'ours\n' > conflict.txt && git add conflict.txt && git commit -qm ours
  git switch -qc "$1" main~1 && printf 'theirs\n' > conflict.txt && git add conflict.txt && git commit -qm theirs
  git switch -q "$1" && git rebase -q "$2" >/dev/null 2>&1
  rebasing "$PWD"
}

# Without this the rebase cases would still pass if the rebase never stopped.
rebasing() {
  git -C "$1" symbolic-ref --quiet HEAD >/dev/null \
    && { echo "FAIL  no conflicted rebase in $1: HEAD is not detached"; fail=1; }
}

echo "== rebase of an issue branch stopped on a conflict"
conflict fix/2-rebase main
check allow Bash  "$G add -A"
check allow Bash  "$G rebase --continue"
check allow Bash  "$G rebase --abort"
check allow Write "$W/r/conflict.txt"
git rebase --abort >/dev/null 2>&1

echo "== rebase of main stopped on a conflict"
git switch -q main && git rebase -q fix/2-rebase >/dev/null 2>&1
rebasing "$W/r"
check deny  Bash  "$G add -A"
check deny  Write "$W/r/conflict.txt"
git rebase --abort >/dev/null 2>&1
git switch -q fix/1-test

printf '#!/bin/sh\necho stub ok\n' > run-tests.sh; chmod +x run-tests.sh
check allow Bash  "$G push -u origin HEAD"
printf '#!/bin/sh\necho "--- FAIL: TestScanDetectsMovedFile"; exit 1\n' > run-tests.sh
check deny  Bash  "$G push -u origin HEAD"

true

# ── Worktrees (#27) ──────────────────────────────────────
W2=$(mktemp -d)
trap 'rm -rf "$W" "$W2"' EXIT
git clone -q "$SRC" "$W2/r" && cd "$W2/r"
fixture_main && cp -r "$SRC/.claude" . && cp "$SRC/run-tests.sh" . && git add -A && git commit -qm fixture
export CLAUDE_PROJECT_DIR="$W2/r"
G="git"; C="commit"

# The worktree is a second checkout of the same repo, on a valid issue branch.
WT="$W2/r/.claude/worktrees/agent-x"
git worktree add -q -b fix/99-agent-work "$WT" >/dev/null 2>&1
printf '#!/bin/sh\necho worktree tests ok\n' > "$WT/run-tests.sh"; chmod +x "$WT/run-tests.sh"
printf '#!/bin/sh\necho MAIN CHECKOUT TESTS RAN; exit 1\n' > "$W2/r/run-tests.sh"; chmod +x "$W2/r/run-tests.sh"

check() { # expected tool arg cwd
  local exp="$1" tool="$2" arg="$3" cwd="$4" got
  got=$(jq -nc --arg t "$tool" --arg a "$arg" --arg cwd "$cwd" \
    'if $t=="Bash" then {tool_name:$t,cwd:$cwd,tool_input:{command:$a}} else {tool_name:$t,cwd:$cwd,tool_input:{file_path:$a}} end' \
    | "$W2/r/.claude/hooks/branch-guard.sh" | jq -r '.hookSpecificOutput.permissionDecision // empty')
  got=${got:-allow}
  if [[ "$got" == "$exp" ]]; then echo "ok    $exp  ${arg//$'\n'/ } [cwd=${cwd##*/}]"
  else echo "FAIL  want $exp got $got  ${arg//$'\n'/ } [cwd=${cwd##*/}]"; fail=1; fi
}

echo "== worktree on issue branch, main checkout on main"
check allow Write "$WT/README.md" "$WT"
check deny  Write "$W2/r/README.md" "$W2/r"
check allow Bash  "$G add -A" "$WT"
check deny  Bash  "$G add -A" "$W2/r"
check allow Bash  "$G $C -m x" "$WT"
check deny  Bash  "$G push origin main" "$WT"
# The gate must run the worktree's own script: the main checkout's copy fails.
check allow Bash  "$G push -u origin HEAD" "$WT"

echo "== rebase in a worktree reads that worktree's head-name"
printf 'ours\n' > "$W2/r/conflict.txt"
git -C "$W2/r" add conflict.txt run-tests.sh && git -C "$W2/r" commit -qm ours
printf 'theirs\n' > "$WT/conflict.txt"
git -C "$WT" add conflict.txt run-tests.sh && git -C "$WT" commit -qm theirs
git -C "$WT" rebase -q main >/dev/null 2>&1
git -C "$WT" symbolic-ref --quiet HEAD >/dev/null \
  && { echo "FAIL  no conflicted rebase in the worktree: HEAD is not detached"; fail=1; }
check allow Bash  "$G add -A" "$WT"
check allow Bash  "$G -C $WT add -A" "$W2/r"
check deny  Bash  "$G add -A" "$W2/r"
git -C "$WT" rebase --abort >/dev/null 2>&1

echo "== worktree switched to main"
git -C "$WT" switch -q main 2>/dev/null || git -C "$WT" checkout -q --detach
check deny Bash "$G $C -m x" "$WT"

echo "== -C into a nested worktree is judged by that worktree"
git -C "$WT" switch -q fix/99-agent-work
check allow Bash "$G -C $WT $C -m x" "$W2/r"
check allow Bash "$G -C $WT add -A" "$W2/r"
git -C "$WT" switch -q main 2>/dev/null || git -C "$WT" checkout -q --detach
check deny  Bash "$G -C $WT $C -m x" "$W2/r"

# ── Every shape of a push runs the gate or is refused (#280) ──
# WTF is a worktree on an issue branch whose gate fails, so a push that
# reaches the gate is refused with the gate's reason, and one that skips it
# is let through: the reason tells the two apart.
git -C "$WT" switch -q fix/99-agent-work
WTF="$W2/r/.claude/worktrees/agent-f"
git worktree add -q -b fix/98-failing "$WTF" >/dev/null 2>&1
printf '#!/bin/sh\necho WORKTREE GATE FAILED; exit 1\n' > "$WTF/run-tests.sh"; chmod +x "$WTF/run-tests.sh"

why() { # expected-reason-pattern arg cwd [extra tool_input json]
  local want="$1" arg="$2" cwd="$3" extra="${4:-{\}}" got
  got=$(jq -nc --arg a "$arg" --arg cwd "$cwd" --argjson x "$extra" \
    '{tool_name:"Bash",cwd:$cwd,tool_input:({command:$a} + $x)}' \
    | "$W2/r/.claude/hooks/branch-guard.sh" | jq -r '.hookSpecificOutput.permissionDecisionReason // empty')
  got=${got:-allowed}
  if [[ "$got" =~ $want ]]; then echo "ok    /$want/  ${arg//$'\n'/⏎} [cwd=${cwd##*/}]"
  else echo "FAIL  want /$want/ got '${got%%$'\n'*}'  ${arg//$'\n'/⏎} [cwd=${cwd##*/}]"; fail=1; fi
}
GATE='run-tests.sh failed in .*agent-f'
REFUSED='cannot tell|cannot see|only known|cannot be judged|not judged|hides|alias'

echo "== pushes that reach the gate"
for c in \
  "$G push" \
  "$G push -u origin HEAD" \
  "$G -c push.default=current push" \
  "$G -c x=y -C $WTF push" \
  "/usr/bin/$G push"; do
  why "$GATE" "$c" "$WTF"
done
for c in \
  "$G -C $WTF push" \
  "cd $WTF && $G push" \
  "cd $WTF; $G push -u origin HEAD" \
  "(cd $WTF && $G push)" \
  "( cd $WTF; $G push )" \
  "cd $WTF && $G push -u origin HEAD 2>&1 | tail -5" \
  "bash -c 'cd $WTF && $G push'" \
  "sh -c \"cd $WTF && $G push\"" \
  "bash -lc '$G -C $WTF push -u origin HEAD'" \
  "timeout 600 $G -C $WTF push" \
  "timeout -s KILL 600 $G -C $WTF push" \
  "env FOO=1 $G -C $WTF push" \
  "env -i PATH=/usr/bin $G -C $WTF push" \
  "env -C $WTF $G push" \
  "command $G -C $WTF push" \
  "nohup $G -C $WTF push > /dev/null 2>&1 &" \
  "nice -n 5 $G -C $WTF push" \
  "time $G -C $WTF push" \
  "timeout 600 bash -c 'cd $WTF && $G push'" \
  "eval '$G -C $WTF push'" \
  "eval $G -C $WTF push" \
  "echo x; $G -C $WTF push" \
  "true && $G -C $WTF push" \
  "false || $G -C $WTF push" \
  "for i in 1 2; do $G -C $WTF push; done" \
  "while true; do $G -C $WTF push; break; done" \
  "if true; then $G -C $WTF push; fi" \
  "{ $G -C $WTF push; }" \
  "echo \$($G -C $WTF push)" \
  "echo \`$G -C $WTF push\`" \
  "bash <<'EOF'
cd $WTF && $G push
EOF" \
  "bash <<< \"$G -C $WTF push\"" \
  "find $WTF -maxdepth 0 -exec $G -C $WTF push \;" \
  "cd $WTF && $G $C -m \"\$(cat <<'EOF'
a message ) with a parenthesis
EOF
)\" && $G push"; do
  why "$GATE" "$c" "$W2/r"
done
why "$GATE" "$G -C $WTF push" "$W2"
why "$GATE" "cd $WTF && $G push" "$W2"
why "$GATE" "$G push -u origin HEAD" "$WTF" '{"run_in_background":true}'

echo "== pushes the guard cannot judge are refused"
for c in \
  "cd \"\$X\" && $G push" \
  "cd - && $G push" \
  "$G -C \"\$X\" push" \
  "for d in a b; do $G -C \$d push; done" \
  "echo fix/1-x | xargs $G push origin" \
  "ls | xargs -I{} $G -C {} push" \
  "CMD='$G push'; eval \"\$CMD\"" \
  "CMD='$G push'; bash -c \"\$CMD\"" \
  "G=git; \$G push" \
  "$G \$SUB" \
  "echo '$G push' | sh" \
  "$G push origin \"\$(git branch --show-current)\"" \
  "$G --git-dir=$W2/r/.git push" \
  "$G --work-tree $W2/r push" \
  "GIT_DIR=$W2/r/.git $G push" \
  "export GIT_DIR=$W2/r/.git; $G push" \
  "$G -c alias.p=push p"; do
  why "$REFUSED" "$c" "$WT"
done

echo "== a cd that does not outlast its command does not move the push"
for c in \
  "(cd $WT && make) && $G push" \
  "cd $WT | true; $G push" \
  "bash -c 'cd $WT' && $G push" \
  "cd $WT & $G push"; do
  why "Refusing to push from branch 'main'" "$c" "$W2/r"
done
why "Refusing to push from branch 'main'" "cd $W2/r && $G push" "$WT"
why "Refusing to push from branch 'main'" "$G -C $WT add -A && $G push" "$W2/r"

echo "== a configured alias is judged as what it runs"
git -C "$W2/r" config alias.pu push
git -C "$W2/r" config alias.shpush '!git push'
why "$GATE" "$G pu" "$WTF"
why "$REFUSED" "$G shpush" "$WTF"
git -C "$W2/r" config --unset alias.pu; git -C "$W2/r" config --unset alias.shpush

echo "== ordinary commands are left alone"
for c in \
  "$G -C $WT push -u origin HEAD" \
  "cd $WT && $G push -u origin HEAD" \
  "(cd $WT && $G push -u origin HEAD)"; do
  why "allowed" "$c" "$W2/r"
done
why "allowed" "$G push -u origin HEAD" "$WT"
for c in \
  "$G status" \
  "$G log --oneline -5 | head -3" \
  "echo $G push" \
  "echo \"cd x && $G push\"" \
  "gh pr create --title x --body \"then \$G push && $G push origin main\"" \
  "$G $C -m \"\$(cat <<'EOF'
push it
EOF
)\"" \
  "grep -rn '$G push' .claude/hooks" \
  "\"\$HOME/bin/tool\" --flag" \
  "eval \"\$(ssh-agent -s)\"" \
  "cd \"\$X\" && ls"; do
  why "allowed" "$c" "$WT"
done

echo "== commits take the same shapes"
for c in \
  "(cd $W2/r && $G $C -m x)" \
  "bash -c '$G -C $W2/r $C -m x'" \
  "timeout 5 $G -C $W2/r $C -m x" \
  "cd $W2/r && $G $C -m x" \
  "for i in 1; do $G -C $W2/r add -A; done"; do
  why "Refusing 'git (commit|add)' on branch 'main'" "$c" "$WT"
done
why "$REFUSED" "cd \"\$X\" && $G $C -m x" "$WT"
why "$REFUSED" "$G --git-dir=$W2/r/.git $C -m x" "$WT"
why "allowed" "cd $WT && $G add -A && $G $C -m x" "$W2/r"
why "allowed" "bash -c 'cd $WT && $G $C -m x'" "$W2/r"
why "allowed" "$G ls-files | xargs $G add" "$WT"
git -C "$W2/r" worktree remove --force "$WTF" >/dev/null 2>&1

echo "== unrelated repo is none of our business"
OTHER="$W2/other"; mkdir -p "$OTHER" && git -C "$OTHER" init -q && git -C "$OTHER" commit -q --allow-empty -m init
check allow Write "$OTHER/file.txt" "$OTHER"
check allow Bash  "$G $C -m x" "$OTHER"

git -C "$W2/r" worktree remove --force "$WT" >/dev/null 2>&1

if (( fail )); then
  echo "❌ branch-guard tests failed"
  exit 1
fi
echo "✅ branch-guard tests passed"

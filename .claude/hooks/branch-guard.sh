#!/usr/bin/env bash
# PreToolUse on Edit|Write|NotebookEdit|Bash: all work happens on an issue
# branch named <type>/<issue>-<slug>, never on main, and nothing is pushed
# unless ./run-tests.sh passes.
set -uo pipefail

BRANCH_RE='^(feat|fix|chore|docs|refactor|test|perf|ci|build|revert)/[0-9]+-[a-z0-9][a-z0-9._-]*$'
HINT="Work only on issue branches named <type>/<issue>-<slug> (e.g. fix/42-audio-regression). Find or create the GitHub issue first (see the gh skill), then: gh issue develop <N> --name <type>/<N>-<slug> --base main --checkout"

input="$(cat)"
tool="$(jq -r '.tool_name // empty' <<< "$input")"
project="$(realpath -m "${CLAUDE_PROJECT_DIR:-$(jq -r '.cwd // empty' <<< "$input")}")"
cwd="$(jq -r '.cwd // empty' <<< "$input")"
cwd="${cwd:-$project}"

# Worktrees are separate checkouts of this repo with their own branch and their
# own copy of run-tests.sh, so the checkout is resolved from what is being acted
# on, not from the project directory. They share a common git dir, which is what
# tells a worktree of this repo apart from an unrelated repo on disk.
# rev-parse prints the common dir relative to its own working directory, so it
# is resolved there rather than wherever this hook happens to run.
git_common_dir() { (cd "$1" 2>/dev/null && realpath -m "$(git rev-parse --git-common-dir 2>/dev/null)"); }
project_git_dir="$(git_common_dir "$project")"

checkout_for() {
  local dir="$1" top common
  top="$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null)" || return 1
  common="$(git_common_dir "$dir")"
  [[ "$common" == "$project_git_dir" ]] || return 1
  printf '%s' "$top"
}

decide() {
  jq -n --arg d "$1" --arg r "$2" '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: $d, permissionDecisionReason: $r}}'
  exit 0
}
deny() { decide deny "$1"; }
ask() { decide ask "$1"; }

valid() { [[ "$1" =~ $BRANCH_RE ]]; }

# A rebase that stops on a conflict detaches HEAD, so symbolic-ref yields
# nothing and every git command would be refused — including the `git add` and
# `git rebase --continue` that are the only way out. Both rebase backends record
# the branch being rebased in head-name, which rev-parse locates for a worktree
# too, where it does not sit under .git/ (#97).
current_branch() {
  local repo="$1" branch head_name d
  branch="$(git -C "$repo" symbolic-ref --quiet --short HEAD 2>/dev/null)" && { printf %s "$branch"; return; }
  for d in rebase-merge rebase-apply; do
    head_name="$(cd "$repo" 2>/dev/null && realpath -m "$(git rev-parse --git-path "$d/head-name" 2>/dev/null)")"
    [[ -f "$head_name" ]] || continue
    branch="$(<"$head_name")"
    printf %s "${branch#refs/heads/}"
    return
  done
  printf %s "(detached HEAD)"
}

# True for `git merge --ff-only <upstream>` / `git reset --hard <upstream>` where
# <upstream> is exactly the tracking branch of the checked-out branch.
syncs_with_upstream() {
  local repo="$1" branch="$2"; shift 2
  local upstream target="" mode=0
  upstream="$(git -C "$repo" rev-parse --abbrev-ref --symbolic-full-name "$branch@{upstream}" 2>/dev/null)" || return 1
  [[ -n "$upstream" ]] || return 1
  for a in "$@"; do
    case "$a" in
      --ff-only|--hard) mode=1 ;;
      -*) return 1 ;;
      *) [[ -n "$target" ]] && return 1; target="$a" ;;
    esac
  done
  (( mode )) && [[ "$target" == "$upstream" ]]
}
is_branch() { git -C "$1" show-ref --verify --quiet "refs/heads/$2"; }

# A hook that runs past its timeout, or dies, lets the command through; a gate
# that hangs must therefore end before the hook's 1800 s in settings.json.
GATE_TIMEOUT="${BRANCH_GUARD_GATE_TIMEOUT:-1700}"

run_tests() {
  local dir="$1" log rc=0
  log="$(mktemp)"
  # run-tests.sh gives each checkout its own stack, so gates running in
  # parallel worktrees do not collide (#95).
  (cd "$dir" && timeout -k 60 "$GATE_TIMEOUT" ./run-tests.sh) > "$log" 2>&1 || rc=$?
  if (( rc )); then
    local tail_out
    tail_out="$(tail -n 60 "$log")"
    rm -f "$log"
    (( rc == 124 )) && tail_out="./run-tests.sh did not finish within ${GATE_TIMEOUT}s.
$tail_out"
    deny "Push blocked: ./run-tests.sh failed in $dir. Fix the failures, commit, and push again.
$tail_out"
  fi
  rm -f "$log"
}

DYN=$'\x1d'
UNJUDGED="The branch guard cannot tell what this command would do, so it refuses it. Run git as a plain command with literal paths: git -C <checkout> <subcommand> ..., or cd <checkout> && git ..."

# The subcommands that change a branch or send it somewhere.
changes_branch() {
  case "$1" in
    add|mv|rm|restore|apply|commit|merge|rebase|cherry-pick|revert|reset|am|push|"$DYN"*) return 0 ;;
  esac
  return 1
}

# The directory `cd` leaves the shell in, or nothing when only the running
# shell would know it (a variable, `cd -`, popd).
cd_target() {
  local base="$1" cmd="$2" a
  shift 2
  [[ -n "$base" && "$cmd" != popd ]] || return 0
  for a in "$@"; do
    case "$a" in
      -L|-P|-e|-@|--) continue ;;
      -|-*|+*|"$DYN"*) return 0 ;;
    esac
    beside "$base" "$a"
    return 0
  done
  [[ "$cmd" == cd ]] && printf %s "$HOME"
}

beside() {
  local base="$1" p="$2"
  [[ -n "$base" && "$p" != "$DYN"* ]] || return 0
  [[ "$p" == /* ]] || p="$base/$p"
  realpath -m -- "$p"
}

case "$tool" in
  Edit|Write|NotebookEdit)
    path="$(jq -r '.tool_input.file_path // .tool_input.notebook_path // empty' <<< "$input")"
    [[ -n "$path" ]] || exit 0
    path="$(realpath -m "$path")"
    # Files outside this repo (scratchpad, memory) are not project work.
    repo="$(checkout_for "$(dirname "$path")")" || exit 0
    branch="$(current_branch "$repo")"
    valid "$branch" || deny "Refusing to edit $path on branch '$branch'. $HINT"
    ;;

  Bash)
    cmd="$(jq -r '.tool_input.command // empty' <<< "$input")"
    [[ -n "$cmd" ]] || exit 0
    # A hook that fails lets the command run. For anything that may push,
    # fail closed instead; for everything else a bug here must not stop work.
    [[ "$cmd" == *push* ]] && trap 'rc=$?; (( rc )) && deny "The branch guard failed (exit $rc) while judging a push, so the push is refused."' EXIT

    commands="$(perl "$(dirname "${BASH_SOURCE[0]}")/shell-commands.pl" <<< "$cmd")" \
      || { [[ "$cmd" == *push* ]] && deny "The branch guard could not read this command. $UNJUDGED"; exit 0; }

    # Where the next command runs: cd moves it for the commands after it, a
    # subshell takes it back, and -C applies to its own git command only.
    here="$cwd"
    dirs=()
    declare -A branch_of=()
    declare -A pushed=()
    # Walk the commands in order so "git switch -c fix/1-x && git commit"
    # is judged against the branch the commit will actually land on.
    while IFS=$'\t' read -r kind flags rest; do
      case "$kind" in
        '(') dirs+=("$here"); continue ;;
        ')') if (( ${#dirs[@]} )); then here="${dirs[-1]}"; unset 'dirs[-1]'; fi; continue ;;
        R) deny "$rest. $UNJUDGED" ;;
        C) ;;
        *) continue ;;
      esac
      IFS=$'\x1f' read -ra t <<< "$rest"
      (( ${#t[@]} )) || continue

      case "${t[0]}" in
        cd|pushd|popd)
          [[ "$flags" == *p* ]] || here="$(cd_target "$here" "${t[@]}")"
          continue
          ;;
        gh)
          [[ "${t[1]:-} ${t[2]:-}" == "pr merge" ]] && ask "Merging a PR is the user's decision."
          continue
          ;;
        git) ;;
        *) continue ;;
      esac

      dir="$here"
      gitdir=0
      [[ "$flags" == *d* ]] && gitdir=1
      i=1
      while [[ "${t[i]:-}" == -* ]]; do
        case "${t[i]}" in
          -C)
            # xargs -I may put anything in place of the path.
            if [[ "$flags" == *x* ]]; then dir=""; else dir="$(beside "$dir" "${t[i+1]:-}")"; fi
            ((i++))
            ;;
          -c|--config-env)
            [[ "${t[i+1]:-}" =~ ^[Aa][Ll][Ii][Aa][Ss]\. ]] \
              && deny "An alias given with git ${t[i]} hides which subcommand runs. $UNJUDGED"
            ((i++))
            ;;
          --git-dir|--work-tree) gitdir=1; ((i++)) ;;
          --git-dir=*|--work-tree=*) gitdir=1 ;;
          --namespace|--super-prefix) ((i++)) ;;
        esac
        ((i++))
      done
      sub="${t[i]:-}"
      [[ -n "$sub" ]] || continue
      args=("${t[@]:i+1}")
      # Found among another command's arguments, it may well be text.
      [[ "$flags" == *g* && "$sub" != push ]] && continue

      if changes_branch "$sub"; then
        [[ "$sub" == "$DYN"* ]] && deny "The git subcommand ${sub#"$DYN"} is only known when it runs. $UNJUDGED"
        # Pointing git at a repository by --git-dir or GIT_DIR detaches it from
        # the directory the guard judges by.
        (( gitdir )) && deny "git with --git-dir, --work-tree, GIT_DIR or GIT_WORK_TREE is not judged by the branch guard. $UNJUDGED"
        [[ -n "$dir" ]] || deny "The branch guard cannot tell which checkout 'git $sub' runs in. $UNJUDGED"
        [[ "$sub" == push && "$flags" == *x* ]] \
          && deny "xargs adds arguments to the push that the branch guard cannot see. $UNJUDGED"
      fi
      [[ -n "$dir" ]] || continue
      # Worktrees live under .claude/worktrees inside the repo directory, so
      # the path alone does not say which checkout it belongs to — ask git.
      repo="$(checkout_for "$dir")" || continue

      if ! changes_branch "$sub" && [[ "$sub" != checkout && "$sub" != switch && "$sub" != branch ]]; then
        alias="$(git -C "$repo" config --get "alias.$sub" 2>/dev/null)" || continue
        if [[ "$alias" == '!'* ]]; then
          [[ "$alias" =~ (push|commit|add|merge|rebase|reset|cherry-pick|revert|am|apply|restore|rm|mv) ]] \
            && deny "The git alias '$sub' runs a shell command the branch guard does not read. $UNJUDGED"
          continue
        fi
        read -ra expanded <<< "$alias"
        (( ${#expanded[@]} )) || continue
        sub="${expanded[0]}"
        args=("${expanded[@]:1}" "${args[@]}")
      fi

      branch="${branch_of[$repo]:-}"
      [[ -n "$branch" ]] || branch="$(current_branch "$repo")"

      case "$sub" in
        checkout|switch)
          created=0
          for ((j = 0; j < ${#args[@]}; j++)); do
            # A short-flag cluster counts too: -qc, -qb, --quiet -c, ...
            if [[ "${args[j]}" =~ ^-[a-zA-Z]*[bBcC]$ ]]; then
              new="${args[j+1]:-}"
              valid "$new" || deny "Branch name '${new#"$DYN"}' is not allowed. $HINT"
              branch_of[$repo]="$new"
              created=1
            fi
          done
          if (( !created )); then
            # The branch is the first argument that names one: with "git checkout
            # -q main" only args[0] was looked at, so the flag hid the branch and
            # everything after it in the same command was judged against the
            # branch we were on before (#76).
            for a in "${args[@]}"; do
              [[ "$a" == -* ]] && continue
              if is_branch "$repo" "$a"; then branch_of[$repo]="$a"; fi
              break
            done
          fi
          ;;
        branch)
          if [[ "${#args[@]}" -ge 1 && "${args[0]}" != -* ]]; then
            valid "${args[0]}" || deny "Branch name '${args[0]#"$DYN"}' is not allowed. $HINT"
          fi
          ;;
        add|mv|rm|restore|apply|commit|merge|rebase|cherry-pick|revert|reset|am)
          # Catching up with the remote is not working on main: refusing it is
          # what left main behind after every merged PR, until a release was cut
          # from stale code (#64). Only a move onto the branch's own upstream is
          # allowed, and only as a fast-forward or a reset to exactly that ref.
          if [[ "$sub" == merge || "$sub" == reset ]] && syncs_with_upstream "$repo" "$branch" "${args[@]}"; then
            continue
          fi
          valid "$branch" || deny "Refusing 'git $sub' on branch '$branch'. $HINT"
          ;;
        push)
          for a in "${args[@]}"; do
            [[ "$a" == "$DYN"* ]] \
              && deny "The push names ${a#"$DYN"}, which is only known when it runs; name the branch or use HEAD. $UNJUDGED"
            [[ "$a" =~ (^|:|/|^\+)(main|master)$ || "$a" == --all || "$a" == --mirror ]] \
              && deny "Pushing to main is not allowed; push the issue branch and open a PR instead."
          done
          valid "$branch" || deny "Refusing to push from branch '$branch'. $HINT"
          pushed[$repo]=1
          ;;
      esac
    done <<< "$commands"

    for r in "${!pushed[@]}"; do run_tests "$r"; done
    ;;
esac
exit 0

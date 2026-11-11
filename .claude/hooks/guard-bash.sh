#!/bin/bash
# PreToolUse(Bash): rules that must hold even when the model skips AGENTS.md.
input=$(cat)
cmd=$(jq -r '.tool_input.command // ""' <<<"$input")
dir=$(jq -r '.cwd // empty' <<<"$input")
dir=${dir:-$CLAUDE_PROJECT_DIR}
secrets=$(cat "$CLAUDE_PROJECT_DIR/.claude/hooks/secrets.ere")
# A fake key in apps/native/internal/memorywork/pipeline_test.go.
fixture='fk-abcdefghijklmnopqrstuvwxyz'
deny() {
  jq -n --arg r "$1" '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$r}}'
  exit 0
}
g() { git -C "$dir" "$@" 2>/dev/null; }
has() { grep -Eq "(^|[;&|(]|\\\$\\()[[:space:]]*git[[:space:]]+$1([[:space:]]|\$)" <<<"$cmd"; }

case "$cmd" in
  *.config/dp/keys.txt*) deny "~/.config/dp/keys.txt is the user's key pool: agents never read it. Ask the user for FACTORY_API_KEY." ;;
  *mygo/update-keys*) deny "The updater's signing key stays in its file; release.yml reads it from secrets." ;;
esac

if has push; then
  grep -Eq -- '[[:space:]](-f|--force|--force-with-lease|--mirror|--delete|-d)([[:space:]]|=|$)|[[:space:]]\+[^ ]' <<<"$cmd" &&
    deny "Force push, delete and mirror rewrite published history; a published tag is never moved."
  grep -Eq '[[:space:]]([^ ]*:)?(refs/heads/)?main([[:space:]]|$)' <<<"$cmd" &&
    deny "Never push to main: push a branch and open a PR."
  [ "$(g branch --show-current)" = main ] && ! grep -Eq -- '--tags|refs/tags/' <<<"$cmd" &&
    deny "This checkout is on main: push from a branch."
fi

# In the main checkout the git dir is .git; in a linked worktree it is .git/worktrees/<name>.
if has '(checkout|switch)' && [ "$(g rev-parse --git-dir)" = .git ]; then
  deny "Do not switch or create branches in the main checkout: git worktree add .worktrees/droi--<name>."
fi

if has commit; then
  [ "$(g branch --show-current)" = main ] && deny "Never commit to main: commit on a branch in a worktree."
  g diff --cached -U0 -- go.work | grep -E '^\+[^+]' | grep -vqE '^\+[[:space:]]*(go [0-9.]+|use \./apps/native)?[[:space:]]*$' &&
    deny "go.work uses a local checkout: unstage it (AGENTS.md, droid-sdk-go)."
  g diff --cached -U0 -- tests/package.json | grep -q '^+.*"link:' &&
    deny "tests/package.json links a local Fake Daemon: unstage it (AGENTS.md, droid-sdk-go)."
  g diff --cached -U0 | grep '^+' | grep -vF -- "$fixture" | grep -Eq "$secrets" &&
    deny "The staged change looks like it holds a key or a private key."
fi
exit 0

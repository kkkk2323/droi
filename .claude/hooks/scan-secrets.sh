#!/bin/bash
# PreToolUse(Edit|Write|MultiEdit): no key is written into the tree.
input=$(cat)
secrets=$(cat "$CLAUDE_PROJECT_DIR/.claude/hooks/secrets.ere")
text=$(jq -r '[.tool_input.content, .tool_input.new_string, (.tool_input.edits // [] | .[].new_string)] | map(select(. != null)) | join("\n")' <<<"$input")
if grep -vF -- 'fk-abcdefghijklmnopqrstuvwxyz' <<<"$text" | grep -Eq "$secrets"; then
  jq -n '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:"This edit looks like it writes a key or a private key. Use an environment variable or a placeholder."}}'
fi
exit 0

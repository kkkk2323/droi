#!/bin/bash
# Feeds the PreToolUse hooks sample calls and checks each decision:
#   .claude/hooks/test.sh <main checkout> <a linked worktree on a branch>
set -u
here=$(cd "$(dirname "$0")" && pwd)
export CLAUDE_PROJECT_DIR=$(cd "$here/../.." && pwd)
main=$1 tree=$2
fails=0
check() { # want hook cwd json-tool-input
  local want=$1 hook=$2 cwd=$3 input=$4 got
  out=$(jq -n --arg cwd "$cwd" --argjson ti "$input" '{cwd:$cwd,tool_input:$ti}' | "$here/$hook")
  got=allow
  grep -q '"deny"' <<<"$out" && got=deny
  if [ "$got" != "$want" ]; then
    echo "FAIL want $want got $got: $hook $cwd $input"
    fails=$((fails + 1))
  fi
}
bash() { check "$1" guard-bash.sh "$2" "$(jq -n --arg c "$3" '{command:$c}')"; }

bash deny "$tree" 'cat ~/.config/dp/keys.txt'
bash deny "$tree" 'FACTORY_API_KEY=$(grep -m1 fk- ~/.config/dp/keys.txt) go test ./...'
bash deny "$tree" 'git push --force origin x'
bash deny "$tree" 'git push origin +x'
bash deny "$tree" 'git push origin main'
bash deny "$tree" 'npm test && git push origin HEAD:main'
bash deny "$tree" 'git push origin --delete v1.0.0'
bash allow "$tree" 'git push -u origin chore/agent-rules'
bash allow "$tree" 'git push origin chore/main-fix'
bash deny "$main" 'git push'
bash allow "$main" 'git push origin refs/tags/v9.9.9'
bash deny "$main" 'git checkout -b foo'
bash deny "$main" 'git switch other'
bash allow "$tree" 'git checkout -b foo'
bash deny "$main" 'git commit -m x'
bash allow "$tree" 'echo hi && ls'
bash allow "$main" 'git status'

# Staged changes, in a throwaway repository on a branch.
repo=$(mktemp -d)
git -C "$repo" init -q -b work
key="fk-$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 24)"
printf 'k = "%s"\n' "$key" >"$repo/a.go"
git -C "$repo" add a.go
bash deny "$repo" 'git commit -m x'
git -C "$repo" rm -q --cached a.go
printf 'go 1.27.3\n\nuse ./apps/native\nuse ../droid-sdk-go\n' >"$repo/go.work"
git -C "$repo" add go.work
bash deny "$repo" 'git commit -m x'
printf 'go 1.27.3\n\nuse ./apps/native\n' >"$repo/go.work"
git -C "$repo" add go.work
bash allow "$repo" 'git commit -m x'
printf 'f("fk-abcdefghijklmnopqrstuvwxyz")\n' >"$repo/b_test.go"
git -C "$repo" add b_test.go
bash allow "$repo" 'git commit -m x'
rm -rf "$repo"

edit() { check "$1" scan-secrets.sh "$tree" "$2"; }
edit deny "$(jq -n --arg s "$key" '{file_path:"x.go",new_string:("k := \"" + $s + "\"")}')"
# Built in two halves so that this file does not hold what it tests for.
edit deny "$(jq -n --arg h '-----BEGIN RSA PRIVATE' '{file_path:"k.pem",content:($h + " KEY-----\nMII\n")}')"
edit deny "$(jq -n --arg s "$key" '{file_path:"x",edits:[{new_string:"a"},{new_string:$s}]}')"
edit allow '{"file_path":"x_test.go","new_string":"\"fk-abcdefghijklmnopqrstuvwxyz\""}'
edit allow '{"file_path":"x.go","new_string":"apiKey := os.Getenv(\"FACTORY_API_KEY\")"}'

[ "$fails" = 0 ] && echo "all hook checks passed" || { echo "$fails failed"; exit 1; }

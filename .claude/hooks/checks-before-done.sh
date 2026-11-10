#!/bin/bash
# Stop: AGENTS.md's Validation Workflow runs before the turn may end.
input=$(cat)
cd "$(jq -r '.cwd // empty' <<<"$input")" 2>/dev/null || cd "$CLAUDE_PROJECT_DIR" || exit 0
base=$(git merge-base HEAD origin/main 2>/dev/null) || exit 0
changed=$({ git diff --name-only "$base"; git ls-files --others --exclude-standard; } | sort -u)
[ -n "$changed" ] || exit 0
gitdir=$(git rev-parse --git-dir)
state=$({ git rev-parse HEAD; git diff HEAD; git ls-files --others --exclude-standard | xargs shasum 2>/dev/null; } | shasum | cut -d' ' -f1)
[ "$(cat "$gitdir/claude-checks-passed" 2>/dev/null)" = "$state" ] && exit 0
# A tree that fails three rounds in a row goes back to the user instead of looping.
if [ "$(jq -r '.stop_hook_active // false' <<<"$input")" = true ]; then
  n=$(($(cat "$gitdir/claude-checks-tries" 2>/dev/null || echo 0) + 1))
else
  n=1
fi
echo "$n" >"$gitdir/claude-checks-tries"
[ "$n" -gt 3 ] && exit 0
log=$(mktemp)
fail() {
  { echo "Not done: $1 failed. Fix it, then finish (or tell the user it still fails)."; tail -n 60 "$log"; } >&2
  exit 2
}
if grep -q '^apps/native/' <<<"$changed"; then
  out=$("$(go env GOROOT)/bin/gofmt" -l apps/native)
  [ -z "$out" ] || { echo "$out" >"$log"; fail gofmt; }
  for os in darwin windows linux; do
    GOOS=$os CGO_ENABLED=0 go vet ./apps/native/... >"$log" 2>&1 || fail "go vet (GOOS=$os)"
  done
  go test ./apps/native/... >"$log" 2>&1 || fail "go test ./apps/native/..."
  go test -cpu 1 ./apps/native/internal/app >"$log" 2>&1 || fail "go test -cpu 1 ./apps/native/internal/app"
fi
if grep -Eq '^(apps/mobile|packages|tests)/|^(package\.json|pnpm-lock\.yaml|tsconfig\.json|vitest\.config\.ts)$' <<<"$changed"; then
  { pnpm check && pnpm test; } >"$log" 2>&1 || fail "pnpm check && pnpm test"
fi
if grep -Eq '^(apps/mobile|packages/daemon-layer|tests/mobile)/' <<<"$changed"; then
  pnpm test:e2e:phone >"$log" 2>&1 || fail "pnpm test:e2e:phone"
fi
echo "$state" >"$gitdir/claude-checks-passed"

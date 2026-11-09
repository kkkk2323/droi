#!/usr/bin/env bash
# The native app's Go tests on this computer, as the PR workflow runs them, at once: the view tests
# (internal/app) in shards, and again on one CPU, beside every other package. About a minute on an
# M4 Pro, where `go test ./apps/native/...` takes four and a half: the view tests run one after another
# in one process, and they are nearly all of it.
#
#   scripts/test-native.sh             # 8 shards, each run twice: as is and on one CPU (-cpu 1)
#   SHARDS=4 scripts/test-native.sh    # fewer processes at once
#
# The one-CPU run is the PR workflow's: a view test that races the Fake Daemon's or the Fake Kernel's
# answers fails there, not in a Release. Results are go test's, so a package nothing changed in is cached.
# Each shard's output is in the log folder it prints; a failing one's tail is shown.
set -euo pipefail

fail() { echo "test-native: $*" >&2; exit 1; }
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
shards="${SHARDS:-8}"
[[ "$shards" =~ ^[1-9][0-9]*$ ]] || fail "SHARDS must be a whole number, not $shards"

# A view test without node skips rather than fails, and one without the Kernel binary falls back to the
# Fake Daemon: both would pass having tested nothing.
for tool in go node bun pnpm; do
  command -v "$tool" >/dev/null || fail "$tool is not on PATH"
done
export DROI_FAKEKERNEL_REQUIRED=1
# The variables a running Droi gives its Kernel and every tool the Kernel runs: a test that read them
# would use this computer's Memory folder and Kernel. DROI_KERNEL_BIN, which picks a build, stays.
for name in $(compgen -e | grep -E '^DROI_(KERNEL(_[A-Z0-9_]+)?|MEMORY_DIR|USER_DIR)$' | grep -vx DROI_KERNEL_BIN || true); do
  unset "$name"
done

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) platform=darwin-arm64 ;;
  Darwin-x86_64) platform=darwin-x64 ;;
  Linux-x86_64) platform=linux-x64 ;;
  Linux-aarch64 | Linux-arm64) platform=linux-arm64 ;;
  *) fail "unsupported machine: $(uname -s)-$(uname -m)" ;;
esac

logs="$(mktemp -d "${TMPDIR:-/tmp}/test-native.XXXXXX")"
start=$SECONDS

echo "== the Kernel ($platform)"
pnpm install --frozen-lockfile --silent
# Built every time, so no test runs an old one; a build the same as before keeps its modification time,
# because go test reruns a test whose files changed, and a new time is a change to it.
kernel="apps/native/assets/kernel/$platform/kernel"
[[ -f "$kernel" ]] && cp -p "$kernel" "$logs/kernel.before"
(cd apps/kernel && bun run build.ts "$platform" >/dev/null)
if [[ -f "$logs/kernel.before" ]] && cmp -s "$logs/kernel.before" "$kernel"; then
  touch -r "$logs/kernel.before" "$kernel"
fi
rm -f "$logs/kernel.before"

echo "== building the tests"
go test -run '^$' ./apps/native/... >/dev/null

tests="$(go test -list '^Test' ./apps/native/internal/app | grep -E '^Test')"
[[ -n "$tests" ]] || fail "no view test was listed"
# A name the pattern would read as a regex is refused rather than silently matched to nothing.
odd="$(printf '%s\n' "$tests" | grep -vxE '[A-Za-z0-9_]+' || true)"
[[ -z "$odd" ]] || fail "unexpected test names: $odd"

names=()
pids=()
run() {
  local name="$1"
  shift
  "$@" >"$logs/$name.log" 2>&1 &
  names+=("$name")
  pids+=("$!")
}

echo "== $(printf '%s\n' "$tests" | wc -l | tr -d ' ') view tests in $shards shards, twice; the other packages beside them"
run packages go test $(go list ./apps/native/... | grep -v '/internal/app$')
for shard in $(seq 1 "$shards"); do
  pattern="$(printf '%s\n' "$tests" | awk -v shard="$shard" -v of="$shards" 'NR % of == shard - 1 { b = b (b ? "|" : "") $0 } END { print "^(" b ")$" }')"
  run "view-$shard" go test -timeout 10m -run "$pattern" ./apps/native/internal/app
  run "view-$shard-cpu1" go test -timeout 10m -cpu 1 -run "$pattern" ./apps/native/internal/app
done

failed=()
for i in "${!pids[@]}"; do
  wait "${pids[$i]}" || failed+=("${names[$i]}")
done

# macOS's bash 3.2 calls an empty array unbound under set -u: hence the +.
for name in ${failed[@]+"${failed[@]}"}; do
  echo
  echo "== $name failed: $logs/$name.log"
  grep -E '^(--- FAIL|FAIL|panic:)' "$logs/$name.log" | head -20 || tail -20 "$logs/$name.log"
done
echo
if [[ ${#failed[@]} -gt 0 ]]; then
  fail "${#failed[@]} of ${#names[@]} runs failed in $((SECONDS - start))s; logs in $logs"
fi
echo "All ${#names[@]} runs passed in $((SECONDS - start))s; logs in $logs"

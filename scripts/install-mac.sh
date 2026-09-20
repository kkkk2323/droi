#!/usr/bin/env bash
# Build the macOS app and replace the installed copy: `pnpm install:mac`.
# Quits a running Droi, swaps /Applications/Droi.app, verifies the signature
# and relaunches. Set DROI_NO_LAUNCH=1 to skip the relaunch.
set -euo pipefail

cd "$(dirname "$0")/.."
pnpm build:mac

app="dist/mac-universal/Droi.app"
target="/Applications/Droi.app"
[ -d "$app" ] || { echo "build output missing: $app" >&2; exit 1; }

if pgrep -x Droi >/dev/null; then
  osascript -e 'tell application "Droi" to quit' || true
  for _ in $(seq 1 20); do pgrep -x Droi >/dev/null || break; sleep 0.5; done
fi

rm -rf "$target"
ditto "$app" "$target"
codesign --verify --deep --strict "$target"
echo "installed $(defaults read "$target/Contents/Info.plist" CFBundleShortVersionString) to $target"

if [ -z "${DROI_NO_LAUNCH:-}" ]; then
  open -a "$target"
fi

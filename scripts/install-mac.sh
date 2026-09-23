#!/usr/bin/env bash
# Build the macOS app and replace the installed copy: `pnpm install:mac`.
# Quits a running Droi, swaps /Applications/Droi.app and relaunches. Set DROI_NO_LAUNCH=1 to skip the relaunch.
set -euo pipefail

cd "$(dirname "$0")/../apps/desktop"

# A universal build signs a 400 MB fat Electron framework twice over; the
# local install only needs this machine's architecture.
case "$(uname -m)" in
  arm64) arch=arm64 ;;
  *) arch=x64 ;;
esac
pnpm build && pnpm exec electron-builder --mac "--$arch"

app="dist/mac-$arch/Droi.app"
target="/Applications/Droi.app"
[ -d "$app" ] || { echo "build output missing: $app" >&2; exit 1; }

if pgrep -x Droi >/dev/null; then
  osascript -e 'tell application "Droi" to quit' || true
  for _ in $(seq 1 20); do pgrep -x Droi >/dev/null || break; sleep 0.5; done
fi

rm -rf "$target"
ditto "$app" "$target"
echo "installed $(defaults read "$target/Contents/Info.plist" CFBundleShortVersionString) to $target"

if [ -z "${DROI_NO_LAUNCH:-}" ]; then
  open -a "$target"
fi

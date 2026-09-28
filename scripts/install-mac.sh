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

# pgrep does not see the app's process on every macOS; Launch Services does.
running() { [ "$(osascript -e 'application id "com.droi.app" is running' 2>/dev/null)" = true ]; }
if running; then
  osascript -e 'tell application id "com.droi.app" to quit' || true
  for _ in $(seq 1 20); do running || break; sleep 0.5; done
  if running; then
    echo "Droi did not quit (a Session may be running); quit it and run this again." >&2
    exit 1
  fi
fi

rm -rf "$target"
ditto "$app" "$target"
echo "installed $(defaults read "$target/Contents/Info.plist" CFBundleShortVersionString) to $target"

if [ -z "${DROI_NO_LAUNCH:-}" ]; then
  open -a "$target"
fi

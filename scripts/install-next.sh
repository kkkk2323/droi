#!/usr/bin/env bash
# Builds Droi Next (apps/native/next) from this checkout and installs it as /Applications/Droi Next.app,
# beside a released Droi, which it does not touch. macOS only.
#
#   scripts/install-next.sh            # sign with this Mac's first code signing identity
#   DROI_NEXT_SIGN="Apple Development: …" scripts/install-next.sh
#
# A stable identity keeps what macOS granted the app (Full Disk Access, …) across rebuilds; without one the
# app is signed ad hoc and each build is a new app to macOS.
set -euo pipefail

fail() { echo "install-next: $*" >&2; exit 1; }
[[ "$(uname -s)" == Darwin ]] || fail "macOS only"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

case "$(uname -m)" in
  arm64) kernel=darwin-arm64 goarch=arm64 ;;
  x86_64) kernel=darwin-x64 goarch=amd64 ;;
  *) fail "unsupported machine: $(uname -m)" ;;
esac

echo "== dependencies"
pnpm install --frozen-lockfile --silent

echo "== the Kernel ($kernel)"
(cd apps/kernel && bun run build.ts --stage "$kernel")

next=apps/native/next
# Droi's resources (the staged Kernel above all) are Droi Next's too.
ln -sfn ../resources "$next/resources"
# Droi's mygo.json, as Droi Next: its name, identifier and icon, and no updates.
node -e '
  const fs = require("fs")
  const app = JSON.parse(fs.readFileSync("apps/native/mygo.json", "utf8"))
  delete app.updates
  Object.assign(app, { name: "Droi Next", slug: "droi-next", identifier: "com.droi.next", icon: "icon.png", out: "build" })
  for (const [path, file] of Object.entries(app.macos?.helperEntitlements ?? {})) app.macos.helperEntitlements[path] = "../" + file
  fs.writeFileSync("apps/native/next/mygo.json", JSON.stringify(app, null, 2) + "\n")
'

sign="${DROI_NEXT_SIGN:-$(security find-identity -v -p codesigning | sed -n 's/^ *1) [0-9A-F]* "\(.*\)"$/\1/p')}"
echo "== Droi Next (signed ${sign:-ad hoc})"
(cd "$next" && go tool mygo build -platform "darwin/$goarch" ${sign:+-sign "$sign"})

app="$next/build/darwin-$goarch/Droi Next.app"
[[ -d "$app" ]] || fail "no app at $app"

if pgrep -xq "Droi Next"; then
  echo "== quitting the running Droi Next"
  osascript -e 'quit app "Droi Next"' || true
  for _ in $(seq 1 50); do pgrep -xq "Droi Next" || break; sleep 0.2; done
  pgrep -xq "Droi Next" && fail "Droi Next did not quit; quit it and run again"
fi

echo "== installing"
rm -rf "/Applications/Droi Next.app"
ditto "$app" "/Applications/Droi Next.app"
version="$(defaults read "/Applications/Droi Next.app/Contents/Info" CFBundleShortVersionString)"
echo "Installed Droi Next $version ($(git rev-parse --short HEAD)) in /Applications."

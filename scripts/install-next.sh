#!/usr/bin/env bash
# Builds this checkout's Droi on its Kernel and installs it. macOS only.
#
#   scripts/install-next.sh         # as Droi Next (apps/native/next), beside a released Droi, which it does not touch
#   scripts/install-next.sh droi    # as Droi (apps/native/local), in place of the released Droi; quit Droi first
#
# Neither has an update feed. Installing as Droi first backs up the released Droi's app and folders to
# ~/Backups/droi-<time> (APFS clones, so they take no room until either copy changes).
#
#   DROI_NEXT_SIGN="Apple Development: …" scripts/install-next.sh
#
# A stable identity keeps what macOS granted the app (Full Disk Access, …) across rebuilds; without one the
# app is signed ad hoc and each build is a new app to macOS.
set -euo pipefail

fail() { echo "install-next: $*" >&2; exit 1; }
[[ "$(uname -s)" == Darwin ]] || fail "macOS only"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

case "${1:-next}" in
  next) dir=apps/native/next name="Droi Next" ;;
  droi) dir=apps/native/local name="Droi" ;;
  *) fail "usage: install-next.sh [next|droi]" ;;
esac
installed="/Applications/$name.app"

case "$(uname -m)" in
  arm64) kernel=darwin-arm64 goarch=arm64 ;;
  x86_64) kernel=darwin-x64 goarch=amd64 ;;
  *) fail "unsupported machine: $(uname -m)" ;;
esac

# Installing as Droi replaces the app whose data it keeps: a running one would write while it is copied.
if [[ "$name" == Droi ]] && pgrep -xq Droi; then
  fail "Droi is running; quit it (its Sessions stop), then run this again from a terminal"
fi

echo "== dependencies"
pnpm install --frozen-lockfile --silent

echo "== the Kernel ($kernel)"
(cd apps/kernel && bun run build.ts --stage "$kernel")

# Droi's resources (the staged Kernel above all) are the build's too.
ln -sfn ../resources "$dir/resources"
# Droi's mygo.json with no updates; Droi Next also has its own name, identifier and icon.
DIR="$dir" NAME="$name" node -e '
  const fs = require("fs")
  const app = JSON.parse(fs.readFileSync("apps/native/mygo.json", "utf8"))
  delete app.updates
  app.out = "build"
  if (process.env.NAME === "Droi Next") Object.assign(app, { name: "Droi Next", slug: "droi-next", identifier: "com.droi.next", icon: "icon.png" })
  for (const [path, file] of Object.entries(app.macos?.helperEntitlements ?? {})) app.macos.helperEntitlements[path] = "../" + file
  fs.writeFileSync(process.env.DIR + "/mygo.json", JSON.stringify(app, null, 2) + "\n")
'

sign="${DROI_NEXT_SIGN:-$(security find-identity -v -p codesigning | sed -n 's/^ *1) [0-9A-F]* "\(.*\)"$/\1/p')}"
echo "== $name (signed ${sign:-ad hoc})"
(cd "$dir" && go tool mygo build -platform "darwin/$goarch" ${sign:+-sign "$sign"})

app="$dir/build/darwin-$goarch/$name.app"
[[ -d "$app" ]] || fail "no app at $app"

if pgrep -xq "$name"; then
  [[ "$name" == Droi ]] && fail "Droi was started during the build; quit it and run this again"
  echo "== quitting the running $name"
  osascript -e "quit app \"$name\"" || true
  for _ in $(seq 1 50); do pgrep -xq "$name" || break; sleep 0.2; done
  pgrep -xq "$name" && fail "$name did not quit; quit it and run again"
fi

if [[ "$name" == Droi ]]; then
  backup="$HOME/Backups/droi-$(date +%Y%m%d-%H%M%S)"
  echo "== backing up the released Droi to $backup"
  mkdir -p "$backup"
  for path in "$installed" "$HOME/Library/Application Support/Droi" "$HOME/.droi" "$HOME/.factory/settings.json"; do
    [[ -e "$path" ]] && cp -cRp "$path" "$backup/"
  done
  echo "Backed up: $(ls "$backup" | tr '\n' ' ')"
fi

echo "== installing"
rm -rf "$installed"
ditto "$app" "$installed"
version="$(defaults read "$installed/Contents/Info" CFBundleShortVersionString)"
echo "Installed $name $version ($(git rev-parse --short HEAD)) in /Applications."

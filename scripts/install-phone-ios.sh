#!/usr/bin/env bash
# Build the Phone App and install it on the connected iPhone: `pnpm install:phone`.
# A Release build signed with the Apple Development certificate in the
# keychain. With a free Apple account the signature lasts seven days; run this
# again to renew it.
# DROI_IPHONE picks a device (name or UDID) when several are connected;
# APPLE_TEAM_ID overrides the team read from the certificate.
# DROI_HTTP_DOMAINS lets the app use plain http to more domain suffixes
# (comma separated, e.g. "myhome" for laptop.myhome reached through Surge).
# Keep it in apps/ios/.env.local (git-ignored), which this script reads.
set -euo pipefail

cd "$(dirname "$0")/../apps/ios"
if [ -f .env.local ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env.local
  set +a
fi

if [ -z "${APPLE_TEAM_ID:-}" ]; then
  APPLE_TEAM_ID=$(security find-certificate -c "Apple Development" -p 2>/dev/null |
    openssl x509 -noout -subject 2>/dev/null |
    sed -n 's/.*OU *= *\([A-Z0-9]*\).*/\1/p' || true)
fi
if [ -z "$APPLE_TEAM_ID" ]; then
  echo "No Apple Development certificate in the keychain. Add your Apple account in" >&2
  echo "Xcode → Settings → Accounts and create one, or set APPLE_TEAM_ID." >&2
  exit 1
fi
export APPLE_TEAM_ID

device="${DROI_IPHONE:-}"
if [ -z "$device" ]; then
  devices=$(mktemp)
  trap 'rm -f "$devices"' EXIT
  xcrun devicectl list devices --json-output "$devices" >/dev/null
  device=$(/usr/bin/python3 - "$devices" <<'PY'
import json, sys
devices = json.load(open(sys.argv[1]))["result"]["devices"]
# Since Xcode 27 the list also holds simulators, which count as paired.
# A phone paired once but out of reach stays listed with tunnelState "unavailable".
for d in devices:
    hw, conn = d.get("hardwareProperties", {}), d.get("connectionProperties", {})
    if (hw.get("deviceType") == "iPhone" and hw.get("reality") != "simulated"
            and conn.get("pairingState") == "paired" and conn.get("tunnelState") != "unavailable"):
        print(hw["udid"])
        break
PY
)
fi
if [ -z "$device" ]; then
  echo "No paired iPhone found. Connect it (USB or the same network), unlock it and trust this Mac." >&2
  exit 1
fi

version=$(/usr/bin/python3 -c 'import json; print(json.load(open("../native/mygo.json"))["version"])')
command -v xcodegen >/dev/null || { echo "Install XcodeGen first: brew install xcodegen" >&2; exit 1; }
xcodegen generate --quiet
# Not a plain `xcodebuild install`: only -allowProvisioningUpdates makes the
# provisioning profile a free account needs.
xcodebuild -project Droi.xcodeproj -scheme Droi -configuration Release \
  -destination "id=$device" -derivedDataPath build -allowProvisioningUpdates \
  DEVELOPMENT_TEAM="$APPLE_TEAM_ID" \
  DROI_HTTP_DOMAINS="${DROI_HTTP_DOMAINS:-}" -quiet build
xcrun devicectl device install app --device "$device" build/Build/Products/Release-iphoneos/Droi.app
xcrun devicectl device process launch --device "$device" com.kkkk2323.droi >/dev/null ||
  echo "Installed but not launched: is the iPhone locked, or the developer not trusted yet?"
echo "Installed Droi $version on the iPhone."
echo "First install: trust the developer in Settings → General → VPN & Device Management."

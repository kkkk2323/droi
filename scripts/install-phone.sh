#!/usr/bin/env bash
# Build the Phone App and install it on the connected iPhone: `pnpm install:phone`.
# A Release build with the JS bundle inside, signed with the Apple Development
# certificate in the keychain. With a free Apple account the signature lasts
# seven days; run this again to renew it.
# DROI_IPHONE picks a device (name or UDID) when several are connected;
# APPLE_TEAM_ID overrides the team read from the certificate.
# DROI_HTTP_DOMAINS lets the app use plain http to more domain suffixes
# (comma separated, e.g. "myhome" for laptop.myhome reached through Surge).
# Keep it in apps/mobile/.env.local (git-ignored); Expo reads that file itself.
set -euo pipefail

cd "$(dirname "$0")/../apps/mobile"
# CocoaPods fails on non-ASCII paths without a UTF-8 locale.
export LANG="${LANG:-en_US.UTF-8}" LC_ALL="${LC_ALL:-en_US.UTF-8}"

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
  device=$(node -e '
    const { result } = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))
    // Since Xcode 27 the list also holds simulators, which count as paired.
    const phone = result.devices.find(
      (d) =>
        d.hardwareProperties?.deviceType === "iPhone" &&
        d.hardwareProperties?.reality !== "simulated" &&
        d.connectionProperties?.pairingState === "paired",
    )
    if (phone) console.log(phone.hardwareProperties.udid)
  ' "$devices")
fi
if [ -z "$device" ]; then
  echo "No paired iPhone found. Connect it (USB or the same network), unlock it and trust this Mac." >&2
  exit 1
fi

npx expo prebuild --platform ios --no-install
(cd ios && pod install)
# Not `expo run:ios`: only xcodebuild can be told to create the provisioning
# profile a free account needs (-allowProvisioningUpdates).
xcodebuild -workspace ios/Droi.xcworkspace -scheme Droi -configuration Release \
  -destination "id=$device" -derivedDataPath build -allowProvisioningUpdates \
  DEVELOPMENT_TEAM="$APPLE_TEAM_ID" CODE_SIGN_STYLE=Automatic -quiet build
xcrun devicectl device install app --device "$device" build/Build/Products/Release-iphoneos/Droi.app
xcrun devicectl device process launch --device "$device" com.kkkk2323.droi >/dev/null ||
  echo "Installed but not launched: is the iPhone locked, or the developer not trusted yet?"
echo "Installed Droi $(node -p 'require("../desktop/package.json").version') on the iPhone."
echo "First install: trust the developer in Settings → General → VPN & Device Management."

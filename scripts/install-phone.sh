#!/usr/bin/env bash
# Build the Phone App (apps/native/phone, MyGo) and install it on the connected
# iPhone: `pnpm install:phone`. Signed with the Apple Development certificate
# in the keychain; with a free Apple account the profile lasts seven days, so
# run this again to renew it.
# DROI_IPHONE picks a device (name or UDID) when several are connected;
# APPLE_TEAM_ID overrides the team read from the certificate.
# Needs Xcode signed in to the Apple account (Settings → Accounts) and xcodegen
# (brew install xcodegen), which makes the throwaway project Xcode creates the
# profile for.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
native="$root/apps/native"
id=$(node -p 'require(process.argv[1]).identifier' "$native/phone/mygo.json")
version=$(node -p 'require(process.argv[1]).version' "$native/phone/mygo.json")

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

device="${DROI_IPHONE:-}"
if [ -z "$device" ]; then
  devices=$(mktemp)
  trap 'rm -f "$devices"' EXIT
  xcrun devicectl list devices --json-output "$devices" >/dev/null
  device=$(node -e '
    const { result } = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))
    // Since Xcode 27 the list also holds simulators, which count as paired.
    // A phone paired once but out of reach stays listed with tunnelState "unavailable".
    const phone = result.devices.find(
      (d) =>
        d.hardwareProperties?.deviceType === "iPhone" &&
        d.hardwareProperties?.reality !== "simulated" &&
        d.connectionProperties?.pairingState === "paired" &&
        d.connectionProperties?.tunnelState !== "unavailable",
    )
    if (phone) console.log(phone.hardwareProperties.udid)
  ' "$devices")
fi
if [ -z "$device" ]; then
  echo "No paired iPhone found. Connect it (USB or the same network), unlock it and trust this Mac." >&2
  exit 1
fi

# Xcode creates a free account's profile only for a project it builds, so build
# an empty app of the same identifier for the device first; MyGo then signs
# with the profile it left.
work=$(mktemp -d)
trap 'rm -rf "$work" "${devices:-}"' EXIT
cat > "$work/Main.swift" <<'SWIFT'
import SwiftUI
@main struct ProfileApp: App { var body: some Scene { WindowGroup { Text("Droi") } } }
SWIFT
cat > "$work/project.yml" <<YAML
name: DroiProfile
targets:
  DroiProfile:
    type: application
    platform: iOS
    deploymentTarget: "17.0"
    sources: [Main.swift]
    settings:
      PRODUCT_BUNDLE_IDENTIFIER: $id
      DEVELOPMENT_TEAM: $APPLE_TEAM_ID
      CODE_SIGN_STYLE: Automatic
      GENERATE_INFOPLIST_FILE: YES
YAML
(cd "$work" && xcodegen generate --quiet)
xcodebuild -project "$work/DroiProfile.xcodeproj" -scheme DroiProfile -configuration Debug \
  -destination "id=$device" -derivedDataPath "$work/build" -allowProvisioningUpdates -quiet build

(cd "$native" && go tool mygo build -platform ios phone)
ipa="$native/phone/build/ios-arm64/droi-phone-$version.ipa"
xcrun devicectl device install app --device "$device" "$ipa"
xcrun devicectl device process launch --device "$device" "$id" >/dev/null ||
  echo "Installed but not launched: is the iPhone locked, or the developer not trusted yet?"
echo "Installed Droi $version on the iPhone."
echo "First install: trust the developer in Settings → General → VPN & Device Management."

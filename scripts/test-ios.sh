#!/usr/bin/env bash
# Run the Phone App's UI tests on the iOS 27 simulator: `scripts/test-ios.sh`.
# Extra arguments go to xcodebuild, e.g. -only-testing:DroiUITests/PairingTests.
#
# An XCUITest cannot start processes, so this starts a host beside the
# simulator (apps/ios/DroiUITests/Harness/fake-daemon-host.mjs) that starts a
# Fake Daemon for each test, then stops it when the run ends.
# DROI_SIMULATOR picks another simulator (UDID); DROI_HOST_PORT the host's port;
# FAKE_DAEMON_MJS another bundled Fake Daemon (default: the one in droid-sdk-go
# at the version apps/native/go.mod pins, or a checkout in ~/dev/droid-sdk-go-repo).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
simulator="${DROI_SIMULATOR:-A8E7E5E5-C826-4B63-8BA0-89ABFB7BD77E}"
port="${DROI_HOST_PORT:-41999}"
build="$root/apps/ios/build"
mkdir -p "$build"

command -v xcodegen >/dev/null || { echo "Install XcodeGen first: brew install xcodegen" >&2; exit 1; }
command -v node >/dev/null || { echo "Install node first" >&2; exit 1; }

if [ -z "${FAKE_DAEMON_MJS:-}" ]; then
  local_checkout="$HOME/dev/droid-sdk-go-repo/fakedaemon/fake-daemon.mjs"
  if [ -f "$local_checkout" ]; then
    FAKE_DAEMON_MJS="$local_checkout"
  else
    FAKE_DAEMON_MJS="$(cd "$root/apps/native" && go list -m -f '{{.Dir}}' github.com/kkkk2323/droid-sdk-go)/fakedaemon/fake-daemon.mjs"
  fi
fi
[ -f "$FAKE_DAEMON_MJS" ] || { echo "No Fake Daemon at $FAKE_DAEMON_MJS" >&2; exit 1; }

# The Fake Daemon's /meta answers the release version, as a Gateway of this release would.
DROID_FAKE_APP_VERSION="$(/usr/bin/python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$root/apps/native/mygo.json")"
export DROID_FAKE_APP_VERSION FAKE_DAEMON_MJS DROI_HOST_PORT="$port"
# xcodebuild hands TEST_RUNNER_* variables of its own environment to the test runner.
export TEST_RUNNER_DROI_HOST_PORT="$port" TEST_RUNNER_DROI_APP_VERSION="$DROID_FAKE_APP_VERSION"

xcrun simctl boot "$simulator" 2>/dev/null || true

node "$root/apps/ios/DroiUITests/Harness/fake-daemon-host.mjs" >"$build/fake-daemon-host.log" 2>&1 &
host=$!
trap 'kill "$host" 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do
  curl -fs "http://127.0.0.1:$port/health" >/dev/null 2>&1 && break
  kill -0 "$host" 2>/dev/null || { cat "$build/fake-daemon-host.log" >&2; exit 1; }
  sleep 0.1
done

cd "$root/apps/ios"
export APPLE_TEAM_ID="${APPLE_TEAM_ID:-}"
xcodegen generate --quiet

rm -rf "$build/tests.xcresult"
set +e
xcodebuild test \
  -project Droi.xcodeproj -scheme Droi \
  -destination "id=$simulator" \
  -derivedDataPath "$build/dd-tests" \
  -resultBundlePath "$build/tests.xcresult" \
  "$@"
status=$?
set -e
exit $status

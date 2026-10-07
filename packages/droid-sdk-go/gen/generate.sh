#!/bin/sh
# Regenerates package protocol and the Client's typed methods from a release
# of @factory/droid-sdk:
#
#   gen/generate.sh [version]     (default: the version in protocol/zz_methods.go)
#
# Needs Node and npm. The SDK's sources are not published, but its bundle
# keeps every zod schema and the DaemonClient's method table readable; the
# scripts here pull both out of it.
set -eu
cd "$(dirname "$0")"
version=${1:-$(sed -n 's/^const SDKVersion = "\(.*\)"$/\1/p' ../protocol/zz_methods.go)}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

npm install --silent --no-audit --no-fund --prefix "$work" "@factory/droid-sdk@$version" >/dev/null
sdk="$work/node_modules/@factory/droid-sdk/dist"
chunk=$(grep -l 'var DaemonClient = class' "$sdk"/chunk-*.mjs | head -1)
[ -n "$chunk" ] || { echo "no DaemonClient in the $version bundle" >&2; exit 1; }

node expose.cjs "$chunk" "$work/chunk.mjs"
node split.cjs "$chunk" "$work/mod" >/dev/null
node methods.cjs "$work/mod/daemon-sdk/src/DaemonClient.ts.js" "$work/methods.json"
SDK_VERSION=$version GO_MODULE=github.com/kkkk2323/droi/packages/droid-sdk-go node gen.mjs "$work/chunk.mjs" "$work/methods.json" ../protocol
gofmt -w ../protocol ../zz_client_methods.go
echo "generated from @factory/droid-sdk $version"

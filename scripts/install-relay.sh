#!/bin/sh
# Installs Droi Relay (ADR 0034) on a Linux server with systemd, or upgrades it:
#
#   curl -fsSL https://github.com/kkkk2323/droi/releases/latest/download/install-relay.sh | sudo sh -s -- --domain relay.example.com
#
# It downloads the latest droi-relay to /usr/local/bin and runs `droi-relay install` with the
# arguments given; without any, an upgrade keeps the settings the relay has.
set -eu

if [ "$(uname -s)" != Linux ]; then
	echo "Droi Relay installs on Linux with systemd, not $(uname -s)." >&2
	exit 1
fi
if [ "$(id -u)" -ne 0 ]; then
	echo "Permission denied: installing adds /usr/local/bin/droi-relay and a systemd service. Run it again with sudo." >&2
	exit 1
fi
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	echo "Droi Relay runs on amd64 or arm64, not $(uname -m)." >&2
	exit 1
	;;
esac

base=${DROI_RELAY_DOWNLOAD:-https://github.com/kkkk2323/droi/releases/latest/download}
tmp=$(mktemp)
trap 'rm -f "$tmp" "$tmp.gz"' EXIT
echo "Downloading Droi Relay (linux/$arch)…"
if ! curl -fL --progress-bar --connect-timeout 20 -o "$tmp.gz" "$base/droi-relay-linux-$arch.gz"; then
	echo "Could not download $base/droi-relay-linux-$arch.gz. Check that this server reaches github.com, then run this again." >&2
	exit 1
fi
gunzip -c "$tmp.gz" > "$tmp"
install -m 0755 "$tmp" /usr/local/bin/droi-relay
exec /usr/local/bin/droi-relay install "$@"

package gateway

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// PairingInfo is what Settings → Remote Access shows.
type PairingInfo struct {
	// Link is what a phone opens, and the QR code's content:
	// http://<pairing host or first LAN address>:<port>/#pair=<Pairing Token>,
	// with &relay=<Relay address> when there is a Relay (ADR 0034), or the
	// Relay address alone, <Relay address>/#pair=<Pairing Token>, when the
	// Gateway listens on no LAN address. "" with neither.
	Link string
	// LANAddresses are where the Gateway listens; empty while Remote Access is off.
	LANAddresses []string
	Port         int
	// Relay is where a phone reaches the Gateway through the Relay, "" without one.
	Relay string
}

// Pairing builds the pairing link. pairingHost (the PairingHost setting, ""
// for none) replaces the LAN address in the link, but still needs the
// Gateway on a LAN address to answer it. relay is the Relay address, "" for
// none.
func Pairing(lanAddresses []string, port int, pairingHost, token, relay string) PairingInfo {
	info := PairingInfo{LANAddresses: lanAddresses, Port: port, Relay: relay}
	if len(lanAddresses) == 0 {
		if relay != "" {
			info.Link = relay + "/#pair=" + token
		}
		return info
	}
	host := pairingHost
	if host == "" {
		host = lanAddresses[0]
	}
	info.Link = "http://" + host + ":" + strconv.Itoa(port) + "/#pair=" + token
	if relay != "" {
		info.Link += "&" + RelayFragment + "=" + url.QueryEscape(relay)
	}
	return info
}

// RelayFragment is the pairing link's fragment parameter that carries the
// Relay address.
const RelayFragment = "relay"

// Pairing is the PairingInfo for this Gateway's listeners and Relay.
func (g *Gateway) Pairing(pairingHost, token string) PairingInfo {
	return Pairing(g.LANAddresses(), g.port, pairingHost, token, g.RelayAddress())
}

// ComputerName is the name a phone shows for this computer: macOS's
// Computer Name ("Clive's MacBook Pro") when it can be read, else the host
// name without ".local".
func ComputerName() string {
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--get", "ComputerName").Output(); err == nil {
			if name := strings.TrimSpace(string(out)); name != "" {
				return name
			}
		}
	}
	host, _ := os.Hostname()
	return strings.TrimSuffix(host, ".local")
}

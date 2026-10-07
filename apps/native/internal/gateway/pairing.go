package gateway

import (
	"context"
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
	// http://<pairing host or first LAN address>:<port>/#pair=<Pairing Token>.
	// "" while the Gateway listens on no LAN address.
	Link string
	// LANAddresses are where the Gateway listens; empty while Remote Access is off.
	LANAddresses []string
	Port         int
}

// Pairing builds the pairing link. pairingHost (the PairingHost setting, ""
// for none) replaces the LAN address in the link, but still needs the
// Gateway on a LAN address to answer it.
func Pairing(lanAddresses []string, port int, pairingHost, token string) PairingInfo {
	info := PairingInfo{LANAddresses: lanAddresses, Port: port}
	if len(lanAddresses) == 0 {
		return info
	}
	host := pairingHost
	if host == "" {
		host = lanAddresses[0]
	}
	info.Link = "http://" + host + ":" + strconv.Itoa(port) + "/#pair=" + token
	return info
}

// Pairing is the PairingInfo for this Gateway's listeners.
func (g *Gateway) Pairing(pairingHost, token string) PairingInfo {
	return Pairing(g.LANAddresses(), g.port, pairingHost, token)
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

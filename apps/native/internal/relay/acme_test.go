package relay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestACertificateFromAnACMEServer runs install's certificate step, then a
// relay with a domain, against an ACME server that checks tls-alpn-01 for
// real: Pebble, with its DNS pointing every name at this computer. From
// Pebble's checkout (github.com/letsencrypt/pebble):
//
//	pebble-challtestsrv -defaultIPv4 127.0.0.1 -dns01 :8053 -http01 "" -https01 "" -tlsalpn01 "" -doh "" &
//	pebble -config test/config/pebble-config.json -dnsserver 127.0.0.1:8053 &
//	DROI_RELAY_ACME=https://localhost:14000/dir DROI_RELAY_ACME_ROOT=$PWD/test/certs/pebble.minica.pem \
//	  go test ./apps/native/internal/relay -run ACME -v
//
// Pebble checks the challenge on port 5001, where the relay answers it.
func TestACertificateFromAnACMEServer(t *testing.T) {
	dirURL, root := os.Getenv("DROI_RELAY_ACME"), os.Getenv("DROI_RELAY_ACME_ROOT")
	if dirURL == "" || root == "" {
		t.Skip("set DROI_RELAY_ACME and DROI_RELAY_ACME_ROOT to an ACME server such as Pebble")
	}
	pem, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pem)
	oldDir, oldClient, oldAddr := acmeDirectory, acmeHTTPClient, httpsAddr
	t.Cleanup(func() { acmeDirectory, acmeHTTPClient, httpsAddr = oldDir, oldClient, oldAddr })
	acmeDirectory = dirURL
	acmeHTTPClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	httpsAddr = "127.0.0.1:5001"

	const domain = "relay.droi.test"
	dir := t.TempDir()
	ln, err := net.Listen("tcp", httpsAddr)
	if err != nil {
		t.Fatal(err)
	}
	err = obtainCertificate(ln, domain, certDir(dir))
	ln.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(certDir(dir), domain)); err != nil {
		t.Fatalf("the certificate is not kept for the service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() {
		served <- Serve(ctx, ServeOptions{Domain: domain, CertDir: certDir(dir), Server: ServerOptions{Key: "k", Version: "test", Logf: t.Logf}})
	}()
	tr := &http.Transport{
		// Pebble's certificates chain to a root it makes at start.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, httpsAddr)
		},
	}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	var resp *http.Response
	for range 50 {
		if resp, err = client.Get("https://" + domain + "/healthz"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	leaf := resp.TLS.PeerCertificates[0]
	if resp.StatusCode != 200 || !slices.Contains(leaf.DNSNames, domain) || resp.TLS.NegotiatedProtocol == "h2" {
		t.Fatalf("%s, names %v, protocol %q", resp.Status, leaf.DNSNames, resp.TLS.NegotiatedProtocol)
	}
	t.Logf("served a certificate for %v from %q", leaf.DNSNames, leaf.Issuer.CommonName)

	// A computer's socket, an HTTP/1.1 upgrade, goes through.
	c, _, err := websocket.Dial(ctx, "wss://"+domain+"/agent", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	reg, _ := json.Marshal(frame{T: "register", TokenHash: TokenHash("abcdefghijklmnopqrstuvwx"), Key: "k", Proto: Protocol})
	if err := c.Write(ctx, websocket.MessageText, reg); err != nil {
		t.Fatal(err)
	}
	var f frame
	if _, data, err := c.Read(ctx); err != nil || json.Unmarshal(data, &f) != nil || f.T != "registered" {
		t.Fatalf("%+v %v", f, err)
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

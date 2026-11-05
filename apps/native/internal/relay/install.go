package relay

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	serviceName = "droi-relay"
	unitFile    = "/etc/systemd/system/" + serviceName + ".service"
	systemDir   = "/var/lib/droi-relay"
	configName  = "relay.json"
	keyName     = "key"
)

// defaultDir is where the key, the settings and the certificate live: the
// service's directory as root, else one in the user's home.
func defaultDir() string {
	if os.Geteuid() == 0 {
		return systemDir
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, serviceName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", serviceName)
}

// config is how the relay is reached, as install keeps it in relay.json.
type config struct {
	Domain    string `json:"domain,omitempty"`
	Listen    string `json:"listen,omitempty"`
	PublicURL string `json:"publicUrl,omitempty"`
}

var domainName = regexp.MustCompile(`(?i)^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,63}$`)

func (c config) checkServe() error {
	if c.Domain == "" {
		return nil
	}
	if c.Listen != "" || c.PublicURL != "" {
		return errors.New("give --domain, or --listen with --public-url, not both")
	}
	if !domainName.MatchString(c.Domain) {
		return fmt.Errorf("%q is not a domain name such as relay.example.com", c.Domain)
	}
	return nil
}

// checkInstall also wants the address phones reach the relay at.
func (c config) checkInstall() error {
	if c.Domain == "" && (c.Listen == "" || c.PublicURL == "") {
		return errors.New("give --domain relay.example.com, or --listen 127.0.0.1:8787 with --public-url https://relay.example.com for a reverse proxy")
	}
	if err := c.checkServe(); err != nil || c.Domain != "" {
		return err
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("--public-url %q is not an address such as https://relay.example.com", c.PublicURL)
	}
	return nil
}

// public is the relay's address without the key.
func (c config) public() string {
	if c.Domain != "" {
		return "https://" + strings.ToLower(c.Domain)
	}
	return strings.TrimRight(c.PublicURL, "/")
}

// InstallCommand is what installs a relay for domain on a server,
// relay.example.com's when domain is not a domain name.
func InstallCommand(domain string) string {
	domain = strings.TrimSpace(domain)
	if !domainName.MatchString(domain) {
		domain = "relay.example.com"
	}
	return "curl -fsSL " + installScript + " | sudo sh -s -- --domain " + strings.ToLower(domain)
}

// installScript downloads the latest droi-relay and runs its install.
const installScript = "https://github.com/kkkk2323/droi/releases/latest/download/install-relay.sh"

// Address is the relay address a computer is given: the relay's public
// address with its key in the fragment, which never leaves the computer.
func Address(public, key string) string {
	return strings.TrimRight(public, "/") + "/#key=" + url.QueryEscape(key)
}

func readConfig(dir string) (config, error) {
	var c config
	b, err := os.ReadFile(filepath.Join(dir, configName))
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func writeFile(file string, b []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// loadKey is DROI_RELAY_KEY, else the key kept in dir, made when there is
// none and create is set.
func loadKey(dir string, create bool) (string, error) {
	if k := os.Getenv("DROI_RELAY_KEY"); k != "" {
		return k, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, keyName))
	if k := strings.TrimSpace(string(b)); err == nil && k != "" {
		return k, nil
	}
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "", fmt.Errorf("no permission to read the key in %s; run it with sudo", dir)
	case !create:
		return "", fmt.Errorf("there is no relay in %s; install one with: sudo droi-relay install --domain relay.example.com", dir)
	}
	return newKey(dir)
}

func newKey(dir string) (string, error) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	k := base64.RawURLEncoding.EncodeToString(b)
	return k, writeFile(filepath.Join(dir, keyName), []byte(k+"\n"), 0o600)
}

func needRoot() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("installing needs Linux with systemd; on %s run: droi-relay serve --listen ADDR", runtime.GOOS)
	}
	if os.Geteuid() != 0 {
		return errors.New("permission denied: installing adds a systemd service. Run it again with sudo")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemctl was not found; installing needs systemd. Run the relay with: droi-relay serve")
	}
	return nil
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)+" "+err.Error()))
	}
	return nil
}

func installed() bool {
	_, err := os.Stat(unitFile)
	return err == nil
}

func unit(exe, dir string) string {
	return `[Unit]
Description=Droi Relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=` + exe + ` serve --dir ` + dir + `
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=` + dir + `

[Install]
WantedBy=multi-user.target
`
}

func installMain(args []string, version string) (err error) {
	f, err := parseFlags("install", args, false)
	if err != nil {
		return err
	}
	if err := needRoot(); err != nil {
		return err
	}
	cfg := f.cfg
	if cfg == (config{}) {
		if kept, err := readConfig(f.dir); err == nil {
			cfg = kept
		}
	}
	if err := cfg.checkInstall(); err != nil {
		return usageError{err}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	fmt.Printf("Installing Droi Relay %s (%s/%s)…\n", version, runtime.GOOS, runtime.GOARCH)
	if installed() {
		// Installing again: the running relay holds the port, and runs
		// again as it was if this install fails.
		_ = systemctl("stop", serviceName)
		defer func() {
			if err != nil {
				_ = systemctl("start", serviceName)
			}
		}()
	}
	listen := cfg.Listen
	if cfg.Domain != "" {
		listen = httpsAddr
		if _, err := net.LookupHost(cfg.Domain); err != nil {
			return fmt.Errorf("%s has no DNS record yet. Add an A record that points it to this server's public IP address, wait a few minutes, then run this again", cfg.Domain)
		}
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return portError(listen, err)
	}
	if cfg.Domain != "" {
		fmt.Printf("Getting an HTTPS certificate from Let's Encrypt for %s…\n", cfg.Domain)
		err = obtainCertificate(ln, cfg.Domain, certDir(f.dir))
	}
	ln.Close()
	if err != nil {
		return err
	}
	key, err := loadKey(f.dir, true)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := writeFile(filepath.Join(f.dir, configName), append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := writeFile(unitFile, []byte(unit(exe, f.dir)), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", serviceName}, {"restart", serviceName}} {
		if err := systemctl(args...); err != nil {
			return err
		}
	}
	if err := waitHealthy(cfg); err != nil {
		return fmt.Errorf("the relay did not start: %v. See its log: journalctl -u %s -e", err, serviceName)
	}
	fmt.Printf(`Droi Relay is running. It starts again after a reboot.

Relay address (paste it into Droi → Settings → Remote Access on your computer):

    %s

Show it again: sudo droi-relay address
Log: journalctl -u %s -f
`, Address(cfg.public(), key), serviceName)
	return nil
}

// obtainCertificate gets domain's certificate into cacheDir, unless it is
// there, answering Let's Encrypt's tls-alpn-01 challenge on ln. It asks
// once: retrying a failed check at once would only spend the few failures
// Let's Encrypt allows an hour.
func obtainCertificate(ln net.Listener, domain, cacheDir string) error {
	m := certManager(domain, cacheDir)
	srv := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(tls.NewListener(ln, tlsConfig(m))) }()
	defer srv.Close()
	done := make(chan error, 1)
	go func() {
		// An ECDSA client, as every phone and computer now is, so the
		// certificate got is the one they are served.
		_, err := m.GetCertificate(&tls.ClientHelloInfo{
			ServerName:        domain,
			CipherSuites:      []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			SupportedCurves:   []tls.CurveID{tls.CurveP256},
			SignatureSchemes:  []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256},
			SupportedVersions: []uint16{tls.VersionTLS13, tls.VersionTLS12},
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("could not get an HTTPS certificate for %s: %v\n\nLet's Encrypt checks the domain on TCP port 443 of this server. Check that %s's A record points to this server and that TCP port 443 is open in the server's firewall and in the cloud provider's security group, then run this again", domain, err, domain)
		}
		return nil
	case <-time.After(3 * time.Minute):
		return fmt.Errorf("Let's Encrypt did not answer in 3 minutes for %s; run this again later", domain)
	}
}

// waitHealthy waits for the installed relay to answer /healthz, over HTTPS
// with its certificate for a domain.
func waitHealthy(cfg config) error {
	client := &http.Client{Timeout: 5 * time.Second}
	target := "http://" + loopback(cfg.Listen) + "/healthz"
	if cfg.Domain != "" {
		target = "https://" + cfg.Domain + "/healthz"
		client.Transport = &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, loopback(httpsAddr))
			},
		}
	}
	var err error
	for range 20 {
		var resp *http.Response
		if resp, err = client.Get(target); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = fmt.Errorf("it answered %s", resp.Status)
		}
		time.Sleep(time.Second)
	}
	return err
}

// loopback is addr with a wildcard host made this machine's.
func loopback(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func addressMain(args []string, _ string) error {
	f, err := parseFlags("address", args, false)
	if err != nil {
		return err
	}
	return printAddress(f.dir)
}

func printAddress(dir string) error {
	key, err := loadKey(dir, false)
	if err != nil {
		return err
	}
	cfg, err := readConfig(dir)
	if err != nil || cfg.public() == "" {
		return fmt.Errorf("the relay in %s has no public address; install it with --domain, or with --listen and --public-url", dir)
	}
	fmt.Println(Address(cfg.public(), key))
	return nil
}

func resetKeyMain(args []string, _ string) error {
	f, err := parseFlags("reset-key", args, false)
	if err != nil {
		return err
	}
	if _, err := loadKey(f.dir, false); err != nil {
		return err
	}
	if _, err := newKey(f.dir); err != nil {
		return err
	}
	if installed() {
		if err := systemctl("restart", serviceName); err != nil {
			return err
		}
	}
	fmt.Println("The relay has a new key. Computers that use the old relay address are refused: in Droi → Settings → Remote Access, click Change relay and paste the new address:")
	fmt.Println()
	return printAddress(f.dir)
}

func uninstallMain(args []string, _ string) error {
	f, err := parseFlags("uninstall", args, false)
	if err != nil {
		return err
	}
	if err := needRoot(); err != nil {
		return err
	}
	if installed() {
		_ = systemctl("disable", "--now", serviceName)
		if err := os.Remove(unitFile); err != nil {
			return err
		}
		_ = systemctl("daemon-reload")
	}
	// Only what the relay keeps, in case DIR holds anything else.
	for _, name := range []string{keyName, configName, "certs"} {
		if err := os.RemoveAll(filepath.Join(f.dir, name)); err != nil {
			return err
		}
	}
	_ = os.Remove(f.dir)
	if exe, err := os.Executable(); err == nil && filepath.Base(exe) == serviceName {
		_ = os.Remove(exe)
	}
	fmt.Println("Droi Relay is removed: its service, key and certificate. Computers that used it no longer connect through it.")
	return nil
}

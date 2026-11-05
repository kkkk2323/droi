package relay

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

const usageText = `usage: droi-relay <command> [flags]

Droi Relay (ADR 0034) is a server that Droi on a computer connects out to,
so that a paired phone away from the computer's network reaches it through
this server. Install it on a Linux server with systemd that both can reach.

Commands:
  install     install it as a systemd service, get its HTTPS certificate
              and print the relay address (run with sudo)
  address     print the relay address again
  reset-key   make a new key; computers then need the new relay address
  uninstall   stop the service and remove the relay, its key and certificate
  serve       run the relay in the foreground (what the service runs)

Flags of install and serve:
  --domain NAME      serve HTTPS on port 443 for NAME, with a certificate
                     from Let's Encrypt; NAME's A record must point here
  --listen ADDR      serve plain HTTP on ADDR instead, for a reverse proxy
                     that serves HTTPS ...
  --public-url URL   ... at URL, such as https://relay.example.com
  --dir DIR          where the key, the settings and the certificate live
                     (default %s)

install without flags installs again with the settings it has, such as after
an upgrade. serve also takes --tls-cert FILE and --tls-key FILE (HTTPS on
--listen with this certificate) and --max-body-mb N (the largest request
body, default 128). DROI_RELAY_KEY, when set, replaces the key in DIR.
`

func usage() string { return fmt.Sprintf(usageText, defaultDir()) }

// Main is droi-relay; args are what follows the program's name. It returns
// the exit code.
func Main(args []string, version string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage())
		return 2
	}
	cmd, args := args[0], args[1:]
	var run func([]string, string) error
	switch cmd {
	case "serve":
		run = serveMain
	case "install":
		run = installMain
	case "address":
		run = addressMain
	case "reset-key":
		run = resetKeyMain
	case "uninstall":
		run = uninstallMain
	case "help", "-h", "--help":
		fmt.Print(usage())
		return 0
	default:
		fmt.Fprintf(os.Stderr, "droi-relay: there is no command %q\n\n%s", cmd, usage())
		return 2
	}
	err := run(args, version)
	var bad usageError
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		fmt.Print(usage())
		return 0
	case errors.As(err, &bad):
		fmt.Fprintf(os.Stderr, "droi-relay %s: %s\n\n%s", cmd, err, usage())
		return 2
	}
	fmt.Fprintf(os.Stderr, "droi-relay %s: %s\n", cmd, err)
	return 1
}

// usageError is a command given flags it cannot use.
type usageError struct{ error }

// flags are what install, serve and the other commands were given.
type flags struct {
	cfg             config
	dir             string
	tlsCert, tlsKey string
	maxBodyMB       int64
}

func parseFlags(name string, args []string, serve bool) (flags, error) {
	var f flags
	fs := flag.NewFlagSet("droi-relay "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&f.cfg.Domain, "domain", "", "")
	fs.StringVar(&f.cfg.Listen, "listen", "", "")
	fs.StringVar(&f.cfg.PublicURL, "public-url", "", "")
	fs.StringVar(&f.dir, "dir", defaultDir(), "")
	if serve {
		fs.StringVar(&f.tlsCert, "tls-cert", "", "")
		fs.StringVar(&f.tlsKey, "tls-key", "", "")
		fs.Int64Var(&f.maxBodyMB, "max-body-mb", 128, "")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return f, err
		}
		return f, usageError{err}
	}
	if fs.NArg() > 0 {
		return f, usageError{fmt.Errorf("unexpected %q", fs.Arg(0))}
	}
	if (f.tlsCert == "") != (f.tlsKey == "") {
		return f, usageError{errors.New("give both --tls-cert and --tls-key, or neither")}
	}
	return f, nil
}

func serveMain(args []string, version string) error {
	f, err := parseFlags("serve", args, true)
	if err != nil {
		return err
	}
	cfg := f.cfg
	if cfg == (config{}) {
		// The service runs `serve --dir DIR` with what install kept there.
		if kept, err := readConfig(f.dir); err == nil {
			cfg = kept
		}
	}
	if cfg.Domain == "" && cfg.Listen == "" {
		cfg.Listen = ":8787"
	}
	if err := cfg.checkServe(); err != nil {
		return usageError{err}
	}
	key, err := loadKey(f.dir, true)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Serve(ctx, ServeOptions{
		Listen: cfg.Listen, TLSCert: f.tlsCert, TLSKey: f.tlsKey,
		Domain: cfg.Domain, CertDir: certDir(f.dir),
		Server: ServerOptions{Key: key, Version: version, MaxBody: f.maxBodyMB << 20},
	})
}

// ServeOptions is where Serve listens and its Server's options.
type ServeOptions struct {
	// Listen is where to serve plain HTTP, or HTTPS with TLSCert and
	// TLSKey; with Domain, ":443" when empty.
	Listen          string
	TLSCert, TLSKey string
	// Domain, when set, serves HTTPS for it with a certificate from
	// Let's Encrypt, kept in CertDir.
	Domain  string
	CertDir string
	Server  ServerOptions
}

// Serve runs a Server until ctx ends, then ends every Agent's socket so
// the Hosts dial again at once.
func Serve(ctx context.Context, o ServeOptions) error {
	s := NewServer(o.Server)
	listen := o.Listen
	if listen == "" && o.Domain != "" {
		listen = httpsAddr
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return portError(listen, err)
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 30 * time.Second}
	scheme := "http"
	switch {
	case o.Domain != "":
		srv.TLSConfig = tlsConfig(certManager(o.Domain, o.CertDir))
		// The relay's sockets are HTTP/1.1 upgrades, which HTTP/2 has not.
		srv.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
		ln = tls.NewListener(ln, srv.TLSConfig)
		scheme = "https"
	case o.TLSCert != "":
		scheme = "https"
	}
	where := scheme + "://" + ln.Addr().String()
	if o.Domain != "" {
		where = "https://" + o.Domain
	}
	log.Printf("relay: Droi Relay %s listening on %s", o.Server.Version, where)
	errc := make(chan error, 1)
	go func() {
		if o.TLSCert != "" {
			errc <- srv.ServeTLS(ln, o.TLSCert, o.TLSKey)
		} else {
			errc <- srv.Serve(ln)
		}
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	s.Close()
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	return nil
}

// httpsAddr is where a relay with a domain serves HTTPS; Let's Encrypt
// checks the domain on port 443.
var httpsAddr = ":443"

// acmeDirectory is the certificate authority's ACME directory, and
// acmeHTTPClient, nil for the default, what reaches it.
var (
	acmeDirectory  = autocert.DefaultACMEDirectory
	acmeHTTPClient *http.Client
)

func certDir(dir string) string { return filepath.Join(dir, "certs") }

func certManager(domain, cacheDir string) *autocert.Manager {
	return &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domain),
		Cache:      autocert.DirCache(cacheDir),
		Client:     &acme.Client{DirectoryURL: acmeDirectory, HTTPClient: acmeHTTPClient},
	}
}

// tlsConfig serves m's certificates and answers its tls-alpn-01
// challenges, over HTTP/1.1 only.
func tlsConfig(m *autocert.Manager) *tls.Config {
	c := m.TLSConfig()
	c.NextProtos = []string{"http/1.1", acme.ALPNProto}
	return c
}

// portError says what to do about a port that cannot be listened on.
func portError(addr string, err error) error {
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Errorf("port %s is in use by another program. Stop that program, or run the relay behind it as a reverse proxy: %s", portOf(addr), proxyDocs)
	}
	if errors.Is(err, syscall.EACCES) {
		return fmt.Errorf("no permission to listen on port %s; run it with sudo", portOf(addr))
	}
	return err
}

func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return addr
}

// proxyDocs explains how to run a relay behind Caddy or nginx.
const proxyDocs = "https://github.com/kkkk2323/droi/blob/main/apps/native/README.md#behind-a-reverse-proxy"

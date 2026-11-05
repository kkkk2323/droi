package relay

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const usageText = `usage: %[1]s relay [--listen ADDR] [--tls-cert FILE --tls-key FILE]

Runs a Relay (ADR 0034): a server that %[2]s Hosts dial out to, so that a
phone away from a Host's network reaches it through this server. Put it on a
machine both can reach, behind HTTPS: give it a certificate, or run it behind
Caddy or nginx. Then set its address in %[2]s → Settings → Remote Access.

  --listen ADDR     where to listen (default :8787)
  --tls-cert FILE   serve HTTPS with this certificate chain ...
  --tls-key FILE    ... and its private key
  --max-body-mb N   the largest request body (default 128)

DROI_RELAY_KEY, when set, is the registration key every Host must present;
without it any Host may register (a phone still needs the Host's Relay Id
and Pairing Token to reach it).
`

// Options is what Main needs from the program around it.
type Options struct {
	// ID and Name name the program in the help: "droi", "Droi".
	ID, Name string
	Version  string
}

// Main is `droi relay`; args are what follows "relay". It returns the exit
// code.
func Main(args []string, o Options) int {
	fs := flag.NewFlagSet(o.ID+" relay", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	listen := fs.String("listen", ":8787", "")
	cert := fs.String("tls-cert", "", "")
	key := fs.String("tls-key", "", "")
	maxBody := fs.Int64("max-body-mb", 128, "")
	usage := fmt.Sprintf(usageText, o.ID, o.Name)
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		fmt.Print(usage)
		return 0
	} else if err != nil || fs.NArg() > 0 || (*cert == "") != (*key == "") {
		if err == nil {
			err = errors.New("give both --tls-cert and --tls-key, or neither")
			if fs.NArg() > 0 {
				err = fmt.Errorf("unexpected %q", fs.Arg(0))
			}
		}
		fmt.Fprintln(os.Stderr, o.ID+" relay:", err)
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := Serve(ctx, ServeOptions{Listen: *listen, TLSCert: *cert, TLSKey: *key, Server: ServerOptions{Key: os.Getenv("DROI_RELAY_KEY"), Version: o.Version, MaxBody: *maxBody << 20}}); err != nil {
		fmt.Fprintln(os.Stderr, o.ID+" relay:", err)
		return 1
	}
	return 0
}

// ServeOptions is where Serve listens and its Server's options.
type ServeOptions struct {
	Listen          string
	TLSCert, TLSKey string
	Server          ServerOptions
}

// Serve runs a Server until ctx ends, then ends every Agent's socket so
// the Hosts dial again at once.
func Serve(ctx context.Context, o ServeOptions) error {
	s := NewServer(o.Server)
	ln, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 30 * time.Second}
	access := "open"
	if o.Server.Key != "" {
		access = "registration key"
	}
	scheme := "http"
	if o.TLSCert != "" {
		scheme = "https"
	}
	log.Printf("relay: Droi Relay %s listening on %s://%s (access: %s)", o.Server.Version, scheme, ln.Addr(), access)
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

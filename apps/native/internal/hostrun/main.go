package hostrun

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

const usage = `usage: droi host [--port N]

Runs a Droi Host without a window: the Daemon, the Gateway (Remote Access
always on) and Memory. Prints a pairing link and QR code, and stops on
Ctrl-C or SIGTERM. Settings, Memory and logs live in
$XDG_CONFIG_HOME/droi (default ~/.config/droi). Sign in with "droid login",
or set FACTORY_API_KEY.

  --port N   the Gateway's port (default 41417)
`

// Main is `droi host`; args are what follows "host". It returns the exit
// code.
func Main(args []string, version, executable string) int {
	port, help, err := parseArgs(args)
	if help {
		fmt.Print(usage)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "droi host:", err)
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "droi host:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	st, err := os.Stdout.Stat()
	err = Run(ctx, Options{
		Home:       home,
		Version:    version,
		Env:        os.Getenv,
		Executable: executable,
		Port:       port,
		Out:        os.Stdout,
		Terminal:   err == nil && st.Mode()&os.ModeCharDevice != 0,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "droi host:", err)
		return 1
	}
	return 0
}

// parseArgs reads what follows "host": the Gateway's port, 0 for the
// default, and whether help was asked for.
func parseArgs(args []string) (port int, help bool, err error) {
	fs := flag.NewFlagSet("droi host", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&port, "port", 0, "")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		return 0, true, nil
	} else if err != nil {
		return 0, false, err
	}
	if fs.NArg() > 0 {
		return 0, false, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	set := false
	fs.Visit(func(*flag.Flag) { set = true })
	if set && (port < 1 || port > 65535) {
		return 0, false, fmt.Errorf("--port %d is not a port", port)
	}
	return port, false, nil
}

package host

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// DaemonAnchorCommand runs the app's own executable as the Daemon's parent of
// record on Windows. There the Daemon cannot take a signal it could shut down
// on, and only watches a parent PID (`--parent-pid`, every 2 s): it shuts
// down in order once that process is gone. The anchor lives until its input
// closes, so the Host ends it, and the Daemon with it, by closing that pipe;
// the pipe also closes when the Host dies.
const DaemonAnchorCommand = "daemon-anchor"

// AnchorMain is the anchor's entry: it waits for its input to close.
func AnchorMain() int { return anchorMain(os.Stdin) }

func anchorMain(in io.Reader) int {
	_, _ = io.Copy(io.Discard, in)
	return 0
}

// anchor is a running daemon-anchor and the end of its input the Host holds.
type anchor struct {
	cmd *exec.Cmd
	in  io.WriteCloser
}

// startAnchor runs name with args as an anchor: the app's executable with
// DaemonAnchorCommand.
func startAnchor(name string, args ...string) (*anchor, error) {
	cmd := exec.Command(name, args...)
	hideConsole(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &anchor{cmd: cmd, in: in}, nil
}

func (a *anchor) pid() string { return strconv.Itoa(a.cmd.Process.Pid) }

// end closes the anchor's input and waits for it to go, so that the Daemon's
// next poll finds no process there.
func (a *anchor) end() {
	_ = a.in.Close()
	done := make(chan struct{})
	go func() { _ = a.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = a.cmd.Process.Kill()
		<-done
	}
}

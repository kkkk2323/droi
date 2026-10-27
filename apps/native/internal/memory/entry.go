package memory

import (
	"fmt"
	"io"
	"os"
)

// ServeMain is the Memory Server's entry (ADR 0031): the stdio MCP Server the
// Runtime Overlay registers as droi-memory, serving the Memory folder named by
// DROI_MEMORY_DIR. It answers the process's exit code.
func ServeMain() int {
	return serveMain(os.LookupEnv, os.Stdin, os.Stdout, os.Stderr)
}

func serveMain(lookupEnv func(string) (string, bool), stdin io.Reader, stdout, stderr io.Writer) int {
	dir, _ := lookupEnv("DROI_MEMORY_DIR")
	if dir == "" {
		fmt.Fprint(stderr, "droi-memory: DROI_MEMORY_DIR is not set\n")
		return 1
	}
	sessions := SessionsDir(lookupEnv)
	transcripts := map[string]string{}
	// A miss is not cached: the Daemon may not have written the file yet.
	transcriptOf := func(sessionID string) string {
		known := transcripts[sessionID]
		if known == "" {
			known = FindSessionTranscript(sessions, sessionID)
		}
		if known != "" {
			transcripts[sessionID] = known
		}
		return known
	}
	err := Serve(stdin, stdout, ServerOptions{
		Dir: dir,
		IsMemorySession: func(sessionID string) bool {
			t := transcriptOf(sessionID)
			return t != "" && IsMemorySessionTranscript(t)
		},
		Stderr: stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "droi-memory: Error: %s\n", err)
		return 1
	}
	return 0
}

// HookMain is Memory's hook entry (ADR 0031): the Runtime Overlay runs it at
// SessionStart with the event as JSON on stdin. A hook must never break a
// Session, so it always answers 0 and a failure only goes to stderr.
func HookMain() int {
	return hookMain(os.LookupEnv, os.Stdin, os.Stdout, os.Stderr)
}

func hookMain(lookupEnv func(string) (string, bool), stdin io.Reader, stdout, stderr io.Writer) int {
	if err := runHookEntry(lookupEnv, stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "droi-memory hook: %s\n", err)
	}
	return 0
}

func runHookEntry(lookupEnv func(string) (string, bool), stdin io.Reader, stdout io.Writer) error {
	dir, _ := lookupEnv("DROI_MEMORY_DIR")
	if dir == "" {
		return nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	input, err := ParseHookInput(data)
	if err != nil {
		return err
	}
	// What the hook prints still reaches the Session when the record failed.
	out, err := RunHook(dir, input)
	if _, werr := io.WriteString(stdout, out); werr != nil && err == nil {
		err = werr
	}
	return err
}

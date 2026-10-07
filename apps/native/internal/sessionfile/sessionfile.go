// Package sessionfile is what a Client copies to point another Session at
// this one: the Session's id, or a few lines with its title, Workspace and
// transcript file. Only the Gateway knows where that file is on the computer
// (a port of sessionDetails in session-file.ts; asking the Gateway is the
// Client's own I/O).
package sessionfile

import "strings"

// Details are the lines "Copy session details" puts on the clipboard; cwd and
// file are left out when empty.
func Details(sessionID, title, cwd, file string) string {
	lines := []string{"Title: " + title, "Session ID: " + sessionID}
	if cwd != "" {
		lines = append(lines, "Workspace: "+cwd)
	}
	if file != "" {
		lines = append(lines, "Transcript: "+file)
	}
	return strings.Join(lines, "\n")
}

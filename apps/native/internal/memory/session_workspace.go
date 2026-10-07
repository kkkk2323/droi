// What a Session is, from the files the Daemon keeps for it. The Daemon starts
// the Memory Server in its own directory, not the Session's, and tells it only
// the Session id (in each tool call's `_meta`); the Session's transcript opens
// with a session_start line that carries the cwd, and its tags sit in the
// settings file beside it.

package memory

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// MemorySessionTag marks a Memory Session (ADR 0011), as
	// packages/daemon-layer/src/memory-session.ts names it.
	MemorySessionTag = "droi.memory"
	// ScratchTag marks a Session in a Scratch Workspace (ADR 0008).
	ScratchTag = "droi.scratch"
)

// SessionsDir is ~/.factory/sessions, or under FACTORY_HOME_OVERRIDE as the
// Host and the Daemon honour it. A nil lookupEnv reads the process environment.
func SessionsDir(lookupEnv func(string) (string, bool)) string {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if home, ok := lookupEnv("FACTORY_HOME_OVERRIDE"); ok {
		return filepath.Join(home, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".factory", "sessions")
}

const firstLineBytes = 64 * 1024

// ReadSessionWorkspace is the cwd on the transcript's session_start line; ""
// when there is none.
func ReadSessionWorkspace(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, firstLineBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return ""
	}
	first, _, _ := bytes.Cut(buf[:n], []byte("\n"))
	var parsed map[string]any
	if json.Unmarshal(first, &parsed) != nil || parsed["type"] != "session_start" {
		return ""
	}
	cwd, _ := parsed["cwd"].(string)
	return cwd
}

// SessionSettingsFile is the Session's settings file, which the Daemon keeps
// beside its transcript.
func SessionSettingsFile(transcriptFile string) string {
	if strings.HasSuffix(transcriptFile, ".jsonl") {
		return strings.TrimSuffix(transcriptFile, ".jsonl") + ".settings.json"
	}
	return transcriptFile
}

// hasSessionTag reads the Session's tags from its settings file; none when the
// file is absent or unreadable.
func hasSessionTag(transcriptFile, tag string) bool {
	b, err := os.ReadFile(SessionSettingsFile(transcriptFile))
	if err != nil {
		return false
	}
	var parsed map[string]any
	if json.Unmarshal(b, &parsed) != nil {
		return false
	}
	tags, _ := parsed["tags"].([]any)
	for _, t := range tags {
		if m, ok := t.(map[string]any); ok && m["name"] == tag {
			return true
		}
	}
	return false
}

// IsMemorySessionTranscript says whether the transcript belongs to a Memory
// Session, from the tags in its settings file.
func IsMemorySessionTranscript(transcriptFile string) bool {
	return hasSessionTag(transcriptFile, MemorySessionTag)
}

// IsScratchSessionTranscript says whether the transcript belongs to a Session
// in a Scratch Workspace. Such a Session has no Project Memory: its folder is
// made for it and trashed with it, so nothing saved under that path would be
// found again.
func IsScratchSessionTranscript(transcriptFile string) bool {
	return hasSessionTag(transcriptFile, ScratchTag)
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// FindSessionTranscript looks the Session's transcript up in every Workspace
// folder the Daemon keeps; "" when absent.
func FindSessionTranscript(dir, sessionID string) string {
	if !sessionIDPattern.MatchString(sessionID) {
		return ""
	}
	folders, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, folder := range folders {
		file := filepath.Join(dir, folder.Name(), sessionID+".jsonl")
		if _, err := os.Stat(file); err == nil {
			return file
		}
	}
	return ""
}

// FindSessionWorkspace is the Workspace of a Session the Daemon keeps files
// for; "" when unknown.
func FindSessionWorkspace(dir, sessionID string) string {
	if file := FindSessionTranscript(dir, sessionID); file != "" {
		return ReadSessionWorkspace(file)
	}
	return ""
}

// What a Session is, from the files the Daemon keeps for it. The Daemon starts
// the Memory Server in its own directory, not the Session's, and tells it only
// the Session id (in each tool call's `_meta`); the Session's transcript opens
// with a session_start line that carries the cwd, and its tags sit in the
// settings file beside it.

package memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// MemorySessionTag marks a Memory Session (ADR 0011), as
// packages/daemon-layer/src/memory-session.ts names it.
const MemorySessionTag = "droi.memory"

// SessionsDir is ~/.factory/sessions, or .factory/sessions in
// FACTORY_HOME_OVERRIDE, which droid takes for the home folder. A nil
// lookupEnv reads the process environment.
func SessionsDir(lookupEnv func(string) (string, bool)) string {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if home, ok := lookupEnv("FACTORY_HOME_OVERRIDE"); ok {
		return filepath.Join(home, ".factory", "sessions")
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

// ReadSessionParent is the Session the transcript's Session was handed off
// from (/handoff), as its session_start line names it; "" for none. whole is
// false when the transcript has no whole session_start line yet.
func ReadSessionParent(file string) (parent string, whole bool) {
	f, err := os.Open(file)
	if err != nil {
		return "", false
	}
	defer f.Close()
	line, err := bufio.NewReader(io.LimitReader(f, firstLineBytes)).ReadBytes('\n')
	if err != nil {
		return "", false
	}
	var start struct {
		Type   string `json:"type"`
		Parent string `json:"parent"`
	}
	if json.Unmarshal(line, &start) != nil || start.Type != "session_start" {
		return "", false
	}
	return start.Parent, true
}

// SessionSettingsFile is the Session's settings file, which the Daemon keeps
// beside its transcript.
func SessionSettingsFile(transcriptFile string) string {
	if strings.HasSuffix(transcriptFile, ".jsonl") {
		return strings.TrimSuffix(transcriptFile, ".jsonl") + ".settings.json"
	}
	return transcriptFile
}

// SessionTags are the names of the Session's tags, from its settings file;
// none when the file is absent or unreadable.
func SessionTags(transcriptFile string) []string {
	var out []string
	for _, t := range sessionTags(transcriptFile) {
		out = append(out, t.Name)
	}
	return out
}

// SessionTagMetadata is the metadata of the Session's tag name, from its
// settings file; ok is false without the tag.
func SessionTagMetadata(transcriptFile, name string) (metadata map[string]string, ok bool) {
	for _, t := range sessionTags(transcriptFile) {
		if t.Name == name {
			return t.Metadata, true
		}
	}
	return nil, false
}

type sessionTag struct {
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata"`
}

func sessionTags(transcriptFile string) []sessionTag {
	b, err := os.ReadFile(SessionSettingsFile(transcriptFile))
	if err != nil {
		return nil
	}
	var parsed struct {
		Tags []sessionTag `json:"tags"`
	}
	if json.Unmarshal(b, &parsed) != nil {
		return nil
	}
	return parsed.Tags
}

// IsMemorySessionTranscript says whether the transcript belongs to a Memory
// Session, from the tags in its settings file.
func IsMemorySessionTranscript(transcriptFile string) bool {
	return slices.Contains(SessionTags(transcriptFile), MemorySessionTag)
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

// The Memory Server's tools (ADR 0031); the protocol is pkg/mcpserver's.
// Droid reads the Memory folder with them and writes only a Memory Note,
// when the user asks it to remember, forget or correct something.

package memory

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
)

// ServerName is the Memory Server's name in the Runtime Overlay and in MCP.
const ServerName = "droi-memory"

//go:embed tools.json
var toolsJSON []byte

// Tools is the tools/list answer: names, descriptions, input schemas and
// annotations of the Memory tools.
func Tools() json.RawMessage { return json.RawMessage(strings.TrimSpace(string(toolsJSON))) }

// ServerOptions are what the Memory Server needs.
type ServerOptions struct {
	// Dir is the Memory folder.
	Dir string
	// IsMemorySession says whether the calling Session is a Memory Session,
	// which works only from what it is sent.
	IsMemorySession func(sessionID string) bool
	// Now is the time; nil is time.Now.
	Now func() time.Time
	// Stderr gets the server's own complaints; nil is os.Stderr.
	Stderr io.Writer
}

const memorySessionRefusal = "Memory tools are not available in a Memory Session. Answer from the material you were given."

func (o ServerOptions) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o ServerOptions) stderr() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

// HandleToolCall runs one tool call; sessionID is the calling Session, which
// the Daemon puts in the call's _meta.
func HandleToolCall(o ServerOptions, name string, args map[string]any, sessionID string) (mcpserver.Result, error) {
	if sessionID != "" && o.IsMemorySession != nil && o.IsMemorySession(sessionID) {
		return mcpserver.Text(memorySessionRefusal, true), nil
	}
	switch name {
	case "memory_search":
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return mcpserver.Text("memory_search needs a query.", true), nil
		}
		return mcpserver.Text(search(o.Dir, query, intArg(args, "limit", 20, 100)), false), nil
	case "memory_read":
		asked, _ := args["path"].(string)
		file, rel, err := Resolve(o.Dir, asked)
		if err != nil {
			return mcpserver.Text(err.Error(), true), nil
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if strings.HasPrefix(rel, SessionSummariesDir+"/") {
			if err := RecordRead(o.Dir, rel); err != nil {
				// Reads only keep a Session Summary from aging out; a lost one
				// must not fail the call.
				fmt.Fprintf(o.stderr(), "droi-memory: read log failed: %s\n", err)
			}
		}
		return mcpserver.Text(numbered(rel, string(b), intArg(args, "offset", 1, math.MaxInt32), intArg(args, "limit", 2_000, 2_000)), false), nil
	case "memory_list":
		where, _ := args["workspace"].(string)
		out, err := list(o.Dir, where, intArg(args, "limit", 50, 300))
		if err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Text(out, false), nil
	case "memory_note":
		text, _ := args["text"].(string)
		rel, err := WriteNote(o.Dir, text, sessionID, o.now())
		if err != nil {
			return mcpserver.Text(err.Error(), true), nil
		}
		return mcpserver.Text("Saved "+rel+". Droi folds it into the Memory Summary within a few minutes, and every new Session sees it until then.", false), nil
	}
	return mcpserver.Text("Unknown tool "+name+".", true), nil
}

func intArg(args map[string]any, key string, def, max int) int {
	n, ok := args[key].(float64)
	if !ok {
		return def
	}
	return int(math.Min(math.Max(1, math.Floor(n)), float64(max)))
}

func numbered(rel, text string, offset, limit int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if offset > len(lines) {
		return fmt.Sprintf("%s has %d lines.", rel, len(lines))
	}
	end := min(len(lines), offset-1+limit)
	var b strings.Builder
	fmt.Fprintf(&b, "%s, lines %d-%d of %d:\n", rel, offset, end, len(lines))
	for i := offset - 1; i < end; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i+1, lines[i])
	}
	return b.String()
}

var quoted = regexp.MustCompile(`"([^"]+)"|(\S+)`)

// terms are the query's words, a quoted phrase kept whole, lowercased.
func terms(query string) []string {
	var out []string
	for _, m := range quoted.FindAllStringSubmatch(query, -1) {
		t := strings.ToLower(strings.TrimSpace(m[1] + m[2]))
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

type hit struct {
	file    string
	line    int
	text    string
	matched int
	mod     time.Time
}

// search finds the lines that hold every term, newest file first; only when
// none does, the lines that hold the most of them.
func search(dir, query string, limit int) string {
	words := terms(query)
	files, _ := Files(dir)
	var hits []hit
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			lower := strings.ToLower(line)
			n := 0
			for _, w := range words {
				if strings.Contains(lower, w) {
					n++
				}
			}
			if n > 0 {
				hits = append(hits, hit{f.Path, i + 1, strings.TrimSpace(line), n, f.ModTime})
			}
		}
	}
	all := slices.DeleteFunc(slices.Clone(hits), func(h hit) bool { return h.matched < len(words) })
	if len(all) > 0 {
		hits = all
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].matched != hits[b].matched {
			return hits[a].matched > hits[b].matched
		}
		return hits[a].mod.After(hits[b].mod)
	})
	if len(hits) == 0 {
		return fmt.Sprintf("Nothing in Memory matches %q.", query)
	}
	shown := hits[:min(limit, len(hits))]
	var b strings.Builder
	kind := "every word of"
	if len(all) == 0 {
		kind = "some words of"
	}
	fmt.Fprintf(&b, "%d lines in Memory hold %s %q", len(hits), kind, query)
	if len(shown) < len(hits) {
		fmt.Fprintf(&b, ", the first %d", len(shown))
	}
	b.WriteString(" (read a file with memory_read):\n")
	for _, h := range shown {
		fmt.Fprintf(&b, "%s:%d: %s\n", h.file, h.line, clip(h.text, 300))
	}
	return b.String()
}

// list names the Memory's files: the Session Summaries newest first, with
// their Session's day, Workspace and title, then the Memory Notes.
func list(dir, workspace string, limit int) (string, error) {
	files, err := Files(dir)
	if err != nil {
		return "", err
	}
	var summaries, notes []string
	hasSummary := false
	for _, f := range slices.Backward(files) {
		switch {
		case f.Path == SummaryFile:
			hasSummary = true
		case strings.HasPrefix(f.Path, NotesDir+"/"):
			notes = append(notes, "- "+f.Path)
		default:
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
			if err != nil {
				continue
			}
			h := Header(string(b))
			if workspace != "" && !strings.Contains(strings.ToLower(h["workspace"]), strings.ToLower(workspace)) {
				continue
			}
			day, _, _ := strings.Cut(h["updated_at"], "T")
			summaries = append(summaries, fmt.Sprintf("- %s · %s · %s · %s", f.Path, day, h["workspace"], h["title"]))
		}
	}
	var b strings.Builder
	if hasSummary {
		b.WriteString(SummaryFile + ": the Memory Summary, shown at the start of every Session.\n")
	}
	if len(summaries) == 0 {
		b.WriteString("No Session Summaries")
		if workspace != "" {
			b.WriteString(" for " + workspace)
		}
		b.WriteString(".\n")
	} else {
		fmt.Fprintf(&b, "%d Session Summaries, newest first", len(summaries))
		if len(summaries) > limit {
			fmt.Fprintf(&b, ", the first %d", limit)
			summaries = summaries[:limit]
		}
		b.WriteString(":\n" + strings.Join(summaries, "\n") + "\n")
	}
	if len(notes) > 0 {
		fmt.Fprintf(&b, "%d Memory Notes:\n%s\n", len(notes), strings.Join(notes, "\n"))
	}
	return b.String(), nil
}

// Serve serves the Memory Server's MCP on the two streams until the input ends.
func Serve(in io.Reader, out io.Writer, o ServerOptions) error {
	return mcpserver.Serve(in, out, mcpserver.Server{
		Name:    ServerName,
		Title:   "Droi Memory",
		Version: "2.0.0",
		Tools:   Tools(),
		Call: func(name string, args map[string]any, sessionID string) (mcpserver.Result, error) {
			return HandleToolCall(o, name, args, sessionID)
		},
	})
}

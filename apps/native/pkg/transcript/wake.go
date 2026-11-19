package transcript

import (
	"slices"
	"strconv"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"
)

// Notice is the Daemon telling the model, on its own, that work it left
// running moved on: a background Script called notify(), or a Script or a
// command ended. Droid wakes to it and answers, though no one spoke: the
// message is for the model only, so a transcript that shows only the
// user's view has nothing where the answer starts.
type Notice struct {
	// ID is the id of the Daemon's message.
	ID   string
	Kind NoticeKind
	// Run is the Script's run, or the command's tool use.
	Run string
	// Text is what the Daemon told: the notification, or the end of the
	// command's output. "" when the message came without it, as a
	// command's end does live.
	Text      string
	ExitCode  *int
	Status    string
	CreatedAt float64
	// Summary is the tools the Script calls, or what the command said it
	// does; "" when the call that started it is not loaded.
	Summary string
	// after is the id of the last message before it that a transcript
	// shows, "" for none.
	after string
}

type NoticeKind uint8

const (
	ScriptNotified NoticeKind = iota
	ScriptEnded
	CommandEnded
)

const (
	scriptNotify     = "script:notify:"
	notificationHead = "Script notification:"
)

// noticeOf is the kind of Notice a message id names and the run it is
// about. The ids: script:notify:<session>:<run>:<tool use>,
// script-completion:<session>:<run>:<tool use>, execute-completion:<tool use>.
func noticeOf(id string) (NoticeKind, string, bool) {
	switch {
	case strings.HasPrefix(id, scriptNotify):
		return ScriptNotified, idPart(id[len(scriptNotify):], 1), true
	case strings.HasPrefix(id, scriptCompletion):
		return ScriptEnded, idPart(id[len(scriptCompletion):], 1), true
	case strings.HasPrefix(id, executeCompletion):
		return CommandEnded, id[len(executeCompletion):], true
	}
	return 0, "", false
}

func idPart(s string, i int) string {
	parts := strings.Split(s, ":")
	if i < len(parts) {
		return parts[i]
	}
	return ""
}

func readNotice(m protocol.FactoryDroidMessage, kind NoticeKind, run string) Notice {
	n := Notice{ID: m.ID, Kind: kind, Run: run, CreatedAt: m.CreatedAt}
	text := messageText(m)
	switch kind {
	case ScriptNotified:
		n.Text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), notificationHead))
	case ScriptEnded:
		n.Status = terminalStatus(text)
	case CommandEnded:
		if c := exitCode.FindStringSubmatch(text); c != nil {
			code, _ := strconv.Atoi(c[1])
			n.ExitCode = &code
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "</system-reminder>")
		for _, line := range strings.Split(text, "\n") {
			if s, ok := strings.CutPrefix(line, "summary: "); ok {
				n.Summary = firstLine(s, 120)
				break
			}
		}
		if _, out, ok := strings.Cut(text, "\noutput: "); ok {
			n.Text = commandOutput(out)
		}
	}
	return n
}

// droid ends a command's record with what it tells the model to do of it.
var outputEnds = []string{"\n\nThis is a new asynchronous", "\n\nThis background process was intentionally stopped"}

// commandOutput is the output a command's end record quotes, without what
// droid tells the model after it, each line as a terminal leaves it after
// its carriage returns (a progress bar's last state).
func commandOutput(out string) string {
	for _, end := range outputEnds {
		if i := strings.Index(out, end); i >= 0 {
			out = out[:i]
		}
	}
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		l = strings.TrimRight(l, "\r")
		if j := strings.LastIndexByte(l, '\r'); j >= 0 {
			l = l[j+1:]
		}
		lines[i] = l
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// NoticeReader reads a Session's Notices again and again as its messages
// change, reading each Notice once.
type NoticeReader struct {
	seen map[string]*seenNotice
	gen  uint64
	out  []Notice
}

type seenNotice struct {
	gen    uint64
	blocks []rawRef
	n      Notice
}

// Read is the Notices among messages, in their order. messages are the
// Session's stored ones, unfiltered (Session.ReadMessages): a transcript's
// own leave the Notices out. What it returns is good until the next Read.
func (nr *NoticeReader) Read(messages []protocol.FactoryDroidMessage) []Notice {
	nr.gen++
	nr.out = nr.out[:0]
	for i, m := range messages {
		kind, run, ok := noticeOf(m.ID)
		if !ok {
			continue
		}
		if nr.seen == nil {
			nr.seen = map[string]*seenNotice{}
		}
		s := nr.seen[m.ID]
		if s == nil || !sameBlocks(s.blocks, m.Content) {
			s = &seenNotice{blocks: make([]rawRef, len(m.Content)), n: readNotice(m, kind, run)}
			for j, b := range m.Content {
				s.blocks[j] = refOf(b)
			}
			s.n.after = shownBefore(messages, i)
			nr.seen[m.ID] = s
		}
		s.gen = nr.gen
		nr.out = append(nr.out, s.n)
	}
	for id, s := range nr.seen {
		if s.gen != nr.gen {
			delete(nr.seen, id)
		}
	}
	return nr.out
}

// shownBefore is the id of the last message before messages[i] that a
// transcript shows.
func shownBefore(messages []protocol.FactoryDroidMessage, i int) string {
	for j := i - 1; j >= 0; j-- {
		if _, _, ok := noticeOf(messages[j].ID); ok {
			continue
		}
		if len(session.FilterMessagesForUI(messages[j:j+1])) > 0 {
			return messages[j].ID
		}
	}
	return ""
}

// IsWake is whether e is what woke the Session while no one spoke to it:
// a user entry of one Wake block.
func IsWake(e *Entry) bool {
	return e.User && len(e.Blocks) == 1 && e.Blocks[0].Kind == Wake
}

// WakeLabel is what a wake's row says woke the Session, and what the work
// does: the tools a Script calls, or a command's summary.
func WakeLabel(b Block) (label, detail string) {
	if len(b.Notices) == 0 {
		return l10n.L("Background Script ran"), callNames(b.Calls)
	}
	n := b.Notices[len(b.Notices)-1]
	detail = n.Summary
	if detail == "" && n.Kind != CommandEnded {
		detail = callNames(b.Calls)
	}
	// Notices that came one after another without a word between share a row.
	if more := len(b.Notices) - 1; more > 0 {
		if detail == "" {
			detail = l10n.L("%d more", more)
		} else {
			detail = l10n.L("%s · %d more", detail, more)
		}
	}
	switch n.Kind {
	case ScriptNotified:
		return l10n.L("Background Script sent a notification"), detail
	case ScriptEnded:
		switch n.Status {
		case "failed":
			return l10n.L("Background Script failed"), detail
		case "cancelled":
			return l10n.L("Background Script was cancelled"), detail
		}
		return l10n.L("Background Script finished"), detail
	}
	if n.ExitCode != nil && *n.ExitCode != 0 {
		return l10n.L("Background command exited with code %d", *n.ExitCode), detail
	}
	return l10n.L("Background command finished"), detail
}

// WakeFailed is whether the work that woke the Session ended badly.
func WakeFailed(b Block) bool {
	if len(b.Notices) == 0 {
		return false
	}
	n := b.Notices[len(b.Notices)-1]
	return n.ExitCode != nil && *n.ExitCode != 0 || n.Status == "failed" || n.Status == "cancelled"
}

func callNames(calls []*ToolCall) string {
	var names []string
	for _, c := range calls {
		if !slices.Contains(names, c.Use.Name) {
			names = append(names, c.Use.Name)
		}
	}
	return strings.Join(names, " · ")
}

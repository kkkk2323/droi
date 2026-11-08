package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// The session list on Kernel: what the sidebar lists, and how it follows Kernel, once the window has a Kernel.

// kernelHarness is the window over sessionsScenario with its Sessions on a Fake Kernel.
func kernelHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, sessionsScenario(), "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	return h
}

// testConn is a connection of the test's own, as another window would be.
func (h *harness) testConn() *kernel.Client {
	h.t.Helper()
	if h.kernel.reader == nil {
		h.kernel.reader = h.kernel.k.Dial(h.t)
	}
	return h.kernel.reader
}

func (h *harness) listedIDs() []string {
	var ids []string
	for _, s := range h.a.listedNow() {
		ids = append(ids, s.SessionID)
	}
	return ids
}

// TestTheListComesFromKernel: once the window has a Kernel, the sidebar lists conversation.list's rows, by the
// Sessions' own ids, and the Daemon is not asked for its list.
func TestTheListComesFromKernel(t *testing.T) {
	h := kernelHarness(t)
	ids := h.listedIDs()
	for _, s := range h.d.Sessions {
		if !slices.Contains(ids, s.SessionID) {
			t.Errorf("%q (%s) is not listed: %v", s.Title, s.SessionID, ids)
		}
	}
	if n := h.count("daemon.list_available_sessions"); n != 0 {
		t.Errorf("the Daemon was asked for its list %d times", n)
	}
}

// TestTheListReadsEveryPage: Kernel answers the list a page at a time; the sidebar reads them all, each row once.
func TestTheListReadsEveryPage(t *testing.T) {
	h := kernelHarness(t)
	c := h.testConn()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const extra = 2*sessionPage + 30
	for i := range extra {
		res, err := c.Create(ctx, kernel.CreateParams{Cwd: h.kernel.k.Workspace, Kind: "chat"})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: res.ConversationID, Title: kernel.TitleText("Extra")}); err != nil {
			t.Fatal(err)
		}
	}
	h.a.reloadList()
	want := len(h.d.Sessions) + extra
	h.until("every page", func() bool { return len(h.a.listedNow()) == want })
	ids := h.listedIDs()
	slices.Sort(ids)
	if len(slices.Compact(ids)) != want {
		t.Fatalf("the list holds a row twice: %d rows, %d distinct", want, len(slices.Compact(ids)))
	}
}

// TestARowFollowsKernel: a title or archive another window sets reaches the row through index.change, without the
// list being read again; an archived Session leaves the list.
func TestARowFollowsKernel(t *testing.T) {
	h := kernelHarness(t)
	c := h.testConn()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conv, _ := h.kernel.id(h.d.Sessions[1].SessionID)
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: conv, Title: kernel.TitleText("Dark mode, everywhere")}); err != nil {
		t.Fatal(err)
	}
	h.until("the new title", func() bool { return h.hasText("Dark mode, everywhere") })
	if h.hasText("Add dark mode toggle") {
		t.Error("the old title is still shown")
	}
	archived := true
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: conv, Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	h.until("the archived row to leave", func() bool { return !h.hasText("Dark mode, everywhere") })
}

// TestAnAutomationRunIsNotASessionOfAWorkspace: a run the Host's Automations started is a conversation of kind
// automation; the sidebar keeps it out of the Workspaces, as it does a Daemon run, and knows its Automation by name.
func TestAnAutomationRunIsNotASessionOfAWorkspace(t *testing.T) {
	h := kernelHarness(t)
	c := h.testConn()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := c.Create(ctx, kernel.CreateParams{Cwd: h.kernel.k.Workspace, Kind: "automation"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: res.ConversationID, Title: kernel.TitleText("[Automation] Nightly lint")}); err != nil {
		t.Fatal(err)
	}
	h.a.reloadList()
	id := h.a.sessionIDOf(res.ConversationID)
	var run sessions.Summary
	h.until("the run in the list", func() bool {
		for _, s := range h.a.listedNow() {
			if s.SessionID == id {
				run = s
				return true
			}
		}
		return false
	})
	if aid, name := sessions.AutomationRun(run.Tags); aid == "" || name != "Nightly lint" {
		t.Fatalf("the run reads as Automation %q named %q", aid, name)
	}
	if h.hasText("[Automation] Nightly lint") {
		t.Error("the run is listed among the Workspaces' Sessions")
	}
}

// TestASessionStartedElsewhereIsListed: a conversation another window makes reaches the list when Kernel says so.
func TestASessionStartedElsewhereIsListed(t *testing.T) {
	h := kernelHarness(t)
	c := h.testConn()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := c.Create(ctx, kernel.CreateParams{Cwd: h.kernel.k.Workspace, Kind: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: res.ConversationID, Title: kernel.TitleText("Made elsewhere")}); err != nil {
		t.Fatal(err)
	}
	h.until("the new row", func() bool { return findSummary(h.a.listedNow(), res.ConversationID.String()) != nil })
}

// TestANewSessionStartsOnKernel: the New session page makes a Kernel conversation in the Workspace, with a model
// Kernel offers, and sends it the first message; the sidebar lists it and the sidebar shows it working.
func TestANewSessionStartsOnKernel(t *testing.T) {
	h := kernelHarness(t)
	dir := t.TempDir()
	h.openStartPage()
	h.a.newPage.pick, h.a.newPage.picked = dir, true
	h.frame()
	h.start("Write the release notes")
	h.sentText("Write the release notes")
	var conv kernel.ID
	h.until("the conversation", func() bool {
		for _, s := range h.a.listedNow() {
			if id, ok := h.a.kernelID(s.SessionID); ok && s.Cwd == dir {
				conv = id
				return true
			}
		}
		return false
	})
	if h.a.Route().SessionID != conv.String() {
		t.Errorf("the window shows %q, want the new conversation %d", h.a.Route().SessionID, conv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opened, err := h.testConn().Open(ctx, kernel.OpenParams{ID: conv, Window: &fakekernel.DefaultWindow})
	if err != nil {
		t.Fatal(err)
	}
	var agent kernel.Agent
	if err := json.Unmarshal(opened.View.Doc("pi.agent"), &agent); err != nil || agent.Model == nil || agent.Model.ModelID != fakekernel.ModelID {
		t.Errorf("the conversation runs %s (%v), want Kernel's model", opened.View.Doc("pi.agent"), err)
	}
	// The page's draft is the Daemon's, for its commands and skills; nothing else is.
	reqs, _ := h.d.Requests()
	for _, r := range reqs {
		if r.Method == "daemon.initialize_session" && !strings.Contains(string(r.Params), sessions.DraftTag) {
			t.Errorf("the Daemon was asked for a Session: %s", r.Params)
		}
	}
}

// TestANewSessionNeedsAUsableDirectory: a Workspace that is not a directory on this computer is said, and nothing
// is made.
func TestANewSessionNeedsAUsableDirectory(t *testing.T) {
	h := kernelHarness(t)
	h.openStartPage()
	h.a.newPage.pick, h.a.newPage.picked = "/no/such/place", true
	h.frame()
	h.start("Hello")
	h.until("the error", func() bool { return h.hasText("/no/such/place is not a usable directory.") })
}

// TestARefreshKeepsArchivedRowsShown: with archived Sessions shown, every read of the list has them, however
// recent: a refresh (a new Session, a busy row the list did not have) does not drop them.
func TestARefreshKeepsArchivedRowsShown(t *testing.T) {
	h := kernelHarness(t)
	c := h.testConn()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := c.Create(ctx, kernel.CreateParams{Cwd: h.kernel.k.Workspace, Kind: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	archived := true
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: res.ConversationID, Title: kernel.TitleText("Recent archive"), Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	prefs.ShowArchived.Set(h.a.prefs, true)
	h.a.reloadList()
	id := res.ConversationID.String()
	// A read Kernel's own index.changed started may land after this one, and in its place.
	h.until("the archived conversation listed", func() bool { return findSummary(h.a.listedNow(), id) != nil })
	h.a.refreshList()
	if findSummary(h.a.listedNow(), id) == nil {
		t.Fatal("a refresh dropped a recent archived row")
	}
}

// TestAnUnarchivedRowComesBack: a row archived elsewhere leaves the list, and comes back when it is unarchived,
// also one the window first heard of from Kernel, and one whose last activity puts it beyond the first page.
func TestAnUnarchivedRowComesBack(t *testing.T) {
	h := kernelHarness(t)
	c := h.testConn()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	yes, no := true, false

	res, err := c.Create(ctx, kernel.CreateParams{Cwd: h.kernel.k.Workspace, Kind: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: res.ConversationID, Title: kernel.TitleText("Made elsewhere")}); err != nil {
		t.Fatal(err)
	}
	id := res.ConversationID.String()
	h.until("the new row", func() bool { return findSummary(h.a.listedNow(), id) != nil })
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: res.ConversationID, Archived: &yes}); err != nil {
		t.Fatal(err)
	}
	h.until("the archived row to leave", func() bool { return findSummary(h.a.listedNow(), id) == nil })
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: res.ConversationID, Archived: &no}); err != nil {
		t.Fatal(err)
	}
	h.until("the unarchived row", func() bool { return findSummary(h.a.listedNow(), id) != nil })

	old := time.Now().Add(-48 * time.Hour).UnixMilli()
	imported, err := c.Import(ctx, kernel.ImportParams{Cwd: h.kernel.k.Workspace, Kind: "chat", Title: "An old archive",
		CreatedAt: &old, Archived: &yes, Entries: []kernel.ImportEntry{}})
	if err != nil {
		t.Fatal(err)
	}
	for range kernelPage + 1 {
		if _, err := c.Create(ctx, kernel.CreateParams{Cwd: h.kernel.k.Workspace, Kind: "chat"}); err != nil {
			t.Fatal(err)
		}
	}
	h.a.reloadList()
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: imported.ConversationID, Archived: &no}); err != nil {
		t.Fatal(err)
	}
	h.until("the old unarchived row", func() bool { return findSummary(h.a.listedNow(), imported.ConversationID.String()) != nil })
}

// TestAnIndexChangeOutlivesARead: a read of the list answers with rows from before an index change that arrived
// while it was out; the change is applied to them again, so an archived row does not come back.
func TestAnIndexChangeOutlivesARead(t *testing.T) {
	h := kernelHarness(t)
	row := *findSummary(h.a.listedNow(), h.d.Sessions[0].SessionID)
	conv, _ := h.a.kernelID(row.SessionID)
	before := h.a.listedNow()
	gen := h.a.startList()
	archived := true
	h.a.applyIndexChanged(kernel.IndexChanged{ID: conv, Archived: &archived, LastActivityAt: row.UpdatedAt * 1000})
	h.a.setList(gen, slices.Clone(before), 0, false, nil)
	if findSummary(h.a.listedNow(), row.SessionID) != nil {
		t.Fatal("a read from before the archive brought the row back")
	}
}

// TestARowHeardOfDuringTheFirstReadIsListed: an index change of a conversation the first read of the list does not
// have, which arrives while that read is out, has the list read again for it once the read lands.
func TestARowHeardOfDuringTheFirstReadIsListed(t *testing.T) {
	h := kernelHarness(t)
	row := *findSummary(h.a.listedNow(), h.d.Sessions[0].SessionID)
	conv, _ := h.a.kernelID(row.SessionID)
	without := slices.DeleteFunc(slices.Clone(h.a.listedNow()), func(s sessions.Summary) bool { return s.SessionID == row.SessionID })
	h.a.mu.Lock()
	h.a.listed, h.a.listDone = without, false
	h.a.listRev++
	h.a.mu.Unlock()
	gen := h.a.startList()
	title := "Heard of early"
	h.a.applyIndexChanged(kernel.IndexChanged{ID: conv, Title: &title, LastActivityAt: row.UpdatedAt * 1000})
	h.a.setList(gen, slices.Clone(without), 0, false, nil)
	h.until("the row read again", func() bool { return findSummary(h.a.listedNow(), row.SessionID) != nil })
}

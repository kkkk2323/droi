package archive

import (
	"context"
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

type calls struct{ log []string }

func (c *calls) ArchiveSession(_ context.Context, p protocol.ArchiveSessionParams) (*protocol.ArchiveSessionResult, error) {
	c.log = append(c.log, "archive "+p.SessionID)
	return nil, nil
}

func (c *calls) UnarchiveSession(_ context.Context, p protocol.UnarchiveSessionParams) (*protocol.UnarchiveSessionResult, error) {
	c.log = append(c.log, "unarchive "+p.SessionID)
	return nil, nil
}

func (c *calls) Trash(_ context.Context, path string) error {
	c.log = append(c.log, "trash "+path)
	return nil
}

func (c *calls) Restore(_ context.Context, path string) error {
	c.log = append(c.log, "restore "+path)
	return nil
}

func TestAWorkspaceSessionIsArchivedAlone(t *testing.T) {
	var c calls
	s := sessions.Summary{SessionID: "a", Cwd: "/work/app"}
	listed := []sessions.Summary{s, {SessionID: "parent"}}
	if err := Conversation(context.Background(), &c, &c, listed, s); err != nil {
		t.Fatal(err)
	}
	if err := Unarchive(context.Background(), &c, &c, listed, s); err != nil {
		t.Fatal(err)
	}
	if want := []string{"archive a", "unarchive a"}; !reflect.DeepEqual(c.log, want) {
		t.Errorf("log = %v", c.log)
	}
}

func TestAScratchConversationGoesWithItsChainAndFolder(t *testing.T) {
	var c calls
	scratch := sessions.Summary{SessionID: "child", ParentID: "parent", Cwd: "/s/2026-01-02-abc123"}
	listed := []sessions.Summary{
		scratch,
		{SessionID: "parent", ParentID: "root", Cwd: scratch.Cwd},
		{SessionID: "root", Cwd: scratch.Cwd, ArchivedAt: "2026-01-01"},
	}
	if err := Conversation(context.Background(), &c, &c, listed, scratch); err != nil {
		t.Fatal(err)
	}
	if want := []string{"archive child", "archive parent", "trash /s/2026-01-02-abc123"}; !reflect.DeepEqual(c.log, want) {
		t.Errorf("archive log = %v", c.log)
	}

	c.log = nil
	if err := Unarchive(context.Background(), &c, &c, listed, scratch); err != nil {
		t.Fatal(err)
	}
	if want := []string{"restore /s/2026-01-02-abc123", "unarchive child", "unarchive parent", "unarchive root"}; !reflect.DeepEqual(c.log, want) {
		t.Errorf("unarchive log = %v", c.log)
	}
}

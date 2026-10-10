package app

import (
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

func TestTitleHitsMatchTheTitleNewestFirst(t *testing.T) {
	listed := []sessions.Summary{
		{SessionID: "old", Title: "空调遥控", UpdatedAt: 10},
		{SessionID: "new", Title: "微信小程序空调控制转网页版", UpdatedAt: 20},
		{SessionID: "other", Title: "Fix the login race", UpdatedAt: 30},
		{SessionID: "sub", Title: "Worker: 空调", UpdatedAt: 40, CallingSessionID: "new"},
		{SessionID: "case", Title: "LOGIN page", UpdatedAt: 5},
	}
	var got []string
	for _, h := range titleHits(listed, "空调") {
		got = append(got, h.sessionID)
	}
	if len(got) != 2 || got[0] != "new" || got[1] != "old" {
		t.Fatalf("title hits %v", got)
	}
	if hits := titleHits(listed, "login"); len(hits) != 2 {
		t.Fatalf("case-insensitive title hits %+v", hits)
	}
}

func TestMergeHitsKeepsTheFirstAndTakesSnippets(t *testing.T) {
	snippet := []snippetRun{{text: "空调", match: true}}
	got := mergeHits([]searchHit{{sessionID: "a"}}, []searchHit{{sessionID: "b"}, {sessionID: "a", snippet: snippet}})
	if len(got) != 2 || got[0].sessionID != "a" || len(got[0].snippet) != 1 || got[1].sessionID != "b" {
		t.Fatalf("merged %+v", got)
	}
	var many []searchHit
	for i := range searchLimit + 5 {
		many = append(many, searchHit{sessionID: string(rune('A' + i))})
	}
	if got := mergeHits(nil, many); len(got) != searchLimit {
		t.Fatalf("merged %d hits, want %d", len(got), searchLimit)
	}
}

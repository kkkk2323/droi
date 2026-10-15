package app

import (
	"slices"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/subagents"
)

// knownSubagents is the frame's list as every view reads it: the Daemon's
// list with the subagents the Store already knows of. The Daemon announces a
// subagent (child_session_available) before its Session is on disk, so the
// list may lack it until the run ends, or carry it without its caller. Every
// view (Task cards, the header's menu and trail, the sidebar) reads this one
// list, so they cannot tell different stories. rev is bumped when the
// subagents added change, for what is built from the list.
func (a *App) knownSubagents(listed []sessions.Summary, rev uint64) ([]sessions.Summary, uint64) {
	if a.ctl == nil {
		return listed, rev
	}
	store := a.ctl.Store()
	var sig []string
	var extra []sessions.Summary
	for parent, children := range store.SubagentSessionIDsByParent() {
		for toolUse, id := range children {
			if i := slices.IndexFunc(listed, func(s sessions.Summary) bool { return s.SessionID == id }); i >= 0 {
				if listed[i].CallingSessionID == "" {
					listed[i].CallingSessionID, listed[i].CallingToolUseID = parent, toolUse
					sig = append(sig, "caller:"+id)
				}
				continue
			}
			s := sessions.Summary{SessionID: id, CallingSessionID: parent, CallingToolUseID: toolUse, UpdatedAt: a.subagentSeen(id)}
			if h := store.Session(id); h != nil {
				s.Title, s.Cwd = h.Title(), h.Cwd()
			}
			if sum, ok := store.SubagentInvocationSummary(id); ok && s.Title == "" && sum.Description != "" {
				s.Title = subagents.SubagentName(sum.SubagentType) + ": " + sum.Description
			}
			extra = append(extra, s)
			sig = append(sig, "added:"+id)
		}
	}
	slices.Sort(sig)
	if key := strings.Join(sig, ","); key != a.subagentKey {
		a.subagentKey = key
		a.subagentRev++
	}
	return append(listed, extra...), rev + a.subagentRev
}

// subagentSeen is when this window first heard of a subagent the list does
// not have, as its row's time.
func (a *App) subagentSeen(id string) int64 {
	if a.subagentFirstSeen == nil {
		a.subagentFirstSeen = map[string]int64{}
	}
	if t, ok := a.subagentFirstSeen[id]; ok {
		return t
	}
	t := a.cfg.Now().Unix()
	a.subagentFirstSeen[id] = t
	return t
}

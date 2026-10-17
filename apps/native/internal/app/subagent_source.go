package app

import (
	"cmp"
	"slices"
	"strconv"
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
// subagents added change, for what is built from the list. listed is the
// snapshot's, shared with other frames: it is copied before a write. The
// list is built again only when the snapshot or the Store's subagents
// changed, and is the same slice until then, which the views key their
// own memos on.
func (a *App) knownSubagents(listed []sessions.Summary, rev uint64) ([]sessions.Summary, uint64) {
	if a.ctl == nil {
		return listed, rev
	}
	store := a.ctl.Store()
	type child struct{ parent, toolUse, id string }
	var children []child
	for parent, byTool := range store.SubagentSessionIDsByParent() {
		for toolUse, id := range byTool {
			children = append(children, child{parent, toolUse, id})
		}
	}
	slices.SortFunc(children, func(x, y child) int {
		return cmp.Or(cmp.Compare(x.id, y.id), cmp.Compare(x.parent, y.parent), cmp.Compare(x.toolUse, y.toolUse))
	})
	titles, cwds := make([]string, len(children)), make([]string, len(children))
	var key strings.Builder
	key.WriteString(strconv.FormatUint(rev, 10))
	for i, ch := range children {
		if h := store.Session(ch.id); h != nil {
			titles[i], cwds[i] = h.Title(), h.Cwd()
		}
		if sum, ok := store.SubagentInvocationSummary(ch.id); ok && titles[i] == "" && sum.Description != "" {
			titles[i] = subagents.SubagentName(sum.SubagentType) + ": " + sum.Description
		}
		key.WriteString("|" + ch.parent + "/" + ch.toolUse + "/" + ch.id + "/" + titles[i] + "/" + cwds[i])
	}
	if m := &a.known; m.ok && m.key == key.String() {
		return m.list, rev + a.subagentRev
	}

	at := make(map[string]int, len(children))
	for i, s := range listed {
		at[s.SessionID] = i
	}
	var sig []string
	owned := false
	var extra []sessions.Summary
	for i, ch := range children {
		if j, ok := at[ch.id]; ok {
			if listed[j].CallingSessionID == "" {
				if !owned {
					listed, owned = slices.Clone(listed), true
				}
				listed[j].CallingSessionID, listed[j].CallingToolUseID = ch.parent, ch.toolUse
				sig = append(sig, "caller:"+ch.id)
			}
			continue
		}
		extra = append(extra, sessions.Summary{SessionID: ch.id, CallingSessionID: ch.parent, CallingToolUseID: ch.toolUse,
			UpdatedAt: a.subagentSeen(ch.id), Title: titles[i], Cwd: cwds[i]})
		sig = append(sig, "added:"+ch.id)
	}
	slices.Sort(sig)
	if k := strings.Join(sig, ","); k != a.subagentKey {
		a.subagentKey = k
		a.subagentRev++
	}
	if len(extra) > 0 {
		listed = append(listed, extra...)
	}
	a.known = knownList{key: key.String(), list: listed, ok: true}
	return listed, rev + a.subagentRev
}

// knownList is the list knownSubagents last built, and what from.
type knownList struct {
	key  string
	list []sessions.Summary
	ok   bool
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

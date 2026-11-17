package app

import (
	"cmp"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

const prRefreshInterval = 15 * time.Second

// workstreamPR is one pull request associated with a Workstream agent.
type workstreamPR struct {
	URL        string
	Number     int
	Title      string
	State      string // "open", "draft", "merged", "closed"
	SessionID  string
	AgentTitle string
	Branch     string
}

type prCache struct {
	wsID    string
	sig     string
	at      time.Time
	list    []workstreamPR
	byAgent map[string][]workstreamPR
	have    bool
	reading bool
}

var prURLRe = regexp.MustCompile(`https?://[^\s"<>)\]\\]+/(?:pull|merge_requests)/(\d+)`)

func normalizePRURL(raw string) string {
	m := prURLRe.FindString(raw)
	if m == "" {
		return ""
	}
	u, err := url.Parse(m)
	if err != nil {
		return m
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/")
}

func prNumberFromURL(raw string) int {
	m := prURLRe.FindStringSubmatch(raw)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func extractPRURLs(text string) []string {
	if text == "" {
		return nil
	}
	matches := prURLRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	var out []string
	for _, m := range matches {
		if norm := normalizePRURL(m); norm != "" && !slices.Contains(out, norm) {
			out = append(out, norm)
		}
	}
	return out
}

func (a *App) sessionPRURLs(id string) []string {
	s := a.sessionOf(id)
	if s == nil {
		return nil
	}
	var seen []string
	add := func(text string) {
		for _, u := range extractPRURLs(text) {
			if !slices.Contains(seen, u) {
				seen = append(seen, u)
			}
		}
	}
	s.ReadMessages(func(msgs []protocol.FactoryDroidMessage) {
		for _, m := range msgs {
			for _, b := range m.Content {
				switch b.Type {
				case protocol.ContentBlockTypeText:
					var tb struct {
						Text string `json:"text"`
					}
					if json.Unmarshal(b.Raw, &tb) == nil {
						add(tb.Text)
					}
				case protocol.ContentBlockTypeToolResult:
					add(string(b.Raw))
				}
			}
		}
	})
	return seen
}

type prQueryPlan struct {
	sig      string
	subjects []protocol.DaemonPullRequestSubject
	fallback []workstreamPR
	agents   map[string]prAgentMeta
}

type prAgentMeta struct {
	tab        string
	resolvedID string
	title      string
	branch     string
}

func (a *App) buildPRQueryPlan(ctx *wsContext) prQueryPlan {
	plan := prQueryPlan{agents: map[string]prAgentMeta{}}
	if ctx.w == nil {
		return plan
	}
	var sigParts []string
	sigParts = append(sigParts, ctx.id)
	seenURL := map[string]bool{}
	for _, ag := range ctx.w.Agents {
		sid := a.resolve(ag.SessionID)
		title := trimOr(ag.Title, L("Agent"))
		cwd := cmp.Or(ag.WorktreePath, ctx.w.Workspace)
		meta := prAgentMeta{tab: ag.SessionID, resolvedID: sid, title: title, branch: ag.Branch}
		plan.agents[ag.SessionID] = meta
		plan.agents[sid] = meta
		if ag.Branch != "" || cwd != "" {
			sub, err := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectBranch{
				Kind:      protocol.DaemonPullRequestSubjectKindBranch,
				SessionID: sid,
				Cwd:       cwd,
			})
			if err == nil {
				plan.subjects = append(plan.subjects, sub)
				sigParts = append(sigParts, "b:"+sid+":"+ag.Branch)
			}
		}
		var urls []string
		if ag.LastReport != nil {
			for _, u := range extractPRURLs(ag.LastReport.Text) {
				if !slices.Contains(urls, u) {
					urls = append(urls, u)
				}
			}
		}
		for _, u := range a.sessionPRURLs(sid) {
			if !slices.Contains(urls, u) {
				urls = append(urls, u)
			}
		}
		for _, u := range urls {
			if seenURL[u] {
				continue
			}
			seenURL[u] = true
			plan.fallback = append(plan.fallback, workstreamPR{
				URL:        u,
				Number:     prNumberFromURL(u),
				State:      "open",
				SessionID:  ag.SessionID,
				AgentTitle: title,
				Branch:     ag.Branch,
			})
			sub, err := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectURL{
				Kind:      protocol.DaemonPullRequestSubjectKindURL,
				SessionID: sid,
				Cwd:       cwd,
				URL:       u,
			})
			if err == nil {
				plan.subjects = append(plan.subjects, sub)
				sigParts = append(sigParts, "u:"+sid+":"+u)
			}
		}
	}
	plan.sig = strings.Join(sigParts, "|")
	return plan
}

func (a *App) loadWorkstreamPRs(ctx *wsContext, force bool) {
	if ctx.w == nil {
		return
	}
	pc := &a.workstreams.prs
	plan := a.buildPRQueryPlan(ctx)
	now := a.cfg.Now()
	stale := !pc.have || pc.wsID != ctx.id || pc.sig != plan.sig || now.Sub(pc.at) >= prRefreshInterval
	if !force && !stale {
		return
	}
	if len(plan.subjects) == 0 {
		pc.have, pc.wsID, pc.sig, pc.at = true, ctx.id, plan.sig, now
		pc.list, pc.byAgent = nil, nil
		return
	}
	if pc.reading {
		return
	}
	resolve := a.workstreams.resolvePRs
	if resolve == nil && a.ctl == nil {
		list, byAgent := mergeWorkstreamPRs(nil, plan.fallback, plan.agents)
		pc.have, pc.wsID, pc.sig, pc.at = true, ctx.id, plan.sig, now
		pc.list, pc.byAgent = list, byAgent
		return
	}
	pc.reading = true
	go func() {
		var res *protocol.ResolvePullRequestStatusesResult
		var err error
		reason := protocol.DaemonPullRequestLookupReasonBranchResolved
		if force {
			reason = protocol.DaemonPullRequestLookupReasonManualRefresh
		}
		lookups := make([]protocol.DaemonPullRequestLookup, 0, len(plan.subjects))
		for _, sub := range plan.subjects {
			lookup := protocol.DaemonPullRequestLookup{
				Subject: sub,
				Reason:  reason,
			}
			if force {
				inv := true
				lookup.Invalidate = &inv
			}
			lookups = append(lookups, lookup)
		}
		params := protocol.ResolvePullRequestStatusesParams{Lookups: lookups}
		if resolve != nil {
			res, err = resolve(a.ctx, params)
		} else {
			cl, cerr := a.ctl.Client()
			if cerr != nil {
				err = cerr
			} else {
				res, err = cl.ResolvePullRequestStatuses(a.ctx, params)
			}
		}
		var resolved []protocol.DaemonPullRequestStatusResult
		if err == nil && res != nil {
			resolved = res.Statuses
		}
		list, byAgent := mergeWorkstreamPRs(resolved, plan.fallback, plan.agents)
		a.cfg.Update(func() {
			pc.reading, pc.have = false, true
			pc.wsID, pc.sig, pc.at = ctx.id, plan.sig, a.cfg.Now()
			pc.list, pc.byAgent = list, byAgent
		})
	}()
}

func mergeWorkstreamPRs(results []protocol.DaemonPullRequestStatusResult, fallback []workstreamPR, agents map[string]prAgentMeta) ([]workstreamPR, map[string][]workstreamPR) {
	byURL := map[string]workstreamPR{}
	var order []string
	upsert := func(pr workstreamPR) {
		key := normalizePRURL(pr.URL)
		if key == "" {
			return
		}
		pr.URL = key
		if pr.Number == 0 {
			pr.Number = prNumberFromURL(key)
		}
		if prev, ok := byURL[key]; ok {
			if pr.Title == "" {
				pr.Title = prev.Title
			}
			if pr.SessionID == "" {
				pr.SessionID = prev.SessionID
			}
			if pr.AgentTitle == "" {
				pr.AgentTitle = prev.AgentTitle
			}
			if pr.Branch == "" {
				pr.Branch = prev.Branch
			}
			if pr.State == "" {
				pr.State = prev.State
			}
			byURL[key] = pr
			return
		}
		byURL[key] = pr
		order = append(order, key)
	}
	for _, r := range results {
		pr, ok := prFromResult(r, agents)
		if ok {
			upsert(pr)
		}
	}
	for _, fb := range fallback {
		if _, ok := byURL[normalizePRURL(fb.URL)]; !ok {
			upsert(fb)
		}
	}
	list := make([]workstreamPR, 0, len(order))
	byAgent := map[string][]workstreamPR{}
	for _, key := range order {
		pr := byURL[key]
		list = append(list, pr)
		if pr.SessionID != "" {
			byAgent[pr.SessionID] = append(byAgent[pr.SessionID], pr)
		}
	}
	slices.SortStableFunc(list, func(x, y workstreamPR) int { return cmp.Compare(y.Number, x.Number) })
	return list, byAgent
}

func prFromResult(r protocol.DaemonPullRequestStatusResult, agents map[string]prAgentMeta) (workstreamPR, bool) {
	var sid, subjURL string
	if sv, err := r.Subject.Value(); err == nil {
		switch s := sv.(type) {
		case *protocol.DaemonPullRequestSubjectBranch:
			sid = s.SessionID
		case *protocol.DaemonPullRequestSubjectURL:
			sid, subjURL = s.SessionID, s.URL
		}
	}
	meta := agents[sid]
	pr := workstreamPR{
		SessionID:  cmp.Or(meta.tab, sid),
		AgentTitle: meta.title,
		Branch:     cmp.Or(r.Branch, meta.branch),
	}
	if r.Record != nil {
		st := string(r.Record.State)
		if st == string(protocol.DaemonPullRequestStateOpen) ||
			st == string(protocol.DaemonPullRequestStateDraft) ||
			st == string(protocol.DaemonPullRequestStateMerged) ||
			st == string(protocol.DaemonPullRequestStateClosed) {
			pr.State = st
			pr.URL = cmp.Or(r.Record.URL, subjURL)
			pr.Title = r.Record.Title
			if r.Record.Number != nil {
				pr.Number = int(*r.Record.Number)
			}
		}
	}
	if pr.URL == "" && r.Status != nil {
		if sv, err := r.Status.Value(); err == nil {
			if open, ok := sv.(*protocol.DaemonPullRequestStatusOpen); ok {
				pr.URL = cmp.Or(open.URL, subjURL)
				pr.Title = cmp.Or(pr.Title, open.Title)
				if pr.State == "" {
					pr.State = "open"
				}
			}
		}
	}
	if pr.URL == "" {
		return workstreamPR{}, false
	}
	if pr.State == "" {
		pr.State = "open"
	}
	return pr, true
}

func (a *App) agentPRs(tab, resolvedID string) []workstreamPR {
	byAgent := a.workstreams.prs.byAgent
	if len(byAgent) == 0 {
		return nil
	}
	if prs := byAgent[tab]; len(prs) > 0 {
		return prs
	}
	if resolvedID != "" && resolvedID != tab {
		return byAgent[resolvedID]
	}
	return nil
}

func prStatusVisual(t *theme.Tokens, state string) (icon string, color ui.Color, label string) {
	switch state {
	case "draft":
		return "git-pull-request", t.MutedForeground, L("Draft")
	case "merged":
		return "circle-check", t.Info, L("Merged")
	case "closed":
		return "circle-x", t.DestructiveForeground, L("Closed")
	default:
		return "git-pull-request", t.Success, L("Open")
	}
}

func (a *App) agentPRBadge(c *ui.Context, pr workstreamPR) {
	k, t := a.kit, a.kit.T
	icon, color, statusLabel := prStatusVisual(t, pr.State)
	num := L("PR")
	if pr.Number > 0 {
		num = "#" + strconv.Itoa(pr.Number)
	}
	tip := num + " · " + statusLabel
	if pr.Title != "" {
		tip = pr.Title + " (" + statusLabel + ")"
	}
	b := ui.ButtonBase(c.Key("pr:"+pr.URL)).Label(L("Pull request %s, %s", num, statusLabel)).Tooltip(tip).
		Height(k.Px(18)).PaddingX(k.Px(5)).Gap(k.Px(3)).AlignItems(ui.Center).Shrink(0).Radius(k.Px(4)).
		Border(1, color.Alpha(0.35)).Background(color.Alpha(0.1)).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(color.Alpha(0.18))
	}
	b.Children(func() {
		k.Icon(c, icon, 10, color)
		k.Text(c, num, 10, 14).FontWeight(500).TextColor(color).Font(k.Mono)
	})
	if b.Clicked() {
		c.OpenURL(pr.URL)
	}
}

func (a *App) prsPanel(c *ui.Context, ctx *wsContext) {
	k, t := a.kit, a.kit.T
	a.loadWorkstreamPRs(ctx, false)
	pc := &a.workstreams.prs
	empty := func(title, text string) {
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(k.Px(2)).PaddingX(k.Px(24)).Children(func() {
			k.Text(c, title, k.Dense(12, 13), k.Dense(18, 20)).FontWeight(500).Role(ui.RoleStatus)
			if text != "" {
				k.Text(c, text, k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.MutedForeground).TextAlign(ui.Center)
			}
		})
	}
	if ctx.w == nil {
		empty(L("Loading pull requests…"), "")
		return
	}
	ui.Row(c).Shrink(0).Height(k.Px(34)).PaddingX(k.Px(12)).Gap(k.Px(6)).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Children(func() {
		count := len(pc.list)
		label := L("%d pull requests", count)
		if count == 1 {
			label = L("1 pull request")
		}
		k.Text(c, label, k.Dense(11, 12), 16).TextColor(t.MutedForeground).Grow(1)
		if k.IconButton(c, "refresh-cw", L("Refresh pull requests"), 26).Disabled(pc.reading).Clicked() {
			a.loadWorkstreamPRs(ctx, true)
		}
	})
	if !pc.have && pc.reading {
		empty(L("Loading pull requests…"), "")
		return
	}
	if len(pc.list) == 0 {
		empty(L("No pull requests yet"), L("Pull requests opened by this workstream’s agents show up here."))
		return
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Padding(k.Px(6), k.Px(8), k.Px(12), k.Px(8)).Children(func() {
		ui.Column(c).Role(ui.RoleList).Label(L("Pull requests")).Gap(k.Px(2)).Children(func() {
			for _, pr := range pc.list {
				a.prRow(c, pr)
			}
		})
	})
}

func (a *App) prRow(c *ui.Context, pr workstreamPR) {
	k, t := a.kit, a.kit.T
	icon, color, statusLabel := prStatusVisual(t, pr.State)
	num := ""
	if pr.Number > 0 {
		num = "#" + strconv.Itoa(pr.Number)
	}
	heading := cmp.Or(pr.Title, pr.Branch, pr.URL)
	if num != "" && pr.Title != "" {
		heading = num + " " + pr.Title
	} else if num != "" {
		heading = num + " · " + heading
	}
	var subParts []string
	subParts = append(subParts, statusLabel)
	if pr.AgentTitle != "" {
		subParts = append(subParts, pr.AgentTitle)
	}
	if pr.Branch != "" {
		subParts = append(subParts, pr.Branch)
	}
	sub := strings.Join(subParts, " · ")
	ui.Row(c.Key("pr-row:" + pr.URL)).Role(ui.RoleListItem).Children(func() {
		b := ui.ButtonBase(c).Label(listName(heading, statusLabel)).Tooltip(pr.URL).Grow(1).MinWidth(0).
			Padding(k.Px(6), k.Px(8), k.Px(6), k.Px(8)).Gap(k.Px(8)).Justify(ui.Start).Radius(k.Px(8)).Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(t.Muted.Alpha(0.6))
		}
		b.Children(func() {
			ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Shrink(0).Children(func() {
				k.Icon(c, icon, 14, color)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(1)).Children(func() {
				k.Text(c, heading, k.Dense(12, 13), k.Dense(18, 19.5)).FontWeight(500).TextColor(t.Foreground).SingleLine()
				k.Text(c, sub, k.Dense(11, 12), k.Dense(16, 17)).TextColor(t.MutedForeground).SingleLine()
			})
			k.Icon(c, "arrow-up-right", 12, t.MutedForeground)
		})
		if b.Clicked() {
			c.OpenURL(pr.URL)
		}
		b.ContextMenu(func(m *ui.Menu) {
			if m.Item(L("Open in browser")).Chosen() {
				c.OpenURL(pr.URL)
			}
			if m.Item(L("Copy link")).Chosen() {
				c.WriteClipboard(pr.URL)
			}
		})
	})
}

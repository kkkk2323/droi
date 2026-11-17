package app

import (
	"cmp"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
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

func prRepoSlug(raw string) string {
	norm := normalizePRURL(raw)
	if norm == "" {
		return ""
	}
	u, err := url.Parse(norm)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	p := strings.Trim(u.Path, "/")
	lp := strings.ToLower(p)
	var repo string
	switch {
	case strings.Index(lp, "/pull/") > 0:
		repo = p[:strings.Index(lp, "/pull/")]
	case strings.Index(lp, "/-/merge_requests/") > 0:
		repo = p[:strings.Index(lp, "/-/merge_requests/")]
	case strings.Index(lp, "/merge_requests/") > 0:
		repo = p[:strings.Index(lp, "/merge_requests/")]
	}
	repo = strings.Trim(repo, "/")
	if host == "" || repo == "" {
		return ""
	}
	return strings.ToLower(host + "/" + repo)
}

func stripFencedCode(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	inFence := false
	var fenceChar byte
	fenceLen := 0
	for _, line := range lines {
		trimmed := strings.TrimLeft(strings.TrimSuffix(line, "\r"), " \t")
		if !inFence {
			if len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
				ch := trimmed[0]
				n := 0
				for n < len(trimmed) && trimmed[n] == ch {
					n++
				}
				rest := trimmed[n:]
				if n >= 3 && !(ch == '`' && strings.ContainsRune(rest, '`')) {
					inFence = true
					fenceChar = ch
					fenceLen = n
					continue
				}
			}
			kept = append(kept, line)
			continue
		}
		if len(trimmed) >= fenceLen && trimmed[0] == fenceChar {
			n := 0
			for n < len(trimmed) && trimmed[n] == fenceChar {
				n++
			}
			if n >= fenceLen && strings.Trim(trimmed[n:], " \t") == "" {
				inFence = false
			}
		}
	}
	return strings.Join(kept, "\n")
}

func stripInlineCode(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		if text[i] != '`' {
			b.WriteByte(text[i])
			i++
			continue
		}
		tickLen := 0
		for i+tickLen < len(text) && text[i+tickLen] == '`' {
			tickLen++
		}
		closed := false
		for j := i + tickLen; j < len(text); {
			if text[j] != '`' {
				j++
				continue
			}
			runLen := 0
			for j+runLen < len(text) && text[j+runLen] == '`' {
				runLen++
			}
			if runLen == tickLen {
				i = j + runLen
				closed = true
				break
			}
			j += runLen
		}
		if !closed {
			b.WriteString(text[i : i+tickLen])
			i += tickLen
		}
	}
	return b.String()
}

func stripMarkdownCode(text string) string {
	return stripInlineCode(stripFencedCode(text))
}

func extractPRURLs(text string) []string {
	if text == "" {
		return nil
	}
	cleaned := stripMarkdownCode(text)
	matches := prURLRe.FindAllString(cleaned, -1)
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
			if m.Role != protocol.MessageRoleAssistant {
				continue
			}
			for _, b := range m.Content {
				if b.Type != protocol.ContentBlockTypeText {
					continue
				}
				var tb struct {
					Text string `json:"text"`
				}
				if json.Unmarshal(b.Raw, &tb) == nil {
					add(tb.Text)
				}
			}
		}
	})
	return seen
}

func findGitConfigPaths(dir string) []string {
	if dir == "" {
		return nil
	}
	cur := filepath.Clean(dir)
	for {
		dotGit := filepath.Join(cur, ".git")
		info, err := os.Stat(dotGit)
		if err == nil {
			if info.IsDir() {
				return []string{filepath.Join(dotGit, "config")}
			}
			b, rerr := os.ReadFile(dotGit)
			if rerr != nil {
				return nil
			}
			var gitdir string
			for _, line := range strings.Split(string(b), "\n") {
				line = strings.TrimSpace(line)
				if len(line) >= len("gitdir:") && strings.EqualFold(line[:len("gitdir:")], "gitdir:") {
					gitdir = strings.TrimSpace(line[len("gitdir:"):])
					break
				}
			}
			if gitdir == "" {
				return nil
			}
			if !filepath.IsAbs(gitdir) {
				gitdir = filepath.Join(cur, gitdir)
			}
			var paths []string
			if cb, cerr := os.ReadFile(filepath.Join(gitdir, "commondir")); cerr == nil {
				common := strings.TrimSpace(string(cb))
				if common != "" {
					if !filepath.IsAbs(common) {
						common = filepath.Join(gitdir, common)
					}
					paths = append(paths, filepath.Join(common, "config"))
				}
			}
			paths = append(paths, filepath.Join(gitdir, "config"))
			return paths
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return nil
}

func stripGitConfigComment(line string) string {
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case '#', ';':
			if !inQuote {
				return line[:i]
			}
		}
	}
	return line
}

func parseGitConfigRemotes(data string) []string {
	var urls []string
	inRemote := false
	for _, rawLine := range strings.Split(data, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			inner := strings.TrimSpace(line[1 : len(line)-1])
			inRemote = len(inner) > len("remote") &&
				strings.EqualFold(inner[:len("remote")], "remote") &&
				(inner[len("remote")] == ' ' || inner[len("remote")] == '\t' || inner[len("remote")] == '"')
			continue
		}
		if !inRemote {
			continue
		}
		line = strings.TrimSpace(stripGitConfigComment(line))
		key, val, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "url") {
			continue
		}
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if val != "" {
			urls = append(urls, val)
		}
	}
	return urls
}

func normalizeGitRemoteSlug(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		host := strings.ToLower(u.Hostname())
		p := strings.Trim(u.Path, "/")
		p = strings.TrimSuffix(p, ".git")
		p = strings.Trim(p, "/")
		if host == "" || p == "" {
			return ""
		}
		return strings.ToLower(host + "/" + p)
	}
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 {
		return ""
	}
	if at := strings.IndexByte(raw, '@'); at >= 0 && at < colon {
		raw = raw[at+1:]
		colon = strings.IndexByte(raw, ':')
	}
	host, p, ok := strings.Cut(raw, ":")
	if !ok {
		return ""
	}
	host = strings.ToLower(strings.TrimSpace(host))
	p = strings.Trim(strings.TrimSpace(p), "/")
	p = strings.TrimSuffix(p, ".git")
	p = strings.Trim(p, "/")
	if host == "" || p == "" {
		return ""
	}
	return strings.ToLower(host + "/" + p)
}

func gitRemoteSlugs(dir string) map[string]bool {
	paths := findGitConfigPaths(dir)
	if len(paths) == 0 {
		return nil
	}
	var slugs map[string]bool
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, u := range parseGitConfigRemotes(string(b)) {
			if slug := normalizeGitRemoteSlug(u); slug != "" {
				if slugs == nil {
					slugs = map[string]bool{}
				}
				slugs[slug] = true
			}
		}
	}
	return slugs
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
	listed := a.listedNow()
	remotesByDir := map[string]map[string]bool{}
	remotesFor := func(dir string) map[string]bool {
		if dir == "" {
			return nil
		}
		if r, ok := remotesByDir[dir]; ok {
			return r
		}
		r := gitRemoteSlugs(dir)
		remotesByDir[dir] = r
		return r
	}
	var sigParts []string
	sigParts = append(sigParts, ctx.id)
	seenURL := map[string]bool{}
	for _, ag := range ctx.w.Agents {
		sid := a.resolve(ag.SessionID)
		title := trimOr(ag.Title, L("Agent"))
		cwd := cmp.Or(ag.WorktreePath, ctx.w.Workspace)
		repoDir := cmp.Or(ctx.w.Workspace, ag.WorktreePath)
		remotes := remotesFor(repoDir)
		meta := prAgentMeta{tab: ag.SessionID, resolvedID: sid, title: title, branch: ag.Branch}
		plan.agents[ag.SessionID] = meta
		plan.agents[sid] = meta
		reportAt := ""
		if ag.LastReport != nil {
			reportAt = ag.LastReport.At
		}
		sigParts = append(sigParts, "a:"+ag.SessionID+":"+sid+":"+ag.ArchivedAt+":"+ag.LastActivityAt+":"+reportAt)
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
		type candidatePR struct {
			url   string
			title string
		}
		var candidates []candidatePR
		addCandidate := func(u, prTitle string, checkRemote bool) {
			norm := normalizePRURL(u)
			if norm == "" {
				return
			}
			if checkRemote && len(remotes) > 0 {
				if slug := prRepoSlug(norm); slug == "" || !remotes[slug] {
					return
				}
			}
			for i := range candidates {
				if candidates[i].url == norm {
					if candidates[i].title == "" && prTitle != "" {
						candidates[i].title = prTitle
					}
					return
				}
			}
			candidates = append(candidates, candidatePR{url: norm, title: prTitle})
		}
		ids := []string{sid}
		if ag.SessionID != "" && ag.SessionID != sid {
			ids = append(ids, ag.SessionID)
		}
		for _, id := range ids {
			if id == "" {
				continue
			}
			if sum := findSummary(listed, id); sum != nil {
				for _, art := range sum.Artifacts {
					if art.Kind == string(protocol.SessionArtifactKindPullRequest) {
						addCandidate(art.URL, art.Title, false)
					}
				}
			}
		}
		if ag.LastReport != nil {
			for _, u := range extractPRURLs(ag.LastReport.Text) {
				addCandidate(u, "", true)
			}
		}
		for _, id := range ids {
			for _, u := range a.sessionPRURLs(id) {
				addCandidate(u, "", true)
			}
		}
		for _, cand := range candidates {
			if seenURL[cand.url] {
				continue
			}
			seenURL[cand.url] = true
			plan.fallback = append(plan.fallback, workstreamPR{
				URL:        cand.url,
				Number:     prNumberFromURL(cand.url),
				Title:      cand.title,
				State:      "open",
				SessionID:  ag.SessionID,
				AgentTitle: title,
				Branch:     ag.Branch,
			})
			sub, err := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectURL{
				Kind:      protocol.DaemonPullRequestSubjectKindURL,
				SessionID: sid,
				Cwd:       cwd,
				URL:       cand.url,
			})
			if err == nil {
				plan.subjects = append(plan.subjects, sub)
				sigParts = append(sigParts, "u:"+sid+":"+cand.url)
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
	sigChanged := pc.have && pc.wsID == ctx.id && pc.sig != plan.sig
	stale := !pc.have || pc.wsID != ctx.id || sigChanged || now.Sub(pc.at) >= prRefreshInterval
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
		list, byAgent := mergeWorkstreamPRs(nil, plan.fallback, plan.agents, false)
		pc.have, pc.wsID, pc.sig, pc.at = true, ctx.id, plan.sig, now
		pc.list, pc.byAgent = list, byAgent
		return
	}
	wsID := ctx.id
	shouldInvalidate := force || sigChanged
	pc.reading = true
	go func() {
		var res *protocol.ResolvePullRequestStatusesResult
		var err error
		lookups := make([]protocol.DaemonPullRequestLookup, 0, len(plan.subjects))
		for _, sub := range plan.subjects {
			reason := protocol.DaemonPullRequestLookupReasonBranchResolved
			if sub.Kind == protocol.DaemonPullRequestSubjectKindURL {
				reason = protocol.DaemonPullRequestLookupReasonTranscriptLink
			}
			if force {
				reason = protocol.DaemonPullRequestLookupReasonManualRefresh
			}
			lookup := protocol.DaemonPullRequestLookup{
				Subject: sub,
				Reason:  reason,
			}
			if shouldInvalidate {
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
		rpcOK := err == nil && res != nil
		if rpcOK {
			resolved = res.Statuses
		}
		list, byAgent := mergeWorkstreamPRs(resolved, plan.fallback, plan.agents, rpcOK)
		a.cfg.Update(func() {
			pc.reading, pc.have = false, true
			pc.wsID, pc.sig, pc.at = wsID, plan.sig, a.cfg.Now()
			pc.list, pc.byAgent = list, byAgent
		})
	}()
}

func isFallbackUnavailableReason(r protocol.DaemonPullRequestUnavailableReason) bool {
	return r == protocol.DaemonPullRequestUnavailableReasonRateLimited ||
		r == protocol.DaemonPullRequestUnavailableReasonUnsupportedRemote
}

func resultAllowsURLFallback(r protocol.DaemonPullRequestStatusResult) (string, bool) {
	sv, err := r.Subject.Value()
	if err != nil {
		return "", false
	}
	u, ok := sv.(*protocol.DaemonPullRequestSubjectURL)
	if !ok {
		return "", false
	}
	key := normalizePRURL(u.URL)
	if key == "" {
		return "", false
	}
	if r.Record != nil {
		if r.Record.State == protocol.DaemonPullRequestStateUnavailable && isFallbackUnavailableReason(r.Record.Reason) {
			return key, true
		}
		return "", false
	}
	if r.Status != nil {
		if stv, err := r.Status.Value(); err == nil {
			if unavail, ok := stv.(*protocol.DaemonPullRequestStatusUnavailable); ok && isFallbackUnavailableReason(unavail.Reason) {
				return key, true
			}
		}
	}
	return "", false
}

func mergeWorkstreamPRs(results []protocol.DaemonPullRequestStatusResult, fallback []workstreamPR, agents map[string]prAgentMeta, rpcOK bool) ([]workstreamPR, map[string][]workstreamPR) {
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
	fallbackAllowed := map[string]bool{}
	for _, r := range results {
		if pr, ok := prFromResult(r, agents); ok {
			upsert(pr)
		} else if rpcOK {
			if key, ok := resultAllowsURLFallback(r); ok {
				fallbackAllowed[key] = true
			}
		}
	}
	for _, fb := range fallback {
		key := normalizePRURL(fb.URL)
		if key == "" {
			continue
		}
		if cur, ok := byURL[key]; ok {
			if cur.Title == "" && fb.Title != "" {
				cur.Title = fb.Title
				byURL[key] = cur
			}
			continue
		}
		if !rpcOK || fallbackAllowed[key] {
			upsert(fb)
		}
	}
	list := make([]workstreamPR, 0, len(order))
	for _, key := range order {
		list = append(list, byURL[key])
	}
	slices.SortStableFunc(list, func(x, y workstreamPR) int { return cmp.Compare(y.Number, x.Number) })
	byAgent := map[string][]workstreamPR{}
	for _, pr := range list {
		if pr.SessionID != "" {
			byAgent[pr.SessionID] = append(byAgent[pr.SessionID], pr)
		}
	}
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

func (a *App) agentMorePRsBadge(c *ui.Context, wsID string, more, total int) {
	k, t := a.kit, a.kit.T
	label := "+" + strconv.Itoa(more)
	tip := L("Show all %d pull requests", total)
	b := ui.ButtonBase(c.Key("pr-more")).Label(tip).Tooltip(tip).
		Height(k.Px(18)).PaddingX(k.Px(5)).AlignItems(ui.Center).Shrink(0).Radius(k.Px(4)).
		Border(1, t.Border).Background(t.Muted.Alpha(0.5)).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(t.Muted)
	}
	b.Children(func() {
		k.Text(c, label, 10, 14).FontWeight(500).TextColor(t.MutedForeground).Font(k.Mono)
	})
	if b.Clicked() {
		a.setWsState(wsID, func(u *workstreamUI) { u.Panel = panelPRs })
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

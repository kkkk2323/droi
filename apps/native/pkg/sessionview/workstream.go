package sessionview

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// AgentInfo is what the app knows of a Workstream's agent by its Session id:
// its name and what it does or waits for now, "" for an idle one.
type AgentInfo struct {
	Title    string
	Activity string
	// Waiting marks an agent that waits on the user.
	Waiting bool
}

// wsRow is a Workstream tool call's row: one line with its icon, what it did
// and how it went, and optionally a panel, a button to an agent's tab, or a
// click of its own.
type wsRow struct {
	icon string
	// title is the agent's name in bold; line says what happened to it.
	title, line string
	// badge is a small mark after the line, such as a report's kind.
	badge      string
	badgeColor func() ui.Color
	state      transcript.CallState
	// agent is the Session of the agent the row opens from its button.
	agent string
	// panel draws what the row opens to; nil when it opens to nothing.
	panel func()
	// click is what the whole row does when it opens to nothing.
	click func()
}

// agentOf is the agent a call is about with its name filled in from the
// app when the call has not told it yet.
func (v *View) agentOf(call *transcript.ToolCall) (id, title string, info AgentInfo) {
	id, title = transcript.AgentOf(call)
	if v.env.Agent != nil && id != "" {
		if got, ok := v.env.Agent(id); ok {
			info = got
			if title == "" {
				title = got.Title
			}
		}
	}
	return id, title, info
}

// workstreamRow draws the row of a Workstream tool call.
func (v *View) workstreamRow(c *ui.Context, call *transcript.ToolCall, tool string) {
	k, t := v.look()
	id, title, info := v.agentOf(call)
	name := title
	if name == "" && id != "" {
		name = id[:min(len(id), 8)]
	}
	r := wsRow{state: transcript.StateOf(call), agent: id}
	r.line = transcript.WorkstreamLine(call, tool, name)
	failure := ""
	if r.state == transcript.CallFailed {
		failure = transcript.ResultText(call.Result)
	}
	prose := func(text string) func() {
		return func() {
			ui.Column(c).Selectable().Padding(k.Px(10), k.Px(14), k.Px(12), k.Px(14)).Gap(k.Px(8)).Children(func() {
				v.reply(c, text, mdReply, false)
				if failure != "" {
					k.Text(c, failure, 11.5, 17.25).Font(k.Mono).TextColor(t.DestructiveForeground).Selectable()
				}
			})
		}
	}
	plain := func(text string) func() {
		return func() {
			ui.Column(c).Selectable().Padding(k.Px(10)).Children(func() {
				color := t.MutedForeground
				if r.state == transcript.CallFailed {
					color = t.DestructiveForeground
				}
				k.Text(c, text, 11.5, 17.25).Font(k.Mono).TextColor(color).Selectable()
			})
		}
	}
	switch tool {
	case transcript.ToolSpawnAgent:
		r.icon, r.title = "bot", title
		if r.title == "" {
			r.title = l10n.L("New agent")
		}
		// The agent's own state says more than "Started" once it runs.
		if r.state == transcript.CallDone && info.Activity != "" {
			r.line = info.Activity
		}
		prompt, _ := call.Input["prompt"].(string)
		if strings.TrimSpace(prompt) != "" || failure != "" {
			r.panel = prose(prompt)
		}
		if r.state != transcript.CallDone {
			r.agent = ""
		}
	case transcript.ToolSendMessage:
		r.icon = "send"
		message, _ := call.Input["message"].(string)
		if strings.TrimSpace(message) != "" || failure != "" {
			r.panel = prose(message)
		}
	case transcript.ToolListAgents:
		r.icon, r.agent = "users", ""
		r.click = v.env.OpenAgents
		if failure != "" {
			r.panel, r.click = plain(failure), nil
		}
	case transcript.ToolInterruptAgent:
		r.icon = "square"
		if failure != "" {
			r.panel = plain(failure)
		}
	case transcript.ToolArchiveAgent:
		r.icon = "archive"
		if text := transcript.ResultText(call.Result); text != "" {
			r.panel = plain(text)
		}
	case transcript.ToolReport:
		r.icon, r.agent = "send", ""
		kind, _ := call.Input["kind"].(string)
		r.badge, r.badgeColor = v.reportKind(kind)
		message, _ := call.Input["message"].(string)
		if strings.TrimSpace(message) != "" || failure != "" {
			r.panel = prose(message)
		}
	}
	v.wsRow(c, call.Use.ID, r)
}

// reportKind is the mark of a report's kind: its words and its colour.
func (v *View) reportKind(kind string) (string, func() ui.Color) {
	t := v.kit().T
	switch kind {
	case "done":
		return l10n.L("Done"), func() ui.Color { return t.Success }
	case "blocked":
		return l10n.L("Blocked"), func() ui.Color { return t.AttentionForeground }
	case "update":
		return l10n.L("Update"), func() ui.Color { return t.MutedForeground }
	}
	return "", nil
}

// wsRow draws a row of a Workstream tool call.
func (v *View) wsRow(c *ui.Context, key string, r wsRow) {
	k, t := v.look()
	open := v.flag("ws:"+key, false)
	label := r.line
	if r.title != "" {
		label = r.title + ": " + r.line
	}
	ui.Column(c.Key(key)).Children(func() {
		var p ui.CollapsibleParts
		content := func(hover bool, chevron func()) {
			fg, iconColor := t.Foreground, t.MutedForeground
			if r.state == transcript.CallFailed {
				fg, iconColor = t.DestructiveForeground, t.DestructiveForeground
			}
			k.Icon(c, r.icon, 14, iconColor)
			if r.title != "" {
				k.Text(c, r.title, 12.5, 18.75).FontWeight(500).TextColor(fg).SingleLine().Shrink(1).MinWidth(0)
			}
			lineColor := t.MutedForeground
			if r.title == "" {
				lineColor = fg
			}
			text := k.Text(c, r.line, 12.5, 18.75).TextColor(lineColor).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			if r.title == "" {
				text.FontWeight(500)
			}
			if r.badge != "" {
				ui.Row(c).Height(k.Px(16)).PaddingX(k.Px(5)).AlignItems(ui.Center).Shrink(0).Radius(k.Px(4)).Border(1, t.Border).Children(func() {
					k.Text(c, r.badge, 10, 14).FontWeight(500).TextColor(r.badgeColor())
				})
			}
			switch r.state {
			case transcript.CallRunning:
				k.Spinner(c, 14, t.MutedForeground).Role(ui.RoleStatus).Label(l10n.L("Running"))
			case transcript.CallFailed:
				k.Icon(c, "circle-x", 14, t.DestructiveForeground).Label(l10n.L("Failed"))
			default:
				k.Icon(c, "check", 14, t.Success).Label(l10n.L("Succeeded"))
			}
			if chevron != nil {
				chevron()
			}
		}
		row := func() {
			switch {
			case r.panel != nil:
				p = v.rowTrigger(c, open, label)
				hover := p.Trigger.Hovered() || p.Trigger.Focused()
				p.Trigger.Children(func() {
					content(hover, func() {
						ch := k.Icon(c, "chevron-right", 14, t.MutedForeground).Rotate(90 * p.Progress())
						if !hover && !*open && p.Progress() == 0 {
							ch.Opacity(0)
						}
					})
				})
			case r.click != nil:
				b := ui.ButtonBase(c).Label(label).FillWidth().Height(k.Px(28)).Gap(k.Px(8)).PaddingX(k.Px(6)).Margin(0, 0, 0, -k.Px(6)).
					Radius(k.Px(8)).FontSize(k.Px(12.5)).Cursor(ui.CursorPointer)
				if b.Hovered() {
					b.Background(t.Muted.Alpha(0.7))
				}
				b.Children(func() { content(b.Hovered(), nil) })
				if b.Clicked() {
					r.click()
				}
			default:
				ui.Row(c).Role(ui.RoleStatus).Label(label).FillWidth().Height(k.Px(28)).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() { content(false, nil) })
			}
		}
		if r.agent == "" || v.env.OpenSession == nil {
			row()
		} else {
			ui.Row(c).Gap(k.Px(4)).Children(func() {
				row()
				if r.panel != nil {
					p.Trigger.Shrink(1).MinWidth(0)
				}
				name := r.title
				if name == "" {
					name = r.agent
				}
				if k.IconButton(c, "arrow-up-right", l10n.L("Open %s", name), 28).Clicked() {
					v.env.OpenSession(r.agent)
				}
			})
		}
		if r.panel != nil {
			p.Panel(func() {
				ui.Column(c).Margin(k.Px(2), 0, k.Px(4), 0).Radius(k.Px(10)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().Children(r.panel)
			})
		}
	})
}

// groupHeader is the line over a run of calls done to several agents: what
// was done to how many, with a spinner while any call runs.
func (v *View) groupHeader(c *ui.Context, call *transcript.ToolCall, label string) {
	k, t := v.look()
	tool, _ := transcript.WorkstreamTool(call)
	icon := map[string]string{
		transcript.ToolSpawnAgent: "bot", transcript.ToolSendMessage: "send",
		transcript.ToolInterruptAgent: "square", transcript.ToolArchiveAgent: "archive",
	}[tool]
	ui.Row(c).Role(ui.RoleStatus).Label(label).Height(k.Px(24)).Gap(k.Px(6)).AlignItems(ui.Center).Children(func() {
		k.Icon(c, icon, 12, t.MutedForeground)
		k.Text(c, label, 12, 16).FontWeight(500).TextColor(t.MutedForeground).SingleLine()
	})
}

// agentMessageCard is a message the Host gave the orchestrator for an agent's
// report: a card that opens to the report, with a button to the agent.
func (v *View) agentMessageCard(c *ui.Context, id string, m transcript.AgentMessage) {
	k, t := v.look()
	open := v.flag("agentmsg:"+id, false)
	_, _, info := v.agentOf(&transcript.ToolCall{Input: map[string]any{"agent_id": m.ID}})
	title := m.Title
	if title == "" {
		title = info.Title
	}
	heading := l10n.L("Received message from %s", title)
	badge, color := v.reportKind(m.Kind)
	v.column(c).Role(ui.RoleGroup).Label(heading).PaddingY(k.Px(12)).Children(func() {
		ui.Column(c).FillWidth().Radius(k.Px(12)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().Children(func() {
			var p ui.CollapsibleParts
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				p = ui.CollapsibleBase(c, open)
				v.holdOnToggle(p.Trigger)
				tr := p.Trigger.Label(heading).Grow(1).Shrink(1).MinWidth(0).Gap(k.Px(10)).Padding(k.Px(8), k.Px(10)).Cursor(ui.CursorPointer)
				if tr.Hovered() {
					tr.Background(t.Muted.Alpha(0.6))
				}
				tr.Children(func() {
					ui.Box(c).Size(k.Px(28), k.Px(28)).Radius(k.Px(8)).Background(t.Muted).Center().Shrink(0).Children(func() {
						k.Icon(c, "inbox", 14, t.MutedForeground)
					})
					k.Text(c, heading, 13, 19.5).FontWeight(500).TextColor(t.Foreground).SingleLine().Grow(1).Shrink(1).MinWidth(0)
					if badge != "" {
						ui.Row(c).Height(k.Px(16)).PaddingX(k.Px(5)).AlignItems(ui.Center).Shrink(0).Radius(k.Px(4)).Border(1, t.Border).Children(func() {
							k.Text(c, badge, 10, 14).FontWeight(500).TextColor(color())
						})
					}
					k.Icon(c, "chevron-right", 14, t.MutedForeground).Rotate(90 * p.Progress())
				})
				if v.env.OpenSession != nil {
					if k.IconButton(c, "arrow-up-right", l10n.L("Open %s", title), 28).Margin(0, k.Px(8), 0, 0).Clicked() {
						v.env.OpenSession(m.ID)
					}
				}
			})
			p.Panel(func() {
				ui.Column(c).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Padding(k.Px(10), k.Px(14), k.Px(12), k.Px(14)).Selectable().Children(func() {
					v.reply(c, m.Body, mdReply, false)
				})
			})
		})
	})
}

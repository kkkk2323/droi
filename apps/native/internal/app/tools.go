package app

import (
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/apps/native/internal/mcp"
	"github.com/kkkk2323/droi/apps/native/internal/skills"
	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// toolsState is the Skills and MCP servers dialog of one Session: what the
// Daemon listed and what the dialog is showing.
type toolsState struct {
	open bool
	tab  string
	// view is the MCP tab's page: "" for the list, "catalogue" or "form".
	view    string
	from    *protocol.MCPRegistryServer
	form    mcp.ServerForm
	problem string
	query   string
	remove  string // the server whose removal waits for a second click

	// Guarded by mu: filled by goroutines.
	mu               sync.Mutex
	loadedAt         time.Time
	loaded           bool
	skills           []protocol.SkillInfo
	projectAvailable bool
	skillsErr        string
	servers          []protocol.MCPServerStatusInfo
	summary          protocol.MCPStatusSummary
	serversErr       string
	mcpTools         []protocol.MCPToolInfo
	registry         []protocol.MCPRegistryServer
	registryState    int // 0 not asked, 1 loading, 2 loaded, 3 failed
	busy             string
	actErr           string
}

// toolsStale makes the next frame list the skills and servers again, as an
// MCP notification or a switch changed them.
func (v *sessionView) toolsStale() {
	v.tools.mu.Lock()
	v.tools.loadedAt = time.Time{}
	v.tools.mu.Unlock()
}

// loadTools lists the Session's skills, servers and tools when the last
// list is older than 30 seconds: the droid CLI or the Factory App may have
// switched one meanwhile.
// skillsShown are the skills without their bodies and resources, which the
// Daemon sends whole and the window never shows: tens of skills hold MBs.
func skillsShown(list []protocol.SkillInfo) []protocol.SkillInfo {
	out := make([]protocol.SkillInfo, len(list))
	for i, s := range list {
		s.Content, s.Resources = "", nil
		out[i] = s
	}
	return out
}

func (v *sessionView) loadTools() {
	ts := &v.tools
	ts.mu.Lock()
	if v.a.ctl == nil || time.Since(ts.loadedAt) < 30*time.Second {
		ts.mu.Unlock()
		return
	}
	ts.loadedAt = time.Now()
	ts.mu.Unlock()
	go func() {
		defer v.a.redraw()
		cl, err := v.a.ctl.Client()
		if err != nil {
			ts.mu.Lock()
			ts.skillsErr, ts.serversErr, ts.loaded = err.Error(), err.Error(), true
			ts.loadedAt = time.Time{}
			ts.mu.Unlock()
			return
		}
		sk, err1 := cl.ListSkills(v.a.ctx, protocol.ListSkillsParams{SessionID: v.id})
		sv, err2 := cl.ListMCPServers(v.a.ctx, protocol.ListMCPServersParams{SessionID: v.id})
		tl, err3 := cl.ListMCPTools(v.a.ctx, protocol.ListMCPToolsParams{SessionID: v.id})
		ts.mu.Lock()
		defer ts.mu.Unlock()
		ts.loaded = true
		ts.skillsErr, ts.serversErr = "", ""
		if err1 != nil {
			ts.skillsErr = err1.Error()
		} else {
			ts.skills = skillsShown(sk.Skills)
			ts.projectAvailable = sk.ProjectAvailable != nil && *sk.ProjectAvailable
		}
		if err2 != nil {
			ts.serversErr = err2.Error()
		} else {
			ts.servers, ts.summary = sv.Servers, sv.Summary
		}
		if err3 == nil {
			ts.mcpTools = tl.Tools
		}
	}()
}

// toolsAct runs one change through the Daemon for name (a skill or a
// server), then lists everything again. It reports whether it went through.
func (v *sessionView) toolsAct(name string, run func() error, done func(ok bool)) {
	ts := &v.tools
	ts.mu.Lock()
	ts.busy, ts.actErr = name, ""
	ts.mu.Unlock()
	go func() {
		err := run()
		ts.mu.Lock()
		ts.busy = ""
		if err != nil {
			ts.actErr = err.Error()
		}
		ts.loadedAt = time.Time{}
		ts.mu.Unlock()
		v.composer.mu.Lock()
		v.composer.slashAt = time.Time{}
		v.composer.mu.Unlock()
		v.a.cfg.Update(func() {
			if done != nil {
				done(err == nil)
			}
		})
	}()
}

var mcpDot = map[string]func(k *kit.Kit) ui.Color{
	"connected":    func(k *kit.Kit) ui.Color { return k.T.Success },
	"connecting":   func(k *kit.Kit) ui.Color { return k.T.Attention },
	"failed":       func(k *kit.Kit) ui.Color { return k.T.DestructiveForeground },
	"disconnected": func(k *kit.Kit) ui.Color { return k.T.MutedForeground.Alpha(0.4) },
	"disabled":     func(k *kit.Kit) ui.Color { return k.T.MutedForeground.Alpha(0.4) },
}

// toolsButton is the footer's count of skills and MCP servers, which opens
// their dialog; hovering lists the servers.
func (v *sessionView) toolsButton(c *ui.Context) {
	k, t := v.a.kit, v.a.kit.T
	ts := &v.tools
	v.loadTools()
	ts.mu.Lock()
	skillList, servers := ts.skills, mcp.SortServers(ts.servers)
	ts.mu.Unlock()

	off := 0
	for _, s := range skillList {
		if s.Enabled != nil && !*s.Enabled {
			off++
		}
	}
	has := map[string]bool{}
	for _, s := range servers {
		has[string(s.Status)] = true
	}
	dot := t.MutedForeground.Alpha(0.4)
	switch {
	case has["failed"]:
		dot = t.DestructiveForeground
	case has["connected"]:
		dot = t.Success
	case has["connecting"]:
		dot = t.Attention
	}
	var summary []string
	if len(skillList) == 0 {
		summary = append(summary, L("No skills"))
	} else if off > 0 {
		summary = append(summary, L("%d skills, %d disabled", len(skillList), off))
	} else {
		summary = append(summary, L("%d skills", len(skillList)))
	}
	if len(servers) == 0 {
		summary = append(summary, L("No MCP servers"))
	} else {
		lines := make([]string, len(servers))
		for i, s := range servers {
			lines[i] = s.Name + " · " + l10n.T(trimOr(mcp.StatusLabels[string(s.Status)], string(s.Status)))
		}
		summary = append(summary, strings.Join(lines, "\n"))
	}

	skillsLabel := L("Skills")
	if len(skillList) > 0 {
		skillsLabel += " "
		if off > 0 {
			skillsLabel += itoa(len(skillList)-off) + "/"
		}
		skillsLabel += itoa(len(skillList))
	}
	mcpLabel := "MCP"
	if len(servers) > 0 {
		mcpLabel += " " + itoa(len(servers))
	}

	b := ui.ButtonBase(c).Label(L("Skills and MCP servers")).Tooltip(strings.Join(summary, "\n\n")).Height(k.Px(28)).Shrink(0).
		Gap(k.Px(6)).PaddingX(k.Px(8)).Radius(k.Px(6)).Cursor(ui.CursorPointer).Expanded(ts.open)
	color := t.Foreground.Alpha(0.75)
	if b.Hovered() || ts.open {
		b.Background(t.Muted)
		color = t.Foreground
	}
	b.Children(func() {
		k.Icon(c, "wrench", 14, color)
		k.Text(c, skillsLabel, 13, 19.5).TextColor(color).SingleLine()
		k.Text(c, "·", 13, 19.5).TextColor(t.Foreground.Alpha(0.3))
		kit.Dot(c, k.Px(6), dot)
		k.Text(c, mcpLabel, 13, 19.5).TextColor(color).SingleLine()
	})
	if b.Clicked() {
		ts.open = true
		ts.view, ts.remove = "", ""
		if ts.tab == "" {
			ts.tab = "skills"
		}
		v.toolsStale()
	}
	v.toolsDialog(c)
}

func (v *sessionView) toolsDialog(c *ui.Context) {
	k, t := v.a.kit, v.a.kit.T
	ts := &v.tools
	w, h := c.Size()
	ui.DialogBase(c, &ts.open, func(backdrop, panel *ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.3))
		maxH := min(k.Px(640), h*0.85)
		panel.Role(ui.RoleDialog).Label(L("Skills and MCP servers")).Width(min(k.Px(640), w-k.Px(32))).
			Radius(k.Px(12)).Border(1, t.Border).Background(t.Popover).TextColor(t.PopoverForeground).Clip().
			Shadow(0, k.Px(20), k.Px(25), -k.Px(5), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Children(func() {
			ui.Row(c).Gap(k.Px(4)).Padding(k.Px(8), k.Px(12)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
				ui.Row(c).Role(ui.RoleTabList).Label(L("Skills and MCP servers")).Gap(k.Px(2)).Children(func() {
					for _, tab := range [][2]string{{"skills", L("Skills")}, {"mcp", L("MCP servers")}} {
						tb := kit.Selected(ui.ButtonBase(c).Role(ui.RoleTab).Label(tab[1]), ts.tab == tab[0]).Height(k.Px(28)).PaddingX(k.Px(10)).
							Radius(k.Px(6)).Cursor(ui.CursorPointer)
						color := t.MutedForeground
						if ts.tab == tab[0] || tb.Hovered() {
							tb.Background(t.Accent)
							color = t.Foreground
						}
						tb.Children(func() { k.Text(c, tab[1], 13, 19.5).TextColor(color) })
						if tb.Clicked() {
							ts.tab = tab[0]
						}
					}
				})
				ui.Box(c).Grow(1)
				if k.IconButton(c, "x", L("Close"), 24).Clicked() {
					ts.open = false
				}
			})
			// The dialog takes the height of what it holds, so the panel
			// gets one of its own: the dialog's room under the 45px tab row.
			ui.Scroll(c).Role(ui.RoleGroup).Label(L("Tab panel")).MaxHeight(maxH - k.Px(45)).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Padding(k.Px(12)).Children(func() {
					if ts.tab == "mcp" {
						v.mcpTab(c)
					} else {
						v.skillsTab(c)
					}
				})
			})
		})
	})
}

func (v *sessionView) toolBadge(c *ui.Context, label string) {
	k, t := v.a.kit, v.a.kit.T
	ui.Box(c).Padding(k.Px(1), k.Px(6)).Radius(k.Px(6)).Background(t.Muted).Children(func() {
		k.Text(c, label, 11, 16.5).TextColor(t.MutedForeground).SingleLine()
	})
}

func (v *sessionView) toolLoading(c *ui.Context, msg string) {
	k, t := v.a.kit, v.a.kit.T
	ui.Row(c).Gap(k.Px(8)).Padding(k.Px(8)).Children(func() {
		k.Spinner(c, 14, t.MutedForeground)
		k.Text(c, msg, 14, 20).TextColor(t.MutedForeground)
	})
}

func (v *sessionView) toolFailure(c *ui.Context, msg string) {
	k, t := v.a.kit, v.a.kit.T
	k.Text(c, msg, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).Padding(k.Px(6), k.Px(8)).
		Radius(k.Px(6)).Background(t.Destructive.Alpha(0.1))
}

func (v *sessionView) toolNote(c *ui.Context, msg string) {
	k, t := v.a.kit, v.a.kit.T
	k.Text(c, msg, 14, 20).TextColor(t.MutedForeground).Padding(k.Px(8))
}

// skillsTab is the skills grouped by where they live, each with the switch
// the Daemon allows; personal skills have none.
func (v *sessionView) skillsTab(c *ui.Context) {
	a := v.a
	k, t := a.kit, a.kit.T
	ts := &v.tools
	ts.mu.Lock()
	loaded, list, errMsg, project, busy, actErr := ts.loaded, ts.skills, ts.skillsErr, ts.projectAvailable, ts.busy, ts.actErr
	ts.mu.Unlock()
	switch {
	case !loaded:
		v.toolLoading(c, L("Loading skills…"))
		return
	case errMsg != "":
		v.toolFailure(c, errMsg)
		return
	case len(list) == 0:
		v.toolNote(c, L("No skills are installed."))
		return
	}
	ui.Column(c).Gap(k.Px(16)).Children(func() {
		if actErr != "" {
			v.toolFailure(c, actErr)
		}
		for _, g := range skills.GroupSkills(list) {
			label := l10n.T(trimOr(skills.LocationLabels[g.Location], g.Location))
			ui.Column(c).Role(ui.RoleGroup).Label(label).Key("skills:" + g.Location).Children(func() {
				k.Text(c, label, 12, 16).FontWeight(500).TextColor(t.MutedForeground).Padding(0, k.Px(8), k.Px(4), k.Px(8))
				ui.Column(c).Role(ui.RoleList).Gap(k.Px(2)).Children(func() {
					for _, s := range g.Skills {
						v.skillRow(c, s, project, busy == s.Name)
					}
				})
			})
		}
	})
}

func (v *sessionView) skillRow(c *ui.Context, s protocol.SkillInfo, project, saving bool) {
	a := v.a
	k, t := a.kit, a.kit.T
	off := s.Enabled != nil && !*s.Enabled
	sw, can := skills.SkillSwitch(s, project)
	row := ui.Row(c).Role(ui.RoleListItem).Label(s.Name).Key(string(s.Location)+":"+s.Name).AlignItems(ui.Start).Gap(k.Px(12)).
		Padding(k.Px(6), k.Px(8)).Radius(k.Px(10))
	if off {
		row.Opacity(0.7)
	}
	row.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Wrap().Gap(k.Px(6)).Children(func() {
				k.Text(c, s.Name, 14, 20).FontWeight(500)
				if off {
					v.toolBadge(c, l10n.T(skills.DisabledLabel(s)))
				}
				if s.UserInvocable != nil && !*s.UserInvocable {
					v.toolBadge(c, L("Model only"))
				}
				if saving {
					k.Spinner(c, 12, t.MutedForeground)
				}
			})
			if s.Description != "" {
				k.Text(c, s.Description, 13, 20).TextColor(t.MutedForeground).MaxLines(2).Margin(k.Px(2), 0, 0, 0)
			}
		})
		if string(s.Location) == "personal" {
			return
		}
		if a.toggle(c, L("%s enabled", s.Name), !off, !can || saving) && can {
			name, level := s.Name, sw.Level
			v.toolsAct(name, func() error {
				cl, err := a.ctl.Client()
				if err != nil {
					return err
				}
				_, err = cl.SetSkillDisabled(a.ctx, protocol.SetSkillDisabledParams{SessionID: v.id, SkillName: name, Disabled: !off,
					SettingsLevel: protocol.EditableSkillSettingsLevel(level)})
				return err
			}, nil)
		}
	})
}

// mcpTab is the MCP servers with their switches, or the catalogue, or the
// form adding a server by hand.
func (v *sessionView) mcpTab(c *ui.Context) {
	ts := &v.tools
	ts.mu.Lock()
	loaded, errMsg, servers, summary, actErr := ts.loaded, ts.serversErr, mcp.SortServers(ts.servers), ts.summary, ts.actErr
	ts.mu.Unlock()
	if !loaded {
		v.toolLoading(c, L("Loading MCP servers…"))
		return
	}
	if errMsg != "" {
		v.toolFailure(c, errMsg)
		return
	}
	taken := make([]string, len(servers))
	for i, s := range servers {
		taken[i] = s.Name
	}
	switch ts.view {
	case "catalogue":
		v.mcpCatalogue(c, taken)
		return
	case "form":
		v.mcpForm(c, taken)
		return
	}
	k := v.a.kit
	ui.Column(c).Gap(k.Px(12)).Children(func() {
		if ce := summary.ConfigError; ce != nil {
			v.toolFailure(c, ce.Path+": "+ce.Message)
		}
		if actErr != "" {
			v.toolFailure(c, actErr)
		}
		if len(servers) == 0 {
			v.toolNote(c, L("No MCP servers are configured."))
		} else {
			ui.Column(c).Role(ui.RoleList).Label(L("MCP servers")).Gap(k.Px(2)).Children(func() {
				for _, s := range servers {
					v.serverRow(c, s)
				}
			})
		}
		ui.Row(c).PaddingX(k.Px(8)).Children(func() {
			if v.a.smallButton(c, kit.Outline, L("Add server"), "plus", false).Clicked() {
				ts.view, ts.query = "catalogue", ""
				v.loadRegistry()
			}
		})
	})
}

func (v *sessionView) loadRegistry() {
	ts := &v.tools
	ts.mu.Lock()
	if ts.registryState == 1 || ts.registryState == 2 {
		ts.mu.Unlock()
		return
	}
	ts.registryState = 1
	ts.mu.Unlock()
	go func() {
		defer v.a.redraw()
		cl, err := v.a.ctl.Client()
		var res *protocol.ListMCPRegistryResult
		if err == nil {
			res, err = cl.ListMCPRegistry(v.a.ctx, protocol.ListMCPRegistryParams{SessionID: v.id})
		}
		ts.mu.Lock()
		defer ts.mu.Unlock()
		if err != nil {
			ts.registryState = 3
			return
		}
		ts.registry, ts.registryState = res.Servers, 2
	}()
}

type mcpClient = *droid.Client

// mcpCall runs one server change through the Daemon's client.
func (v *sessionView) mcpCall(name string, run func(cl mcpClient) error, done func(ok bool)) {
	v.toolsAct(name, func() error {
		cl, err := v.a.ctl.Client()
		if err != nil {
			return err
		}
		return run(cl)
	}, done)
}

func (v *sessionView) serverRow(c *ui.Context, s protocol.MCPServerStatusInfo) {
	a := v.a
	k, t := a.kit, a.kit.T
	ts := &v.tools
	ts.mu.Lock()
	busy := ts.busy == s.Name
	var tools []protocol.MCPToolInfo
	for _, tool := range ts.mcpTools {
		if tool.ServerName == s.Name {
			tools = append(tools, tool)
		}
	}
	ts.mu.Unlock()
	status := string(s.Status)
	label := l10n.T(trimOr(mcp.StatusLabels[status], status))
	readOnly, droi, on := mcp.IsReadOnly(s), mcp.IsDroiMemory(s), status != "disabled"
	toolCount := len(tools)
	if s.ToolCount != nil {
		toolCount = int(*s.ToolCount)
	}
	name := s.Name
	ui.Column(c).Role(ui.RoleListItem).Label(s.Name).Key("server:"+s.Name).Padding(k.Px(6), k.Px(8)).Radius(k.Px(10)).Children(func() {
		ui.Row(c).AlignItems(ui.Start).Gap(k.Px(12)).Children(func() {
			dot := t.MutedForeground.Alpha(0.4)
			if f := mcpDot[status]; f != nil {
				dot = f(k)
			}
			d := kit.Dot(c, k.Px(8), dot).Role(ui.RoleImage).Label(label).Tooltip(label).Margin(k.Px(7), 0, 0, 0)
			if status == "connecting" {
				d.Opacity(float32(0.5 + 0.5*pulseWave(c)))
			}
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Row(c).Wrap().Gap(k.Px(6)).Children(func() {
					k.Text(c, s.Name, 14, 20).FontWeight(500)
					v.toolBadge(c, string(s.ServerType))
					switch string(s.Source) {
					case "org":
						v.toolBadge(c, L("Organization"))
					case "project":
						v.toolBadge(c, L("Project"))
					}
					if droi {
						v.toolBadge(c, "Droi")
					}
				})
				line := label
				if toolCount > 0 && on {
					line += " · " + toolCountLabel(toolCount)
				}
				if s.Error != "" {
					line += " · " + s.Error
				}
				k.Text(c, line, 13, 20).TextColor(t.MutedForeground).Margin(k.Px(2), 0, 0, 0)
				if droi {
					k.Text(c, L("Droi’s Memory. Turn it on or off on the computer in Settings → Memory."), 13, 20).TextColor(t.MutedForeground)
				}
				if s.PendingAuthURL != "" {
					v.signInNotice(c, s)
				}
				open := v.flag("mcp-tools:"+s.Name, false)
				ui.Row(c).Wrap().Gap(k.Px(4)).Margin(k.Px(4), 0, 0, 0).Children(func() {
					if on && toolCount > 0 {
						b := k.Button(c, kit.Ghost, 24, false, toolCountLabel(toolCount)).Expanded(*open).PaddingX(k.Px(8)).Gap(k.Px(4))
						b.Children(func() {
							rot := float32(0)
							if *open {
								rot = 90
							}
							k.Icon(c, "chevron-right", 12, t.MutedForeground).Rotate(rot)
							k.Text(c, toolCountLabel(toolCount), 12, 16).FontWeight(500).TextColor(t.MutedForeground)
						})
						if b.Clicked() {
							*open = !*open
						}
					}
					small := func(variant kit.Variant, label string) bool {
						b := k.Button(c, variant, 24, false, label).PaddingX(k.Px(8)).Disabled(busy)
						color := t.Foreground
						if variant == kit.Destructive {
							color = t.Destructive
						}
						b.Children(func() { k.Text(c, label, 12, 16).FontWeight(500).TextColor(color) })
						return b.Clicked()
					}
					if !readOnly && on && mcp.NeedsSignIn(s) && s.PendingAuthURL == "" && small(kit.Outline, L("Sign in")) {
						v.mcpCall(name, func(cl mcpClient) error {
							_, err := cl.AuthenticateMCPServer(a.ctx, protocol.AuthenticateMCPServerParams{SessionID: v.id, ServerName: name})
							return err
						}, nil)
					}
					if !readOnly && s.HasAuthTokens != nil && *s.HasAuthTokens && small(kit.Ghost, L("Sign out")) {
						v.mcpCall(name, func(cl mcpClient) error {
							_, err := cl.ClearMCPAuth(a.ctx, protocol.ClearMCPAuthParams{SessionID: v.id, ServerName: name})
							return err
						}, nil)
					}
					if mcp.CanRemove(s) {
						if ts.remove == s.Name {
							if small(kit.Destructive, L("Remove %s", s.Name)) {
								ts.remove = ""
								v.mcpCall(name, func(cl mcpClient) error {
									_, err := cl.RemoveMCPServer(a.ctx, protocol.RemoveMCPServerParams{SessionID: v.id, ServerName: name, SettingsLevel: "user"})
									return err
								}, nil)
							}
							if small(kit.Ghost, L("Keep")) {
								ts.remove = ""
							}
						} else if small(kit.Ghost, L("Remove")) {
							ts.remove = s.Name
						}
					}
				})
				if on && toolCount > 0 && *open {
					v.toolList(c, s, tools, readOnly)
				}
			})
			if droi {
				return
			}
			if a.toggle(c, L("%s enabled", s.Name), on, readOnly || busy) && !readOnly {
				enabled := !on
				v.mcpCall(name, func(cl mcpClient) error {
					_, err := cl.ToggleMCPServer(a.ctx, protocol.ToggleMCPServerParams{SessionID: v.id, ServerName: name, Enabled: enabled, SettingsLevel: "user"})
					return err
				}, nil)
			}
		})
	})
}

func (v *sessionView) toolList(c *ui.Context, s protocol.MCPServerStatusInfo, tools []protocol.MCPToolInfo, readOnly bool) {
	a := v.a
	k, t := a.kit, a.kit.T
	if len(tools) == 0 {
		v.toolLoading(c, L("Loading tools…"))
		return
	}
	ui.Column(c).Role(ui.RoleList).Label(L("%s tools", s.Name)).Gap(k.Px(2)).Padding(k.Px(4), 0, 0, k.Px(4)).Children(func() {
		for _, tool := range tools {
			ui.Row(c).Key("tool:" + tool.Name).Gap(k.Px(12)).PaddingY(k.Px(2)).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					k.Text(c, tool.Name, 12, 16).Font(k.Mono)
					if tool.Description != "" {
						k.Text(c, tool.Description, 12, 16).TextColor(t.MutedForeground).MaxLines(1)
					}
				})
				if a.toggle(c, L("%s enabled", tool.Name), tool.IsEnabled, readOnly) && !readOnly {
					server, name, enabled := s.Name, tool.Name, !tool.IsEnabled
					v.mcpCall(server, func(cl mcpClient) error {
						_, err := cl.ToggleMCPTool(a.ctx, protocol.ToggleMCPToolParams{SessionID: v.id, ServerName: server, ToolName: name, Enabled: enabled})
						return err
					}, nil)
				}
			})
		}
	})
}

func (v *sessionView) signInNotice(c *ui.Context, s protocol.MCPServerStatusInfo) {
	a := v.a
	k, t := a.kit, a.kit.T
	name, url := s.Name, s.PendingAuthURL
	ui.Row(c).Role(ui.RoleStatus).Label(L("Sign in to %s", name)).Wrap().Gap(k.Px(8)).Margin(k.Px(4), 0, 0, 0).Padding(k.Px(6), k.Px(8)).
		Radius(k.Px(6)).Background(t.Attention.Alpha(0.1)).Children(func() {
		k.Text(c, trimOr(s.PendingAuthMessage, L("Sign in to continue")), 13, 20).Grow(1).MinWidth(0)
		l := ui.ButtonBase(c).Role(ui.RoleLink).Label(L("Open sign-in page")).Gap(k.Px(4)).Cursor(ui.CursorPointer)
		l.Children(func() {
			k.Text(c, L("Open sign-in page"), 13, 20).FontWeight(500).Underline()
			k.Icon(c, "external-link", 12, t.Foreground)
		})
		if l.Clicked() {
			c.OpenURL(url)
		}
		b := k.Button(c, kit.Ghost, 24, false, L("Cancel")).PaddingX(k.Px(8))
		b.Children(func() { k.Text(c, L("Cancel"), 12, 16).FontWeight(500) })
		if b.Clicked() {
			v.mcpCall(name, func(cl mcpClient) error {
				_, err := cl.CancelMCPAuth(a.ctx, protocol.CancelMCPAuthParams{SessionID: v.id, ServerName: name})
				return err
			}, nil)
		}
	})
}

func (v *sessionView) subHeader(c *ui.Context, title string, back func()) {
	k := v.a.kit
	ui.Row(c).Gap(k.Px(4)).Children(func() {
		if k.IconButton(c, "chevron-left", L("Back"), 24).Clicked() {
			back()
		}
		k.Text(c, title, 14, 20).Role(ui.RoleHeading).FontWeight(500)
	})
}

func (v *sessionView) addServer(params protocol.AddMCPServerParams) {
	a := v.a
	params.SessionID = v.id
	v.mcpCall(params.Name, func(cl mcpClient) error {
		_, err := cl.AddMCPServer(a.ctx, params)
		return err
	}, func(ok bool) {
		if ok {
			v.tools.view = ""
		}
	})
}

// mcpCatalogue is Factory's catalogue of servers not added yet, searchable.
func (v *sessionView) mcpCatalogue(c *ui.Context, taken []string) {
	a := v.a
	k, t := a.kit, a.kit.T
	ts := &v.tools
	ts.mu.Lock()
	state, registry, busy, actErr := ts.registryState, ts.registry, ts.busy, ts.actErr
	ts.mu.Unlock()
	ui.Column(c).Gap(k.Px(8)).Children(func() {
		v.subHeader(c, L("Add MCP server"), func() { ts.view = "" })
		ui.Row(c).Children(func() {
			in := a.field(c, &ts.query, L("Search the catalogue"), L("Search Factory's catalogue"), false, false).Padding(0, k.Px(12), 0, k.Px(32)).AutoFocus()
			_ = in
			k.Icon(c, "search", 14, t.MutedForeground).Absolute().Left(k.Px(10)).Top(k.Px(11))
		})
		if actErr != "" {
			v.toolFailure(c, actErr)
		}
		entries := mcp.FilterRegistry(registry, ts.query, taken)
		switch {
		case state <= 1:
			v.toolLoading(c, L("Loading the catalogue…"))
		case state == 3:
			k.Text(c, L("The catalogue is not available."), 14, 20).TextColor(t.MutedForeground).PaddingX(k.Px(8))
		case len(entries) == 0:
			msg := L("Everything in the catalogue is added.")
			if q := strings.TrimSpace(ts.query); q != "" {
				msg = L("Nothing in the catalogue matches “%s”.", q)
			}
			k.Text(c, msg, 14, 20).TextColor(t.MutedForeground).PaddingX(k.Px(8))
		default:
			ui.Column(c).Role(ui.RoleList).Label(L("Catalogue")).Gap(k.Px(2)).Children(func() {
				for _, e := range entries {
					entry := e
					setup := mcp.NeedsSetup(entry)
					row := ui.Row(c).Role(ui.RoleListItem).Label(entry.Name).Key("reg:"+entry.Name).AlignItems(ui.Start).Gap(k.Px(12)).
						Padding(k.Px(6), k.Px(8)).Radius(k.Px(10))
					if row.Hovered() {
						row.Background(t.Muted.Alpha(0.5))
					}
					row.Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							ui.Row(c).Gap(k.Px(6)).Children(func() {
								k.Text(c, entry.Name, 14, 20).FontWeight(500)
								v.toolBadge(c, string(entry.Type))
							})
							k.Text(c, entry.Description, 13, 20).TextColor(t.MutedForeground).MaxLines(2)
							if entry.Note != "" {
								k.Text(c, entry.Note, 12, 16).TextColor(t.MutedForeground.Alpha(0.8)).Margin(k.Px(2), 0, 0, 0)
							}
						})
						verb, label := L("Add"), L("Add %s", entry.Name)
						if setup {
							verb, label = L("Set up"), L("Set up %s", entry.Name)
						}
						b := k.Button(c, kit.Outline, 24, false, label).PaddingX(k.Px(8)).Margin(k.Px(2), 0, 0, 0).Shrink(0).Disabled(busy != "")
						b.Children(func() {
							if busy == entry.Name {
								k.Spinner(c, 12, t.Foreground)
							}
							k.Text(c, verb, 12, 16).FontWeight(500)
						})
						if b.Clicked() {
							form := mcp.FormFromRegistry(entry)
							params, err := mcp.ParseServerForm(form)
							if setup || err != nil {
								ts.view, ts.from, ts.form, ts.problem = "form", &entry, form, ""
							} else {
								v.addServer(params)
							}
						}
					})
				}
			})
		}
		ui.Row(c).Gap(k.Px(4)).Padding(k.Px(8), k.Px(8), 0, k.Px(8)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			k.Text(c, L("Not in the catalogue?"), 13, 20).TextColor(t.MutedForeground)
			b := k.Button(c, kit.Ghost, 24, false, L("Add a server by hand")).PaddingX(k.Px(8))
			b.Children(func() { k.Text(c, L("Add a server by hand"), 12, 16).FontWeight(500) })
			if b.Clicked() {
				ts.view, ts.from, ts.form, ts.problem = "form", nil, mcp.EmptyServerForm, ""
			}
		})
	})
}

// toolCountLabel is "1 tool" or "3 tools".
func toolCountLabel(n int) string {
	if n == 1 {
		return L("%d tool", n)
	}
	return L("%d tools", n)
}

var serverTypes = []kit.Option{
	{Value: "stdio", Label: l10n.N("Command (stdio)")},
	{Value: "http", Label: "HTTP"},
	{Value: "sse", Label: "SSE"},
}

// mcpForm adds a server by hand, or sets up a catalogue entry.
func (v *sessionView) mcpForm(c *ui.Context, taken []string) {
	a := v.a
	k, t := a.kit, a.kit.T
	ts := &v.tools
	ts.mu.Lock()
	busy, actErr := ts.busy, ts.actErr
	ts.mu.Unlock()
	f := &ts.form
	types := make([]kit.Option, len(serverTypes))
	for i, o := range serverTypes {
		types[i] = kit.Option{Value: o.Value, Label: l10n.T(o.Label)}
	}
	title := L("Add a server by hand")
	if ts.from != nil {
		title = L("Set up %s", ts.from.Name)
	}
	ui.Column(c).Role(ui.RoleGroup).Label(L("Add MCP server")).Gap(k.Px(8)).Children(func() {
		v.subHeader(c, title, func() { ts.view = "catalogue" })
		if ts.from != nil && ts.from.Note != "" {
			k.Text(c, ts.from.Note, 13, 20).Padding(k.Px(6), k.Px(10)).Radius(k.Px(6)).Background(t.Attention.Alpha(0.1))
		}
		ui.Row(c).Wrap().Gap(k.Px(8)).Children(func() {
			a.field(c, &f.Name, L("Server name"), L("Name"), false, false)
			if typ, ok := a.fieldSelect(c, L("Server type"), string(f.Type), types, false); ok {
				f.Type = protocol.MCPServerType(typ)
			}
		})
		if f.Type != protocol.MCPServerTypeStdio {
			a.field(c, &f.URL, L("Server URL"), "https://…", false, false)
			ui.Row(c).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Children(func() {
				k.TextArea(c, &f.Headers, L("Headers"), L("Headers, one per line: Authorization: Bearer …"),
					kit.AreaStyle{Pad: [4]float32{8, 12, 8, 12}, Size: 12, Line: 16, Mono: true, MinLines: 2, Color: t.Foreground})
			})
		} else {
			a.field(c, &f.Command, L("Command"), L("Command, e.g. npx"), true, false)
			a.field(c, &f.Args, L("Arguments"), L("Arguments, separated by spaces"), true, false)
		}
		problem := ts.problem
		if problem == "" {
			problem = actErr
		}
		if problem != "" {
			v.toolFailure(c, problem)
		}
		ui.Row(c).Gap(k.Px(4)).Justify(ui.End).Children(func() {
			if a.smallButton(c, kit.Ghost, L("Cancel"), "", false).Clicked() {
				ts.view = "catalogue"
			}
			if a.smallButton(c, kit.Primary, L("Add server"), "", busy != "").Clicked() {
				params, err := mcp.ParseServerForm(*f)
				switch {
				case err != nil:
					ts.problem = err.Error()
				case contains(taken, params.Name):
					ts.problem = L("There is already a server named \"%s\".", params.Name)
				default:
					ts.problem = ""
					v.addServer(params)
				}
			}
		})
	})
}

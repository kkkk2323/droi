package app

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/settingsview"
)

// workstreamNameMax is the longest name of a Workstream, in characters.
const workstreamNameMax = 120

// openNewWorkstream shows the New Workstream dialog, its fields blank but
// for the workspace of the Session used last.
func (a *App) openNewWorkstream() {
	s := &a.workstreams
	s.newOpen = true
	s.form = workstreamForm{symbol: sessions.DefaultWorkstreamSymbol}
	s.checks, s.typed, s.notice = nil, "", ""
	if recent := a.recentWorkspaces(a.listedNow()); len(recent) > 0 {
		s.form.workspace = recent[0].Path
	}
}

// newWorkstreamDialog is the dialog that starts a Workstream: a symbol and a
// name, and the folder its orchestrator works in. Enter in the name starts
// it.
func (a *App) newWorkstreamDialog(c *ui.Context) {
	s := &a.workstreams
	if !s.newOpen {
		return
	}
	k, t := a.kit, a.kit.T
	f := &s.form
	width, _ := c.Size()
	if utf8.RuneCountInString(f.name) > workstreamNameMax {
		f.name = string([]rune(f.name)[:workstreamNameMax])
	}
	dir := strings.TrimSpace(f.workspace)
	check := a.checkWorkspace(c, dir)
	problem := func(field string) string {
		switch {
		case f.errOn == field && f.err != "" && map[string]string{"name": f.name, "workspace": f.workspace}[field] == f.errFor:
			return f.err
		case field == "workspace":
			return check.problem
		}
		return ""
	}
	empty := strings.TrimSpace(f.name) == ""
	label := func(text string, required bool) {
		ui.Row(c).Gap(k.Px(4)).Children(func() {
			k.Text(c, strings.ToUpper(text), 11, 16).Font(k.Mono).TextColor(t.MutedForeground)
			if required {
				k.Text(c, "*", 11, 16).TextColor(t.DestructiveForeground).Label(L("required"))
			}
		})
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Role(ui.RoleDialog).Label(L("New Workstream")).Width(min(k.Px(560), width-k.Px(32))).Padding(k.Px(24)).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Shadow(0, k.Px(20), k.Px(25), -k.Px(5), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Gap(k.Px(14)).Children(func() {
			ui.Row(c).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Grow(1).Gap(k.Px(4)).Children(func() {
					k.Text(c, L("BETA"), 11, 16).FontWeight(500).TextColor(t.AttentionForeground).Font(k.Mono)
					k.Text(c, L("New Workstream"), k.Dense(16, 18), 24).Role(ui.RoleHeading).FontWeight(500)
				})
				if k.IconButton(c, "x", L("Close"), 28).Clicked() {
					open = false
				}
			})
			ui.Column(c).Gap(k.Px(6)).Children(func() {
				label(L("Workstream name"), true)
				ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
					a.symbolPicker(c, f)
					in := a.field(c, &f.name, L("Workstream name"), L("What should this workstream accomplish?"), false, false)
					in.Bind(&s.nameBox).AutoFocus()
					if problem("name") != "" {
						in.Border(1, t.Destructive.Alpha(0.6))
					}
					if in.Submitted() {
						a.startWorkstream()
					}
				})
				ui.Row(c).Justify(ui.End).Children(func() {
					k.Text(c, L("%d/%d", utf8.RuneCountInString(f.name), workstreamNameMax), 11, 16).Font(k.Mono).TextColor(t.MutedForeground).FontFeatures("tnum")
				})
				if p := problem("name"); p != "" {
					k.Text(c, p, k.Dense(12, 13), k.Dense(18, 20)).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
				}
			})
			ui.Column(c).Gap(k.Px(6)).Children(func() {
				label(L("Working directory"), false)
				ui.Row(c).Gap(k.Px(8)).Children(func() {
					in := a.field(c, &f.workspace, L("Workstream workspace"), "/Users/you/project", true, false).Bind(&s.workspaceBox)
					if problem("workspace") != "" {
						in.Border(1, t.Destructive.Alpha(0.6))
					}
					if !a.following() && a.smallButton(c, kit.Outline, L("Choose…"), "folder-open", false).Height(k.Px(k.Dense(30, 36))).Label(L("Choose a workspace")).Clicked() {
						go func() {
							if picked := a.pickFolder(L("Choose a workspace")); picked != "" {
								a.cfg.Update(func() { f.workspace = picked })
							}
						}()
					}
				})
				if p := problem("workspace"); p != "" {
					k.Text(c, p, k.Dense(12, 13), k.Dense(18, 20)).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
				} else if dir != "" {
					k.Text(c, L("The orchestrator starts in %s. Agents on this machine start there too.", dir), k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.MutedForeground)
				}
				if check.done && !check.unknown && check.problem == "" && !check.git {
					note := L("This folder is not a git repository, so agents share it instead of working in worktrees of their own.")
					ui.Row(c).Role(ui.RoleStatus).Label(note).Gap(k.Px(6)).AlignItems(ui.Start).Children(func() {
						k.Icon(c, "info", 12, t.MutedForeground).Margin(k.Px(3), 0, 0, 0)
						k.Text(c, note, k.Dense(11, 12), 18).TextColor(t.MutedForeground).Grow(1).MinWidth(0)
					})
				}
				if check.git && check.branch != "" {
					ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(4), 0, 0, 0).Children(func() {
						k.Text(c, strings.ToUpper(L("Branch")), 11, 16).Font(k.Mono).TextColor(t.MutedForeground)
						k.Icon(c, "git-branch", 12, t.MutedForeground)
						k.Text(c, check.branch, k.Dense(12, 13), 18).Font(k.Mono)
					})
				}
			})
			a.workstreamNotice(c)
			ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(6), 0, 0, 0).Children(func() {
				ui.Row(c).Grow(1).Gap(k.Px(6)).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Size(k.Px(20), k.Px(20)).Center().Radius(k.Px(4)).Border(1, t.Border).Children(func() {
						k.Icon(c, "corner-down-left", 12, t.MutedForeground)
					})
					k.Text(c, L("to start"), 11, 16).Font(k.Mono).TextColor(t.MutedForeground)
				})
				if a.smallButton(c, kit.Outline, L("Cancel"), "", false).Clicked() {
					open = false
				}
				if a.smallButton(c, kit.Primary, L("Start"), "", empty || s.acting != "").Label(L("Start")).Clicked() {
					a.startWorkstream()
				}
			})
		})
	})
	if !open {
		s.newOpen = false
	}
}

// symbolPicker is the button by the name that picks the Workstream's symbol
// from a grid of them.
func (a *App) symbolPicker(c *ui.Context, f *workstreamForm) {
	k, t := a.kit, a.kit.T
	name := f.symbol
	b := ui.ButtonBase(c).Label(L("Workstream symbol: %s", name)).Tooltip(L("Workstream symbol")).Expanded(f.symbolOpen).Size(k.Px(k.Dense(30, 36)), k.Px(k.Dense(30, 36))).Radius(k.Px(8)).
		Border(1, t.Input).Center().Cursor(ui.CursorPointer)
	if b.Hovered() || f.symbolOpen {
		b.Background(t.Muted)
	}
	b.Children(func() { k.Icon(c, name, 16, t.Foreground) })
	if b.Clicked() {
		f.symbolOpen = !f.symbolOpen
	}
	ui.PopoverBase(c, b, &f.symbolOpen, func(p ui.Element) {
		p.Role(ui.RoleDialog).Label(L("Workstream symbol")).Margin(k.Px(6), 0, 0, 0).Padding(k.Px(8)).Radius(k.Px(8)).Border(1, t.Border).Background(t.Popover).
			Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).Gap(k.Px(2)).Children(func() {
			const perRow = 6
			for start := 0; start < len(sessions.WorkstreamSymbols); start += perRow {
				ui.Row(c).Gap(k.Px(2)).Children(func() {
					for _, sym := range sessions.WorkstreamSymbols[start:min(start+perRow, len(sessions.WorkstreamSymbols))] {
						on := sym == f.symbol
						cell := kit.Selected(ui.ButtonBase(c.Key(sym)).Label(L("Workstream symbol: %s", sym)).Tooltip(sym), on).Size(k.Px(32), k.Px(32)).Radius(k.Px(6)).Center().Cursor(ui.CursorPointer)
						switch {
						case on:
							cell.Background(t.Accent)
						case cell.Hovered():
							cell.Background(t.Muted)
						}
						cell.Children(func() { k.Icon(c, sym, 16, t.Foreground) })
						if cell.Clicked() {
							f.symbol, f.symbolOpen = sym, false
						}
					}
				})
			}
		})
	})
}

// startWorkstream starts the dialog's Workstream on the computer the window
// shows, then opens its orchestrator's Session; a field in error takes the
// focus.
func (a *App) startWorkstream() {
	s := &a.workstreams
	f := &s.form
	f.err, f.errOn = "", ""
	name, workspace := strings.TrimSpace(f.name), strings.TrimSpace(f.workspace)
	switch {
	case name == "":
		f.err, f.errOn = L("Enter a name."), "name"
		s.nameBox.Focus()
	case workspace == "":
		f.err, f.errOn = L("Choose a workspace."), "workspace"
		s.workspaceBox.Focus()
	}
	if f.err == "" && s.checks[workspace].problem != "" {
		// The folder may have been made since: the Host decides, and the
		// dialog asks again.
		delete(s.checks, workspace)
		a.askWorkspace(workspace)
	}
	gw := a.cfg.Gateway
	if f.err != "" {
		f.errFor = map[string]string{"name": f.name, "workspace": f.workspace}[f.errOn]
	}
	if f.err != "" || gw == nil {
		return
	}
	in := newWorkstreamOf(name, workspace, f.symbol)
	var made gateway.WorkstreamCreated
	a.wsAct("start", func() (string, error) {
		var err error
		made, err = gw.CreateWorkstream(a.ctx, in)
		return "", err
	}, func(err error) string { return a.startFailed(err) }, func() {
		s.newOpen = false
		go a.refreshList()
		a.Go(Route{Name: "session", SessionID: made.OrchestratorID})
	})
}

// startFailed is what the dialog says when the Host would not start the
// Workstream. A Host from before a Workstream started from a name asks for a
// goal, which only an update helps.
func (a *App) startFailed(err error) string {
	var refused *gateway.StatusError
	if errors.As(err, &refused) && refused.Status == http.StatusBadRequest && strings.Contains(strings.ToLower(refused.Message), "goal") {
		if name := a.computerName(); name != "" {
			return L("%s runs an older Droi that cannot start a workstream from a name. Update Droi there.", name)
		}
		return L("This Droi is older than the window and cannot start a workstream from a name. Update Droi, then try again.")
	}
	return L("Could not start the workstream: %s", err)
}

// settingsForm is what the Workstream settings dialog's fields hold.
type settingsForm struct {
	name        string
	nameBox     ui.Handle
	symbol      workstreamForm
	orchOpen    bool
	orchPicker  pickerState
	agentOpen   bool
	agentPicker pickerState
}

// openWorkstreamSettings shows the settings dialog of Workstream id.
func (a *App) openWorkstreamSettings(id string) {
	s := &a.workstreams
	s.settingsFor = id
	s.settings = settingsForm{}
	s.settings.symbol.symbol = sessions.DefaultWorkstreamSymbol
	if w := a.findWorkstream(id); w != nil {
		s.settings.name = w.Title
		if w.Symbol != "" {
			s.settings.symbol.symbol = w.Symbol
		}
	}
	if orch := a.orchestratorSession(id); orch != "" {
		if sel := findSummary(a.listedNow(), orch); sel != nil {
			if sel.Title != "" {
				s.settings.name = sel.Title
			}
			s.settings.symbol.symbol = sessions.WorkstreamSymbol(sel.Tags)
		}
	}
	a.loadScriptModels(true)
}

// settingsRow is a row of the settings dialog: its name and what it does at
// the left, its control at the right.
func (a *App) settingsRow(c *ui.Context, title, text string, control func()) {
	k, t := a.kit, a.kit.T
	ui.Row(c).FillWidth().Gap(k.Px(16)).AlignItems(ui.Center).Padding(k.Px(12), 0).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(2)).Children(func() {
			k.Text(c, title, k.Dense(13, 14), 20).FontWeight(500)
			if text != "" {
				k.Text(c, text, k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.MutedForeground)
			}
		})
		ui.Row(c).Width(k.Px(240)).Shrink(0).Justify(ui.End).Children(control)
	})
}

// workstreamSettingsDialog is the dialog of a Workstream's name and models: the
// orchestrator's, which its Session takes from its next turn, and the
// agents', which the Host moves them to.
func (a *App) workstreamSettingsDialog(c *ui.Context) {
	s := &a.workstreams
	w := a.findWorkstream(s.settingsFor)
	if s.settingsFor == "" {
		return
	}
	k, t := a.kit, a.kit.T
	f := &s.settings
	width, _ := c.Size()
	id := s.settingsFor
	orch := a.orchestratorSession(id)
	var choices []models.Choice
	var defaultModel, defaultEffort string
	if dv := a.sessionDefaults(); dv != nil {
		choices, defaultModel, defaultEffort = dv.Models, dv.ModelID, dv.ReasoningEffort
	}
	orchModel, orchEffort := defaultModel, defaultEffort
	if os := a.sessionOf(orch); os != nil && a.loaded(orch) {
		set := os.Settings()
		if set.ModelID != "" {
			orchModel, orchEffort = set.ModelID, string(set.ReasoningEffort)
		}
		if len(choices) == 0 {
			choices = models.ToChoices(os.AvailableModels())
		}
	}
	setOrch := func(p protocol.UpdateSessionSettingsParams) {
		p.SessionID = orch
		go func() {
			cl, err := a.ctl.Client()
			if err == nil {
				_, err = cl.UpdateSessionSettings(a.ctx, p)
			}
			if err != nil {
				a.cfg.Update(func() {
					s.noticeAt, s.noticeErr, s.notice = a.cfg.Now(), true, L("Could not save the orchestrator’s model: %s", err)
				})
			}
		}()
	}
	var agentModel, agentEffort string
	if w != nil {
		agentModel, agentEffort = w.AgentModel, w.AgentReasoningEffort
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Role(ui.RoleDialog).Label(L("Workstream settings")).Width(min(k.Px(640), width-k.Px(32))).Padding(k.Px(24)).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Shadow(0, k.Px(20), k.Px(25), -k.Px(5), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				k.Text(c, L("Workstream settings"), k.Dense(16, 18), 24).Role(ui.RoleHeading).FontWeight(500).Grow(1)
				if k.IconButton(c, "x", L("Close"), 28).Clicked() {
					open = false
				}
			})
			a.settingsRow(c, L("Name"), L("Shown in the sidebar and above the workstream."), func() {
				ui.Row(c).Grow(1).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
					before := f.symbol.symbol
					a.symbolPicker(c, &f.symbol)
					if f.symbol.symbol != before {
						sym := f.symbol.symbol
						a.editWorkstream(id, nil, &sym)
					}
					in := a.field(c, &f.name, L("Workstream name"), "", false, false).Bind(&f.nameBox)
					if in.Submitted() {
						title := f.name
						a.editWorkstream(id, &title, nil)
					}
				})
			})
			k.Text(c, L("Models"), k.Dense(12, 13), 18).FontWeight(500).TextColor(t.MutedForeground).Margin(k.Px(14), 0, 0, 0)
			a.settingsRow(c, L("Orchestrator model"), L("Plans the workstream and coordinates its agents. Applies from its next turn."), func() {
				settingsview.ModelEffortField(c, a.kit, a.prefs, L("Orchestrator model"), "", orch == "", &f.orchOpen, &f.orchPicker, choices, orchModel, orchEffort,
					func(next string) { setOrch(protocol.UpdateSessionSettingsParams{ModelID: next}) },
					func(next string) {
						setOrch(protocol.UpdateSessionSettingsParams{ReasoningEffort: protocol.ReasoningEffort(next)})
					})
			})
			a.settingsRow(c, L("Agent model"), L("Model for all agents. Running agents switch at their next turn; Inherit applies only to new agents."), func() {
				settingsview.ModelEffortField(c, a.kit, a.prefs, L("Agent model"), L("Inherit"), w == nil, &f.agentOpen, &f.agentPicker, choices, agentModel, agentEffort,
					func(next string) { a.saveAgentModel(id, next, fitEffort(choices, next, agentEffort)) },
					func(next string) { a.saveAgentModel(id, agentModel, next) })
			})
			a.scriptHint(c, orchModel, agentModel)
			a.workstreamNotice(c)
		})
	})
	if !open {
		if w != nil {
			title := f.name
			a.editWorkstream(id, &title, nil)
		}
		s.settingsFor = ""
	}
}

// scriptHint tells that the model the agents will use runs only Direct tools,
// and offers to allow Script for it on this computer.
func (a *App) scriptHint(c *ui.Context, orchModel, agentModel string) {
	model := agentModel
	if model == "" {
		model = orchModel
	}
	if model == "" || a.scriptCapable(model) != scriptNo {
		return
	}
	k, t := a.kit, a.kit.T
	text := L("This model can only use Direct tools: it cannot run tools as scripts.")
	ui.Row(c).Role(ui.RoleStatus).Label(text).FillWidth().Gap(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(10), 0, 0, 0).Padding(k.Px(8), k.Px(10)).Radius(k.Px(8)).
		Background(t.Attention.Alpha(0.08)).Children(func() {
		k.Icon(c, "info", 14, t.Attention)
		k.Text(c, text, k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.MutedForeground).Grow(1).MinWidth(0)
		if !a.following() && a.smallButton(c, kit.Outline, L("Allow Script"), "", false).Clicked() {
			list := prefs.ScriptModels.Get(a.prefs)
			if !slices.Contains(list, model) {
				prefs.ScriptModels.Set(a.prefs, append(list, model))
			}
			a.loadScriptModels(true)
		}
	})
}

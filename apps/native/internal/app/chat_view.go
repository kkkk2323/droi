package app

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge"
	"github.com/kkkk2323/droi/apps/native/internal/defaults"
	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/apps/native/internal/models"
)

// ChatBridge is the Host's Chat Bridge that Settings → Chat Bridge reaches
// (ADR 0020).
type ChatBridge interface {
	Status() chatbridge.Status
	// Restart connects again with the settings as they are now.
	Restart()
}

// chatState is what Settings → Chat Bridge keeps between frames: the robot
// being typed, and a save waiting for its Daemon restart.
type chatState struct {
	loaded                         bool
	botID, secret, workspace, user string
	err                            string
	pending                        *host.ChatBridgeSettings
}

// sessionDefaultModel stands for an empty model: the Session defaults'.
const sessionDefaultModel = "session-default"

// chatTab is Settings → Chat Bridge: the robot and its connection, who may
// talk to it, and how Chat Sessions run.
func (a *App) chatTab(c *ui.Context) {
	st := a.cfg.Host.Settings.Get().ChatBridge
	cs := &a.chat
	if !cs.loaded {
		cs.botID, cs.workspace, cs.loaded = st.BotID, st.Workspace, true
	}
	a.settingGroup(c, "", "",
		func() { a.chatConnectionRow(c) },
		func() { a.chatRobotRow(c, st) },
	)
	users := []func(){func() { a.chatAllowedRow(c, st.AllowedUsers) }}
	if len(st.RefusedUsers) > 0 {
		users = append(users, func() { a.chatRefusedRow(c, st.RefusedUsers) })
	}
	a.settingGroup(c, L("Who may talk to the robot"), "", users...)
	a.settingGroup(c, L("Chat Sessions"), L("Changes apply to Chat Sessions started from now on."),
		func() { a.chatAutonomyRow(c, st.AutonomyLevelOrDefault()) },
		func() { a.chatModelRow(c, st.Model) },
	)
}

func (a *App) chatConnectionRow(c *ui.Context) {
	k, t := a.kit, a.kit.T
	status := chatbridge.Status{State: chatbridge.Off}
	if a.cfg.ChatBridge != nil {
		status = a.cfg.ChatBridge.Status()
	}
	label, desc := L("Off"), L("Set a robot below to run what people ask it in WeCom as Chat Sessions on this computer.")
	switch status.State {
	case chatbridge.Connecting:
		label, desc = L("Connecting"), L("Connecting to WeCom…")
	case chatbridge.Connected:
		label, desc = L("Connected"), L("Messages to the robot reach this computer over WeCom’s long connection; it needs no public address.")
	case chatbridge.NotConnected:
		label, desc = L("Not connected"), L("The Chat Bridge tries again by itself; save the robot to try now.")
	}
	a.settingRowWith(c, func() {
		k.Text(c, L("Connection"), 14, 20).Role(ui.RoleHeading).FontWeight(500)
	}, desc, func() { a.statusPill(c, status.State == chatbridge.Connected, label) }, func() {
		if status.State != chatbridge.NotConnected || status.Reason == "" {
			return
		}
		k.Text(c, status.Reason, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).Selectable().Margin(-k.Px(4), 0, 0, 0)
	})
}

func (a *App) chatRobotRow(c *ui.Context, st host.ChatBridgeSettings) {
	k, t := a.kit, a.kit.T
	cs := &a.chat
	line := func(label string, field func()) {
		ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
			k.Text(c, label, 13, 20).TextColor(t.MutedForeground).SingleLine().Width(k.Px(88)).Shrink(0)
			field()
		})
	}
	submitted := false
	a.settingRow(c, L("Robot"), L("The Bot ID and Secret of a WeCom smart robot in API mode, set to use the long connection. Every Chat Session works in the Workspace. Clear the Bot ID to turn the Chat Bridge off."), nil, func() {
		ui.Column(c).Gap(k.Px(8)).Children(func() {
			line(L("Bot ID"), func() {
				submitted = a.field(c, &cs.botID, L("WeCom Bot ID"), "aib…", true, false).Submitted() || submitted
			})
			line(L("Secret"), func() {
				placeholder := ""
				if st.Secret != "" {
					placeholder = L("•••••••••••• (Secret stored)")
				}
				submitted = a.field(c, &cs.secret, L("WeCom Secret"), placeholder, false, true).Submitted() || submitted
			})
			line(L("Workspace"), func() {
				submitted = a.field(c, &cs.workspace, L("Chat Workspace folder"), L("Full path of a folder"), true, false).Submitted() || submitted
				if a.smallButton(c, kit.Outline, L("Choose…"), "", false).Label(L("Choose the Chat Workspace")).Clicked() {
					go func() {
						if dir := a.pickFolder(L("Choose the Chat Workspace")); dir != "" {
							a.cfg.Update(func() { cs.workspace = dir })
						}
					}()
				}
			})
			if cs.pending != nil {
				ui.Row(c).Role(ui.RoleGroup).Label(L("Turn the Chat Bridge on")).Wrap().AlignItems(ui.Center).Gap(k.Px(8)).Margin(k.Px(4), 0, 0, 0).Children(func() {
					k.Text(c, L("Turn the Chat Bridge on? The Daemon restarts so that Chat Sessions can use their droi-chat tools without asking. Sessions that are working stop."), 13, 20).
						TextColor(t.MutedForeground).Grow(1).Basis(0).MinWidth(k.Px(192))
					if a.smallButton(c, kit.Primary, L("Restart Daemon"), "", false).Clicked() {
						next := *cs.pending
						cs.pending = nil
						a.applyChatRobot(next, true)
					}
					if a.smallButton(c, kit.Ghost, L("Cancel"), "", false).Clicked() {
						cs.pending = nil
					}
				})
				return
			}
			ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(4), 0, 0, 0).Children(func() {
				if a.smallButton(c, kit.Outline, L("Save"), "", false).Label(L("Save robot")).Clicked() {
					submitted = true
				}
				if cs.err != "" {
					k.Text(c, cs.err, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).Grow(1).Basis(0)
				}
			})
		})
	})
	if submitted {
		a.saveChatRobot(st)
	}
}

// saveChatRobot saves the robot as typed. Turning the Chat Bridge on asks
// first: the Daemon reads its pre-approval of droi-chat when it starts.
func (a *App) saveChatRobot(st host.ChatBridgeSettings) {
	cs := &a.chat
	next := st
	next.BotID = strings.TrimSpace(cs.botID)
	if s := strings.TrimSpace(cs.secret); s != "" {
		next.Secret = s
	}
	next.Workspace = strings.TrimSpace(cs.workspace)
	cs.err = ""
	switch {
	case next.Workspace != "" && !filepath.IsAbs(next.Workspace):
		cs.err = L("The Workspace needs a full path.")
		return
	case next.On() && next.Secret == "":
		cs.err = L("The robot needs its Secret.")
		return
	case next.On() && next.Workspace == "":
		cs.err = L("Chat Sessions need a Workspace.")
		return
	}
	if next.On() && !st.On() {
		cs.pending = &next
		return
	}
	a.applyChatRobot(next, false)
}

// applyChatRobot saves the robot and restarts the Chat Bridge with it.
func (a *App) applyChatRobot(next host.ChatBridgeSettings, restartDaemon bool) {
	h := a.cfg.Host
	if err := h.Settings.Update(func(s *host.Settings) {
		s.ChatBridge.BotID, s.ChatBridge.Secret, s.ChatBridge.Workspace = next.BotID, next.Secret, next.Workspace
	}); err != nil {
		a.chat.err = err.Error()
		return
	}
	a.chat.secret = ""
	if a.cfg.ChatBridge != nil {
		a.cfg.ChatBridge.Restart()
	}
	h.Changed()
	if restartDaemon {
		go h.RestartDaemon()
	}
}

func (a *App) chatAllowedRow(c *ui.Context, allowed []string) {
	k, t := a.kit, a.kit.T
	cs := &a.chat
	add := func() {
		id := strings.TrimSpace(cs.user)
		if id == "" {
			return
		}
		cs.user = ""
		a.hostUpdate(false, func(s *host.Settings) { allowChatUser(s, id) })
	}
	desc := L("WeCom user ids as WeCom sends them, often encrypted per company. Nobody else can start a Chat Session or answer its Prompts.")
	a.settingRow(c, L("Allowed users"), desc, nil, func() {
		ui.Column(c).Role(ui.RoleList).Label(L("Allowed users")).Gap(k.Px(8)).Children(func() {
			if len(allowed) == 0 {
				k.Text(c, L("Nobody yet: the robot refuses every sender."), 13, 20).TextColor(t.MutedForeground)
			}
			for _, id := range allowed {
				ui.Row(c.Key("allowed:" + id)).Role(ui.RoleListItem).Label(id).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
					k.Text(c, id, 13, 20).Font(k.Mono).TextColor(t.Foreground).SingleLine().Selectable().Grow(1).MinWidth(0)
					if k.IconButton(c, "x", L("Remove %s", id), 32).Clicked() {
						a.hostUpdate(false, func(s *host.Settings) {
							s.ChatBridge.AllowedUsers = slices.DeleteFunc(s.ChatBridge.AllowedUsers, func(u string) bool { return u == id })
						})
					}
				})
			}
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				if a.field(c, &cs.user, L("WeCom user id"), L("WeCom user id"), true, false).Submitted() {
					add()
				}
				if a.smallButton(c, kit.Outline, L("Add"), "", strings.TrimSpace(cs.user) == "").Label(L("Add user")).Clicked() {
					add()
				}
			})
		})
	})
}

func (a *App) chatRefusedRow(c *ui.Context, refused []string) {
	k, t := a.kit, a.kit.T
	a.settingRow(c, L("Refused senders"), L("People the robot turned away, newest first. Allow one and their next message starts a Chat Session."), nil, func() {
		ui.Column(c).Role(ui.RoleList).Label(L("Refused senders")).Gap(k.Px(8)).Children(func() {
			for _, id := range slices.Backward(refused) {
				ui.Row(c.Key("refused:" + id)).Role(ui.RoleListItem).Label(id).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
					k.Text(c, id, 13, 20).Font(k.Mono).TextColor(t.Foreground).SingleLine().Selectable().Grow(1).MinWidth(0)
					if a.smallButton(c, kit.Outline, L("Allow"), "", false).Label(L("Allow %s", id)).Clicked() {
						a.hostUpdate(false, func(s *host.Settings) { allowChatUser(s, id) })
					}
				})
			}
		})
	})
}

// allowChatUser puts id on the Chat Bridge's allowlist and takes it off the
// refused senders.
func allowChatUser(s *host.Settings, id string) {
	if !slices.Contains(s.ChatBridge.AllowedUsers, id) {
		s.ChatBridge.AllowedUsers = append(s.ChatBridge.AllowedUsers, id)
	}
	s.ChatBridge.RefusedUsers = slices.DeleteFunc(s.ChatBridge.RefusedUsers, func(u string) bool { return u == id })
}

func (a *App) chatAutonomyRow(c *ui.Context, level string) {
	opts := make([]kit.Option, len(autonomyLevels))
	for i, l := range autonomyLevels {
		opts[i] = kit.Option{Value: l, Label: models.AutonomyLabels[l]}
	}
	a.settingRow(c, L("Autonomy level"), L("How much Droid may do in a Chat Session before it asks in the Chat Thread."), func() {
		if next, ok := a.fieldSelect(c, l10n.N("Chat autonomy level"), level, opts, false); ok {
			a.hostUpdate(false, func(s *host.Settings) { s.ChatBridge.AutonomyLevel = next })
		}
	}, nil)
}

func (a *App) chatModelRow(c *ui.Context, model string) {
	var choices []models.Choice
	if dv := a.sessionDefaults(); dv != nil {
		choices = dv.Models
	}
	opts := []kit.Option{{Value: sessionDefaultModel, Label: L("Session default")}}
	for _, m := range defaults.PickableModels(choices, true) {
		opts = append(opts, kit.Option{Value: m.ID, Label: m.Label})
	}
	value := model
	if value == "" {
		value = sessionDefaultModel
	}
	a.settingRow(c, L("Model"), L("Session default is the model new Sessions start with on this computer."), func() {
		if next, ok := a.fieldSelect(c, l10n.N("Chat model"), value, opts, false); ok {
			if next == sessionDefaultModel {
				next = ""
			}
			a.hostUpdate(false, func(s *host.Settings) { s.ChatBridge.Model = next })
		}
	}, nil)
}

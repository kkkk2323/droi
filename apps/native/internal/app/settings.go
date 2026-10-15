package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/alerts"
	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/settingsview"
)

// settingsTab is a section of Settings.
type settingsTab struct {
	id, label, icon string
	needsHost       bool
}

var settingsTabs = []settingsTab{
	{"account", l10n.N("Account"), "user-round", true},
	{"general", l10n.N("General"), "settings-2", false},
	{"computer", l10n.N("Computer"), "laptop", false},
	{"defaults", l10n.N("Session defaults"), "sliders-horizontal", false},
	{"memory", l10n.N("Memory"), "brain", true},
	{"worktrees", l10n.N("Worktrees"), "git-fork", false},
	{"notifications", l10n.N("Notifications"), "bell", false},
	{"remote", l10n.N("Remote Access"), "smartphone", true},
	{"advanced", l10n.N("Advanced"), "wrench", true},
	{"about", l10n.N("About"), "info", false},
}

// settingsState is Settings' own: the page to go back to, the shared
// sections' state, and what is being typed in its fields.
type settingsState struct {
	*settingsview.Settings
	back Route

	apiKey, baseURL, droidPath, prompt, scratch string
	// computerLink is the pairing link being typed; the check of one runs
	// off the UI thread, and a refusal stays until the next try.
	computerLink, computerErr string
	computerChecking          bool
	loaded                    bool
	err                       string
	worktrees                 managedWorktrees
}

// settingsPage is the SettingsPage: the sections down the left, the one
// shown on the right.
func (a *App) settingsPage(c *ui.Context, status controller.Status) {
	k, t := a.kit, a.kit.T
	s := &a.settings
	var tabs []settingsTab
	for _, tab := range settingsTabs {
		if a.cfg.Host != nil || !tab.needsHost {
			tabs = append(tabs, tab)
		}
	}
	current := tabs[0]
	for _, tab := range tabs {
		if tab.id == a.route.Tab {
			current = tab
		}
	}
	if !s.loaded && a.cfg.Host != nil {
		st := a.cfg.Host.Settings.Get()
		s.baseURL, s.droidPath = derefOr(st.FactoryAPIBaseURL), derefOr(st.DroidPath)
		s.prompt, s.scratch = derefOr(st.AppendSystemPrompt), derefOr(st.ScratchFolder)
		s.loaded = true
	}
	if c.Shortcut(0, ui.KeyEscape) {
		a.Go(s.back)
	}
	ui.Row(c).Role(ui.RoleGroup).Label(L("Settings")).Fill().AlignItems(ui.Stretch).Background(t.Sidebar).TextColor(t.Foreground).Children(func() {
		ui.Column(c).Role(ui.RoleGroup).Label(L("Settings sections")).Width(240).Shrink(0).Gap(k.Px(4)).Padding(44, k.Px(8), k.Px(12), k.Px(8)).
			BorderWidth(0, 1, 0, 0).BorderColor(t.Border).DragWindow().Children(func() {
			nav := func(icon, label string, selected bool) ui.Element {
				b := ui.ButtonBase(c).Label(label).Height(k.Px(32)).Gap(k.Px(8)).PaddingX(k.Px(8)).Radius(k.Px(10)).Justify(ui.Start).Cursor(ui.CursorPointer)
				color := t.SidebarForeground
				switch {
				case selected:
					kit.Selected(b, true).Background(t.SidebarAccent)
					color = t.SidebarAccentFg
				case b.Hovered():
					b.Background(t.SidebarAccent.Alpha(0.6))
				}
				if icon == "arrow-left" {
					color = t.MutedForeground
					if b.Hovered() {
						color = t.Foreground
					}
				}
				b.Children(func() {
					k.Icon(c, icon, 16, t.MutedForeground)
					k.Text(c, label, 13, 19.5).TextColor(color).SingleLine()
				})
				return b
			}
			if nav("arrow-left", L("Back"), false).Margin(0, 0, k.Px(8), 0).Clicked() {
				a.Go(s.back)
			}
			for _, tab := range tabs {
				if nav(tab.icon, l10n.T(tab.label), tab.id == current.id).Key(tab.id).Clicked() {
					a.route.Tab = tab.id
				}
			}
			if a.following() {
				k.Text(c, L("Account, Memory, Remote Access and Advanced are changed on %s.", a.computerName()), 12, 18).Role(ui.RoleStatus).
					TextColor(t.MutedForeground).Padding(k.Px(8), k.Px(8), 0, k.Px(8))
			}
		})
		ui.Scroll(c).Grow(1).MinWidth(0).FillHeight().Background(t.Background).Children(func() {
			ui.Box(c).Height(44).DragWindow()
			ui.Column(c).FillWidth().MaxWidth(k.Px(672)).AlignSelf(ui.Center).Gap(k.Px(24)).Padding(0, k.Px(32), k.Px(48), k.Px(32)).Children(func() {
				k.Text(c, l10n.T(current.label), 18, 28).Role(ui.RoleHeading).FontWeight(600).LetterSpacing(-k.Px(0.45))
				if s.err != "" {
					k.Text(c, s.err, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
				}
				switch current.id {
				case "account":
					a.accountTab(c)
				case "general":
					a.generalTab(c)
				case "computer":
					a.computerTab(c)
				case "defaults":
					a.defaultsTab(c)
				case "memory":
					a.memoryTab(c)
				case "worktrees":
					a.worktreesTab(c)
				case "notifications":
					a.notificationsTab(c)
				case "remote":
					a.remoteTab(c)
				case "advanced":
					a.advancedTab(c)
				case "about":
					a.aboutTab(c)
				}
			})
		})
	})
}

func derefOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// settingGroup is one card of related settings, a divider between rows.
func (a *App) settingGroup(c *ui.Context, title, footer string, rows ...func()) {
	a.kit.SettingGroup(c, title, footer, rows...)
}

func (a *App) settingRow(c *ui.Context, title, description string, control, below func()) {
	a.kit.SettingRow(c, title, description, control, below)
}

func (a *App) settingRowWith(c *ui.Context, title func(), description string, control, below func()) {
	a.kit.SettingRowWith(c, title, description, control, below)
}

// fieldSelect is a settings row's Select, its list kept open from frame to
// frame by its label.
func (a *App) fieldSelect(c *ui.Context, label, value string, options []kit.Option, disabled bool) (string, bool) {
	return a.settings.Select(c, label, value, options, disabled)
}

func (a *App) toggle(c *ui.Context, label string, on, disabled bool) bool {
	return a.kit.Toggle(c, label, on, disabled)
}

func (a *App) field(c *ui.Context, value *string, label, placeholder string, mono, password bool) ui.Element {
	return a.kit.Field(c, value, label, placeholder, mono, password)
}

func (a *App) smallButton(c *ui.Context, v kit.Variant, label, icon string, disabled bool) ui.Element {
	return a.kit.SmallButton(c, v, label, icon, disabled)
}

func (a *App) statusPill(c *ui.Context, ok bool, label string) { a.kit.StatusPill(c, ok, label) }

func (a *App) generalTab(c *ui.Context) {
	a.settings.General(c, a.applyPrefs)
	a.settingGroup(c, L("Sidebar"), "", func() {
		a.settingRow(c, L("Show archived sessions"), L("List archived sessions in the sidebar alongside the active ones."), func() {
			on := prefs.ShowArchived.Get(a.prefs)
			if a.toggle(c, L("Show archived sessions"), on, false) {
				prefs.ShowArchived.Set(a.prefs, !on)
				go a.reloadList()
			}
		}, nil)
	})
}

func (a *App) aboutTab(c *ui.Context) {
	version := a.cfg.Version
	a.settingGroup(c, "", "",
		func() {
			var control, below func()
			if a.cfg.Updater != nil {
				control = func() { a.updateControl(c) }
				if s := a.cfg.Updater.State(); showsNotes(s) {
					below = func() { a.updateNotes(c, s) }
				}
			}
			a.settingRow(c, L("Droi %s", version), "", control, below)
		},
		func() {
			a.settingRow(c, L("Where your data lives"), L("Sessions, settings and the Factory API key stay on this computer; phones reach it through the Gateway. What Droid works on goes to Factory, which runs the models."), nil, nil)
		},
	)
}

// hostUpdate saves Shell settings and restarts the Daemon when they change
// how it runs.
func (a *App) hostUpdate(restart bool, fn func(*host.Settings)) {
	h := a.cfg.Host
	if err := h.Settings.Update(fn); err != nil {
		a.settings.err = err.Error()
		return
	}
	h.Changed()
	if restart {
		go h.RestartDaemon()
	}
}

func (a *App) accountTab(c *ui.Context) {
	login := a.cfg.Host.LoginState()
	a.openSignInPage(c, login)
	a.accountsGroup(c, login)
	a.settingGroup(c, "", "", func() { a.apiKeyRow(c, login) })
}

func (a *App) apiKeyRow(c *ui.Context, login host.LoginState) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	s := &a.settings
	fromEnv := a.cfg.Env != nil && a.cfg.Env("FACTORY_API_KEY") != ""
	hasKey := h.Settings.APIKey() != ""
	signedIn := login.Status == host.SignedIn
	desc := L("Only used when neither Droi nor the droid CLI is signed in. Stored on this computer; phones never see it.")
	if fromEnv {
		desc = L("Supplied by FACTORY_API_KEY in the environment; the stored key is ignored.")
	}
	pill := L("Missing")
	switch {
	case hasKey:
		pill = L("Set")
	case signedIn:
		pill = L("Not needed")
	}
	save := func(key string) {
		a.hostUpdate(!signedIn, func(st *host.Settings) { st.APIKey = host.OptString(key) })
		s.apiKey = ""
	}
	a.settingRow(c, L("Factory API key"), desc, func() { a.statusPill(c, hasKey, pill) }, func() {
		ui.Row(c).Gap(k.Px(8)).Children(func() {
			placeholder := "fk-…"
			if hasKey {
				placeholder = L("•••••••••••• (key stored)")
			}
			in := a.field(c, &s.apiKey, L("Factory API key"), placeholder, false, true).Disabled(fromEnv)
			if in.Submitted() && strings.TrimSpace(s.apiKey) != "" {
				save(s.apiKey)
			}
			if a.smallButton(c, kit.Outline, L("Save"), "", strings.TrimSpace(s.apiKey) == "" || fromEnv).Label(L("Save key")).Clicked() {
				save(s.apiKey)
			}
			if hasKey && !fromEnv && a.smallButton(c, kit.Outline, L("Remove"), "", false).Clicked() {
				save("")
			}
		})
		msg := L("No key set. The Daemon cannot authenticate without one.")
		switch {
		case hasKey:
			msg = L("A key is set.")
		case signedIn:
			msg = L("No key set. None is needed while signed in.")
		}
		k.Text(c, msg, 12, 16).Role(ui.RoleStatus).Label(L("API key status")).TextColor(t.MutedForeground).Margin(k.Px(8), 0, 0, 0)
	})
}

func daemonStatusText(st host.DaemonState) (ok bool, label, detail string) {
	switch st.Status {
	case host.DaemonRunning:
		return true, L("Running"), L("Listening on 127.0.0.1:%d (pid %d).", st.Port, st.PID)
	case host.DaemonStarting:
		if st.Attempt > 1 {
			return false, L("Starting"), L("Waiting for the Daemon to answer on port %d (attempt %d).", st.Port, st.Attempt)
		}
		return false, L("Starting"), L("Waiting for the Daemon to answer on port %d.", st.Port)
	case host.DaemonRestarting:
		return false, L("Not running"), L("Last attempt: %s. Starting again in %ds (attempt %d).", st.Reason, int(st.Delay.Round(time.Second)/time.Second), st.Attempt)
	}
	return false, L("Stopped"), L("The Daemon is not running.")
}

func (a *App) advancedTab(c *ui.Context) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	s := &a.settings
	st := h.Settings.Get()
	saveRow := func(value *string, label, placeholder, button string, mono bool, save func(string)) func() {
		return func() {
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				in := a.field(c, value, label, placeholder, mono, false)
				if in.Submitted() {
					save(*value)
				}
				if a.smallButton(c, kit.Outline, L("Save"), "", false).Label(button).Clicked() {
					save(*value)
				}
			})
		}
	}
	a.settingGroup(c, L("Daemon"), "",
		func() {
			ok, label, detail := daemonStatusText(h.Daemon.State())
			a.settingRow(c, L("Status"), detail, func() {
				a.statusPill(c, ok, label)
				if a.smallButton(c, kit.Outline, L("Restart Daemon"), "refresh-cw", false).Clicked() {
					go h.RestartDaemon()
				}
			}, func() {
				log := host.ReadDaemonLogTail(h.DaemonLogPath())
				if ok || log == "" {
					return
				}
				ui.Scroll(c).MaxHeight(k.Px(160)).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Children(func() {
					k.Text(c, log, 12, 20).Font(k.Mono).Label(L("Daemon log")).TextColor(t.MutedForeground).Selectable().Padding(k.Px(8), k.Px(12))
				})
				if a.smallButton(c, kit.Ghost, L("Show log file"), "", false).AlignSelf(ui.Start).Margin(k.Px(8), 0, 0, 0).Clicked() && a.cfg.ShowInFolder != nil {
					a.cfg.ShowInFolder(h.DaemonLogPath())
				}
			})
		},
		func() {
			found := h.DroidPath()
			desc := L("droid was not found on PATH or in ~/.local/bin. Enter its full path.")
			if found != "" {
				desc = L("Using %s", found)
			}
			label := L("Not found")
			if found != "" {
				label = L("Found")
			}
			a.settingRow(c, L("droid executable"), desc, func() { a.statusPill(c, found != "", label) },
				saveRow(&s.droidPath, L("droid path override"), L("Leave empty to auto-detect"), L("Save path"), true, func(v string) {
					a.hostUpdate(true, func(st *host.Settings) { st.DroidPath = host.OptString(v) })
				}))
		},
	)
	a.settingGroup(c, L("Factory API"), "", func() {
		desc := L("Leave empty to talk to Factory directly. Set it to route the Daemon through a local proxy such as droid-proxy (for example http://127.0.0.1:37650).")
		inherited := ""
		if a.cfg.Env != nil {
			inherited = a.cfg.Env("FACTORY_API_BASE_URL")
		}
		switch {
		case st.FactoryAPIBaseURL != nil:
			desc = L("The Daemon sends its Factory API traffic to this URL (FACTORY_API_BASE_URL). Use it for a local proxy such as droid-proxy.")
		case inherited != "":
			desc = L("Inherited from the environment: %s. Set a value here to override it.", inherited)
		}
		placeholder := "https://api.factory.ai"
		if inherited != "" {
			placeholder = inherited
		}
		a.settingRow(c, L("Factory API base URL"), desc, nil, saveRow(&s.baseURL, L("Factory API base URL"), placeholder, L("Save URL"), true, func(v string) {
			a.hostUpdate(true, func(st *host.Settings) { st.FactoryAPIBaseURL = host.OptString(v) })
		}))
	})
	a.settingGroup(c, L("New Sessions in Droi"), L("Droi’s own settings. They apply to new Sessions from every Client of this computer, not to the droid CLI or the Factory App."),
		func() {
			a.settingRow(c, L("Added to the system prompt"), L("Appended to Droid's own system prompt in every new Session, whether it starts here, in a browser or on a phone. Existing Sessions and subagents keep theirs."), nil, func() {
				ui.Row(c).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Children(func() {
					k.TextArea(c, &s.prompt, L("Added to the system prompt"), L("e.g. Reply in the language I write in. Prefer small, reviewable commits."),
						kit.AreaStyle{Pad: [4]float32{8, 12, 8, 12}, Size: 14, Line: 20, MinLines: 4, Color: t.Foreground})
				})
				stored := derefOr(st.AppendSystemPrompt)
				ui.Row(c).Gap(k.Px(8)).Margin(k.Px(8), 0, 0, 0).Children(func() {
					if a.smallButton(c, kit.Outline, L("Save"), "", strings.TrimSpace(s.prompt) == stored).Label(L("Save system prompt")).Clicked() {
						a.hostUpdate(false, func(st *host.Settings) { st.AppendSystemPrompt = host.OptString(s.prompt) })
					}
					if stored != "" && a.smallButton(c, kit.Ghost, L("Clear"), "", false).Clicked() {
						s.prompt = ""
						a.hostUpdate(false, func(st *host.Settings) { st.AppendSystemPrompt = nil })
					}
				})
			})
		},
		func() {
			a.settingRow(c, L("Scratch folder"), L("A session that does not work in a project gets a new folder of its own in %s. Archiving it moves the folder to the Trash. A change applies to new sessions only.", h.ScratchFolder()), nil,
				saveRow(&s.scratch, L("Scratch folder"), "~/.droi/chats", L("Save folder"), true, func(v string) {
					a.hostUpdate(false, func(st *host.Settings) { st.ScratchFolder = host.OptString(v) })
				}))
		},
	)
}

type defaultsPatch = defaults.Patch

// saveDefaults shows the patched defaults at once and has the Daemon save them.
func (a *App) saveDefaults(dv defaults.View, p defaults.Patch) { a.settings.SaveDefaults(dv, p) }

// pickFolder asks for a folder; "" when none was chosen. Off the main thread.
func (a *App) pickFolder(title string) string {
	if a.cfg.PickFolder != nil {
		return a.cfg.PickFolder(title)
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, Directory: true})
	if err != nil || len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// defaultsTab is the Session defaults, with the notice that the Daemon
// runs an older droid than the one installed.
func (a *App) defaultsTab(c *ui.Context) {
	note := ""
	if a.following() {
		note = L("These are the settings of %s. Existing Sessions keep their settings.", a.computerName())
	}
	a.settings.SessionDefaults(c, note, func() {
		h := a.cfg.Host
		if h == nil || !h.DroidUpdated() {
			return
		}
		a.settingGroup(c, "", "", func() {
			a.settingRow(c, L("droid was updated"), L("The Daemon still runs the earlier version, so models added since are missing here and on the New session page. Restarting it interrupts Sessions that are working."), func() {
				if a.smallButton(c, kit.Primary, L("Restart Daemon"), "refresh-cw", false).Clicked() {
					go h.RestartDaemon()
				}
			}, nil)
		})
	})
}

// customSoundRow is the chosen file under a Sound row set to Custom, nil
// otherwise so the row keeps its height.
func (a *App) customSoundRow(c *ui.Context, choice string, custom *string, pick func()) func() {
	if choice != "custom" || custom == nil || *custom == "" {
		return nil
	}
	return func() {
		k, t := a.kit, a.kit.T
		ui.Row(c).Gap(k.Px(8)).Children(func() {
			k.Text(c, L("Custom file: %s", filepath.Base(*custom)), 13, 19.5).TextColor(t.MutedForeground).SingleLine().Grow(1).MinWidth(0).Tooltip(*custom)
			if a.smallButton(c, kit.Outline, L("Choose…"), "", false).Clicked() {
				pick()
			}
		})
	}
}

// alertPrefs are the sound and notification settings, as the web
// Client's AlertPreferences keep them under droi.alerts.
type alertPrefs struct {
	CompletionSound          string  `json:"completionSound"`
	AwaitingInputSound       string  `json:"awaitingInputSound"`
	CustomCompletionSound    *string `json:"customCompletionSound"`
	CustomAwaitingInputSound *string `json:"customAwaitingInputSound"`
	FocusMode                string  `json:"focusMode"`
	NotifyOnComplete         bool    `json:"notifyOnComplete"`
	NotifyOnWaitingForInput  bool    `json:"notifyOnWaitingForInput"`
}

var soundChoices = []string{"off", "bell", "fx-ok01", "fx-ack01", "custom"}

// soundFileExtensions are the audio files a custom sound may be.
var soundFileExtensions = []string{"wav", "mp3", "m4a", "aac", "ogg", "flac", "aif", "aiff"}

var soundLabels = map[string]string{"off": l10n.N("Off"), "bell": l10n.N("Bell"), "fx-ok01": l10n.N("Soft chime"), "fx-ack01": l10n.N("Acknowledge tone"), "custom": l10n.N("Custom…")}

func (a *App) alertPrefs() alertPrefs {
	p := alertPrefs{CompletionSound: "fx-ok01", AwaitingInputSound: "fx-ack01", FocusMode: "always", NotifyOnComplete: true, NotifyOnWaitingForInput: true}
	if raw := prefs.Alerts.Get(a.prefs); raw != "" {
		_ = json.Unmarshal([]byte(raw), &p)
	}
	return p
}

func (a *App) setAlertPrefs(p alertPrefs) {
	b, _ := json.Marshal(p)
	prefs.Alerts.Set(a.prefs, string(b))
}

func (a *App) notificationsTab(c *ui.Context) {
	p := a.alertPrefs()
	soundRow := func(title, label, desc string, value *string, custom **string, def string) func() {
		pick := func() {
			go func() {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: L("Choose a sound"), Filters: []mygo.FileFilter{{Name: L("Audio"), Extensions: soundFileExtensions}}})
				if err != nil || len(paths) == 0 {
					return
				}
				a.cfg.Update(func() {
					p := a.alertPrefs()
					path := paths[0]
					if label == "Completion sound" {
						p.CompletionSound, p.CustomCompletionSound = "custom", &path
					} else {
						p.AwaitingInputSound, p.CustomAwaitingInputSound = "custom", &path
					}
					a.setAlertPrefs(p)
				})
			}()
		}
		return func() {
			opts := make([]kit.Option, len(soundChoices))
			for i, s := range soundChoices {
				l := l10n.T(soundLabels[s])
				if s == def {
					l = L("%s (default)", l)
				}
				opts[i] = kit.Option{Value: s, Label: l}
			}
			a.settingRow(c, L("Sound"), desc, func() {
				if next, ok := a.fieldSelect(c, label, *value, opts, false); ok {
					// "Custom…" needs a file first; without one the old choice stays.
					if next == "custom" && (*custom == nil || **custom == "") {
						pick()
					} else {
						*value = next
						a.setAlertPrefs(p)
					}
				}
				if a.smallButton(c, kit.Outline, L("Test"), "", false).Label(L("Test %s", strings.ToLower(l10n.T(title)))).Clicked() && a.cfg.PlaySound != nil {
					ev := alerts.Completion
					if label != "Completion sound" {
						ev = alerts.AwaitingInput
					}
					a.cfg.PlaySound(soundFor(p, ev))
				}
			}, a.customSoundRow(c, *value, *custom, pick))
		}
	}
	notifyRow := func(label, desc string, value *bool) func() {
		return func() {
			a.settingRow(c, L("Desktop notification"), desc, func() {
				if a.toggle(c, l10n.T(label), *value, false) {
					*value = !*value
					a.setAlertPrefs(p)
				}
			}, nil)
		}
	}
	a.settingGroup(c, L("When Droid finishes"), "",
		soundRow(l10n.N("Completion sound"), l10n.N("Completion sound"), L("Plays when Droid finishes and the Session stops."), &p.CompletionSound, &p.CustomCompletionSound, "fx-ok01"),
		notifyRow(l10n.N("Notify when Droid finishes"), L("Unless you are looking at that Session."), &p.NotifyOnComplete),
	)
	a.settingGroup(c, L("When Droid needs input"), "",
		soundRow(l10n.N("Needs-input sound"), l10n.N("Needs-input sound"), L("Plays when Droid stops to ask for permission or an answer."), &p.AwaitingInputSound, &p.CustomAwaitingInputSound, "fx-ack01"),
		notifyRow(l10n.N("Notify when Droid needs input"), L("When a Session waits for a permission or an answer."), &p.NotifyOnWaitingForInput),
	)
	a.settingGroup(c, L("Sounds"), "", func() {
		opts := []kit.Option{{Value: "always", Label: L("Always")}, {Value: "focused", Label: L("Only when Droi is focused")}, {Value: "unfocused", Label: L("Only when Droi is in the background")}}
		a.settingRow(c, L("When to play sounds"), "", func() {
			if next, ok := a.fieldSelect(c, l10n.N("When to play sounds"), p.FocusMode, opts, false); ok {
				p.FocusMode = next
				a.setAlertPrefs(p)
			}
		}, nil)
	})
}

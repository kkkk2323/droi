package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/defaults"
	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/models"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/theme"
)

// settingsTab is a section of Settings.
type settingsTab struct {
	id, label, icon string
	needsHost       bool
}

var settingsTabs = []settingsTab{
	{"account", "Account", "user-round", true},
	{"general", "General", "settings-2", false},
	{"defaults", "Session defaults", "sliders-horizontal", false},
	{"notifications", "Notifications", "bell", false},
	{"advanced", "Advanced", "wrench", true},
	{"about", "About", "info", false},
}

// settingsState is Settings' own: the page to go back to, and what is
// being typed in its fields.
type settingsState struct {
	back Route
	open map[string]*bool

	apiKey, baseURL, droidPath, prompt, scratch string
	loaded                                      bool
	err                                         string
	picker                                      pickerState
}

func (s *settingsState) flag(key string) *bool {
	if s.open == nil {
		s.open = map[string]*bool{}
	}
	if s.open[key] == nil {
		s.open[key] = new(bool)
	}
	return s.open[key]
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
	ui.Row(c).Role(ui.RoleGroup).Label("Settings").Fill().AlignItems(ui.Stretch).Background(t.Sidebar).TextColor(t.Foreground).Children(func() {
		ui.Column(c).Role(ui.RoleGroup).Label("Settings sections").Width(240).Shrink(0).Gap(k.Px(4)).Padding(44, k.Px(8), k.Px(12), k.Px(8)).
			BorderWidth(0, 1, 0, 0).BorderColor(t.Border).DragWindow().Children(func() {
			nav := func(icon, label string, selected bool) *ui.Element {
				b := ui.ButtonBase(c).Label(label).Height(k.Px(32)).Gap(k.Px(8)).PaddingX(k.Px(8)).Radius(k.Px(10)).Justify(ui.Start).Cursor(ui.CursorPointer)
				color := t.SidebarForeground
				switch {
				case selected:
					kit.Selected(b, true).Background(t.SidebarAccent)
					color = t.SidebarAccentFg
				case b.Hovered():
					b.Background(t.SidebarAccent.Alpha(0.6))
				}
				if label == "Back" {
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
			if nav("arrow-left", "Back", false).Margin(0, 0, k.Px(8), 0).Clicked() {
				a.Go(s.back)
			}
			for _, tab := range tabs {
				if nav(tab.icon, tab.label, tab.id == current.id).Key(tab.id).Clicked() {
					a.route.Tab = tab.id
				}
			}
		})
		ui.Scroll(c).Grow(1).MinWidth(0).FillHeight().Background(t.Background).Children(func() {
			ui.Box(c).Height(44).DragWindow()
			ui.Column(c).FillWidth().MaxWidth(k.Px(672)).AlignSelf(ui.Center).Gap(k.Px(24)).Padding(0, k.Px(32), k.Px(48), k.Px(32)).Children(func() {
				k.Text(c, current.label, 18, 28).Role(ui.RoleHeading).FontWeight(600).LetterSpacing(-k.Px(0.45))
				if s.err != "" {
					k.Text(c, s.err, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
				}
				switch current.id {
				case "account":
					a.accountTab(c)
				case "general":
					a.generalTab(c)
				case "defaults":
					a.defaultsTab(c)
				case "notifications":
					a.notificationsTab(c)
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
	k, t := a.kit, a.kit.T
	ui.Column(c).Role(ui.RoleGroup).Label(title).Gap(k.Px(6)).Children(func() {
		if title != "" {
			k.Text(c, title, 12, 16).FontWeight(500).TextColor(t.MutedForeground).PaddingX(k.Px(4))
		}
		ui.Column(c).Radius(k.Px(14)).Background(t.Card).Clip().Children(func() {
			for i, row := range rows {
				if i > 0 {
					ui.Box(c).Height(1).Background(t.Border.Alpha(0.7))
				}
				row()
			}
		})
		if footer != "" {
			k.Text(c, footer, 12, 20).TextColor(t.MutedForeground).PaddingX(k.Px(4))
		}
	})
}

// settingRow is a setting: title and explanation on the left, its control
// on the right, and what goes under them.
func (a *App) settingRow(c *ui.Context, title, description string, control, below func()) {
	k, t := a.kit, a.kit.T
	ui.Column(c).Padding(k.Px(12), k.Px(16)).Children(func() {
		ui.Row(c).Wrap().GapX(k.Px(16)).GapY(k.Px(8)).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(k.Px(192)).Children(func() {
				k.Text(c, title, 14, 20).Role(ui.RoleHeading).FontWeight(500)
				if description != "" {
					k.Text(c, description, 13, 20).TextColor(t.MutedForeground).Margin(k.Px(2), 0, 0, 0)
				}
			})
			if control != nil {
				ui.Row(c).MinWidth(0).Gap(k.Px(8)).Shrink(0).Children(control)
			}
		})
		if below != nil {
			ui.Column(c).Margin(k.Px(12), 0, 0, 0).Children(below)
		}
	})
}

// fieldSelect is the framed Select of a settings row: h-8, min-w-32, a
// border on the page's background.
func (a *App) fieldSelect(c *ui.Context, label, value string, options []kit.Option, disabled bool) (string, bool) {
	k, t := a.kit, a.kit.T
	open := a.settings.flag("select:" + label)
	current := value
	for _, o := range options {
		if o.Value == value {
			current = o.Label
		}
	}
	tr := ui.ButtonBase(c).Role(ui.RoleComboBox).Label(label).Expanded(*open).Height(k.Px(32)).MinWidth(k.Px(128)).Gap(k.Px(4)).
		PaddingX(k.Px(10)).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Justify(ui.SpaceBetween).Disabled(disabled).Cursor(ui.CursorPointer)
	if tr.Hovered() {
		tr.Background(t.Muted.Alpha(0.6))
	}
	tr.Children(func() {
		k.Text(c, current, 14, 20).TextColor(t.Foreground).SingleLine()
		k.Icon(c, "chevron-down", 12, t.Foreground).Opacity(0.6)
	})
	if tr.Clicked() {
		*open = !*open
	}
	var picked string
	ok := false
	ui.PopoverBase(c, tr, open, func(p *ui.Element) {
		p.Role(ui.RoleList).Label(label).Margin(k.Px(4), 0, 0, 0).MinWidth(tr.Bounds().W).Padding(k.Px(4)).Radius(k.Px(8)).
			Border(1, t.Border).Background(t.Popover).Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).Children(func() {
			for _, o := range options {
				it := kit.Selected(ui.ButtonBase(c).Key(o.Value).Role(ui.RoleListItem).Label(o.Label), o.Value == value).
					Gap(k.Px(6)).Padding(k.Px(6), k.Px(12), k.Px(6), k.Px(6)).Radius(k.Px(6)).Justify(ui.Start).Cursor(ui.CursorPointer)
				color := t.PopoverForeground
				if it.Hovered() {
					it.Background(t.Accent)
					color = t.AccentForeground
				}
				it.Children(func() {
					ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Children(func() {
						if o.Value == value {
							k.Icon(c, "check", 14, color)
						}
					})
					k.Text(c, o.Label, 13, 19.5).TextColor(color).SingleLine()
				})
				if it.Clicked() {
					*open = false
					if o.Value != value {
						picked, ok = o.Value, true
					}
				}
			}
		})
	})
	return picked, ok
}

// toggle is the settings' Switch: 36x22, the primary color while on.
func (a *App) toggle(c *ui.Context, label string, on, disabled bool) bool {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Role(ui.RoleSwitch).Checked(on).Label(label).Size(k.Px(36), k.Px(22)).Radius(k.Px(11)).Disabled(disabled).Cursor(ui.CursorPointer)
	bg := t.Input
	if on {
		bg = t.Primary
	}
	b.Background(bg)
	x := b.Animate("on", b2f(on), 150*time.Millisecond)
	b.Children(func() {
		ui.Box(c).Size(k.Px(18), k.Px(18)).Radius(k.Px(9)).Background(ui.RGBA(255, 255, 255, 1)).Absolute().Top(k.Px(2)).Left(k.Px(2)+x*k.Px(14)).
			Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.1))
	})
	return b.Clicked()
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// field is a settings text input: h-9, rounded-lg, bordered.
func (a *App) field(c *ui.Context, value *string, label, placeholder string, mono, password bool) *ui.Element {
	k, t := a.kit, a.kit.T
	in := ui.TextInputBase(c, value).Label(label).Placeholder(placeholder).Height(k.Px(36)).Grow(1).MinWidth(0).PaddingX(k.Px(12)).
		Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).FontSize(k.Px(14)).TextColor(t.Foreground)
	if mono {
		in.Font(k.Mono)
	}
	if password {
		in.Password()
	}
	if in.Focused() {
		in.Border(1, t.Ring.Alpha(0.6))
	}
	return in
}

func (a *App) smallButton(c *ui.Context, v kit.Variant, label, icon string, disabled bool) *ui.Element {
	k, t := a.kit, a.kit.T
	b := k.Button(c, v, 32, false, label).Disabled(disabled).FontSize(k.Px(13))
	color := t.Foreground
	if v == kit.Primary {
		color = t.PrimaryForeground
	}
	b.Children(func() {
		if icon != "" {
			k.Icon(c, icon, 16, color)
		}
		k.Text(c, label, 13, 20).FontWeight(500).TextColor(color)
	})
	return b
}

func (a *App) statusPill(c *ui.Context, ok bool, label string) {
	k, t := a.kit, a.kit.T
	color, bg := t.MutedForeground, t.Muted
	if ok {
		color, bg = t.Success, t.Success.Alpha(0.1)
	}
	ui.Row(c).Height(k.Px(22)).Gap(k.Px(6)).PaddingX(k.Px(8)).Radius(k.Px(11)).Background(bg).Children(func() {
		kit.Dot(c, k.Px(6), color)
		k.Text(c, label, 12, 16).FontWeight(500).TextColor(color)
	})
}

func (a *App) generalTab(c *ui.Context) {
	themes := make([]kit.Option, len(theme.Names))
	for i, n := range theme.Names {
		themes[i] = kit.Option{Value: string(n), Label: theme.Labels[n]}
	}
	fonts := []kit.Option{{Value: "geist", Label: "Geist"}, {Value: "system", Label: "System"}}
	sizes := []kit.Option{{Value: "small", Label: "Small"}, {Value: "default", Label: "Default"}, {Value: "large", Label: "Large"}, {Value: "largest", Label: "Largest"}}
	pick := func(p prefs.String, label string, opts []kit.Option) func() {
		return func() {
			if next, ok := a.fieldSelect(c, label, p.Get(a.prefs), opts, false); ok {
				p.Set(a.prefs, next)
				a.applyPrefs()
			}
		}
	}
	a.settingGroup(c, "Appearance", "",
		func() {
			a.settingRow(c, "Theme", "Light is the default. The choice is stored per browser.", pick(prefs.Theme, "Theme", themes), nil)
		},
		func() {
			a.settingRow(c, "Font", "Geist is Droi's own. System uses this device's face, San Francisco on a Mac or iPhone.", pick(prefs.Font, "Font", fonts), nil)
		},
		func() {
			a.settingRow(c, "Text size", "Scales every label, message and code block together. ⌘= and ⌘- zoom on top of this.", pick(prefs.TextSize, "Text size", sizes), nil)
		},
	)
	a.settingGroup(c, "Sidebar", "", func() {
		a.settingRow(c, "Show archived sessions", "List archived sessions in the sidebar alongside the active ones.", func() {
			on := prefs.ShowArchived.Get(a.prefs)
			if a.toggle(c, "Show archived sessions", on, false) {
				prefs.ShowArchived.Set(a.prefs, !on)
				go a.refreshList()
			}
		}, nil)
	})
}

func (a *App) aboutTab(c *ui.Context) {
	version := a.cfg.Version
	a.settingGroup(c, "", "",
		func() { a.settingRow(c, "Droi "+version, "", nil, nil) },
		func() {
			a.settingRow(c, "Where your data lives", "Sessions, settings and the Factory API key stay on this computer; phones reach it through the Gateway. What Droid works on goes to Factory, which runs the models.", nil, nil)
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
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	login := h.LoginState()
	a.settingGroup(c, "Sign-in", "",
		func() {
			switch login.Status {
			case host.SignedIn:
				title := "Signed in with Factory"
				if login.Source == "cli" {
					title = "Signed in with the droid CLI’s login"
				}
				who := ""
				if login.Account != nil {
					who = login.Account.UserID
					if login.Account.Email != nil {
						who = *login.Account.Email
					}
					if login.Account.OrgID != nil {
						who += " · " + *login.Account.OrgID
					}
				}
				a.settingRow(c, title, who, func() {
					if login.Source == "cli" {
						if a.smallButton(c, kit.Outline, "Sign in as someone else", "", false).Clicked() {
							go func() { _, _ = h.Auth.SignIn(a.ctx) }()
						}
						return
					}
					if a.smallButton(c, kit.Outline, "Sign out", "log-out", false).Clicked() {
						h.Auth.SignOut()
					}
				}, func() {
					if login.Source == "cli" {
						k.Text(c, "Reused from ~/.factory, the same login the Daemon runs as. Run `droid logout` in a terminal to drop it.", 14, 20).TextColor(t.MutedForeground)
					}
				})
			case host.Pending:
				a.settingRow(c, "Finish signing in", "Your browser opened Factory. Enter this code there if it asks for one.", func() {
					if a.smallButton(c, kit.Outline, "Cancel", "", false).Clicked() {
						h.Auth.CancelSignIn()
					}
				}, func() {
					if login.Pending == nil {
						return
					}
					ui.Row(c).Wrap().Gap(k.Px(12)).Children(func() {
						k.Text(c, login.Pending.UserCode, 16, 24).Font(k.Mono).LetterSpacing(k.Px(3.2)).Label("Sign-in code").
							Padding(k.Px(6), k.Px(12)).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background)
						ui.Link(c, "Open the sign-in page again", login.Pending.VerificationURIComplete).FontSize(k.Px(14)).Underline()
					})
				})
			default:
				a.settingRow(c, "Sign in with Factory", "Uses the same login as the droid CLI. Droi then needs no API key; sessions and settings stay on this computer.", func() {
					if a.smallButton(c, kit.Primary, "Sign in", "", false).Clicked() {
						go func() { _, _ = h.Auth.SignIn(a.ctx) }()
					}
				}, func() {
					if login.Error != "" {
						k.Text(c, login.Error, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
					}
				})
			}
		},
		func() { a.apiKeyRow(c, login) },
	)
}

func (a *App) apiKeyRow(c *ui.Context, login host.LoginState) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	s := &a.settings
	fromEnv := a.cfg.Env != nil && a.cfg.Env("FACTORY_API_KEY") != ""
	hasKey := h.Settings.APIKey() != ""
	signedIn := login.Status == host.SignedIn
	desc := "Only used when neither Droi nor the droid CLI is signed in. Stored on this computer; phones never see it."
	if fromEnv {
		desc = "Supplied by FACTORY_API_KEY in the environment; the stored key is ignored."
	}
	pill := "Missing"
	switch {
	case hasKey:
		pill = "Set"
	case signedIn:
		pill = "Not needed"
	}
	save := func(key string) {
		a.hostUpdate(!signedIn, func(st *host.Settings) { st.APIKey = host.OptString(key) })
		s.apiKey = ""
	}
	a.settingRow(c, "Factory API key", desc, func() { a.statusPill(c, hasKey, pill) }, func() {
		ui.Row(c).Gap(k.Px(8)).Children(func() {
			placeholder := "fk-…"
			if hasKey {
				placeholder = "•••••••••••• (key stored)"
			}
			in := a.field(c, &s.apiKey, "Factory API key", placeholder, false, true).Disabled(fromEnv)
			if in.Submitted() && strings.TrimSpace(s.apiKey) != "" {
				save(s.apiKey)
			}
			if a.smallButton(c, kit.Outline, "Save", "", strings.TrimSpace(s.apiKey) == "" || fromEnv).Label("Save key").Clicked() {
				save(s.apiKey)
			}
			if hasKey && !fromEnv && a.smallButton(c, kit.Outline, "Remove", "", false).Clicked() {
				save("")
			}
		})
		msg := "No key set. The Daemon cannot authenticate without one."
		switch {
		case hasKey:
			msg = "A key is set."
		case signedIn:
			msg = "No key set. None is needed while signed in."
		}
		k.Text(c, msg, 12, 16).Role(ui.RoleStatus).Label("API key status").TextColor(t.MutedForeground).Margin(k.Px(8), 0, 0, 0)
	})
}

func daemonStatusText(st host.DaemonState) (ok bool, label, detail string) {
	switch st.Status {
	case host.DaemonRunning:
		return true, "Running", "Listening on 127.0.0.1:" + strconv.Itoa(st.Port) + " (pid " + strconv.Itoa(st.PID) + ")."
	case host.DaemonStarting:
		attempt := ""
		if st.Attempt > 1 {
			attempt = " (attempt " + strconv.Itoa(st.Attempt) + ")"
		}
		return false, "Starting", "Waiting for the Daemon to answer on port " + strconv.Itoa(st.Port) + attempt + "."
	case host.DaemonRestarting:
		return false, "Not running", "Last attempt: " + st.Reason + ". Starting again in " + strconv.Itoa(int(st.Delay.Round(time.Second)/time.Second)) + "s (attempt " + strconv.Itoa(st.Attempt) + ")."
	}
	return false, "Stopped", "The Daemon is not running."
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
				if a.smallButton(c, kit.Outline, "Save", "", false).Label(button).Clicked() {
					save(*value)
				}
			})
		}
	}
	a.settingGroup(c, "Daemon", "",
		func() {
			ok, label, detail := daemonStatusText(h.Daemon.State())
			a.settingRow(c, "Status", detail, func() {
				a.statusPill(c, ok, label)
				if a.smallButton(c, kit.Outline, "Restart Daemon", "refresh-cw", false).Clicked() {
					go h.RestartDaemon()
				}
			}, func() {
				log := host.ReadDaemonLogTail(h.DaemonLogPath())
				if ok || log == "" {
					return
				}
				ui.Scroll(c).MaxHeight(k.Px(160)).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Children(func() {
					k.Text(c, log, 12, 20).Font(k.Mono).Label("Daemon log").TextColor(t.MutedForeground).Selectable().Padding(k.Px(8), k.Px(12))
				})
				if a.smallButton(c, kit.Ghost, "Show log file", "", false).AlignSelf(ui.Start).Margin(k.Px(8), 0, 0, 0).Clicked() && a.cfg.ShowInFolder != nil {
					a.cfg.ShowInFolder(h.DaemonLogPath())
				}
			})
		},
		func() {
			found := h.DroidPath()
			desc := "droid was not found on PATH or in ~/.local/bin. Enter its full path."
			if found != "" {
				desc = "Using " + found
			}
			label := "Not found"
			if found != "" {
				label = "Found"
			}
			a.settingRow(c, "droid executable", desc, func() { a.statusPill(c, found != "", label) },
				saveRow(&s.droidPath, "droid path override", "Leave empty to auto-detect", "Save path", true, func(v string) {
					a.hostUpdate(true, func(st *host.Settings) { st.DroidPath = host.OptString(v) })
				}))
		},
	)
	a.settingGroup(c, "Factory API", "", func() {
		desc := "Leave empty to talk to Factory directly. Set it to route the Daemon through a local proxy such as droid-proxy (for example http://127.0.0.1:37650)."
		inherited := ""
		if a.cfg.Env != nil {
			inherited = a.cfg.Env("FACTORY_API_BASE_URL")
		}
		switch {
		case st.FactoryAPIBaseURL != nil:
			desc = "The Daemon sends its Factory API traffic to this URL (FACTORY_API_BASE_URL). Use it for a local proxy such as droid-proxy."
		case inherited != "":
			desc = "Inherited from the environment: " + inherited + ". Set a value here to override it."
		}
		placeholder := "https://api.factory.ai"
		if inherited != "" {
			placeholder = inherited
		}
		a.settingRow(c, "Factory API base URL", desc, nil, saveRow(&s.baseURL, "Factory API base URL", placeholder, "Save URL", true, func(v string) {
			a.hostUpdate(true, func(st *host.Settings) { st.FactoryAPIBaseURL = host.OptString(v) })
		}))
	})
	a.settingGroup(c, "New Sessions in Droi", "Droi’s own settings. They apply to new Sessions from every Client of this computer, not to the droid CLI or the Factory App.",
		func() {
			a.settingRow(c, "Added to the system prompt", "Appended to Droid's own system prompt in every new Session, whether it starts here, in a browser or on a phone. Existing Sessions and subagents keep theirs.", nil, func() {
				ui.Row(c).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Children(func() {
					k.TextArea(c, &s.prompt, "Added to the system prompt", "e.g. Reply in the language I write in. Prefer small, reviewable commits.",
						kit.AreaStyle{Pad: [4]float32{8, 12, 8, 12}, Size: 14, Line: 20, MinLines: 4, Color: t.Foreground, Muted: t.MutedForeground})
				})
				stored := derefOr(st.AppendSystemPrompt)
				ui.Row(c).Gap(k.Px(8)).Margin(k.Px(8), 0, 0, 0).Children(func() {
					if a.smallButton(c, kit.Outline, "Save", "", strings.TrimSpace(s.prompt) == stored).Label("Save system prompt").Clicked() {
						a.hostUpdate(false, func(st *host.Settings) { st.AppendSystemPrompt = host.OptString(s.prompt) })
					}
					if stored != "" && a.smallButton(c, kit.Ghost, "Clear", "", false).Clicked() {
						s.prompt = ""
						a.hostUpdate(false, func(st *host.Settings) { st.AppendSystemPrompt = nil })
					}
				})
			})
		},
		func() {
			a.settingRow(c, "Scratch folder", "A session started with Workspace: None gets a new folder of its own in "+h.ScratchFolder()+". Archiving it moves the folder to the Trash. A change applies to new sessions only.", nil,
				saveRow(&s.scratch, "Scratch folder", "~/.droi/chats", "Save folder", true, func(v string) {
					a.hostUpdate(false, func(st *host.Settings) { st.ScratchFolder = host.OptString(v) })
				}))
		},
	)
}

// defaultsTab is the Session defaults: what a new Session starts with,
// read from and saved to the Daemon.
func (a *App) defaultsTab(c *ui.Context) {
	k, t := a.kit, a.kit.T
	k.Text(c, "Shared with the droid CLI and the Factory App on this computer. Existing Sessions keep their settings.", 13, 19.5).TextColor(t.MutedForeground).Margin(-k.Px(16), 0, 0, 0)
	dv := a.sessionDefaults()
	if dv == nil {
		k.Text(c, "Loading session defaults…", 14, 20).TextColor(t.MutedForeground)
		return
	}
	save := func(p defaults.Patch) {
		view := defaults.ApplyPatch(*dv, p)
		a.newPage.mu.Lock()
		a.newPage.defaults = &view
		a.newPage.mu.Unlock()
		go func() {
			cl, err := a.ctl.Client()
			if err == nil {
				err = cl.Call(a.ctx, "daemon.update_session_defaults", p.Params(), nil)
			}
			a.cfg.Update(func() {
				if err != nil {
					a.settings.err = "Session defaults did not load or save: " + err.Error()
				}
				a.newPage.mu.Lock()
				a.newPage.readAt = time.Time{}
				a.newPage.mu.Unlock()
			})
		}()
	}
	str := func(s string) *string { return &s }
	locked := func(key string) bool { return dv.Locked[key] }
	efforts := func(model, current string) []kit.Option {
		var out []kit.Option
		for _, e := range defaults.ReasoningChoices(dv.Models, model, current) {
			out = append(out, kit.Option{Value: e, Label: effortLabel(e)})
		}
		return out
	}
	modelOpts := func(routers bool) []kit.Option {
		var out []kit.Option
		for _, m := range defaults.PickableModels(dv.Models, routers) {
			out = append(out, kit.Option{Value: m.ID, Label: m.Label})
		}
		return out
	}
	autonomy := make([]kit.Option, 0, len(dv.AvailableAutonomyLevels))
	for _, l := range dv.AvailableAutonomyLevels {
		autonomy = append(autonomy, kit.Option{Value: l, Label: models.AutonomyLabels[l]})
	}
	const orgManaged = "Managed by your organization."
	descIf := func(key, otherwise string) string {
		if locked(key) {
			return orgManaged
		}
		return otherwise
	}
	a.settingGroup(c, "Model and autonomy", "",
		func() {
			a.settingRow(c, "Model", descIf("modelId", ""), func() {
				a.modelField(c, "Default model", locked("modelId"), a.settings.flag("model:default"), &a.settings.picker,
					defaults.PickableModels(dv.Models, true), dv.ModelID, func(next string) {
						p := defaults.Patch{ModelID: str(next)}
						// A level the new model lacks would be refused; start from its first.
						if sup := defaults.ReasoningChoices(dv.Models, next, ""); len(sup) > 0 && !contains(sup, dv.ReasoningEffort) {
							p.ReasoningEffort = str(sup[0])
						}
						save(p)
					})
			}, nil)
		},
		func() {
			a.settingRow(c, "Reasoning level", descIf("reasoningEffort", ""), func() {
				if next, ok := a.fieldSelect(c, "Default reasoning level", dv.ReasoningEffort, efforts(dv.ModelID, dv.ReasoningEffort), locked("reasoningEffort")); ok {
					save(defaults.Patch{ReasoningEffort: str(next)})
				}
			}, nil)
		},
		func() {
			modes := make([]kit.Option, len(defaults.InteractionModes))
			for i, m := range defaults.InteractionModes {
				modes[i] = kit.Option{Value: m.Value, Label: m.Label}
			}
			a.settingRow(c, "Interaction mode", "Auto starts working right away; Spec plans with you before it changes anything.", func() {
				if next, ok := a.fieldSelect(c, "Default interaction mode", dv.InteractionMode, modes, locked("interactionMode")); ok {
					save(defaults.Patch{InteractionMode: str(next)})
				}
			}, nil)
		},
		func() {
			level := dv.AutonomyLevel
			if level == "" {
				level = "off"
			}
			desc := defaults.AutonomyDescriptions[level]
			if desc == "" {
				desc = "How much Droid may do without asking for approval."
			}
			a.settingRow(c, "Autonomy level", desc, func() {
				if next, ok := a.fieldSelect(c, "Default autonomy level", level, autonomy, locked("autonomyLevel")); ok {
					save(defaults.Patch{AutonomyLevel: str(next)})
				}
			}, nil)
		},
	)
	a.settingGroup(c, "Tool calls", "", func() {
		const follow = "follow-droid"
		opts := []kit.Option{{Value: follow, Label: "Follow droid settings"}}
		for _, m := range defaults.ToolModes {
			opts = append(opts, kit.Option{Value: string(m), Label: defaults.ToolModeLabels[m]})
		}
		current := string(defaults.DefaultToolMode(a.prefs))
		if current == "" {
			current = follow
		}
		a.settingRow(c, "Mode", "Script lets the model call tools from a small program that can batch and filter them; Both offers it alongside direct calls. A Session keeps the mode it starts with. Kept on this device.", func() {
			if next, ok := a.fieldSelect(c, "Default tool calls", current, opts, false); ok {
				if next == follow {
					next = ""
				}
				defaults.SetDefaultToolMode(a.prefs, defaults.ToolMode(next))
			}
		}, nil)
	})
	const sameAsMain = "same-as-main"
	a.settingGroup(c, "Spec mode", "",
		func() {
			a.settingRow(c, "Model", "", func() {
				value := dv.SpecModeModelID
				if value == "" {
					value = sameAsMain
				}
				opts := append([]kit.Option{{Value: sameAsMain, Label: "Same as main"}}, modelOpts(true)...)
				if next, ok := a.fieldSelect(c, "Spec mode model", value, opts, locked("specModeModelId")); ok {
					if next == sameAsMain {
						next = ""
					}
					save(defaults.Patch{SpecModeModelID: str(next), SpecModeReasoningEffort: str("")})
				}
			}, nil)
		},
		func() {
			a.settingRow(c, "Reasoning level", "", func() {
				value := dv.SpecModeReasoningEffort
				if value == "" {
					value = sameAsMain
				}
				model := dv.SpecModeModelID
				if model == "" {
					model = dv.ModelID
				}
				cur := dv.SpecModeReasoningEffort
				if cur == "" {
					cur = dv.ReasoningEffort
				}
				opts := append([]kit.Option{{Value: sameAsMain, Label: "Same as main"}}, efforts(model, cur)...)
				if next, ok := a.fieldSelect(c, "Spec mode reasoning level", value, opts, locked("specModeReasoningEffort")); ok {
					if next == sameAsMain {
						next = ""
					}
					save(defaults.Patch{SpecModeReasoningEffort: str(next)})
				}
			}, nil)
		},
	)
	a.settingGroup(c, "Compaction", "",
		func() {
			a.settingRow(c, "Compact automatically", "New Sessions compact their history once it passes the token limit.", func() {
				on := dv.CompactionThresholdCheckEnabled
				if a.toggle(c, "Compact automatically", on, locked("compactionThresholdCheckEnabled")) {
					next := !on
					save(defaults.Patch{CompactionThresholdCheckEnabled: &next})
				}
			}, nil)
		},
		func() {
			limit := dv.CompactionTokenLimit
			if limit == 0 {
				limit = defaults.DefaultCompactionLimit
			}
			vals := append([]int{}, defaults.CompactionLimits...)
			if !containsInt(vals, limit) {
				vals = append(vals, limit)
			}
			sortInts(vals)
			opts := make([]kit.Option, len(vals))
			for i, v := range vals {
				opts[i] = kit.Option{Value: strconv.Itoa(v), Label: defaults.TokenLimitLabel(v)}
			}
			a.settingRow(c, "Token limit", "", func() {
				if next, ok := a.fieldSelect(c, "Compaction token limit", strconv.Itoa(limit), opts, locked("compactionTokenLimit")); ok {
					n, _ := strconv.Atoi(next)
					save(defaults.Patch{CompactionTokenLimit: &n})
				}
			}, nil)
		},
		func() {
			value := dv.CompactionModel
			opts := append([]kit.Option{{Value: defaults.CurrentModel, Label: "Current model"}}, modelOpts(false)...)
			a.settingRow(c, "Summary model", "The model that writes the summary.", func() {
				if next, ok := a.fieldSelect(c, "Compaction model", value, opts, locked("compactionModel")); ok {
					save(defaults.Patch{CompactionModel: str(next)})
				}
			}, nil)
		},
	)
	a.settingGroup(c, "Subagents", "", func() {
		const inherit = "inherit"
		opts := append([]kit.Option{{Value: inherit, Label: "Inherit (calling session)"}}, autonomy...)
		value := dv.SubagentAutonomyLevel
		if value == "" {
			value = inherit
		}
		a.settingRow(c, "Autonomy level", "", func() {
			if next, ok := a.fieldSelect(c, "Subagent autonomy level", value, opts, locked("subagentAutonomyLevel")); ok {
				save(defaults.Patch{SubagentAutonomyLevel: str(next)})
			}
		}, nil)
	})
}

func containsInt(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

func sortInts(v []int) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
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

var soundChoices = []string{"off", "bell", "fx-ok01", "fx-ack01"}

var soundLabels = map[string]string{"off": "Off", "bell": "Bell", "fx-ok01": "Soft chime", "fx-ack01": "Acknowledge tone", "custom": "Custom…"}

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
	soundRow := func(title, label, desc string, value *string, def string) func() {
		return func() {
			opts := make([]kit.Option, len(soundChoices))
			for i, s := range soundChoices {
				l := soundLabels[s]
				if s == def {
					l += " (default)"
				}
				opts[i] = kit.Option{Value: s, Label: l}
			}
			a.settingRow(c, "Sound", desc, func() {
				if next, ok := a.fieldSelect(c, label, *value, opts, false); ok {
					*value = next
					a.setAlertPrefs(p)
				}
				if a.smallButton(c, kit.Outline, "Test", "", false).Label("Test "+strings.ToLower(title)).Clicked() && a.cfg.PlaySound != nil {
					a.cfg.PlaySound(*value)
				}
			}, nil)
		}
	}
	notifyRow := func(label, desc string, value *bool) func() {
		return func() {
			a.settingRow(c, "Desktop notification", desc, func() {
				if a.toggle(c, label, *value, false) {
					*value = !*value
					a.setAlertPrefs(p)
				}
			}, nil)
		}
	}
	a.settingGroup(c, "When Droid finishes", "",
		soundRow("Completion sound", "Completion sound", "Plays when Droid finishes and the Session stops.", &p.CompletionSound, "fx-ok01"),
		notifyRow("Notify when Droid finishes", "Unless you are looking at that Session.", &p.NotifyOnComplete),
	)
	a.settingGroup(c, "When Droid needs input", "",
		soundRow("Needs-input sound", "Needs-input sound", "Plays when Droid stops to ask for permission or an answer.", &p.AwaitingInputSound, "fx-ack01"),
		notifyRow("Notify when Droid needs input", "When a Session waits for a permission or an answer.", &p.NotifyOnWaitingForInput),
	)
	a.settingGroup(c, "Sounds", "", func() {
		opts := []kit.Option{{Value: "always", Label: "Always"}, {Value: "focused", Label: "Only when Droi is focused"}, {Value: "unfocused", Label: "Only when Droi is in the background"}}
		a.settingRow(c, "When to play sounds", "", func() {
			if next, ok := a.fieldSelect(c, "When to play sounds", p.FocusMode, opts, false); ok {
				p.FocusMode = next
				a.setAlertPrefs(p)
			}
		}, nil)
	})
}

package app

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/internal/memorywork"
	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// Memory is the Host's side of Memory that Settings → Memory reaches
// (ADR 0031).
type Memory interface {
	Status() (memorywork.Status, error)
	// UpdateNow summarizes the Sessions left for a few minutes and updates
	// the Memory Summary; it returns once done.
	UpdateNow() error
	ResetPrompts() error
	Folder() string
}

// memoryState is what Settings → Memory keeps between frames.
type memoryState struct {
	status  *memorywork.Status
	err     string
	readAt  time.Time
	loading bool
	// pending is the Memory switch's requested state while its question is up.
	pending  *bool
	updating bool
	result   string
	reset    bool
}

// Memory Sessions run in the Host and nothing tells the window, so the
// status is read again now and then, and often while a pass works.
const (
	memoryRefresh     = time.Minute
	memoryBusyRefresh = 2 * time.Second
)

// memoryStatus reads the status now and then, off the frame.
func (a *App) memoryStatus() (*memorywork.Status, string) {
	m := &a.memory
	if a.cfg.Memory == nil {
		return nil, ""
	}
	every := memoryRefresh
	if m.status != nil && m.status.Working {
		every = memoryBusyRefresh
	}
	if !m.loading && (m.status == nil && m.err == "" || a.cfg.Now().Sub(m.readAt) > every) {
		a.refreshMemory()
	}
	return m.status, m.err
}

func (a *App) refreshMemory() {
	m := &a.memory
	m.loading = true
	go func() {
		st, err := a.cfg.Memory.Status()
		a.cfg.Update(func() {
			m.loading = false
			m.readAt = a.cfg.Now()
			if err != nil {
				m.err = err.Error()
				return
			}
			m.err, m.status = "", &st
		})
	}()
}

// localDate is the date of an ISO time as the user reads it.
func localDate(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("1/2/2006")
}

// localTime is an ISO time as the user reads it.
func localTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("1/2/2006 15:04")
}

// formatKB is a size in bytes to a tenth of a kilobyte: "6.2 KB".
func formatKB(n int) string {
	return strings.TrimSuffix(strconv.FormatFloat(float64(n)/1000, 'f', 1, 64), ".0") + " KB"
}

// memoryTab is Settings → Memory: the switch, the models, Memory's status
// and its Memory Sessions.
func (a *App) memoryTab(c *ui.Context) {
	h := a.cfg.Host
	st := h.Settings.Get()
	a.settingGroup(c, "", "",
		func() { a.memorySwitchRow(c, st.MemoryEnabled) },
		func() {
			a.memoryModelRow(c, L("Session summary model"), L("Writes a summary of each Session once it has been left for a while. A fast, low-cost model is enough."), "model:memory", st.MemoryModelOrDefault(),
				func(s *host.Settings, next string) { s.MemoryModel = host.OptString(next) })
		},
		func() {
			a.memoryModelRow(c, L("Memory Summary model"), L("Folds the Session summaries and your Memory Notes into the Memory Summary every new Session starts with."), "model:memory-summary", st.MemorySummaryModelOrDefault(),
				func(s *host.Settings, next string) { s.MemorySummaryModel = host.OptString(next) })
		},
	)
	if a.cfg.Memory != nil {
		a.memorySection(c)
		a.memorySessionsSection(c)
	}
}

func (a *App) memorySwitchRow(c *ui.Context, on bool) {
	k, t := a.kit, a.kit.T
	m := &a.memory
	desc := L("Off: Droid starts every Session with no recollection of earlier ones. Turn it on to let it remember preferences, conventions and past work on this computer.")
	if on {
		desc = L("On: Droi summarizes each Session once it has been left for a while and keeps a short Memory Summary that every new Session starts with. Only Droi’s Daemon has it; the droid CLI is untouched.")
	}
	var below func()
	if m.pending != nil {
		next := *m.pending
		label := L("Turn Memory off")
		question := L("Turn Memory off? This restarts the Daemon and stops working Sessions.")
		if next {
			label = L("Turn Memory on")
			question = L("Turn Memory on? This restarts the Daemon and stops working Sessions.")
		}
		below = func() {
			ui.Row(c).Role(ui.RoleGroup).Label(label).Wrap().AlignItems(ui.Center).Gap(k.Px(8)).Children(func() {
				k.Text(c, question, 13, 20).
					TextColor(t.MutedForeground).Grow(1).Basis(0).MinWidth(k.Px(192))
				if a.smallButton(c, kit.Primary, L("Restart Daemon"), "", false).Clicked() {
					m.pending = nil
					a.hostUpdate(true, func(s *host.Settings) { s.MemoryEnabled = next })
					a.refreshMemory()
				}
				if a.smallButton(c, kit.Ghost, L("Cancel"), "", false).Clicked() {
					m.pending = nil
				}
			})
		}
	}
	a.settingRowWith(c, func() {
		ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
			k.Text(c, L("Memory"), 14, 20).Role(ui.RoleHeading).FontWeight(500)
			ui.Box(c).PaddingX(k.Px(6)).Radius(k.Px(999)).Border(1, t.Border).Children(func() {
				k.Text(c, L("Beta"), 11, 16).TextColor(t.MutedForeground)
			})
		})
	}, desc, func() {
		// The switch keeps showing the setting until the restart is confirmed.
		if a.toggle(c, L("Memory"), on, false) {
			next := !on
			if m.pending != nil {
				m.pending = nil
			} else {
				m.pending = &next
			}
		}
	}, below)
}

// memoryModelRow picks the model of one kind of Memory Session; it is read
// at each one, so the Daemon needs no restart.
func (a *App) memoryModelRow(c *ui.Context, label, desc, flag, model string, set func(*host.Settings, string)) {
	a.settingRow(c, label, desc, func() {
		var choices = defaults.PickableModels(nil, false)
		if dv := a.sessionDefaults(); dv != nil {
			choices = defaults.PickableModels(dv.Models, false)
		}
		a.modelField(c, label, false, a.settings.Flag(flag), a.settings.Picker(), choices, model, func(next string) {
			a.hostUpdate(false, func(s *host.Settings) { set(s, next) })
		})
	}, nil)
}

func (a *App) memorySection(c *ui.Context) {
	k, t := a.kit, a.kit.T
	m := &a.memory
	st, err := a.memoryStatus()
	var rows []func()
	switch {
	case err != "":
		rows = append(rows, func() {
			a.settingRow(c, L("Memory did not load"), "", nil, func() {
				k.Text(c, err, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
			})
		})
	case st == nil:
		rows = append(rows, func() { a.settingRow(c, L("Loading Memory…"), "", nil, nil) })
	default:
		rows = append(rows, func() {
			desc := L("Not written yet. Droi writes it from Sessions once they have been left for a while; Update now takes the ones left for ten minutes.")
			if st.SummaryBytes > 0 {
				desc = L("%s of %s · updated %s", formatKB(st.SummaryBytes), formatKB(memory.SummaryMaxBytes), localTime(st.SummaryUpdatedAt))
			}
			busy := m.updating || st.Working
			a.settingRow(c, L("Memory Summary"), desc, func() {
				label := L("Update now")
				if busy {
					label = L("Updating…")
				}
				if a.smallButton(c, kit.Outline, label, "", busy).Label(L("Update Memory now")).Clicked() {
					a.updateMemory()
				}
			}, func() {
				if st.LastError != "" && !busy {
					k.Text(c, L("The last update failed (%s): %s Droi tries again within the hour.", localTime(st.LastErrorAt), st.LastError), 13, 20).
						Role(ui.RoleStatus).TextColor(t.Attention)
				}
				if m.result != "" {
					k.Text(c, m.result, 12, 16).Role(ui.RoleStatus).TextColor(t.MutedForeground).Margin(k.Px(4), 0, 0, 0)
				}
			})
		}, func() {
			a.settingRow(c, L("Sessions"), L("%s · %s waiting · %s",
				countL(st.Summaries, l10n.N("%d Session summary"), l10n.N("%d Session summaries")),
				countL(st.Waiting, l10n.N("%d Session"), l10n.N("%d Sessions")),
				countL(st.Notes, l10n.N("%d Memory Note"), l10n.N("%d Memory Notes"))), nil, nil)
		})
	}
	rows = append(rows, func() {
		a.settingRow(c, L("Files and prompts"), L("The Memory Summary, the Session summaries and your Memory Notes as Markdown files, and the prompts Droi writes them with."), func() {
			ui.Row(c).Wrap().Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
				if a.smallButton(c, kit.Outline, L("Open memory folder"), "folder-open", false).Clicked() && a.cfg.OpenPath != nil {
					dir := a.cfg.Memory.Folder()
					go func() { _ = os.MkdirAll(dir, 0o755); _ = a.cfg.OpenPath(dir) }()
				}
				if a.smallButton(c, kit.Ghost, L("Reset prompts to default"), "rotate-ccw", false).Clicked() {
					if e := a.cfg.Memory.ResetPrompts(); e == nil {
						m.reset = true
					} else {
						a.settings.err = e.Error()
					}
				}
			})
		}, func() {
			if m.reset {
				k.Text(c, L("The prompts are back to Droi’s."), 12, 16).Role(ui.RoleStatus).TextColor(t.MutedForeground)
			}
		})
	})
	a.settingGroup(c, L("Memory on this computer"), "", rows...)
}

func (a *App) updateMemory() {
	m := &a.memory
	m.updating, m.result = true, ""
	go func() {
		err := a.cfg.Memory.UpdateNow()
		a.cfg.Update(func() {
			m.updating = false
			if err != nil {
				m.result = err.Error()
			} else {
				m.result = L("Memory is up to date.")
			}
			a.refreshMemory()
		})
	}()
}

// shownMemorySessions is how many Memory Sessions Settings lists.
const shownMemorySessions = 12

// memorySessionsSection lists the latest Memory Sessions; each opens like
// any Session, though no session list shows it.
func (a *App) memorySessionsSection(c *ui.Context) {
	k, t := a.kit, a.kit.T
	st, _ := a.memoryStatus()
	if st == nil {
		return
	}
	var rows []func()
	if len(st.Runs) == 0 {
		rows = append(rows, func() { a.settingRow(c, L("No Memory Sessions yet"), "", nil, nil) })
	}
	for _, run := range st.Runs[:min(len(st.Runs), shownMemorySessions)] {
		rows = append(rows, func() {
			desc := run.Model + " · " + localTime(run.StartedAt)
			ui.Column(c.Key("memory-session:" + run.SessionID + run.StartedAt)).Role(ui.RoleListItem).Label(run.Title).Children(func() {
				a.settingRow(c, run.Title, desc, func() {
					if a.smallButton(c, kit.Ghost, L("Open"), "", run.SessionID == "").Label(L("Open %s", run.Title)).Clicked() {
						a.Go(Route{Name: "session", SessionID: run.SessionID})
					}
				}, func() {
					if run.Error != "" {
						k.Text(c, L("Failed: %s", run.Error), 13, 20).TextColor(t.Attention).Margin(-k.Px(10), 0, 0, 0)
					}
				})
			})
		})
	}
	a.settingGroup(c, L("Memory Sessions"), L("Droi’s own Sessions, which summarize your Sessions and update the Memory Summary. No session list shows them."), rows...)
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/defaults"
	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/memorywork"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

// Memory is the Host's side of Memory that Settings → Memory and the
// corner card reach (ADR 0010).
type Memory interface {
	Overview() (memorywork.Overview, error)
	// Consolidate one Memory, "" being the Global Memory; it returns once done.
	Consolidate(workspace string) (memorywork.Result, error)
	ResetPrompts() error
	Folder() string
}

// memoryState is what Settings → Memory keeps between frames.
type memoryState struct {
	overview *memorywork.Overview
	err      string
	readAt   time.Time
	loading  bool
	// pending is the Memory switch's requested state while its question is up.
	pending *bool
	results map[string]string
	reset   bool
}

// Sessions write to Memory in their own processes; nothing tells the Host.
const memoryRefresh = time.Minute

// memoryOverview reads the overview now and then, off the frame.
func (a *App) memoryOverview() (*memorywork.Overview, string) {
	m := &a.memory
	if a.cfg.Memory == nil {
		return nil, ""
	}
	if !m.loading && (m.overview == nil && m.err == "" || a.cfg.Now().Sub(m.readAt) > memoryRefresh) {
		a.refreshMemory()
	}
	return m.overview, m.err
}

func (a *App) refreshMemory() {
	m := &a.memory
	m.loading = true
	go func() {
		ov, err := a.cfg.Memory.Overview()
		a.cfg.Update(func() {
			m.loading = false
			m.readAt = a.cfg.Now()
			if err != nil {
				m.err = err.Error()
				return
			}
			m.err, m.overview = "", &ov
		})
	}()
}

func formatChars(n int) string {
	if n < 1000 {
		return itoa(n)
	}
	return itoa((n+500)/1000) + "k"
}

// localDate is the date of an ISO time as the user reads it.
func localDate(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("1/2/2006")
}

// memoryTab is Settings → Memory: the switch, the model, and each Memory.
func (a *App) memoryTab(c *ui.Context) {
	h := a.cfg.Host
	st := h.Settings.Get()
	a.settingGroup(c, "", "",
		func() { a.memorySwitchRow(c, st.MemoryEnabled) },
		func() { a.memoryModelRow(c, st.MemoryModelOrDefault()) },
	)
	if a.cfg.Memory != nil {
		a.memoriesSection(c)
	}
}

func (a *App) memorySwitchRow(c *ui.Context, on bool) {
	k, t := a.kit, a.kit.T
	m := &a.memory
	desc := "Off: Droid starts every Session with no recollection of earlier ones. Turn it on to let it remember preferences, conventions and past mistakes on this computer."
	if on {
		desc = "On: Droid can record and search what it learns about you and each Workspace, and recalls your corrections in every new Session. Only Droi’s Daemon has it; the droid CLI is untouched."
	}
	var below func()
	if m.pending != nil {
		next := *m.pending
		word, verb := "off", "detach"
		if next {
			word, verb = "on", "attach"
		}
		below = func() {
			ui.Row(c).Role(ui.RoleGroup).Label("Turn Memory " + word).Wrap().AlignItems(ui.Center).Gap(k.Px(8)).Children(func() {
				k.Text(c, "Turn Memory "+word+"? The Daemon restarts to "+verb+" it. Sessions that are working stop.", 13, 20).
					TextColor(t.MutedForeground).Grow(1).Basis(0).MinWidth(k.Px(192))
				if a.smallButton(c, kit.Primary, "Restart Daemon", "", false).Clicked() {
					m.pending = nil
					a.hostUpdate(true, func(s *host.Settings) { s.MemoryEnabled = next })
					a.refreshMemory()
				}
				if a.smallButton(c, kit.Ghost, "Cancel", "", false).Clicked() {
					m.pending = nil
				}
			})
		}
	}
	a.settingRowWith(c, func() {
		ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
			k.Text(c, "Memory", 14, 20).Role(ui.RoleHeading).FontWeight(500)
			ui.Box(c).PaddingX(k.Px(6)).Radius(k.Px(999)).Border(1, t.Border).Children(func() {
				k.Text(c, "Beta", 11, 16).TextColor(t.MutedForeground)
			})
		})
	}, desc, func() {
		// The switch keeps showing the setting until the restart is confirmed.
		if a.toggle(c, "Memory", on, false) {
			next := !on
			if m.pending != nil {
				m.pending = nil
			} else {
				m.pending = &next
			}
		}
	}, below)
}

func (a *App) memoryModelRow(c *ui.Context, model string) {
	a.settingRow(c, "Memory model", "Runs Droi’s own Memory work: extracting entries from a finished Session and consolidating a Memory that grew large. A cheap model is enough.", func() {
		var choices = defaults.PickableModels(nil, false)
		if dv := a.sessionDefaults(); dv != nil {
			choices = defaults.PickableModels(dv.Models, false)
		}
		a.modelField(c, "Memory model", false, a.settings.flag("model:memory"), &a.settings.picker, choices, model, func(next string) {
			// Read at each Memory Session; the Daemon needs no restart.
			a.hostUpdate(false, func(s *host.Settings) { s.MemoryModel = host.OptString(next) })
		})
	}, nil)
}

func (a *App) memoriesSection(c *ui.Context) {
	k, t := a.kit, a.kit.T
	m := &a.memory
	ov, err := a.memoryOverview()
	rows := []func(){}
	switch {
	case err != "":
		rows = append(rows, func() {
			a.settingRow(c, "Memory did not load", "", nil, func() {
				k.Text(c, err, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
			})
		})
	case ov == nil:
		rows = append(rows, func() { a.settingRow(c, "Loading Memory…", "", nil, nil) })
	case len(ov.Rows) == 0:
		rows = append(rows, func() {
			a.settingRow(c, "Nothing remembered yet", "Entries show up here once Droid saves something about you or a Workspace.", nil, nil)
		})
	default:
		for _, row := range ov.Rows {
			rows = append(rows, func() { a.memoryRow(c, row, ov.LoggedSince) })
		}
	}
	rows = append(rows, func() {
		a.settingRow(c, "Files and prompts", "A read-only Markdown copy of each Memory, and the prompts Droi uses to extract and consolidate entries.", func() {
			ui.Row(c).Wrap().Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
				if a.smallButton(c, kit.Outline, "Open memory folder", "folder-open", false).Clicked() && a.cfg.OpenPath != nil {
					dir := a.cfg.Memory.Folder()
					go func() { _ = os.MkdirAll(dir, 0o755); _ = a.cfg.OpenPath(dir) }()
				}
				if a.smallButton(c, kit.Ghost, "Reset prompts to default", "rotate-ccw", false).Clicked() {
					if e := a.cfg.Memory.ResetPrompts(); e == nil {
						m.reset = true
					} else {
						a.settings.err = e.Error()
					}
				}
			})
		}, func() {
			if m.reset {
				k.Text(c, "The consolidation and extraction prompts are back to Droi’s.", 12, 16).Role(ui.RoleStatus).TextColor(t.MutedForeground)
			}
		})
	})
	a.settingGroup(c, "Memories on this computer", "", rows...)
}

func (a *App) memoryRow(c *ui.Context, row memorywork.Row, loggedSince string) {
	k, t := a.kit, a.kit.T
	m := &a.memory
	name := row.Workspace
	if name == "" {
		name = "Global Memory"
	}
	consolidated := "never consolidated"
	if row.LastConsolidated != "" {
		consolidated = "consolidated " + localDate(row.LastConsolidated)
	}
	desc := plural(row.Entries, "entry", "entries") + " · " + formatChars(row.Chars) + " of " + formatChars(row.SoftLimit) + " characters · " + consolidated
	busy := row.Consolidating || m.results[name] == "\x00"
	ui.Column(c).Role(ui.RoleListItem).Label(name).Key("memory:" + name).Children(func() {
		a.settingRowWith(c, func() {
			if row.Workspace == "" {
				k.Text(c, "Global Memory", 14, 20).Role(ui.RoleHeading).FontWeight(500)
			} else {
				k.Text(c, row.Workspace, 13, 20).Role(ui.RoleHeading).FontWeight(500).Font(k.Mono).SingleLine()
			}
		}, desc, func() {
			v := kit.Outline
			if row.OverSoftLimit {
				v = kit.Primary
			}
			label := "Consolidate"
			if busy {
				label = "Consolidating…"
			}
			if a.smallButton(c, v, label, "", busy || row.Entries < 2).Label("Consolidate " + name).Clicked() {
				a.consolidate(row.Workspace, name)
			}
		}, func() {
			if loggedSince != "" {
				line := plural(row.Searches, "search", "searches") + " since " + localDate(loggedSince)
				if row.Searches > 0 {
					line += ", " + itoa(row.EmptySearches) + " found nothing"
				}
				line += " · " + plural(row.NeverFound, "entry", "entries") + " never found"
				k.Text(c, line, 13, 20).TextColor(t.MutedForeground).Margin(-k.Px(10), 0, 0, 0)
			}
			if row.OverSoftLimit {
				ui.Row(c).Gap(k.Px(4)).AlignItems(ui.Center).Margin(k.Px(4), 0, 0, 0).Children(func() {
					k.Icon(c, "circle-alert", 14, t.Attention)
					k.Text(c, "Over its size; consolidate it to keep writes going.", 13, 20).TextColor(t.Attention)
				})
			}
			if r := m.results[name]; r != "" && r != "\x00" {
				k.Text(c, r, 12, 16).Role(ui.RoleStatus).TextColor(t.MutedForeground).Margin(k.Px(4), 0, 0, 0)
			}
		})
	})
}

func (a *App) consolidate(workspace, name string) {
	m := &a.memory
	if m.results == nil {
		m.results = map[string]string{}
	}
	m.results[name] = "\x00" // running
	go func() {
		res, err := a.cfg.Memory.Consolidate(workspace)
		a.cfg.Update(func() {
			switch {
			case err != nil:
				m.results[name] = err.Error()
			case res.Rejected == 0:
				m.results[name] = "Consolidated " + plural(res.Applied, "category", "categories") + "."
			default:
				m.results[name] = "Consolidated " + itoa(res.Applied) + "; " + itoa(res.Rejected) + " left unchanged because the model’s answer did not hold up."
			}
			a.refreshMemory()
		})
	}()
}

// memoryFullCard is the corner card, once per crossing, when a Project
// Memory grows past its soft limit: past the hard limit Droid can no longer
// write to it.
func (a *App) memoryFullCard(c *ui.Context) {
	h := a.cfg.Host
	if a.cfg.Memory == nil || h == nil || !h.Settings.Get().MemoryEnabled {
		return
	}
	ov, _ := a.memoryOverview()
	if ov == nil {
		return
	}
	noticed := prefs.LargeMemoriesNoticed.Get(a.prefs)
	var large []string
	key := ""
	for _, r := range ov.Rows {
		if r.Workspace != "" && r.OverSoftLimit {
			k := r.Workspace + "\n" + r.LastConsolidated
			large = append(large, k)
			if key == "" && !contains(noticed, k) {
				key = k
			}
		}
	}
	if key == "" {
		return
	}
	// Consolidating makes a new key, so a Memory that grows large again is
	// announced again; keys of Memories no longer large are dropped.
	notice := func() {
		var keep []string
		for _, n := range noticed {
			if contains(large, n) {
				keep = append(keep, n)
			}
		}
		prefs.LargeMemoriesNoticed.Set(a.prefs, append(keep, key))
	}
	workspace, _, _ := strings.Cut(key, "\n")
	name := filepath.Base(workspace)
	k, t := a.kit, a.kit.T
	a.cornerCard(c, "Memory is large", "brain", func() {
		k.Text(c, name+"’s Memory is large", 14, 20).FontWeight(500)
		k.Text(c, "Consolidate it before it fills up; a full Memory takes no new entries.", 12, 16).TextColor(t.MutedForeground).Margin(k.Px(2), 0, 0, 0)
		if a.smallButton(c, kit.Primary, "Open Memory settings", "", false).AlignSelf(ui.Start).Margin(k.Px(8), 0, 0, 0).Clicked() {
			notice()
			a.Go(Route{Name: "settings", Tab: "memory"})
		}
	}, notice)
}

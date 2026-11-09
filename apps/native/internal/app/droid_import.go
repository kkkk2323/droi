package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/v2/internal/droidimport"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kit"
)

// droidPage is how many sessions the list shows before "Show more".
const droidPage = 50

// droidImport is Settings → Advanced's import of droid's sessions (Q8): the sessions found in ~/.factory/sessions,
// the ones the user picked, and how the last import went. Nothing is imported until the user picks it.
type droidImport struct {
	store   *droidimport.Store
	finding bool
	found   bool
	err     string
	sources []droidimport.Source
	filter  string
	picked  map[string]bool
	shown   int

	running bool
	stop    context.CancelFunc
	done    int
	total   int
	current string
	// imported counts the last run's sessions that came over.
	imported int
	failures []droidFailure
	// exported is where the last Markdown export went.
	exported string
}

// droidFailure is a session whose import failed: its conversion, if it got that far, is what Markdown exports.
type droidFailure struct {
	src  droidimport.Source
	conv droidimport.Conversation
	err  string
}

// droidSessionsDir is droid's sessions folder, which every account Droi signs in with shares (ADR 0017).
func (a *App) droidSessionsDir() string {
	return filepath.Join(a.cfg.Host.SharedFactoryHome(), "sessions")
}

func (a *App) droidStore() *droidimport.Store {
	di := &a.settings.droidImport
	if di.store == nil {
		di.store = droidimport.OpenStore(filepath.Join(a.cfg.Host.UserData(), droidimport.StoreFile))
	}
	return di.store
}

// droidImportGroup offers droid's sessions to a Host that runs a Kernel: the Kernel does not read droid's files,
// so a session the user wants to go on with in Droi comes over once, as a conversation of its own.
func (a *App) droidImportGroup(c *ui.Context) {
	h := a.cfg.Host
	if h == nil || !h.KernelEnabled() {
		return
	}
	k, t := a.kit, a.kit.T
	di := &a.settings.droidImport
	status := func(msg string, color ui.Color) {
		k.Text(c, msg, 12, 16).Role(ui.RoleStatus).TextColor(color).Selectable()
	}
	a.settingGroup(c, L("droid sessions"),
		L("Brings the sessions you pick from droid's %s into Droi, each as a conversation you can go on with. droid's files are only read.", tildePath(a.droidSessionsDir())),
		func() {
			label := L("Find sessions")
			if di.found {
				label = L("Find again")
			}
			a.settingRow(c, L("Import droid sessions"), L("Lists droid's sessions that have a conversation in them, newest first. Pick the ones to bring over."), func() {
				if a.smallButton(c, kit.Outline, label, "search", di.finding || di.running).Label(L("Find droid sessions")).Clicked() {
					a.findDroidSessions()
				}
			}, func() {
				if !di.finding && !di.found && di.err == "" {
					return
				}
				ui.Column(c).Gap(k.Px(8)).Children(func() {
					switch {
					case di.finding:
						status(L("Looking for sessions…"), t.MutedForeground)
						return
					case di.err != "":
						status(di.err, t.DestructiveForeground)
						return
					case len(di.sources) == 0:
						status(L("droid has no sessions to import."), t.MutedForeground)
						return
					}
					a.droidSessionList(c)
					a.droidImportActions(c)
				})
			})
		})
}

// findDroidSessions lists droid's sessions off the UI thread: it reads every transcript up to its first message.
func (a *App) findDroidSessions() {
	di := &a.settings.droidImport
	di.finding, di.err = true, ""
	dir := a.droidSessionsDir()
	go func() {
		sources, err := droidimport.List(dir)
		a.cfg.Update(func() {
			di.finding, di.found = false, true
			di.sources, di.shown = sources, droidPage
			if di.picked == nil {
				di.picked = map[string]bool{}
			}
			if err != nil {
				di.err = err.Error()
			}
		})
	}()
}

// droidMatches is the sessions the filter leaves.
func (di *droidImport) matches() []droidimport.Source {
	q := strings.ToLower(strings.TrimSpace(di.filter))
	if q == "" {
		return di.sources
	}
	var out []droidimport.Source
	for _, s := range di.sources {
		if strings.Contains(strings.ToLower(s.Title), q) || strings.Contains(strings.ToLower(s.Cwd), q) || strings.HasPrefix(s.ID, q) {
			out = append(out, s)
		}
	}
	return out
}

// droidState is what the list says about a session: imported and unchanged, imported and gone on with in droid
// since, or not imported.
func (a *App) droidState(src droidimport.Source) (imported, changed bool) {
	r, ok := a.droidStore().Get(src.ID)
	if !ok {
		return false, false
	}
	return r.Current(src), !r.Current(src)
}

func (a *App) droidSessionList(c *ui.Context) {
	k, t := a.kit, a.kit.T
	di := &a.settings.droidImport
	k.Field(c, &di.filter, L("Filter sessions"), L("Filter by title or folder"), false, false)
	matches := di.matches()
	now := time.Now()
	ui.Column(c).Gap(k.Px(2)).Children(func() {
		for i, src := range matches {
			if i >= di.shown {
				break
			}
			imported, changed := a.droidState(src)
			title := src.Title
			if strings.TrimSpace(title) == "" {
				title = L("Untitled session")
			}
			meta := []string{tildePath(src.Cwd), relativeTime(src.ModTime, now)}
			switch {
			case imported:
				meta = append(meta, L("Imported"))
			case changed:
				meta = append(meta, L("Changed since its import"))
			}
			ui.Row(c).Key(src.ID).Gap(k.Px(10)).PaddingY(k.Px(4)).AlignItems(ui.Center).Children(func() {
				if k.Toggle(c, L("Pick %s", title), di.picked[src.ID], di.running || imported) {
					di.picked[src.ID] = !di.picked[src.ID]
				}
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					k.Text(c, title, 13, 18).TextColor(t.Foreground).MaxLines(1)
					k.Text(c, strings.Join(meta, " · "), 12, 16).TextColor(t.MutedForeground).MaxLines(1)
				})
			})
		}
	})
	if len(matches) > di.shown {
		if a.smallButton(c, kit.Ghost, L("Show %d more", min(droidPage, len(matches)-di.shown)), "", false).AlignSelf(ui.Start).Clicked() {
			di.shown += droidPage
		}
	}
}

func (a *App) droidImportActions(c *ui.Context) {
	k, t := a.kit, a.kit.T
	di := &a.settings.droidImport
	status := func(msg string, color ui.Color) {
		k.Text(c, msg, 12, 16).Role(ui.RoleStatus).TextColor(color).Selectable()
	}
	var picked []droidimport.Source
	for _, s := range di.sources {
		if di.picked[s.ID] {
			picked = append(picked, s)
		}
	}
	ui.Row(c).Gap(k.Px(8)).Children(func() {
		if di.running {
			if a.smallButton(c, kit.Outline, L("Stop"), "", false).Label(L("Stop importing")).Clicked() && di.stop != nil {
				di.stop()
			}
			return
		}
		if a.smallButton(c, kit.Ghost, L("Pick all shown"), "", false).Clicked() {
			for i, src := range di.matches() {
				if i >= di.shown {
					break
				}
				if imported, _ := a.droidState(src); !imported {
					di.picked[src.ID] = true
				}
			}
		}
		if len(picked) > 0 && a.smallButton(c, kit.Ghost, L("Clear"), "", false).Label(L("Clear the picks")).Clicked() {
			clear(di.picked)
		}
		label := L("Import")
		if len(picked) > 0 {
			label = L("Import %d", len(picked))
		}
		if a.smallButton(c, kit.Primary, label, "download", len(picked) == 0).Label(L("Import the picked sessions")).Clicked() {
			a.importDroidSessions(picked)
		}
	})
	switch {
	case di.running:
		status(L("Importing %d of %d: %s", di.done+1, di.total, di.current), t.MutedForeground)
	case di.total > 0:
		status(L("Imported %d of %d sessions.", di.imported, di.total), t.MutedForeground)
	}
	for _, f := range di.failures {
		ui.Column(c).Key("failed-" + f.src.ID).Gap(k.Px(4)).Children(func() {
			status(L("%s did not come over: %s", f.src.Title, f.err), t.DestructiveForeground)
			if len(f.conv.Entries) > 0 && a.smallButton(c, kit.Outline, L("Export as Markdown"), "", false).Label(L("Export %s as Markdown", f.src.Title)).AlignSelf(ui.Start).Clicked() {
				a.exportDroidSession(f)
			}
		})
	}
	if di.exported != "" {
		status(L("Saved to %s.", tildePath(di.exported)), t.MutedForeground)
		if a.cfg.ShowInFolder != nil && a.smallButton(c, kit.Ghost, L("Show in folder"), "", false).AlignSelf(ui.Start).Clicked() {
			a.cfg.ShowInFolder(di.exported)
		}
	}
}

// importDroidSessions imports picked, one at a time, off the UI thread. A session that fails is listed with why,
// and the rest still come over; Stop ends the run before the next session.
func (a *App) importDroidSessions(picked []droidimport.Source) {
	di := &a.settings.droidImport
	c, err := a.kernel.client()
	if err != nil {
		di.failures = nil
		di.err = err.Error()
		return
	}
	ctx, stop := context.WithCancel(a.ctx)
	di.running, di.stop = true, stop
	di.done, di.total, di.imported, di.failures, di.exported, di.err = 0, len(picked), 0, nil, "", ""
	store := a.droidStore()
	go func() {
		defer stop()
		perCall := 0
		if info, err := c.Harness(ctx); err == nil {
			perCall = info.Limits.MaxImportEntriesPerCall
		}
		for i, src := range picked {
			if ctx.Err() != nil {
				break
			}
			a.cfg.Update(func() { di.done, di.current = i, src.Title })
			_, conv, err := droidimport.Import(ctx, c, store, src, perCall, a.droidAgent)
			a.cfg.Update(func() {
				if err != nil {
					di.failures = append(di.failures, droidFailure{src: src, conv: conv, err: err.Error()})
					return
				}
				di.imported++
				delete(di.picked, src.ID)
			})
		}
		a.cfg.Update(func() { di.running, di.stop = false, nil })
		a.reloadList()
	}()
}

// droidAgent is what an imported conversation runs with: droid's model when the Kernel offers it, else the
// Session defaults' model, else the Kernel's own default.
func (a *App) droidAgent(conv droidimport.Conversation) *kernel.AgentChange {
	if agent := a.newAgent(conv.Model, conv.Effort); agent != nil {
		return agent
	}
	if dv := a.sessionDefaults(); dv != nil {
		return a.newAgent(dv.ModelID, dv.ReasoningEffort)
	}
	return nil
}

// exportDroidSession writes a failed session as Markdown into Droi's data folder.
func (a *App) exportDroidSession(f droidFailure) {
	di := &a.settings.droidImport
	dir := filepath.Join(a.cfg.Host.UserData(), "droid-import")
	// Named after the transcript's own file, not the id written inside it, so the file stays in dir.
	path := filepath.Join(dir, strings.TrimSuffix(filepath.Base(f.src.Path), ".jsonl")+".md")
	err := os.MkdirAll(dir, 0o755)
	if err == nil {
		err = os.WriteFile(path, []byte(droidimport.Markdown(f.conv)), 0o644)
	}
	if err != nil {
		di.err = err.Error()
		return
	}
	di.exported = path
}

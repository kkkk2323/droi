package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kit"
)

// droidsState is Settings → Droids': the droids the Kernel last listed, and the one being edited.
type droidsState struct {
	list    kernel.Droids
	loaded  bool
	loading bool
	err     string
	// editing is set while the editor is shown; original is the name the droid had, "" for a new one.
	editing  bool
	original string
	form     droidForm
	saving   bool
	// removing is the droid whose removal waits for a second click.
	removing string
}

// droidForm is what is typed in the droid editor.
type droidForm struct {
	name, description, prompt, model, effort string
	// tools is "all", "read-only" or "custom"; names are the custom ones, comma separated.
	tools, names string
}

// The reasoning efforts a droid may ask for, "" for the session's.
var droidEfforts = []string{"", "off", "minimal", "low", "medium", "high", "xhigh", "max"}

// droidsTab is Settings → Droids: the subagents the task tool starts, the two built in and a person's own in
// ~/.droi/droids, each to edit, and a new one to add.
func (a *App) droidsTab(c *ui.Context) {
	k, t := a.kit, a.kit.T
	d := &a.droids
	if !d.loaded && !d.loading {
		a.loadDroids()
	}
	if d.editing {
		a.droidEditor(c)
		return
	}
	k.Text(c, L("Droids are the subagents a session hands work to with the task tool. Each runs in a context of its own with its own prompt, model and tools. Yours are kept in ~/.droi/droids."), 13, 20).
		TextColor(t.MutedForeground)
	if d.err != "" {
		k.Text(c, d.err, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
	}
	rows := []func(){}
	for _, droid := range d.list.Droids {
		rows = append(rows, func() { a.droidRow(c, droid) })
	}
	if len(rows) == 0 {
		rows = append(rows, func() { a.settingRow(c, L("Loading droids…"), "", nil, nil) })
	}
	a.settingGroup(c, "", "", rows...)
	if a.smallButton(c, kit.Secondary, L("New droid"), "plus", d.loading).AlignSelf(ui.Start).Clicked() {
		a.editDroid(nil)
	}
	if len(d.list.Diagnostics) > 0 {
		var problems []func()
		for _, p := range d.list.Diagnostics {
			problems = append(problems, func() { a.settingRow(c, p.Path, p.Message, nil, nil) })
		}
		a.settingGroup(c, L("Files that are not droids"), "", problems...)
	}
}

func (a *App) droidRow(c *ui.Context, droid kernel.Droid) {
	k, t := a.kit, a.kit.T
	d := &a.droids
	builtin := slices.ContainsFunc(builtinDroids, func(name string) bool { return name == droid.Name })
	badge := L("Built in")
	switch {
	case droid.Source == "personal" && builtin:
		badge = L("Yours, in place of the built-in one")
	case droid.Source == "personal":
		badge = L("Yours")
	}
	var facts []string
	facts = append(facts, droidModelLabel(droid.Model, a.kernel.offered()))
	if droid.ReasoningEffort != "" {
		facts = append(facts, droid.ReasoningEffort)
	}
	facts = append(facts, droidToolsLabel(droid.Tools))
	a.settingRowWith(c, func() {
		ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
			k.Text(c, droid.Name, 14, 20).Role(ui.RoleHeading).FontWeight(500).Font(k.Mono)
			ui.Box(c).PaddingX(k.Px(6)).Radius(k.Px(999)).Border(1, t.Border).Children(func() {
				k.Text(c, badge, 11, 16).TextColor(t.MutedForeground)
			})
		})
	}, droid.Description+"\n"+strings.Join(facts, " · "), func() {
		edit := L("Edit")
		if droid.Source != "personal" {
			edit = L("Customize")
		}
		if a.smallButton(c, kit.Ghost, edit, "pencil", d.saving).Clicked() {
			a.editDroid(&droid)
		}
		if droid.Source != "personal" {
			return
		}
		remove := L("Delete")
		if builtin {
			remove = L("Reset")
		}
		if d.removing == droid.Name {
			remove = L("Click again to confirm")
		}
		if a.smallButton(c, kit.Destructive, remove, "trash", d.saving).Clicked() {
			if d.removing != droid.Name {
				d.removing = droid.Name
				return
			}
			d.removing = ""
			a.saveDroid(kernel.DroidsSetParams{Name: droid.Name, Remove: true})
		}
	}, nil)
}

// builtinDroids are the Kernel's own droids' names, which a person's may stand in for.
var builtinDroids = []string{"worker", "explorer"}

// editDroid opens the editor on droid, or on a new one for nil.
func (a *App) editDroid(droid *kernel.Droid) {
	d := &a.droids
	d.editing, d.err, d.removing = true, "", ""
	d.original = ""
	d.form = droidForm{tools: "all"}
	if droid == nil {
		return
	}
	if droid.Source == "personal" {
		d.original = droid.Name
	}
	d.form = droidForm{name: droid.Name, description: droid.Description, prompt: droid.Prompt, model: droid.Model,
		effort: droid.ReasoningEffort, tools: "all"}
	if droid.Tools != nil {
		switch {
		case droid.Tools.Names != nil:
			d.form.tools, d.form.names = "custom", strings.Join(droid.Tools.Names, ", ")
		case droid.Tools.Preset == "read-only":
			d.form.tools = "read-only"
		}
	}
}

func (a *App) droidEditor(c *ui.Context) {
	k, t := a.kit, a.kit.T
	d := &a.droids
	f := &d.form
	title := L("New droid")
	if f.name != "" && d.original != "" {
		title = L("Edit %s", d.original)
	}
	ui.Column(c).Role(ui.RoleGroup).Label(title).Gap(k.Px(20)).Children(func() {
		k.Text(c, title, 15, 22).Role(ui.RoleHeading).FontWeight(600)
		a.formField(c, L("Name"), L("Lower-case letters, digits, - and _. The session names it as subagent_type."), "", func() {
			ui.Row(c).Children(func() { a.field(c, &f.name, L("Droid name"), "code-reviewer", true, false) })
		})
		a.formField(c, L("Description"), L("What it is for: the session reads this to choose a droid."), "", func() {
			ui.Row(c).Children(func() {
				a.field(c, &f.description, L("Droid description"), L("Reviews a diff for correctness risks"), false, false)
			})
		})
		a.formField(c, L("Model"), "", "", func() {
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				models := []kit.Option{{Value: "", Label: L("Same as the session")}}
				for _, m := range a.kernel.offered() {
					models = append(models, kit.Option{Value: m.Provider + "/" + m.ID, Label: modelLabel(m)})
				}
				if next, ok := a.fieldSelect(c, L("Droid model"), droidModelValue(f.model, a.kernel.offered()), models, false); ok {
					f.model = next
				}
				efforts := make([]kit.Option, len(droidEfforts))
				for i, e := range droidEfforts {
					efforts[i] = kit.Option{Value: e, Label: e}
				}
				efforts[0].Label = L("Session's reasoning")
				if next, ok := a.fieldSelect(c, L("Reasoning effort"), f.effort, efforts, false); ok {
					f.effort = next
				}
			})
		})
		a.formField(c, L("Tools"), L("Read-only leaves out write, edit and the shell. Neither can start subagents or ask you anything."), "", func() {
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				options := []kit.Option{{Value: "all", Label: L("All tools")}, {Value: "read-only", Label: L("Read-only")}, {Value: "custom", Label: L("Only these")}}
				if next, ok := a.fieldSelect(c, L("Droid tools"), f.tools, options, false); ok {
					f.tools = next
				}
				if f.tools == "custom" {
					a.field(c, &f.names, L("Tool names"), "read, grep, find, ls", true, false)
				}
			})
		})
		a.formField(c, L("Prompt"), L("Its instructions, added to the session's."), "", func() {
			ui.Row(c).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Children(func() {
				k.TextArea(c, &f.prompt, L("Droid prompt"), L("You review diffs. List each risk with its file and line."),
					kit.AreaStyle{Pad: [4]float32{8, 12, 8, 12}, Size: 14, Line: 20, MinLines: 6, Color: t.Foreground})
			})
		})
		if d.err != "" {
			k.Text(c, d.err, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
		}
		ui.Row(c).Gap(k.Px(8)).Children(func() {
			if a.smallButton(c, kit.Primary, L("Save droid"), "", d.saving).Clicked() {
				droid := f.droid()
				name := d.original
				if name == "" {
					name = droid.Name
				}
				a.saveDroid(kernel.DroidsSetParams{Name: name, Droid: &droid})
			}
			if a.smallButton(c, kit.Ghost, L("Cancel"), "", d.saving).Clicked() {
				d.editing, d.err = false, ""
			}
		})
	})
}

// droid is the form as droids.set takes it.
func (f droidForm) droid() kernel.Droid {
	droid := kernel.Droid{Name: strings.TrimSpace(f.name), Description: strings.TrimSpace(f.description),
		Prompt: strings.TrimSpace(f.prompt), Model: f.model, ReasoningEffort: f.effort}
	switch f.tools {
	case "read-only":
		droid.Tools = &kernel.DroidTools{Preset: "read-only"}
	case "custom":
		names := []string{}
		for _, n := range strings.Split(f.names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				names = append(names, n)
			}
		}
		droid.Tools = &kernel.DroidTools{Names: names}
	}
	return droid
}

// loadDroids asks the Kernel for its droids, off the UI thread.
func (a *App) loadDroids() {
	d := &a.droids
	d.loading = true
	go func() {
		list, err := a.droidsCall(func(c *kernel.Client) (kernel.Droids, error) { return c.Droids(a.ctx) })
		a.cfg.Update(func() {
			d.loading, d.loaded = false, true
			if err != nil {
				d.err = err.Error()
				return
			}
			d.list, d.err = list, ""
		})
	}()
}

// saveDroid writes or removes a droid, and closes the editor once the Kernel took it.
func (a *App) saveDroid(p kernel.DroidsSetParams) {
	d := &a.droids
	d.saving = true
	go func() {
		list, err := a.droidsCall(func(c *kernel.Client) (kernel.Droids, error) { return c.SetDroid(a.ctx, p) })
		a.cfg.Update(func() {
			d.saving = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.list, d.err, d.editing = list, "", false
		})
	}()
}

func (a *App) droidsCall(call func(*kernel.Client) (kernel.Droids, error)) (kernel.Droids, error) {
	c, err := a.kernel.client()
	if err != nil {
		return kernel.Droids{}, err
	}
	return call(c)
}

// droidModelValue is the picker's value for a droid's model: provider/id, found by id alone when that is how the
// file names it.
func droidModelValue(model string, offered []kernel.ModelInfo) string {
	if model == "" || strings.Contains(model, "/") {
		return model
	}
	for _, m := range offered {
		if m.ID == model {
			return m.Provider + "/" + m.ID
		}
	}
	return model
}

func droidModelLabel(model string, offered []kernel.ModelInfo) string {
	if model == "" {
		return L("Session's model")
	}
	value := droidModelValue(model, offered)
	for _, m := range offered {
		if m.Provider+"/"+m.ID == value {
			return modelLabel(m)
		}
	}
	return model
}

func modelLabel(m kernel.ModelInfo) string {
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

func droidToolsLabel(tools *kernel.DroidTools) string {
	switch {
	case tools == nil:
		return L("All tools")
	case tools.Names != nil:
		return strings.Join(tools.Names, ", ")
	case tools.Preset == "read-only":
		return L("Read-only")
	}
	return L("All tools")
}

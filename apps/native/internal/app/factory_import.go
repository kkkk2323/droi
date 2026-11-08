package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/brand"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
)

// factoryImport is Settings → Advanced's import from ~/.factory (Q11): what the last one brought over, and what it
// found different, which the user may take droid's copy of.
type factoryImport struct {
	busy   bool
	done   bool
	copied host.FirstCopy
	err    string
	// replaced is where Droi's own copies went when the user took droid's.
	replaced string
}

// factoryImportGroup offers the import again, on a Host that runs a Kernel: the Kernel reads the user's skills,
// MCP servers and AGENTS.md from Droi's own folder, which the first start filled from ~/.factory.
func (a *App) factoryImportGroup(c *ui.Context) {
	h := a.cfg.Host
	if h == nil || !h.KernelEnabled() {
		return
	}
	k, t := a.kit, a.kit.T
	fi := &a.settings.factoryImport
	run := func(work func() (host.FirstCopy, string, error)) {
		fi.busy, fi.err = true, ""
		go func() {
			copied, replaced, err := work()
			a.cfg.Update(func() {
				fi.busy, fi.done = false, true
				fi.copied, fi.replaced = copied, replaced
				if err != nil {
					fi.err = err.Error()
				}
			})
		}()
	}
	a.settingGroup(c, L("Import from droid"),
		L("Droi keeps its own skills, MCP servers and AGENTS.md in %s. The first start copied them from ~/.factory; changes to either folder after that stay in it.", brand.UserFolder("~")),
		func() {
			a.settingRow(c, L("Import again"), L("Copies the skills, MCP servers and AGENTS.md that ~/.factory has and Droi does not. Nothing of Droi's is replaced."), func() {
				if a.smallButton(c, kit.Outline, L("Import"), "download", fi.busy).Label(L("Import from ~/.factory")).Clicked() {
					run(func() (host.FirstCopy, string, error) {
						copied, err := h.ImportFactoryHome()
						return copied, "", err
					})
				}
			}, func() {
				if !fi.busy && !fi.done {
					return
				}
				ui.Column(c).Gap(k.Px(8)).Children(func() {
					status := func(msg string, color ui.Color) {
						k.Text(c, msg, 12, 16).Role(ui.RoleStatus).TextColor(color).Selectable()
					}
					if fi.busy {
						status(L("Importing…"), t.MutedForeground)
						return
					}
					if fi.err != "" {
						status(fi.err, t.DestructiveForeground)
					}
					if fi.replaced != "" {
						status(L("Droi's own copies were moved to %s.", fi.replaced), t.MutedForeground)
						if a.cfg.ShowInFolder != nil && a.smallButton(c, kit.Ghost, L("Show in folder"), "", false).AlignSelf(ui.Start).Clicked() {
							a.cfg.ShowInFolder(fi.replaced)
						}
						return
					}
					status(importedText(fi.copied), t.MutedForeground)
					if len(fi.copied.Skipped) > 0 {
						status(L("Left out, as links that point nowhere or not files: %s", strings.Join(fi.copied.Skipped, ", ")), t.MutedForeground)
					}
					if len(fi.copied.Differs) > 0 {
						status(L("Different in ~/.factory, and Droi's kept: %s", strings.Join(fi.copied.Differs, ", ")), t.MutedForeground)
						differs := fi.copied.Differs
						if a.smallButton(c, kit.Outline, L("Use droid's"), "", false).Label(L("Use droid's copies")).AlignSelf(ui.Start).Clicked() {
							run(func() (host.FirstCopy, string, error) {
								backup, err := h.ReplaceFromFactoryHome(differs)
								return host.FirstCopy{}, backup, err
							})
						}
					}
				})
			})
		})
}

// importedText says what an import brought over.
func importedText(copied host.FirstCopy) string {
	if copied.Empty() {
		return L("Nothing new to import.")
	}
	var parts []string
	switch {
	case copied.Skills == 1:
		parts = append(parts, L("1 skill"))
	case copied.Skills > 1:
		parts = append(parts, L("%d skills", copied.Skills))
	}
	if copied.MCP {
		parts = append(parts, "mcp.json")
	}
	if copied.Agents {
		parts = append(parts, "AGENTS.md")
	}
	return L("Imported %s.", strings.Join(parts, ", "))
}

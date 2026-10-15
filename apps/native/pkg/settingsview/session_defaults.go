package settingsview

import (
	"maps"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
)

// SessionDefaults is Settings' Session defaults section: what a new
// Session starts with, read from and saved to the Daemon. note is the line
// under the section's heading, "" to say the defaults are shared with the
// droid CLI and the Factory App on this computer; banner, when set, draws
// what the app has to say above the settings.
func (s *Settings) SessionDefaults(c *ui.Context, note string, banner func()) {
	k := s.env.Kit()
	t := k.T
	if note == "" {
		note = l10n.L("Shared with the droid CLI and the Factory App on this computer. Existing Sessions keep their settings.")
	}
	k.Text(c, note, 13, 19.5).TextColor(t.MutedForeground).Margin(-k.Px(16), 0, 0, 0)
	if banner != nil {
		banner()
	}
	dv := s.env.Defaults.Get()
	if dv == nil {
		k.Text(c, l10n.L("Loading session defaults…"), 14, 20).TextColor(t.MutedForeground)
		return
	}
	save := func(p defaults.Patch) { s.SaveDefaults(*dv, p) }
	str := func(s string) *string { return &s }
	locked := func(key string) bool { return dv.Locked[key] }
	efforts := func(model, current string) []kit.Option {
		var out []kit.Option
		for _, e := range defaults.ReasoningChoices(dv.Models, model, current) {
			out = append(out, kit.Option{Value: e, Label: EffortLabel(e)})
		}
		return out
	}
	autonomy := make([]kit.Option, 0, len(dv.AvailableAutonomyLevels))
	for _, l := range dv.AvailableAutonomyLevels {
		autonomy = append(autonomy, kit.Option{Value: l, Label: models.AutonomyLabels[l]})
	}
	orgManaged := l10n.L("Managed by your organization.")
	descIf := func(key, otherwise string) string {
		if locked(key) {
			return orgManaged
		}
		return otherwise
	}
	k.SettingGroup(c, l10n.L("Model and autonomy"), "",
		func() {
			k.SettingRow(c, l10n.L("Model"), descIf("modelId", ""), func() {
				ModelField(c, k, s.env.Prefs, l10n.L("Default model"), locked("modelId"), s.Flag("model:default"), &s.picker,
					defaults.PickableModels(dv.Models, true), dv.ModelID, func(next string) {
						p := defaults.Patch{ModelID: str(next)}
						// A level the new model lacks would be refused; start from its first.
						if sup := defaults.ReasoningChoices(dv.Models, next, ""); len(sup) > 0 && !slices.Contains(sup, dv.ReasoningEffort) {
							p.ReasoningEffort = str(sup[0])
						}
						save(p)
					})
			}, nil)
		},
		func() {
			k.SettingRow(c, l10n.L("Reasoning level"), descIf("reasoningEffort", ""), func() {
				if next, ok := s.Select(c, l10n.N("Default reasoning level"), dv.ReasoningEffort, efforts(dv.ModelID, dv.ReasoningEffort), locked("reasoningEffort")); ok {
					save(defaults.Patch{ReasoningEffort: str(next)})
				}
			}, nil)
		},
		func() {
			modes := make([]kit.Option, len(defaults.InteractionModes))
			for i, m := range defaults.InteractionModes {
				modes[i] = kit.Option{Value: m.Value, Label: m.Label}
			}
			k.SettingRow(c, l10n.L("Interaction mode"), l10n.L("Auto starts working right away; Spec plans with you before it changes anything."), func() {
				if next, ok := s.Select(c, l10n.N("Default interaction mode"), dv.InteractionMode, modes, locked("interactionMode")); ok {
					save(defaults.Patch{InteractionMode: str(next)})
				}
			}, nil)
		},
		func() {
			level := dv.AutonomyLevel
			if level == "" {
				level = "off"
			}
			desc := l10n.T(defaults.AutonomyDescriptions[level])
			if desc == "" {
				desc = l10n.L("How much Droid may do without asking for approval.")
			}
			k.SettingRow(c, l10n.L("Autonomy level"), desc, func() {
				if next, ok := s.Select(c, l10n.N("Default autonomy level"), level, autonomy, locked("autonomyLevel")); ok {
					save(defaults.Patch{AutonomyLevel: str(next)})
				}
			}, nil)
		},
	)
	k.SettingGroup(c, l10n.L("Tool calls"), "", func() {
		const follow = "follow-droid"
		opts := []kit.Option{{Value: follow, Label: l10n.L("Follow droid settings")}}
		for _, m := range defaults.ToolModes {
			opts = append(opts, kit.Option{Value: string(m), Label: defaults.ToolModeLabels[m]})
		}
		current := string(defaults.DefaultToolMode(s.env.Prefs))
		if current == "" {
			current = follow
		}
		k.SettingRow(c, l10n.L("Mode"), l10n.L("Script lets the model call tools from a small program that can batch and filter them; Both offers it alongside direct calls. A Session keeps the mode it starts with. Kept on this device."), func() {
			if next, ok := s.Select(c, l10n.N("Default tool calls"), current, opts, false); ok {
				if next == follow {
					next = ""
				}
				defaults.SetDefaultToolMode(s.env.Prefs, defaults.ToolMode(next))
			}
		}, nil)
	})
	const sameAsMain = "same-as-main"
	k.SettingGroup(c, l10n.L("Spec mode"), "",
		func() {
			k.SettingRow(c, l10n.L("Model"), "", func() {
				ModelFieldOr(c, k, s.env.Prefs, l10n.L("Spec mode model"), l10n.L("Same as main"), locked("specModeModelId"), s.Flag("model:spec"), &s.picker,
					defaults.PickableModels(dv.Models, true), dv.SpecModeModelID, func(next string) {
						save(defaults.Patch{SpecModeModelID: str(next), SpecModeReasoningEffort: str("")})
					})
			}, nil)
		},
		func() {
			k.SettingRow(c, l10n.L("Reasoning level"), "", func() {
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
				opts := append([]kit.Option{{Value: sameAsMain, Label: l10n.L("Same as main")}}, efforts(model, cur)...)
				if next, ok := s.Select(c, l10n.N("Spec mode reasoning level"), value, opts, locked("specModeReasoningEffort")); ok {
					if next == sameAsMain {
						next = ""
					}
					save(defaults.Patch{SpecModeReasoningEffort: str(next)})
				}
			}, nil)
		},
		func() {
			const custom = "custom…"
			paths := defaults.SpecSavePaths(dv.UserFactoryDir)
			choice := defaults.SpecSaveChoiceOf(dv.SpecSaveDir, paths)
			value := string(choice)
			opts := []kit.Option{{Value: string(defaults.SpecUser), Label: l10n.L("User home")}, {Value: string(defaults.SpecProject), Label: l10n.L("Project")}}
			desc := l10n.L("Specs go to %s.", paths.User)
			switch choice {
			case defaults.SpecProject:
				desc = l10n.L("Specs go to the project's %s, or %s outside a project.", paths.Project, paths.User)
			case defaults.SpecCustom:
				value = dv.SpecSaveDir
				opts = append(opts, kit.Option{Value: dv.SpecSaveDir, Label: l10n.L("Custom: %s", dv.SpecSaveDir)})
				desc = l10n.L("Specs from every Session go to this folder.")
			}
			if s.env.PickFolder != nil {
				opts = append(opts, kit.Option{Value: custom, Label: l10n.L("Custom…")})
			}
			k.SettingRow(c, l10n.L("Save folder"), descIf("specSaveDir", desc), func() {
				next, ok := s.Select(c, l10n.N("Spec save folder"), value, opts, locked("specSaveDir"))
				switch {
				case !ok:
				case next == custom:
					go func() {
						if dir := s.env.PickFolder(l10n.L("Choose a spec folder")); dir != "" {
							s.env.Update(func() { save(defaults.Patch{SpecSaveDir: &dir}) })
						}
					}()
				case next == string(defaults.SpecUser):
					save(defaults.Patch{SpecSaveDir: str("")})
				case next == string(defaults.SpecProject):
					save(defaults.Patch{SpecSaveDir: str(paths.Project)})
				}
			}, nil)
		},
	)
	k.SettingGroup(c, l10n.L("Compaction"), "",
		func() {
			k.SettingRow(c, l10n.L("Compact automatically"), l10n.L("New Sessions compact their history once it passes the token limit."), func() {
				on := dv.CompactionThresholdCheckEnabled
				if k.Toggle(c, l10n.L("Compact automatically"), on, locked("compactionThresholdCheckEnabled")) {
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
			k.SettingRow(c, l10n.L("Token limit"), "", func() {
				if next, ok := s.Select(c, l10n.N("Compaction token limit"), strconv.Itoa(limit), limitOptions(limit), locked("compactionTokenLimit")); ok {
					n, _ := strconv.Atoi(next)
					save(defaults.Patch{CompactionTokenLimit: &n})
				}
			}, nil)
		},
		func() {
			overrides := dv.CompactionTokenLimitPerModel
			disabled := locked("compactionTokenLimitPerModel")
			label := func(id string) string {
				if i := slices.IndexFunc(dv.Models, func(m models.Choice) bool { return m.ID == id }); i >= 0 {
					return dv.Models[i].Label
				}
				return id
			}
			var addable []models.Choice
			for _, m := range defaults.PickableModels(dv.Models, true) {
				if _, ok := overrides[m.ID]; !ok {
					addable = append(addable, m)
				}
			}
			with := func(id string, tokens int, keep bool) map[string]int {
				next := maps.Clone(overrides)
				if next == nil {
					next = map[string]int{}
				}
				if keep {
					next[id] = tokens
				} else {
					delete(next, id)
				}
				return next
			}
			k.SettingRow(c, l10n.L("Limits for specific models"), descIf("compactionTokenLimitPerModel", l10n.L("Override the token limit for a model.")), nil, func() {
				ui.Column(c).Role(ui.RoleList).Label(l10n.L("Model compaction limits")).Gap(k.Px(8)).Children(func() {
					for _, id := range slices.Sorted(maps.Keys(overrides)) {
						tokens, name := overrides[id], label(id)
						ui.Row(c.Key(id)).Role(ui.RoleListItem).Label(name).Gap(k.Px(8)).Children(func() {
							k.Text(c, name, 14, 20).TextColor(t.Foreground).SingleLine().Grow(1).MinWidth(0)
							if next, ok := s.Select(c, l10n.L("%s compaction limit", name), strconv.Itoa(tokens), limitOptions(tokens), disabled); ok {
								n, _ := strconv.Atoi(next)
								save(defaults.Patch{CompactionTokenLimitPerModel: with(id, n, true)})
							}
							if k.IconButton(c, "x", l10n.L("Remove the %s limit", name), 32).Disabled(disabled).Clicked() {
								save(defaults.Patch{CompactionTokenLimitPerModel: with(id, 0, false)})
							}
						})
					}
					ui.Row(c).Children(func() {
						ModelField(c, k, s.env.Prefs, l10n.L("Add a model limit"), disabled || len(addable) == 0, s.Flag("model:compaction-limit"), &s.picker, addable, "", func(id string) {
							limit := dv.CompactionTokenLimit
							if limit == 0 {
								limit = defaults.DefaultCompactionLimit
							}
							save(defaults.Patch{CompactionTokenLimitPerModel: with(id, limit, true)})
						})
					})
				})
			})
		},
		func() {
			value := dv.CompactionModel
			if value == defaults.CurrentModel {
				value = ""
			}
			k.SettingRow(c, l10n.L("Summary model"), l10n.L("The model that writes the summary."), func() {
				ModelFieldOr(c, k, s.env.Prefs, l10n.L("Compaction model"), l10n.L("Current model"), locked("compactionModel"), s.Flag("model:compaction"), &s.picker,
					defaults.PickableModels(dv.Models, false), value, func(next string) {
						if next == "" {
							next = defaults.CurrentModel
						}
						save(defaults.Patch{CompactionModel: str(next)})
					})
			}, nil)
		},
	)
	const inherit = "inherit"
	k.SettingGroup(c, l10n.L("Subagents"), "",
		func() {
			opts := append([]kit.Option{{Value: inherit, Label: l10n.L("Inherit (calling session)")}}, autonomy...)
			value := dv.SubagentAutonomyLevel
			if value == "" {
				value = inherit
			}
			k.SettingRow(c, l10n.L("Autonomy level"), "", func() {
				if next, ok := s.Select(c, l10n.N("Subagent autonomy level"), value, opts, locked("subagentAutonomyLevel")); ok {
					save(defaults.Patch{SubagentAutonomyLevel: str(next)})
				}
			}, nil)
		},
		func() {
			const modelDefault = "model-default"
			settings := dv.SubagentModelSettings
			k.SettingRow(c, l10n.L("Task models"), l10n.L("The model and reasoning level for subagents started with light, medium or heavy complexity."), nil, func() {
				ui.Column(c).Gap(k.Px(8)).Children(func() {
					for _, tier := range subagentTierLabels {
						model, effort := settings[tier.id+"Model"], settings[tier.id+"ReasoningEffort"]
						ui.Row(c.Key(tier.id)).Gap(k.Px(8)).Children(func() {
							k.Text(c, l10n.T(tier.name), 14, 20).SingleLine().Width(k.Px(112)).Shrink(0)
							ModelFieldOr(c, k, s.env.Prefs, l10n.T(tier.model), l10n.L("Inherit (calling session)"), locked("subagent."+tier.id+"Model"), s.Flag("model:subagent-"+tier.id), &s.picker,
								defaults.PickableModels(dv.Models, true), model, func(next string) {
									save(defaults.Patch{SubagentModelSettings: defaults.WithSubagentTierModel(settings, tier.id, next)})
								})
							if model == "" {
								return
							}
							cur := effort
							if cur == "" {
								cur = modelDefault
							}
							levels := append([]kit.Option{{Value: modelDefault, Label: l10n.L("Model default")}}, efforts(model, effort)...)
							if next, ok := s.Select(c, tier.reasoning, cur, levels, locked("subagent."+tier.id+"ReasoningEffort")); ok {
								if next == modelDefault {
									next = ""
								}
								save(defaults.Patch{SubagentModelSettings: defaults.WithSubagentTierReasoning(settings, tier.id, next)})
							}
						})
					}
				})
			})
		},
	)
}

// subagentTierLabels name the Subagent complexity tiers, in
// defaults.SubagentTiers' order.
var subagentTierLabels = []struct{ id, name, model, reasoning string }{
	{"light", l10n.N("Light task"), l10n.N("Light task model"), l10n.N("Light task reasoning level")},
	{"medium", l10n.N("Medium task"), l10n.N("Medium task model"), l10n.N("Medium task reasoning level")},
	{"heavy", l10n.N("Heavy task"), l10n.N("Heavy task model"), l10n.N("Heavy task reasoning level")},
}

// limitOptions are the compaction token limits to pick from, with the
// current one even when Factory does not offer it.
func limitOptions(current int) []kit.Option {
	vals := append([]int{}, defaults.CompactionLimits...)
	if !containsInt(vals, current) {
		vals = append(vals, current)
	}
	sortInts(vals)
	opts := make([]kit.Option, len(vals))
	for i, v := range vals {
		opts[i] = kit.Option{Value: strconv.Itoa(v), Label: defaults.TokenLimitLabel(v)}
	}
	return opts
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

// Package defaults is what every new Session starts with, as the Daemon keeps
// it in ~/.factory/settings.json (shared with the droid CLI and the Factory
// App): the view the settings pages show, and the pure rules they edit it by
// (a port of session-defaults.ts and tool-mode.ts).
package defaults

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/apps/native/internal/models"
)

var SubagentTiers = []string{"light", "medium", "heavy"}

// SubagentModelSettings holds `<tier>Model` and `<tier>ReasoningEffort`.
type SubagentModelSettings map[string]string

// View is the defaults as the settings pages show them. An empty string or a
// zero number is a setting the Daemon has no value for.
type View struct {
	ModelID                         string
	ReasoningEffort                 string
	InteractionMode                 string
	AutonomyLevel                   string
	SpecModeModelID                 string
	SpecModeReasoningEffort         string
	SpecSaveDir                     string
	CompactionTokenLimit            int
	CompactionTokenLimitPerModel    map[string]int
	CompactionModel                 string
	CompactionThresholdCheckEnabled bool
	SubagentAutonomyLevel           string
	SubagentModelSettings           SubagentModelSettings
	Models                          []models.Choice
	AvailableAutonomyLevels         []string
	// UserFactoryDir is where the Daemon keeps the user's own files
	// (~/.factory), for the spec folder choices.
	UserFactoryDir string
	// WorktreeDirectory is where the Daemon makes worktrees, "" for its
	// default (~/.factory/worktrees); WorktreeAutoDeleteLimit how many
	// ephemeral ones it keeps, 0 for its default.
	WorktreeDirectory       string
	WorktreeAutoDeleteLimit int
	// Locked holds the settings the organization manages; shown but not
	// editable.
	Locked map[string]bool
}

// Patch is what `daemon.update_session_defaults` takes. A nil field is left
// alone. For the three "same as main" choices a pointer to "" clears the
// choice.
type Patch struct {
	ModelID                         *string
	ReasoningEffort                 *string
	InteractionMode                 *string
	AutonomyLevel                   *string
	SpecModeModelID                 *string
	SpecModeReasoningEffort         *string
	SpecSaveDir                     *string
	CompactionTokenLimit            *int
	CompactionTokenLimitPerModel    map[string]int
	CompactionModel                 *string
	CompactionThresholdCheckEnabled *bool
	SubagentAutonomyLevel           *string
	// SubagentModelSettings replaces the settings as a whole when non-nil.
	SubagentModelSettings SubagentModelSettings
	// WorktreeDirectory "" goes back to the Daemon's default folder.
	WorktreeDirectory       *string
	WorktreeAutoDeleteLimit *int
}

// Params is the patch as the Daemon's request fields; a cleared choice is null.
func (p Patch) Params() map[string]any {
	out := map[string]any{}
	put := func(key string, v *string, nullable bool) {
		switch {
		case v == nil:
		case nullable && *v == "":
			out[key] = nil
		default:
			out[key] = *v
		}
	}
	put("modelId", p.ModelID, false)
	put("reasoningEffort", p.ReasoningEffort, false)
	put("interactionMode", p.InteractionMode, false)
	put("autonomyLevel", p.AutonomyLevel, false)
	put("specModeModelId", p.SpecModeModelID, true)
	put("specModeReasoningEffort", p.SpecModeReasoningEffort, true)
	put("specSaveDir", p.SpecSaveDir, true)
	put("compactionModel", p.CompactionModel, false)
	put("subagentAutonomyLevel", p.SubagentAutonomyLevel, false)
	put("worktreeDirectory", p.WorktreeDirectory, true)
	if p.WorktreeAutoDeleteLimit != nil {
		out["worktreeAutoDeleteLimit"] = *p.WorktreeAutoDeleteLimit
	}
	if p.CompactionTokenLimit != nil {
		out["compactionTokenLimit"] = *p.CompactionTokenLimit
	}
	if p.CompactionTokenLimitPerModel != nil {
		out["compactionTokenLimitPerModel"] = p.CompactionTokenLimitPerModel
	}
	if p.CompactionThresholdCheckEnabled != nil {
		out["compactionThresholdCheckEnabled"] = *p.CompactionThresholdCheckEnabled
	}
	if p.SubagentModelSettings != nil {
		out["subagentModelSettings"] = p.SubagentModelSettings
	}
	return out
}

type Mode struct{ Value, Label string }

var InteractionModes = []Mode{{"auto", l10n.N("Auto")}, {"spec", l10n.N("Spec")}}

var AutonomyDescriptions = map[string]string{
	"off":    l10n.N("Require approval for all actions"),
	"low":    l10n.N("Allow file edits and read-only commands"),
	"medium": l10n.N("Allow reversible commands"),
	"high":   l10n.N("Allow all commands"),
}

// CompactionLimits are the ones Factory offers; a stored value outside them
// is still shown.
var CompactionLimits = []int{
	100_000, 200_000, 250_000, 300_000, 400_000, 500_000, 600_000, 700_000, 800_000, 900_000,
	1_000_000,
}

const DefaultCompactionLimit = 250_000

// CurrentModel is the `compactionModel` for "compact with the Session's own model".
const CurrentModel = "current-model"

// TokenLimitLabel writes 250000 as "250K" and 1000000 as "1M".
func TokenLimitLabel(tokens int) string {
	if tokens >= 1_000_000 {
		return strconv.FormatFloat(float64(tokens)/1_000_000, 'f', -1, 64) + "M"
	}
	return strconv.FormatFloat(float64(tokens)/1_000, 'f', -1, 64) + "K"
}

// ToSessionDefaults reads the Daemon's `get_default_settings` answer (or
// `update_session_defaults`' `defaults`), decoded as generic JSON.
func ToSessionDefaults(raw map[string]any) View {
	str := func(key string) string {
		s, _ := raw[key].(string)
		return s
	}
	or := func(s, fallback string) string {
		if s == "" {
			return fallback
		}
		return s
	}
	v := View{
		ModelID:                         str("modelId"),
		ReasoningEffort:                 str("reasoningEffort"),
		InteractionMode:                 or(str("interactionMode"), "auto"),
		AutonomyLevel:                   str("autonomyLevel"),
		SpecModeModelID:                 str("specModeModelId"),
		SpecModeReasoningEffort:         str("specModeReasoningEffort"),
		SpecSaveDir:                     str("specSaveDir"),
		CompactionTokenLimitPerModel:    map[string]int{},
		CompactionModel:                 or(str("compactionModel"), CurrentModel),
		CompactionThresholdCheckEnabled: raw["compactionThresholdCheckEnabled"] != false,
		SubagentAutonomyLevel:           or(str("subagentAutonomyLevel"), "inherit"),
		SubagentModelSettings:           SubagentModelSettings{},
		AvailableAutonomyLevels:         []string{"off", "low", "medium", "high"},
		Locked:                          lockedKeys(raw["management"], ""),
	}
	if n, ok := raw["compactionTokenLimit"].(float64); ok {
		v.CompactionTokenLimit = int(n)
	}
	if m, ok := raw["compactionTokenLimitPerModel"].(map[string]any); ok {
		for k, n := range m {
			if f, ok := n.(float64); ok {
				v.CompactionTokenLimitPerModel[k] = int(f)
			}
		}
	}
	if m, ok := raw["subagentModelSettings"].(map[string]any); ok {
		for k, s := range m {
			if s, ok := s.(string); ok {
				v.SubagentModelSettings[k] = s
			}
		}
	}
	if list, ok := raw["availableModels"]; ok {
		if b, err := json.Marshal(list); err == nil {
			v.Models, _ = models.DecodeChoices(b)
		}
	}
	if levels, ok := raw["availableAutonomyLevels"].([]any); ok {
		v.AvailableAutonomyLevels = nil
		for _, l := range levels {
			if s, ok := l.(string); ok {
				v.AvailableAutonomyLevels = append(v.AvailableAutonomyLevels, s)
			}
		}
	}
	if presets, ok := raw["specSavePresets"].(map[string]any); ok {
		v.UserFactoryDir, _ = presets["userFactoryDir"].(string)
	}
	v.WorktreeDirectory = str("worktreeDirectory")
	if n, ok := raw["worktreeAutoDeleteLimit"].(float64); ok {
		v.WorktreeAutoDeleteLimit = int(n)
	}
	return v
}

// lockedKeys: `management` marks org-managed keys `{disabled: true}`; nested
// groups become `subagent.lightModel`.
func lockedKeys(management any, prefix string) map[string]bool {
	locked := map[string]bool{}
	group, _ := management.(map[string]any)
	for key, value := range group {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if disabled, has := entry["disabled"]; has {
			if disabled == true {
				locked[prefix+key] = true
			}
		} else {
			maps.Copy(locked, lockedKeys(entry, prefix+key+"."))
		}
	}
	return locked
}

// ApplyPatch is the view after a patch, before the Daemon confirms it.
func ApplyPatch(view View, p Patch) View {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&view.ModelID, p.ModelID)
	set(&view.ReasoningEffort, p.ReasoningEffort)
	set(&view.InteractionMode, p.InteractionMode)
	set(&view.AutonomyLevel, p.AutonomyLevel)
	set(&view.SpecModeModelID, p.SpecModeModelID)
	set(&view.SpecModeReasoningEffort, p.SpecModeReasoningEffort)
	set(&view.SpecSaveDir, p.SpecSaveDir)
	set(&view.CompactionModel, p.CompactionModel)
	set(&view.SubagentAutonomyLevel, p.SubagentAutonomyLevel)
	set(&view.WorktreeDirectory, p.WorktreeDirectory)
	if p.WorktreeAutoDeleteLimit != nil {
		view.WorktreeAutoDeleteLimit = *p.WorktreeAutoDeleteLimit
	}
	if p.CompactionTokenLimit != nil {
		view.CompactionTokenLimit = *p.CompactionTokenLimit
	}
	if p.CompactionTokenLimitPerModel != nil {
		view.CompactionTokenLimitPerModel = p.CompactionTokenLimitPerModel
	}
	if p.CompactionThresholdCheckEnabled != nil {
		view.CompactionThresholdCheckEnabled = *p.CompactionThresholdCheckEnabled
	}
	if p.SubagentModelSettings != nil {
		view.SubagentModelSettings = p.SubagentModelSettings
	}
	return view
}

// ReasoningChoices are the reasoning levels a model supports. Without the
// model (none chosen, or one the Daemon no longer lists) the current value is
// all there is.
func ReasoningChoices(list []models.Choice, modelID, current string) []string {
	if i := slices.IndexFunc(list, func(m models.Choice) bool { return m.ID == modelID }); i >= 0 {
		if supported := list[i].ReasoningEfforts; len(supported) > 0 {
			return supported
		}
	}
	if current != "" {
		return []string{current}
	}
	return []string{"medium"}
}

// PickableModels are the models a setting may pick: listed and not disabled;
// the router is no use for compaction, so routers=false leaves it out.
func PickableModels(list []models.Choice, routers bool) []models.Choice {
	var out []models.Choice
	for _, m := range list {
		if !m.Disabled && (routers || m.Provider != "") {
			out = append(out, m)
		}
	}
	return out
}

// WithSubagentTierModel: the Daemon replaces `subagentModelSettings` as a
// whole, so a change to one tier sends every tier. An empty model hands the
// tier back to the calling Session's model ("Inherit"); either way the tier's
// reasoning level goes with it, and a new model starts from its own default.
func WithSubagentTierModel(settings SubagentModelSettings, tier, model string) SubagentModelSettings {
	next := maps.Clone(settings)
	if next == nil {
		next = SubagentModelSettings{}
	}
	delete(next, tier+"ReasoningEffort")
	if model == "" {
		delete(next, tier+"Model")
	} else {
		next[tier+"Model"] = model
	}
	return next
}

// WithSubagentTierReasoning sets a tier's reasoning level; an empty level
// leaves the tier on its model's own default.
func WithSubagentTierReasoning(settings SubagentModelSettings, tier, effort string) SubagentModelSettings {
	next := maps.Clone(settings)
	if next == nil {
		next = SubagentModelSettings{}
	}
	if effort == "" {
		delete(next, tier+"ReasoningEffort")
	} else {
		next[tier+"ReasoningEffort"] = effort
	}
	return next
}

type SpecSaveChoice string

const (
	SpecUser    SpecSaveChoice = "user"
	SpecProject SpecSaveChoice = "project"
	SpecCustom  SpecSaveChoice = "custom"
)

type SpecPaths struct{ User, Project string }

// SpecSavePaths: where spec files go, as the Factory App offers it: under the
// user's ~/.factory/docs (the Daemon's default, stored as nothing), under the
// project's .factory/docs (a path relative to the Workspace), or a folder of
// the user's choosing.
func SpecSavePaths(userFactoryDir string) SpecPaths {
	folder := ".factory"
	parts := strings.FieldsFunc(userFactoryDir, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) > 0 {
		folder = parts[len(parts)-1]
	}
	return SpecPaths{User: "~/" + folder + "/docs", Project: folder + "/docs"}
}

// SpecSaveChoiceOf classifies the stored folder; "" is nothing stored.
func SpecSaveChoiceOf(stored string, paths SpecPaths) SpecSaveChoice {
	switch stored {
	case "", paths.User:
		return SpecUser
	case paths.Project:
		return SpecProject
	}
	return SpecCustom
}

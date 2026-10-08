// Package skills is Skills as the Daemon lists them for a Session, and the one
// switch a Client may offer on each. The Daemon owns the state: a switch
// writes `disabledSkills` to `~/.factory/settings.json` (user level, every
// project) or to the Workspace's `.factory/settings.json` (project level);
// the droid CLI and the Factory App read the same files. Which skills get a
// switch follows the Factory App: a project skill and a built-in one do, a
// personal skill (`~/.factory/skills`) is managed by editing its files, and
// one the organization turned off is read-only. Where the switch writes is one
// rule: a project skill to its project, a built-in one for the user (a port of
// skills.ts).
package skills

import (
	"slices"

	"github.com/kkkk2323/droi/apps/native/internal/collate"
	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// Level is where a switch is written.
type Level string

const (
	User    Level = "user"
	Project Level = "project"
)

// Switch is the one switch a skill gets.
type Switch struct {
	// On is whether the skill is on now; flipping writes `disabled: On` at Level.
	On    bool
	Level Level
}

var LocationLabels = map[string]string{
	"project":    l10n.N("Project"),
	"personal":   l10n.N("Personal"),
	"builtin":    l10n.N("Built-in"),
	"automation": l10n.N("Automation"),
}

var LocationOrder = []string{"project", "personal", "builtin", "automation"}

func disabledLevels(skill protocol.SkillInfo) []string {
	if skill.DisabledBy == nil || skill.DisabledBy.Kind != protocol.SkillDisabledByKindLedger {
		return nil
	}
	v, err := skill.DisabledBy.Value()
	ledger, ok := v.(*protocol.SkillDisabledByLedger)
	if err != nil || !ok {
		return nil
	}
	var levels []string
	for _, source := range ledger.Sources {
		levels = append(levels, string(source.Level))
	}
	return levels
}

// SkillSwitch is the switch for a skill; false when nothing here can change
// it: a personal skill, one with no Workspace to write a project switch into,
// one turned off in its own file, and one turned off at a level this switch
// does not write (the organization's, or the other of user and project).
func SkillSwitch(skill protocol.SkillInfo, projectAvailable bool) (Switch, bool) {
	var level Level
	switch skill.Location {
	case protocol.SkillLocationProject:
		if !projectAvailable {
			return Switch{}, false
		}
		level = Project
	case protocol.SkillLocationBuiltin:
		level = User
	default:
		return Switch{}, false
	}
	if skill.Enabled == nil || *skill.Enabled {
		return Switch{On: true, Level: level}, true
	}
	levels := disabledLevels(skill)
	if len(levels) == 0 || slices.ContainsFunc(levels, func(l string) bool { return l != string(level) }) {
		return Switch{}, false
	}
	return Switch{On: false, Level: level}, true
}

// DisabledLabel says why a skill is off, for its badge.
func DisabledLabel(skill protocol.SkillInfo) string {
	levels := disabledLevels(skill)
	switch {
	case slices.Contains(levels, "org"):
		return l10n.L("Disabled by organization")
	case skill.DisabledBy != nil && skill.DisabledBy.Kind == protocol.SkillDisabledByKindFrontmatter:
		return l10n.L("Disabled in its file")
	case len(levels) > 0 && !slices.ContainsFunc(levels, func(l string) bool { return l != "project" }):
		return l10n.L("Disabled for this project")
	}
	return l10n.L("Disabled")
}

type Group struct {
	Location string
	Skills   []protocol.SkillInfo
}

// GroupSkills groups the skills by where they live, in a fixed order, each
// group sorted by name.
func GroupSkills(list []protocol.SkillInfo) []Group {
	var groups []Group
	for _, skill := range list {
		location := string(skill.Location)
		i := slices.IndexFunc(groups, func(g Group) bool { return g.Location == location })
		if i < 0 {
			groups = append(groups, Group{Location: location})
			i = len(groups) - 1
		}
		groups[i].Skills = append(groups[i].Skills, skill)
	}
	order := func(location string) int {
		if i := slices.Index(LocationOrder, location); i >= 0 {
			return i
		}
		return len(LocationOrder)
	}
	slices.SortStableFunc(groups, func(a, b Group) int {
		if c := order(a.Location) - order(b.Location); c != 0 {
			return c
		}
		return collate.Compare(a.Location, b.Location)
	})
	for _, g := range groups {
		slices.SortStableFunc(g.Skills, func(a, b protocol.SkillInfo) int { return collate.Compare(a.Name, b.Name) })
	}
	return groups
}

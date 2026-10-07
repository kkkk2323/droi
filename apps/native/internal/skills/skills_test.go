package skills

import (
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

func skill(location protocol.SkillLocation) protocol.SkillInfo {
	on := true
	return protocol.SkillInfo{Name: "review", Location: location, FilePath: "/skills/review/SKILL.md", Enabled: &on}
}

func off(location protocol.SkillLocation, levels ...string) protocol.SkillInfo {
	s := skill(location)
	no := false
	s.Enabled = &no
	var sources []protocol.SkillDisabledByLedgerSourcesItem
	for _, l := range levels {
		sources = append(sources, protocol.SkillDisabledByLedgerSourcesItem{Level: protocol.SettingsLevel(l)})
	}
	by, err := protocol.NewSkillDisabledBy(protocol.SkillDisabledByLedger{Kind: "ledger", Sources: sources})
	if err != nil {
		panic(err)
	}
	s.DisabledBy = &by
	return s
}

func inFile() protocol.SkillInfo {
	s := off(protocol.SkillLocationBuiltin)
	by, _ := protocol.NewSkillDisabledBy(protocol.SkillDisabledByFrontmatter{Kind: "frontmatter"})
	s.DisabledBy = &by
	return s
}

func TestSkillSwitch(t *testing.T) {
	builtin, project := protocol.SkillLocationBuiltin, protocol.SkillLocationProject
	for _, c := range []struct {
		name    string
		skill   protocol.SkillInfo
		project bool
		want    *Switch
	}{
		{"built-in with a project", skill(builtin), true, &Switch{true, User}},
		{"built-in without a project", skill(builtin), false, &Switch{true, User}},
		{"project skill", skill(project), true, &Switch{true, Project}},
		{"project skill without a project", skill(project), false, nil},
		{"off at the user level", off(builtin, "user"), true, &Switch{false, User}},
		{"off at the project level", off(project, "project"), true, &Switch{false, Project}},
		{"off for the project, built-in", off(builtin, "project"), true, nil},
		{"off for user and project", off(builtin, "user", "project"), true, nil},
		{"off for the organization", off(builtin, "org"), true, nil},
		{"off in its own file", inFile(), true, nil},
		{"personal", skill(protocol.SkillLocationPersonal), true, nil},
	} {
		got, ok := SkillSwitch(c.skill, c.project)
		if c.want == nil && ok || c.want != nil && (!ok || got != *c.want) {
			t.Errorf("%s: got %+v, %v", c.name, got, ok)
		}
	}
}

func TestDisabledLabel(t *testing.T) {
	b := protocol.SkillLocationBuiltin
	for want, s := range map[string]protocol.SkillInfo{
		"Disabled by organization":  off(b, "org"),
		"Disabled for this project": off(b, "project"),
		"Disabled":                  off(b, "user"),
		"Disabled in its file":      inFile(),
	} {
		if got := DisabledLabel(s); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if got := DisabledLabel(off(b, "user", "project")); got != "Disabled" {
		t.Errorf("user and project: %q", got)
	}
}

func TestGroupSkills(t *testing.T) {
	named := func(name string, location protocol.SkillLocation) protocol.SkillInfo {
		s := skill(location)
		s.Name = name
		return s
	}
	groups := GroupSkills([]protocol.SkillInfo{
		named("zeta", protocol.SkillLocationBuiltin),
		named("alpha", protocol.SkillLocationBuiltin),
		named("mine", protocol.SkillLocationPersonal),
		named("ours", protocol.SkillLocationProject),
	})
	got := map[string][]string{}
	var order []string
	for _, g := range groups {
		order = append(order, g.Location)
		for _, s := range g.Skills {
			got[g.Location] = append(got[g.Location], s.Name)
		}
	}
	if !reflect.DeepEqual(order, []string{"project", "personal", "builtin"}) {
		t.Errorf("order = %v", order)
	}
	if !reflect.DeepEqual(got["builtin"], []string{"alpha", "zeta"}) || !reflect.DeepEqual(got["project"], []string{"ours"}) {
		t.Errorf("names = %v", got)
	}
}

// Package slash is what "/" can start in the composer: the commands the
// Client runs itself, the Workspace's custom commands and the skills a user
// may invoke. The Daemon expands "/name args" itself when the message arrives,
// so for the latter two the Client only has to offer the names (a port of the
// pure parts of use-slash-items.ts).
package slash

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

type Kind string

const (
	Command Kind = "command"
	Skill   Kind = "skill"
)

type Item struct {
	Name        string
	Description string
	// ArgumentHint is a hint for what follows the name, e.g. "command arguments".
	ArgumentHint string
	Kind         Kind
}

// Builtins are the commands the composer handles before anything reaches the
// Daemon.
var Builtins = []Item{
	{
		Name:         "compact",
		Description:  l10n.N("Summarise the conversation and continue in a new session"),
		ArgumentHint: l10n.N("[instructions]"),
		Kind:         Command,
	},
}

// FromDaemon lists the Daemon's custom commands and the skills a user may
// invoke, a name once, commands first.
func FromDaemon(commands []protocol.CustomCommandInfo, skills []protocol.SkillInfo) []Item {
	var items []Item
	taken := map[string]bool{}
	for _, c := range commands {
		if taken[c.Name] {
			continue
		}
		taken[c.Name] = true
		items = append(items, Item{Name: c.Name, Description: c.Description, ArgumentHint: c.ArgumentHint, Kind: Command})
	}
	for _, s := range skills {
		if s.UserInvocable != nil && !*s.UserInvocable || s.Enabled != nil && !*s.Enabled || taken[s.Name] {
			continue
		}
		items = append(items, Item{Name: s.Name, Description: s.Description, Kind: Skill})
	}
	return items
}

// Merge puts the builtins, which the caller handles itself, first; they win
// over a Daemon item of the same name. Without a Session only they are offered.
func Merge(builtins, fromDaemon []Item) []Item {
	if len(fromDaemon) == 0 {
		return builtins
	}
	out := slices.Clone(builtins)
	for _, item := range fromDaemon {
		if !slices.ContainsFunc(builtins, func(b Item) bool { return b.Name == item.Name }) {
			out = append(out, item)
		}
	}
	return out
}

var leadingWord = regexp.MustCompile(`^/(\S*)$`)

// Query is the "/word" being typed at the start of the composer; false when
// there is none. caret is a byte offset into text.
func Query(text string, caret int) (string, bool) {
	if caret < 0 || caret > len(text) {
		return "", false
	}
	m := leadingWord.FindStringSubmatch(text[:caret])
	if m == nil || strings.IndexFunc(text[caret:], unicode.IsSpace) >= 0 {
		return "", false
	}
	return m[1], true
}

// DefaultLimit is how many items Filter offers.
const DefaultLimit = 8

// Filter lists prefix matches first, then names containing the query, up to limit.
func Filter(items []Item, query string, limit int) []Item {
	q := strings.ToLower(query)
	var starts, contains []Item
	for _, item := range items {
		name := strings.ToLower(item.Name)
		if strings.HasPrefix(name, q) {
			starts = append(starts, item)
		} else if strings.Contains(name, q) {
			contains = append(contains, item)
		}
	}
	out := append(starts, contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

var completeName = regexp.MustCompile(`^/(\S+) `)

// Picked is the command or skill a message starts with, once its name is
// complete ("/name " then the rest): the composer shows it as a tag before
// the text.
func Picked(text string, items []Item) (item Item, rest string, ok bool) {
	m := completeName.FindStringSubmatch(text)
	if m == nil {
		return Item{}, "", false
	}
	i := slices.IndexFunc(items, func(it Item) bool { return it.Name == m[1] })
	if i < 0 {
		return Item{}, "", false
	}
	return items[i], text[len(m[0]):], true
}

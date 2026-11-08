package host

import (
	"maps"
	"slices"
	"strconv"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// The Kernel's approval policy is the Host's to keep (design/m3-kernel.md §2.5): the Kernel holds a copy, which is
// gone when it restarts, and the window pushes it again on every connect. The default level is the Session
// defaults' autonomy, which the Daemon keeps while it runs beside the Kernel; what is stored here is what each
// conversation has of its own.

// ConversationPolicy is one conversation's own approval policy: a level over the default's, "" to follow it, and
// the rules its "always allow" answers added.
type ConversationPolicy struct {
	Autonomy string              `json:"autonomy,omitempty"`
	Rules    []kernel.PolicyRule `json:"rules,omitempty"`
}

// ConversationPolicies are the stored policies, by conversation.
func (h *Host) ConversationPolicies() map[kernel.ID]ConversationPolicy {
	stored := h.Settings.Get().ConversationPolicies
	out := make(map[kernel.ID]ConversationPolicy, len(stored))
	for key, p := range stored {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			continue
		}
		out[kernel.ID(id)] = ConversationPolicy{Autonomy: p.Autonomy, Rules: slices.Clone(p.Rules)}
	}
	return out
}

// SetConversationPolicy stores conversation id's own policy; an empty one is removed.
func (h *Host) SetConversationPolicy(id kernel.ID, p ConversationPolicy) error {
	key := strconv.FormatInt(int64(id), 10)
	if old, ok := h.Settings.Get().ConversationPolicies[key]; ok && old.Autonomy == p.Autonomy && slices.Equal(old.Rules, p.Rules) ||
		!ok && p.Autonomy == "" && len(p.Rules) == 0 {
		return nil
	}
	return h.Settings.Update(func(s *Settings) {
		// A copy: Get hands out the map it had, and a reader may hold it.
		next := maps.Clone(s.ConversationPolicies)
		if next == nil {
			next = map[string]ConversationPolicy{}
		}
		if p.Autonomy == "" && len(p.Rules) == 0 {
			delete(next, key)
		} else {
			next[key] = ConversationPolicy{Autonomy: p.Autonomy, Rules: slices.Clone(p.Rules)}
		}
		s.ConversationPolicies = next
	})
}

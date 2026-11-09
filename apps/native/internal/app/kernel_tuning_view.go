package app

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/models"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/settingsview"
)

// minContextWindow is the smallest context window Settings takes for a model: the Kernel keeps 16,384 tokens free
// for the answer and the newest 20,000 verbatim in a summary, so a smaller window has nothing left to compact.
const minContextWindow = 32_000

// codemodeGroup is codemode's mode on the Kernel, pi's codemode.mode: on or only. It is the Host's, so every
// Session has it from its next message.
func (a *App) codemodeGroup(c *ui.Context) {
	h := a.cfg.Host
	opts := []kit.Option{{Value: kernel.CodemodeOn, Label: "on"}, {Value: kernel.CodemodeOnly, Label: "only"}}
	a.settingGroup(c, L("Codemode"), "", func() {
		a.settingRow(c, L("Mode"), L("on: the model gets every tool, and codemode beside them to run a script that calls them. only: the model gets codemode, read, and the tools a script cannot call (ask_user, Memory), and calls every other tool from a script. Every Session has it from its next message."), func() {
			if next, ok := a.fieldSelect(c, L("Codemode mode"), h.KernelCodemode(), opts, false); ok {
				if err := h.SetKernelCodemode(next); err != nil {
					a.settings.err = err.Error()
				}
			}
		}, nil)
	})
}

// compactionGroup is the model that writes every compaction's summary on the Kernel, or each Session's own. It is
// the Host's, so every Session has it from its next compaction.
func (a *App) compactionGroup(c *ui.Context) {
	h := a.cfg.Host
	current, thinking := h.KernelCompactionModel(), h.KernelCompactionThinking()
	choices := a.keyedKernelChoices()
	if current != "" && !slices.ContainsFunc(choices, func(m models.Choice) bool { return m.ID == current }) {
		// A model its source no longer lists stays shown, so the setting is not hidden; the Kernel uses each
		// Session's own until it is listed again.
		choices = append(choices, models.Choice{ID: current, Label: current})
	}
	a.settingGroup(c, L("Compaction"), "", func() {
		a.settingRow(c, L("Summary model"), L("The model that writes the summary when a Session is compacted. A cheaper, faster model makes compaction cheaper; if it fails, the Session's own model writes the summary."), func() {
			settingsview.ModelEffortField(c, a.kit, a.prefs, L("Compaction model"), L("Session's own model"), false,
				a.settings.Flag("model:kernel-compaction"), a.settings.Picker(), choices, current, thinking,
				func(next string) {
					// A new model starts from its own default level.
					if err := h.SetKernelCompactionModel(next, ""); err != nil {
						a.settings.err = err.Error()
					}
				},
				func(level string) {
					if err := h.SetKernelCompactionModel(current, level); err != nil {
						a.settings.err = err.Error()
					}
				})
		}, nil)
	})
}

// keyedKernelChoices are the models the Kernel offers, for a Settings picker that names a model by host.ModelKey
// (its provider and id) rather than by id alone, as the Kernel's own settings do. Without a connection, the models
// the Model Sources list, without their thinking levels.
func (a *App) keyedKernelChoices() []models.Choice {
	offered := a.kernel.offered()
	if len(offered) == 0 {
		var out []models.Choice
		for _, m := range a.cfg.Host.KernelModelContexts() {
			out = append(out, models.Choice{ID: host.ModelKey(m.Provider, m.ID), Label: contextModelLabel(m), Provider: m.Provider})
		}
		return out
	}
	byID := map[string]models.Choice{}
	for _, ch := range a.kernelChoices() {
		byID[ch.ID] = ch
	}
	out := make([]models.Choice, 0, len(offered))
	for _, m := range offered {
		ch, ok := byID[m.ID]
		if !ok {
			ch = models.Choice{Label: cmp.Or(m.Name, m.ID), ReasoningEfforts: m.Thinking, Provider: m.Provider}
		}
		ch.ID = host.ModelKey(m.Provider, m.ID)
		if m.Provider == "magpie" {
			ch.Label += " · Magpie"
		}
		out = append(out, ch)
	}
	return out
}

// contextWindowsGroup sets the context window of the Kernel's models in Settings → Advanced: a window set smaller
// than the one the model's source lists is also where the Kernel compacts it.
func (a *App) contextWindowsGroup(c *ui.Context) {
	h := a.cfg.Host
	listed := h.KernelModelContexts()
	if len(listed) == 0 {
		return
	}
	k := a.kit
	s := &a.settings
	choices := make([]models.Choice, len(listed))
	var picked *host.KernelModelContext
	for i := range listed {
		m := &listed[i]
		key := host.ModelKey(m.Provider, m.ID)
		choices[i] = models.Choice{ID: key, Label: contextModelLabel(*m), Provider: m.Provider}
		if key == s.contextModel {
			picked = m
		}
	}
	if picked == nil {
		picked = &listed[0]
		s.contextModel = host.ModelKey(picked.Provider, picked.ID)
	}
	save := func() {
		tokens, ok := parseTokens(s.contextTokens)
		switch {
		case !ok:
			s.err = L("Enter the context window in tokens, such as 200000 or 200k.")
			return
		case tokens != 0 && tokens < minContextWindow:
			s.err = L("A context window must be at least %s tokens.", groupedTokens(minContextWindow))
			return
		}
		s.err = ""
		if err := h.SetModelContextWindow(picked.Provider, picked.ID, tokens); err != nil {
			s.err = err.Error()
			return
		}
		s.contextTokens = ""
	}
	rows := []func(){
		func() {
			a.settingRow(c, L("Model"), L("Its source lists %s tokens. Leave the field empty and save to use that again.", groupedTokens(picked.Listed)), func() {
				a.modelField(c, L("Context window model"), false, s.Flag("model:context-window"), s.Picker(), choices, s.contextModel, func(next string) {
					s.contextModel, s.contextTokens = next, ""
				})
			}, func() {
				placeholder := groupedTokens(picked.Listed)
				if picked.Set > 0 {
					placeholder = groupedTokens(picked.Set)
				}
				ui.Row(c).Gap(k.Px(8)).Children(func() {
					in := a.field(c, &s.contextTokens, L("Context window in tokens"), placeholder, true, false)
					if in.Submitted() {
						save()
					}
					if a.smallButton(c, kit.Outline, L("Save"), "", false).Label(L("Save context window")).Clicked() {
						save()
					}
				})
			})
		},
	}
	for _, m := range listed {
		if m.Set == 0 {
			continue
		}
		rows = append(rows, func() {
			a.settingRow(c, contextModelLabel(m), L("%s tokens, in place of the %s its source lists.", groupedTokens(m.Set), groupedTokens(m.Listed)), func() {
				if a.smallButton(c, kit.Ghost, L("Reset"), "", false).Label(L("Reset the context window of %s", m.Name)).Clicked() {
					if err := h.SetModelContextWindow(m.Provider, m.ID, 0); err != nil {
						s.err = err.Error()
					}
				}
			}, nil)
		})
	}
	footer := L("The Kernel compacts a Session when it nears its model's context window, so a smaller window compacts sooner. Working Sessions have a change from their next message.")
	a.settingGroup(c, L("Context windows"), footer, rows...)
}

func contextModelLabel(m host.KernelModelContext) string {
	name := m.Name
	if name == "" {
		name = m.ID
	}
	if m.Provider == "magpie" {
		name += " · Magpie"
	}
	return name
}

// parseTokens reads a count of tokens as a person types it: 200000, 200,000, 200k or 1m. Empty is 0, for the
// source's own window.
func parseTokens(s string) (int, bool) {
	s = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(s, ",", "")))
	s = strings.ReplaceAll(s, "_", "")
	if s == "" {
		return 0, true
	}
	scale := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		scale, s = 1_000, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		scale, s = 1_000_000, strings.TrimSuffix(s, "m")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return int(f * scale), true
}

// groupedTokens is a count with thousands separators.
func groupedTokens(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

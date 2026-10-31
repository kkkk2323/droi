// Package models is the model picker's rows and the settings' labels, for the
// Session settings and the defaults pages (a port of model-choices.ts,
// model-brand.ts and toModelChoices in use-session-settings.ts).
package models

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droid-sdk-go/protocol"
)

// Choice is a model the Daemon offers.
type Choice struct {
	ID               string
	Label            string
	ReasoningEfforts []string
	Disabled         bool
	// Provider is the Daemon's provider id (anthropic, openai, ...); "" for
	// the Auto router.
	Provider string
	// Multiplier is Factory's usage multiplier for the model; nil for custom
	// (BYOK) models.
	Multiplier *float64
	// BehavesLike is the built-in model a custom model behaves like, as droid
	// 1.1 reports it; "" for the rest and from an older Daemon.
	BehavesLike string
}

func choiceOf(m protocol.ModelMetadata, disabled bool) Choice {
	efforts := make([]string, len(m.SupportedReasoningEfforts))
	for i, e := range m.SupportedReasoningEfforts {
		efforts[i] = string(e)
	}
	c := Choice{ID: m.ID, Label: m.DisplayName, ReasoningEfforts: efforts, Disabled: disabled}
	if m.Kind != "router" {
		c.Provider = string(m.ModelProvider)
	}
	if m.IsCustom != nil && *m.IsCustom {
		c.BehavesLike = m.BehavesLikeModelID
	} else {
		c.Multiplier = m.TokenMultiplier
	}
	return c
}

// ToChoices reads the Daemon's model list.
func ToChoices(models []protocol.ModelMetadata) []Choice {
	out := make([]Choice, len(models))
	for i, m := range models {
		out[i] = choiceOf(m, false)
	}
	return out
}

// DecodeChoices reads a JSON model list as the Daemon's defaults carry it,
// where a model may also say it is disabled.
func DecodeChoices(data []byte) ([]Choice, error) {
	var list []struct {
		protocol.ModelMetadata
		Disabled bool `json:"disabled"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	out := make([]Choice, len(list))
	for i, m := range list {
		out[i] = choiceOf(m.ModelMetadata, m.Disabled)
	}
	return out, nil
}

// PickerFilter says which rows the picker lists: "all", "favorites", the
// Magpie gateway's models, or one Brand.
type PickerFilter string

const (
	FilterAll       PickerFilter = "all"
	FilterFavorites PickerFilter = "favorites"
	// FilterMagpie lists the models a Magpie gateway offers, and no others:
	// the gateway serves models that share a name with the Daemon's own, so
	// they get a tab rather than sit among them.
	FilterMagpie PickerFilter = "magpie"
)

// Row is a picker row.
type Row struct {
	Choice
	Brand Brand
}

// VisibleModels are the rows in display order. A non-empty query searches
// every model regardless of the filter; favorites keep the order they were
// starred in. Everything but the Magpie tab leaves the gateway's models out:
// they share their names with the Daemon's, and beside them the list reads as
// one crowd.
func VisibleModels(models []Choice, favorites []string, filter PickerFilter, query string) []Row {
	rows := make([]Row, len(models))
	for i, m := range models {
		rows[i] = Row{Choice: m, Brand: m.Brand()}
	}
	inScope := func(row Row) bool {
		switch filter {
		case FilterMagpie:
			return IsMagpie(row.ID)
		case FilterFavorites:
			return slices.Contains(favorites, row.ID)
		}
		return !IsMagpie(row.ID)
	}
	var out []Row
	if tokens := strings.Fields(strings.ToLower(query)); len(tokens) > 0 {
		for _, row := range rows {
			haystack := strings.ToLower(row.Label + " " + row.ID + " " + BrandLabels[row.Brand])
			if !slices.ContainsFunc(tokens, func(t string) bool { return !strings.Contains(haystack, t) }) {
				out = append(out, row)
			}
		}
		return out
	}
	switch filter {
	case FilterAll:
		for _, row := range rows {
			if inScope(row) {
				out = append(out, row)
			}
		}
		return out
	case FilterFavorites:
		for _, row := range rows {
			if inScope(row) {
				out = append(out, row)
			}
		}
		slices.SortStableFunc(out, func(a, b Row) int {
			return slices.Index(favorites, a.ID) - slices.Index(favorites, b.ID)
		})
		return out
	}
	for _, row := range rows {
		if !inScope(row) {
			continue
		}
		if filter == FilterMagpie || string(row.Brand) == string(filter) {
			out = append(out, row)
		}
	}
	return out
}

// HasMagpie reports that the list holds a Magpie gateway's models, so that
// the picker offers their tab.
func HasMagpie(models []Choice) bool {
	return slices.ContainsFunc(models, func(m Choice) bool { return IsMagpie(m.ID) })
}

// BrandsOf are the brands present in the Daemon's own list, in rail order.
// Magpie's models have their own tab rather than each brand's: their names
// repeat the Daemon's, and a brand tab holding both is the crowd the tab is
// there to avoid.
func BrandsOf(models []Choice) []Brand {
	present := map[Brand]bool{}
	for _, m := range models {
		if !IsMagpie(m.ID) {
			present[m.Brand()] = true
		}
	}
	var out []Brand
	for _, b := range BrandOrder {
		if present[b] {
			out = append(out, b)
		}
	}
	return out
}

// FormatMultiplier writes 1.6 as "1.6×", as Factory shows its usage multipliers.
func FormatMultiplier(multiplier float64) string {
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(multiplier, 'f', 2, 64), 64)
	return strconv.FormatFloat(rounded, 'f', -1, 64) + "×"
}

// EffortLabels names the reasoning efforts the Daemon offers. The names stay
// in English in every language: they are the levels' own names.
var EffortLabels = map[string]string{
	"none":    "None",
	"dynamic": "Dynamic",
	"off":     "Off",
	"minimal": "Minimal",
	"low":     "Low",
	"medium":  "Medium",
	"high":    "High",
	"xhigh":   "Extra high",
	"max":     "Max",
}

var AutonomyLabels = map[string]string{
	"off":    l10n.N("Ask for everything"),
	"low":    l10n.N("Low autonomy"),
	"medium": l10n.N("Medium autonomy"),
	"high":   l10n.N("High autonomy"),
}

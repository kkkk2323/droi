// Package models is the model picker's rows and the settings' labels, for the
// Session settings and the defaults pages (a port of model-choices.ts,
// model-brand.ts and toModelChoices in use-session-settings.ts).
package models

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
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
	if !(m.IsCustom != nil && *m.IsCustom) {
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

// PickerFilter says which rows the picker lists: "all", "favorites", or one
// Brand.
type PickerFilter string

const (
	FilterAll       PickerFilter = "all"
	FilterFavorites PickerFilter = "favorites"
)

// Row is a picker row.
type Row struct {
	Choice
	Brand Brand
}

// VisibleModels are the rows in display order. A non-empty query searches
// every model regardless of the filter; favorites keep the order they were
// starred in.
func VisibleModels(models []Choice, favorites []string, filter PickerFilter, query string) []Row {
	rows := make([]Row, len(models))
	for i, m := range models {
		rows[i] = Row{Choice: m, Brand: BrandOf(m.ID, m.Provider)}
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
		return rows
	case FilterFavorites:
		for _, row := range rows {
			if slices.Contains(favorites, row.ID) {
				out = append(out, row)
			}
		}
		slices.SortStableFunc(out, func(a, b Row) int {
			return slices.Index(favorites, a.ID) - slices.Index(favorites, b.ID)
		})
		return out
	}
	for _, row := range rows {
		if string(row.Brand) == string(filter) {
			out = append(out, row)
		}
	}
	return out
}

// BrandsOf are the brands present in the list, in rail order.
func BrandsOf(models []Choice) []Brand {
	present := map[Brand]bool{}
	for _, m := range models {
		present[BrandOf(m.ID, m.Provider)] = true
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
	"off":    "Ask for everything",
	"low":    "Low autonomy",
	"medium": "Medium autonomy",
	"high":   "High autonomy",
}

// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: PROMOTIONS 173-179, rateOf 186-190; the promotions themselves are droid 0.241.1's registry
// (testdata/droid-0.241.1-costs.txt).

package factory

import (
	"math"
	"time"
)

// promotion is a discount Factory takes off a model's token multiplier while it runs.
type promotion struct {
	discount float64
	from, to time.Time
}

func instant(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// droid's registry names some promotions and writes others into a model's own entry; the names are its own.
var (
	promotionM    = promotion{0.6, instant("2026-08-29T04:00:00Z"), instant("2026-09-05T02:00:00Z")}
	promotionPt   = promotion{0.2, instant("2026-08-22T00:00:00Z"), instant("2026-11-22T00:00:00Z")}
	promotionMt   = promotion{0.5, instant("2026-08-17T21:10:19Z"), instant("2027-01-01T08:00:00Z")}
	mistralLaunch = promotion{0.5, instant("2026-10-06T00:00:00Z"), instant("2026-10-20T00:00:00Z")}
	argonLaunch   = promotion{0.5, instant("2026-10-13T16:00:00Z"), instant("2026-12-01T08:00:00Z")}

	promotions = []promotion{promotionM, promotionPt, promotionMt, mistralLaunch, argonLaunch}

	// modelPromotions are each model's promotions in its entry's order: the first running is the one that counts.
	modelPromotions = map[string][]promotion{
		"gpt-6-astra":        {promotionM},
		"gpt-5.6-sol":        {promotionM, promotionPt},
		"gpt-5.6-terra":      {promotionM},
		"gpt-5.6-luna":       {promotionM},
		"gpt-5.5":            {promotionM},
		"gpt-5.5-fast":       {promotionM},
		"gpt-5.4":            {promotionM},
		"gpt-5.4-fast":       {promotionM},
		"gpt-5.3-codex":      {promotionM},
		"gpt-5.3-codex-fast": {promotionM},
		"grok-4.6":           {promotionM},
		"mistral-large-4":    {mistralLaunch},
		"gemini-4-argon":     {argonLaunch},
		"gemini-3.8-flash":   {promotionMt},
		"gemini-3.7-flash":   {promotionMt},
	}
)

// Price is what a request to the model with id costs at now, as a multiplier of Factory's token rate: its Rate less
// the discount of the first of its promotions running then, and the Rate before that discount (0 when none runs),
// which droid's picker strikes through beside the price. False for a model droid does not offer.
func Price(id string, now time.Time) (rate, was float64, ok bool) {
	m, ok := ModelByID(id)
	if !ok {
		return 0, 0, false
	}
	for _, p := range modelPromotions[id] {
		if !now.Before(p.from) && now.Before(p.to) {
			return math.Round(m.Rate*(1-p.discount)*1e4) / 1e4, m.Rate, true
		}
	}
	return m.Rate, 0, true
}

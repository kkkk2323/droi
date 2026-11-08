package factory

import (
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rate.test.mjs: a model's price is the token multiplier droid's registry lists, less a promotion running. The cases
// are droid 0.241.1's registry costs and 0.251.0's 1017 entry, kept byte for byte in testdata.

func costFixture(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, name := range []string{"droid-0.241.1-costs.txt", "droid-0.251.0-deepseek-1017.txt"} {
		raw, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	return b.String()
}

type fixturePromotion struct {
	discount float64
	from, to time.Time
}

// fixturePrice is a model's price at now as the fixture says it, as upstream's test reads it.
func fixturePrice(t *testing.T, droid, id string, now time.Time) (rate, was float64) {
	t.Helper()
	var line string
	for _, l := range strings.Split(droid, "\n") {
		if strings.HasPrefix(l, `"`+id+`":`) {
			line = l[strings.Index(l, "cost:"):]
		}
	}
	if line == "" {
		t.Fatalf("%s is not in the fixture", id)
	}
	base, _ := strconv.ParseFloat(regexp.MustCompile(`tokenMultiplier:([\d.]+)`).FindStringSubmatch(line)[1], 64)
	parse := func(m []string) fixturePromotion {
		d, _ := strconv.ParseFloat(m[1], 64)
		from, _ := time.Parse(time.RFC3339, m[2])
		to, _ := time.Parse(time.RFC3339, m[3])
		return fixturePromotion{d, from, to}
	}
	var promotions []fixturePromotion
	if m := regexp.MustCompile(`promotions:\[\{discount:([\d.]+),startsAt:new Date\("([^"]+)"\)(?:,expiresAt:new Date\("([^"]+)"\))?`).FindStringSubmatch(line); m != nil {
		promotions = append(promotions, parse(m))
	} else if m := regexp.MustCompile(`promotions:\[([A-Za-z_$][\w$]*(?:,[A-Za-z_$][\w$]*)*)\]`).FindStringSubmatch(line); m != nil {
		for _, name := range strings.Split(m[1], ",") {
			def := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `=\{discount:([\d.]+),startsAt:new Date\("([^"]+)"\)(?:,expiresAt:new Date\("([^"]+)"\))?`).FindStringSubmatch(droid)
			if def == nil {
				t.Fatalf("promotion %s is not defined in the fixture", name)
			}
			promotions = append(promotions, parse(def))
		}
	}
	for _, p := range promotions {
		if !now.Before(p.from) && (p.to.IsZero() || now.Before(p.to)) {
			return math.Round(base*(1-p.discount)*1e4) / 1e4, base
		}
	}
	return base, 0
}

func TestRates(t *testing.T) {
	droid := costFixture(t)

	t.Run("every model's rate is the token multiplier its droid registry entry lists", func(t *testing.T) {
		for _, m := range Models() {
			if want, _ := fixturePrice(t, droid, m.ID, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); m.Rate != want {
				t.Errorf("%s: rate %v, want %v", m.ID, m.Rate, want)
			}
		}
	})

	t.Run("a promotion running comes off the rate, the multiplier before it as rateWas", func(t *testing.T) {
		now := time.Date(2026, 10, 14, 21, 30, 0, 0, time.UTC)
		var discounted []string
		for _, m := range Models() {
			rate, was, ok := Price(m.ID, now)
			wantRate, wantWas := fixturePrice(t, droid, m.ID, now)
			if !ok || rate != wantRate || was != wantWas {
				t.Errorf("%s: %v %v, want %v %v", m.ID, rate, was, wantRate, wantWas)
			}
			if was > 0 {
				discounted = append(discounted, m.ID)
			}
		}
		if want := []string{"gpt-5.6-sol", "mistral-large-4", "gemini-4-argon", "gemini-3.8-flash", "gemini-3.7-flash"}; !slices.Equal(discounted, want) {
			t.Fatalf("discounted %v, want %v", discounted, want)
		}
		if rate, was, _ := Price("gemini-4-argon", now); rate != 0.8 || was != 1.6 {
			t.Fatalf("gemini-4-argon: %v %v", rate, was)
		}
	})

	t.Run("a promotion that has ended, or hasn't started, is no discount at all", func(t *testing.T) {
		if rate, was, _ := Price("gemini-4-argon", time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)); rate != 1.6 || was != 0 {
			t.Fatalf("after: %v %v", rate, was)
		}
		if rate, was, _ := Price("gpt-5.6-sol", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)); rate != 2 || was != 0 {
			t.Fatalf("before: %v %v", rate, was)
		}
	})

	t.Run("at every promotion's start and end, every model is priced as droid's registry says", func(t *testing.T) {
		instants := []time.Time{time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
		for _, p := range promotions {
			for _, at := range []time.Time{p.from, p.to} {
				instants = append(instants, at.Add(-time.Second), at, at.Add(time.Second))
			}
		}
		for _, now := range instants {
			for _, m := range Models() {
				rate, was, _ := Price(m.ID, now)
				if wantRate, wantWas := fixturePrice(t, droid, m.ID, now); rate != wantRate || was != wantWas {
					t.Errorf("%s at %s: %v %v, want %v %v", m.ID, now.Format(time.RFC3339), rate, was, wantRate, wantWas)
				}
			}
		}
	})

	t.Run("a model droid does not offer has no price", func(t *testing.T) {
		if _, _, ok := Price("something-new", time.Now()); ok {
			t.Fatal("priced")
		}
	})
}

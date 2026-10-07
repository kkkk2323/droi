// Which Memory Entries a new one may repeat or contradict: TF-IDF cosine over
// words and CJK bigrams, weighted over the entries it is compared with. On a
// real Memory a duplicate pair scored 0.61 and a superseded pair 0.30, each
// the other's best match, while most entries' best match stayed below 0.25.

package memory

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

const (
	SimilarMin   = 0.25
	SimilarLimit = 3
)

// A small Memory has too few entries to tell which words are common, so the
// weights act as if it held at least this many.
const minCorpus = 20

var stopWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Split("an and are as at be by do for from has have if in is it its no not of on or so than that the then this to was were when with", " ") {
		m[w] = true
	}
	return m
}()

var tokenRuns = regexp.MustCompile(`[a-z0-9]+|[\x{3400}-\x{9fff}]+`)

// SimilarityTokens are the Latin words and numbers of two or more characters,
// and each CJK run as its bigrams.
func SimilarityTokens(text string) []string {
	tokens := []string{}
	for _, run := range tokenRuns.FindAllString(strings.ToLower(text), -1) {
		if c := run[0]; c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			if len(run) >= 2 && !stopWords[run] {
				tokens = append(tokens, run)
			}
			continue
		}
		chars := []rune(run)
		if len(chars) == 1 {
			tokens = append(tokens, run)
		}
		for i := 0; i+1 < len(chars); i++ {
			tokens = append(tokens, string(chars[i:i+2]))
		}
	}
	return tokens
}

// tokenCounts keeps first-seen order so the floating-point sums run in the
// order the TS Map iterates, and a score at the threshold lands the same way.
type tokenCounts struct {
	order []string
	n     map[string]int
}

func countTokens(tokens []string) tokenCounts {
	c := tokenCounts{n: map[string]int{}}
	for _, t := range tokens {
		if c.n[t] == 0 {
			c.order = append(c.order, t)
		}
		c.n[t]++
	}
	return c
}

// SimilarEntries are the candidates most like text, best first, scoring at
// least min, at most limit of them.
func SimilarEntries(text string, candidates []Entry, min float64, limit int) []Entry {
	target := countTokens(SimilarityTokens(text))
	if len(target.order) == 0 || len(candidates) == 0 {
		return nil
	}
	docs := make([]tokenCounts, len(candidates))
	for i, e := range candidates {
		docs[i] = countTokens(SimilarityTokens(e.Text))
	}
	df := map[string]int{}
	for _, t := range target.order {
		df[t]++
	}
	for _, d := range docs {
		for _, t := range d.order {
			df[t]++
		}
	}
	n := float64(max(len(docs)+1, minCorpus))
	weights := func(doc tokenCounts) (map[string]float64, float64) {
		out := map[string]float64{}
		norm := 0.0
		for _, t := range doc.order {
			w := (1 + math.Log(float64(doc.n[t]))) * math.Log((n+1)/(float64(df[t])+0.5))
			out[t] = w
			norm += w * w
		}
		return out, math.Sqrt(norm)
	}
	tw, tnorm := weights(target)
	type scored struct {
		entry Entry
		score float64
	}
	var found []scored
	for i, doc := range docs {
		dw, dnorm := weights(doc)
		dot := 0.0
		for _, t := range target.order {
			dot += tw[t] * dw[t]
		}
		score := 0.0
		if tnorm != 0 && dnorm != 0 {
			score = dot / (tnorm * dnorm)
		}
		if score >= min {
			found = append(found, scored{candidates[i], score})
		}
	}
	sort.SliceStable(found, func(a, b int) bool { return found[a].score > found[b].score })
	var out []Entry
	for i := 0; i < len(found) && i < limit; i++ {
		out = append(out, found[i].entry)
	}
	return out
}

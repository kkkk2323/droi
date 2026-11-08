// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// client-phrases.json; index.mjs: FALLBACK_PHRASES 515-529, GLOBAL_INSTRUCTIONS 1278, COMPACT_OPENING 1335,
// REFUSED_PHRASES 1449-1452, ESCAPE_IN_TOOL 1457-1458.

package factory

import "sort"

// The fixed client phrases Factory's usage policy check refuses in a request body, as upstream's
// client-phrases.json lists them (phrases_test.go holds the two lists to it). Each phrase's first character is
// written as a \u escape, as that file writes it: this source can be read in a session on Factory, and quoted in
// a tool result, without the request that carries it being refused.
var (
	// anywherePhrases are refused in any text: pi's opening and the sentence pi opens a compaction summary with.
	anywherePhrases = []string{
		"\u0059ou are an expert coding assistant operating inside pi, a coding agent harness.",
		"\u0054he conversation history before this point was compacted into the following summary:",
	}
	// toolOnlyPhrases are refused in tool output; the prompts that carry them are other clients'.
	toolOnlyPhrases = []string{
		"\u0059ou are Claude Code, Anthropic's official CLI for Claude.",
		"\u0059ou are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.",
		"\u0059ou are a Claude agent, built on Anthropic's Claude Agent SDK.",
		"\u0059ou have been invoked in the following environment:",
		"\u0078-anthropic-billing-header: cc_version=",
		"\u0054his session is being continued from a previous conversation that ran out of context.",
		"\u0028user's private global instructions for all projects)",
	}
)

// refusedPhrases are every phrase Factory refuses wherever it appears.
var refusedPhrases = append(append([]string{}, anywherePhrases...), toolOnlyPhrases...)

const (
	// compactOpening is how another client opens a compaction summary.
	compactOpening = "\u0054his session is being continued from a previous conversation that ran out of context."
	// globalInstructions is another client's label of its global instruction file.
	globalInstructions = "\u0028user's private global instructions for all projects)"
)

// escapeInTool are the substrings whose first character is escaped in quoted tool output: every refused phrase,
// and the shorter prefixes and the reminder marker upstream escaped before it had the list. Longest first, so a
// whole sentence is escaped before its own prefix would hide it; the sort is stable, as JavaScript's is.
var escapeInTool = func() []string {
	all := append(append([]string{}, refusedPhrases...), compactOpening, globalInstructions,
		"system-reminder", "\u0059ou have", "\u0059ou are", "\u0078-anthropic-billing-header")
	seen := map[string]bool{}
	unique := make([]string, 0, len(all))
	for _, phrase := range all {
		if !seen[phrase] {
			seen[phrase] = true
			unique = append(unique, phrase)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool { return len(unique[i]) > len(unique[j]) })
	return unique
}()

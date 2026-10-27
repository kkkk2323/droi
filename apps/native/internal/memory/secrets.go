// Memory is plain text on disk, shown to the model in later Sessions, so a
// credential that slipped into it would leak everywhere at once: a Memory
// Note with one is refused, and Session Summaries and the Memory Summary
// have theirs redacted, as does the conversation a Session Summary is
// written from.

package memory

import "regexp"

var secretPatterns = []struct {
	kind    string
	pattern *regexp.Regexp
}{
	{"a private key", jsRegexp(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"an OpenAI or Anthropic API key", jsRegexp(`\bsk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,}`)},
	{"a Factory API key", jsRegexp(`\bfk-[A-Za-z0-9_-]{20,}`)},
	{"an AWS access key", jsRegexp(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"a GitHub token", jsRegexp(`\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})`)},
	{"a GitLab token", jsRegexp(`\bglpat-[A-Za-z0-9_-]{20,}`)},
	{"a Slack token", jsRegexp(`\bxox[abposr]-[A-Za-z0-9-]{10,}`)},
	{"a Google API key", jsRegexp(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"a JSON Web Token", jsRegexp(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{"a bearer token", jsRegexp(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]{20,}=*`)},
	{"a password", jsRegexp(`(?i)\b(?:password|passwd|pwd)\s*[=:]\s*\S+`)},
	{"an API key or secret", jsRegexp(`(?i)\b(?:api[_-]?key|secret|access[_-]?token|auth[_-]?token)\s*[=:]\s*['"]?[A-Za-z0-9_\-./+]{16,}`)},
}

// Redacted stands in for a secret in what Memory writes.
const Redacted = "[REDACTED_SECRET]"

// RedactSecrets replaces every secret in text with Redacted.
func RedactSecrets(text string) string {
	for _, p := range secretPatterns {
		text = p.pattern.ReplaceAllString(text, Redacted)
	}
	return text
}

// FindSecret says why the text may not be stored, or "" when it looks clean.
// It never repeats the match.
func FindSecret(text string) string {
	for _, p := range secretPatterns {
		if p.pattern.MatchString(text) {
			return "The text looks like it contains " + p.kind + ". Memory never stores credentials; leave the secret out and describe where it lives instead."
		}
	}
	return ""
}

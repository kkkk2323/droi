// Every Memory write is scanned first: Memory is plain text on disk, exported
// to Markdown and shown to the model in later Sessions, so a credential that
// slipped into an entry would leak everywhere at once.

const PATTERNS: Array<[kind: string, pattern: RegExp]> = [
  ['a private key', /-----BEGIN [A-Z ]*PRIVATE KEY-----/],
  ['an OpenAI or Anthropic API key', /\bsk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,}/],
  ['a Factory API key', /\bfk-[A-Za-z0-9_-]{20,}/],
  ['an AWS access key', /\b(?:AKIA|ASIA)[0-9A-Z]{16}\b/],
  ['a GitHub token', /\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})/],
  ['a GitLab token', /\bglpat-[A-Za-z0-9_-]{20,}/],
  ['a Slack token', /\bxox[abposr]-[A-Za-z0-9-]{10,}/],
  ['a Google API key', /\bAIza[0-9A-Za-z_-]{35}\b/],
  ['a JSON Web Token', /\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/],
  ['a bearer token', /\bBearer\s+[A-Za-z0-9._~+/-]{20,}=*/i],
  ['a password', /\b(?:password|passwd|pwd)\s*[=:]\s*\S+/i],
  [
    'an API key or secret',
    /\b(?:api[_-]?key|secret|access[_-]?token|auth[_-]?token)\s*[=:]\s*['"]?[A-Za-z0-9_\-./+]{16,}/i,
  ],
]

/** Why the text may not be stored, or null when it looks clean. Never repeats the match. */
export function findSecret(text: string): string | null {
  for (const [kind, pattern] of PATTERNS) {
    if (pattern.test(text)) {
      return `The text looks like it contains ${kind}. Memory never stores credentials; leave the secret out and describe where it lives instead.`
    }
  }
  return null
}

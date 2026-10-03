// Which Memory Entries a new one may repeat or contradict: TF-IDF cosine over
// words and CJK bigrams, weighted over the entries it is compared with. On a
// real Memory a duplicate pair scored 0.61 and a superseded pair 0.30, each
// the other's best match, while most entries' best match stayed below 0.25.
import type { MemoryEntry } from './store'

export const SIMILAR_MIN = 0.25
export const SIMILAR_LIMIT = 3

/**
 * A small Memory has too few entries to tell which words are common, so the
 * weights act as if it held at least this many.
 */
const MIN_CORPUS = 20

const STOP_WORDS = new Set(
  'an and are as at be by do for from has have if in is it its no not of on or so than that the then this to was were when with'.split(
    ' ',
  ),
)

/** Latin words and numbers of two or more characters; a CJK run as its bigrams. */
export function similarityTokens(text: string): string[] {
  const tokens: string[] = []
  for (const [run] of text.toLowerCase().matchAll(/[a-z0-9]+|[\u3400-\u9fff]+/g)) {
    if (/^[a-z0-9]/.test(run)) {
      if (run.length >= 2 && !STOP_WORDS.has(run)) tokens.push(run)
      continue
    }
    const chars = [...run]
    if (chars.length === 1) tokens.push(run)
    for (let i = 0; i + 1 < chars.length; i++) tokens.push(`${chars[i]}${chars[i + 1]}`)
  }
  return tokens
}

function counts(tokens: readonly string[]): Map<string, number> {
  const found = new Map<string, number>()
  for (const token of tokens) found.set(token, (found.get(token) ?? 0) + 1)
  return found
}

/** The candidates most like `text`, best first, at or above `min`. */
export function similarEntries(
  text: string,
  candidates: readonly MemoryEntry[],
  { min = SIMILAR_MIN, limit = SIMILAR_LIMIT }: { min?: number; limit?: number } = {},
): MemoryEntry[] {
  const target = counts(similarityTokens(text))
  if (target.size === 0 || candidates.length === 0) return []
  const docs = candidates.map((entry) => counts(similarityTokens(entry.text)))
  const df = new Map<string, number>()
  for (const doc of [target, ...docs])
    for (const token of doc.keys()) df.set(token, (df.get(token) ?? 0) + 1)
  const n = Math.max(docs.length + 1, MIN_CORPUS)
  const weights = (doc: Map<string, number>) => {
    const out = new Map<string, number>()
    let norm = 0
    for (const [token, count] of doc) {
      const w = (1 + Math.log(count)) * Math.log((n + 1) / ((df.get(token) ?? 0) + 0.5))
      out.set(token, w)
      norm += w * w
    }
    return { out, norm: Math.sqrt(norm) }
  }
  const t = weights(target)
  return docs
    .map((doc, i) => {
      const d = weights(doc)
      let dot = 0
      for (const [token, w] of t.out) dot += w * (d.out.get(token) ?? 0)
      return { entry: candidates[i]!, score: t.norm && d.norm ? dot / (t.norm * d.norm) : 0 }
    })
    .filter((s) => s.score >= min)
    .sort((a, b) => b.score - a.score)
    .slice(0, limit)
    .map((s) => s.entry)
}

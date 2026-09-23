// Markdown as a syntax tree for the Phone App's renderer: CommonMark plus
// GitHub's tables, task lists, strikethrough and autolinks. A reply that is
// still streaming may end inside a code fence or a span; it is closed first
// so the half-written part renders as what it will become.
import type { Root } from 'mdast'
import { fromMarkdown } from 'mdast-util-from-markdown'
import { gfmFromMarkdown } from 'mdast-util-gfm'
import { gfm } from 'micromark-extension-gfm'

// A settled text parses the same every time; rows mount again as they scroll
// back into view. The newest texts stay cached, a streaming one never.
const CACHE_SIZE = 200
const parsed = new Map<string, Root>()

export function parseMarkdown(text: string, { streaming = false } = {}): Root {
  if (streaming) return parse(repairStreaming(text))
  let tree = parsed.get(text)
  if (tree) {
    parsed.delete(text)
  } else {
    tree = parse(text)
    if (parsed.size >= CACHE_SIZE) parsed.delete(parsed.keys().next().value!)
  }
  parsed.set(text, tree)
  return tree
}

function parse(text: string): Root {
  return fromMarkdown(text, { extensions: [gfm()], mdastExtensions: [gfmFromMarkdown()] })
}

const FENCE = /^ {0,3}(`{3,}|~{3,})/

/**
 * Closes what a cut-off reply left open: a code fence, then (outside code) an
 * inline code span, bold and italic markers on the last line.
 */
export function repairStreaming(text: string): string {
  let open: string | null = null
  for (const line of text.split('\n')) {
    const fence = FENCE.exec(line)?.[1]
    if (!fence) continue
    if (open === null) open = fence
    else if (fence[0] === open[0] && fence.length >= open.length && line.trim() === fence)
      open = null
  }
  if (open !== null) return `${text}${text.endsWith('\n') ? '' : '\n'}${open}`

  const lastLine = text.slice(text.lastIndexOf('\n') + 1)
  let repaired = text
  let rest = lastLine
  if (count(rest, '`') % 2 === 1) {
    repaired += '`'
    rest = rest.replace(/`[^`]*$/, '')
  }
  // Markers inside a code span do not count.
  const outside = rest.replace(/`[^`]*`/g, '')
  if (count(outside, '**') % 2 === 1) {
    repaired += '**'
  } else if (count(outside.replace(/\*\*/g, ''), '*') % 2 === 1 && !/^\s*\*\s/.test(outside)) {
    repaired += '*'
  }
  return repaired
}

function count(text: string, token: string): number {
  return text.split(token).length - 1
}

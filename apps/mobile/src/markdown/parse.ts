// Markdown as a syntax tree for the Phone App's renderer: CommonMark plus
// GitHub's tables, task lists, strikethrough and autolinks, and TeX math
// between $ and $$. A reply that is
// still streaming may end inside a code fence or a span; it is closed first
// so the half-written part renders as what it will become.
import type { Nodes, Root } from 'mdast'
import { fromMarkdown } from 'mdast-util-from-markdown'
import { gfmFromMarkdown } from 'mdast-util-gfm'
import { mathFromMarkdown } from 'mdast-util-math'
import { gfm } from 'micromark-extension-gfm'
import { math } from 'micromark-extension-math'

// A settled text parses the same every time; rows mount again as they scroll
// back into view. The newest texts stay cached, a streaming one never.
const CACHE_SIZE = 200
const parsed = new Map<string, Root>()

export function parseMarkdown(text: string, { streaming = false } = {}): Root {
  if (streaming) return parseStreaming(text)
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
  return fromMarkdown(text, {
    extensions: [gfm(), math()],
    mdastExtensions: [gfmFromMarkdown(), mathFromMarkdown()],
  })
}

// A streaming reply only grows at its end. Markdown settles its blocks line by
// line, so a block that began on a finished line ends every block before it
// for good; only the line still being written can turn out to continue the
// block above it (`1` becoming `1.` joins a list). The blocks before the last
// one that began on a finished line are kept, and only the text from that
// block's line on is parsed again, so an update to a long reply costs what one
// to a short reply does.
let streamed: {
  text: string
  tree: Root
  kept: number
  tailStart: number
  /** Lines before `tailStart`, to move the tail's positions into the whole text. */
  tailLine: number
} | null = null

// A link or footnote definition resolves references anywhere in the document,
// so a reply with one is parsed whole.
const DEFINITION = /^ {0,3}\[[^\]\n]+\]:/m

function parseStreaming(text: string): Root {
  const last = streamed
  if (last?.text === text) return last.tree
  const reuse = last !== null && text.startsWith(last.text) && !DEFINITION.test(text)
  const kept = reuse ? last.tree.children.slice(0, last.kept) : []
  const base = reuse ? last.tailStart : 0
  const lines = reuse ? last.tailLine : 0
  const tail = parse(repairStreaming(text.slice(base))).children
  if (base > 0) for (const node of tail) shift(node, base, lines)
  const lastLine = text.lastIndexOf('\n') + 1
  let next = { kept: kept.length, tailStart: base, tailLine: lines }
  for (let i = tail.length - 1; i >= 0; i--) {
    const start = tail[i]?.position?.start
    if (start?.offset !== undefined && start.offset < lastLine) {
      const tailStart = text.lastIndexOf('\n', start.offset - 1) + 1
      next = { kept: kept.length + i, tailStart, tailLine: start.line - 1 }
      break
    }
  }
  const tree: Root = { type: 'root', children: [...kept, ...tail] }
  streamed = { text, tree, ...next }
  return tree
}

/** Moves a node parsed from a slice that starts at a line's start to its place in the whole text. */
function shift(node: Nodes, offset: number, lines: number): void {
  if (node.position) {
    for (const point of [node.position.start, node.position.end]) {
      if (point.offset !== undefined) point.offset += offset
      point.line += lines
    }
  }
  if ('children' in node) for (const child of node.children) shift(child, offset, lines)
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

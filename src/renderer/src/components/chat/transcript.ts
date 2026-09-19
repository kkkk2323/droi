// Turn the SDK's flat message list into what the transcript renders: user
// turns, assistant turns whose tool calls carry their results, and nothing
// for bare tool-result messages, which only exist to complete a tool call.
import type { ContentBlock, FactoryDroidMessage } from '@factory/droid-sdk'

export type ToolUseBlock = Extract<ContentBlock, { type: 'tool_use' }>
export type ToolResultBlock = Extract<ContentBlock, { type: 'tool_result' }>
export type TextBlock = Extract<ContentBlock, { type: 'text' }>
export type ThinkingBlock = Extract<ContentBlock, { type: 'thinking' }>

export interface ToolCall {
  use: ToolUseBlock
  result: ToolResultBlock | null
}

export type TranscriptBlock =
  | { kind: 'text'; id: string; text: string }
  | { kind: 'thinking'; id: string; text: string; durationMs: number | undefined }
  | { kind: 'tool'; id: string; call: ToolCall }

export interface TranscriptEntry {
  id: string
  role: 'user' | 'assistant'
  blocks: TranscriptBlock[]
  createdAt: number
  isError: boolean
}

export function buildTranscript(messages: readonly FactoryDroidMessage[]): TranscriptEntry[] {
  const results = new Map<string, ToolResultBlock>()
  for (const message of messages) {
    if (message.role !== 'tool') continue
    for (const block of message.content) {
      if (block.type === 'tool_result') results.set(block.toolUseId, block)
    }
  }

  const entries: TranscriptEntry[] = []
  for (const message of messages) {
    if (message.role !== 'user' && message.role !== 'assistant') continue
    if (message.isUserVisible === false) continue
    const blocks: TranscriptBlock[] = []
    message.content.forEach((block, index) => {
      const id = `${message.id}:${index}`
      switch (block.type) {
        case 'text':
          if (block.text) blocks.push({ kind: 'text', id, text: block.text })
          break
        case 'thinking':
          if (block.thinking)
            blocks.push({
              kind: 'thinking',
              id,
              text: block.thinking,
              durationMs: block.durationMs,
            })
          break
        case 'tool_use':
          blocks.push({
            kind: 'tool',
            id,
            call: { use: block, result: results.get(block.id) ?? null },
          })
          break
        default:
          break
      }
    })
    if (blocks.length === 0 && message.role === 'assistant') continue
    entries.push({
      id: message.id,
      role: message.role,
      blocks,
      createdAt: message.createdAt,
      isError: message.isError === true,
    })
  }
  return entries
}

export function toolResultText(result: ToolResultBlock | null): string {
  if (!result) return ''
  const content: unknown = result.content
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((part: unknown) =>
        typeof part === 'object' && part !== null && 'text' in part
          ? String((part as { text: unknown }).text)
          : '',
      )
      .join('\n')
  }
  return ''
}

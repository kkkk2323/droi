import type { ChatMessage, ToolCallBlock } from '@/types'

export interface TodoItem {
  index: number
  status: 'completed' | 'in_progress' | 'pending'
  text: string
}

export function parseTodos(todosStr: string): TodoItem[] {
  const lines = todosStr.split('\n').filter((l) => l.trim())
  return lines.map((line, i) => {
    const trimmed = line.replace(/^\d+\.\s*/, '').trim()
    let status: TodoItem['status'] = 'pending'
    let text = trimmed
    if (/^\[completed\]/i.test(trimmed)) {
      status = 'completed'
      text = trimmed.replace(/^\[completed\]\s*/i, '')
    } else if (/^\[in_progress\]/i.test(trimmed)) {
      status = 'in_progress'
      text = trimmed.replace(/^\[in_progress\]\s*/i, '')
    } else if (/^\[pending\]/i.test(trimmed)) {
      status = 'pending'
      text = trimmed.replace(/^\[pending\]\s*/i, '')
    }
    return { index: i, status, text }
  })
}

function extractLastTodoString(messages?: ChatMessage[] | null): string {
  const safeMessages = Array.isArray(messages) ? messages : []
  for (let i = safeMessages.length - 1; i >= 0; i--) {
    const msg = safeMessages[i]
    if (!msg || msg.role !== 'assistant') continue
    const blocks = Array.isArray(msg.blocks) ? msg.blocks : []
    for (let j = blocks.length - 1; j >= 0; j--) {
      const block = blocks[j]
      if (!block || typeof block !== 'object') continue
      if (block.kind === 'tool_call' && /^TodoWrite$/i.test(block.toolName)) {
        const tb = block as ToolCallBlock
        const raw = tb.parameters?.todos
        if (typeof raw === 'string' && raw.trim()) return raw
      }
    }
  }
  return ''
}

export function isTodoWriteBlock(block: { kind: string; toolName?: string }): boolean {
  return block.kind === 'tool_call' && /^TodoWrite$/i.test((block as any).toolName || '')
}

export { extractLastTodoString }

export function extractTodos(messages?: ChatMessage[] | null): TodoItem[] {
  const raw = extractLastTodoString(messages)
  if (!raw) return []
  return parseTodos(raw)
}

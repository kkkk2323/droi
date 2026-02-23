import React, { useRef, useState } from 'react'
import { ChevronDown, ChevronRight, Check, Circle, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
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

function extractLastTodoString(messages: ChatMessage[]): string {
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i]
    if (msg.role !== 'assistant') continue
    for (let j = msg.blocks.length - 1; j >= 0; j--) {
      const block = msg.blocks[j]
      if (block.kind === 'tool_call' && /^TodoWrite$/i.test(block.toolName)) {
        const tb = block as ToolCallBlock
        const raw = tb.parameters?.todos
        if (typeof raw === 'string' && raw.trim()) {
          if (tb.result !== undefined) return raw
          return raw
        }
      }
    }
  }
  return ''
}

export function extractTodos(messages: ChatMessage[]): TodoItem[] {
  const raw = extractLastTodoString(messages)
  if (!raw) return []
  return parseTodos(raw)
}

export function isTodoWriteBlock(block: { kind: string; toolName?: string }): boolean {
  return block.kind === 'tool_call' && /^TodoWrite$/i.test((block as any).toolName || '')
}

const EMPTY_TODOS: TodoItem[] = []

interface TodoPanelProps {
  messages: ChatMessage[]
}

export function TodoPanel({ messages }: TodoPanelProps) {
  const [todosExpanded, setTodosExpanded] = useState(false)
  const cacheRef = useRef<{ raw: string; parsed: TodoItem[] }>({ raw: '', parsed: EMPTY_TODOS })

  const rawStr = extractLastTodoString(messages)
  if (rawStr !== cacheRef.current.raw) {
    cacheRef.current = { raw: rawStr, parsed: rawStr ? parseTodos(rawStr) : EMPTY_TODOS }
  }
  const todos = cacheRef.current.parsed

  if (todos.length === 0) return null

  const completedCount = todos.filter((t) => t.status === 'completed').length
  const totalCount = todos.length

  // Hide when all todos are completed
  if (completedCount === totalCount) return null

  return (
    <div className="mx-auto w-full max-w-2xl px-4">
      <div className="rounded-t-xl border border-b-0 border-border bg-card shadow-sm dark:bg-card/80 dark:backdrop-blur-sm dark:border-white/10 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.06)]">
        <div className="px-3">
          <button
            className="flex w-full items-center gap-2 py-2 text-left text-xs font-medium text-foreground/80 hover:text-foreground transition-colors"
            onClick={() => setTodosExpanded(!todosExpanded)}
          >
            {todosExpanded
              ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
              : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />}
            <span>Tasks</span>
            <span className="text-muted-foreground font-normal">
              {completedCount}/{totalCount}
            </span>
            <div className="ml-2 flex-1">
              <div className="h-1.5 w-full max-w-[120px] rounded-full bg-muted overflow-hidden">
                <div
                  className="h-full rounded-full bg-emerald-500 transition-all duration-300"
                  style={{ width: `${totalCount > 0 ? (completedCount / totalCount) * 100 : 0}%` }}
                />
              </div>
            </div>
          </button>
          {todosExpanded && (
            <div className="pb-2 space-y-0.5">
              {todos.map((todo) => (
                <div
                  key={todo.index}
                  className={cn(
                    'flex items-start gap-2 rounded px-2 py-1 text-xs',
                    todo.status === 'completed' && 'text-muted-foreground',
                    todo.status === 'in_progress' && 'text-foreground',
                    todo.status === 'pending' && 'text-muted-foreground/80',
                  )}
                >
                  {todo.status === 'completed' && (
                    <Check className="mt-0.5 size-3 shrink-0 text-emerald-500" />
                  )}
                  {todo.status === 'in_progress' && (
                    <Loader2 className="mt-0.5 size-3 shrink-0 animate-spin text-foreground" />
                  )}
                  {todo.status === 'pending' && (
                    <Circle className="mt-0.5 size-3 shrink-0 text-muted-foreground/50" />
                  )}
                  <span className={cn(todo.status === 'completed' && 'line-through')}>{todo.text}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

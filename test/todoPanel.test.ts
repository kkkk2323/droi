import test from 'node:test'
import assert from 'node:assert/strict'
import { extractTodos, parseTodos } from '../src/renderer/src/lib/todoPanel.ts'

test('extractTodos returns an empty list for missing messages', () => {
  assert.deepEqual(extractTodos(undefined), [])
  assert.deepEqual(extractTodos(null), [])
})

test('extractTodos ignores malformed message blocks', () => {
  const todos = extractTodos([
    {
      id: 'a1',
      role: 'assistant',
      blocks: [undefined as any, { kind: 'text', content: 'hello' } as any],
      timestamp: 1,
    } as any,
  ])

  assert.deepEqual(todos, [])
})

test('extractTodos reads the latest TodoWrite tool payload', () => {
  const todos = extractTodos([
    {
      id: 'a1',
      role: 'assistant',
      blocks: [
        {
          kind: 'tool_call',
          toolName: 'TodoWrite',
          parameters: { todos: '1. [completed] Done\n2. [in_progress] Working' },
        },
      ],
      timestamp: 1,
    } as any,
  ])

  assert.deepEqual(todos, parseTodos('1. [completed] Done\n2. [in_progress] Working'))
})

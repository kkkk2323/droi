import { describe, expect, it } from 'vitest'
import { parseTranscript, userPromptCount } from './transcript'

const line = (value: unknown) => JSON.stringify(value)

describe('parseTranscript', () => {
  it('keeps the user’s prompts and the assistant’s prose, nothing else', () => {
    const jsonl = [
      line({ type: 'session_start', id: 's', cwd: '/w' }),
      line({
        type: 'message',
        id: 'context-1',
        message: {
          role: 'user',
          content: [{ type: 'text', text: '<system-reminder>skills</system-reminder>' }],
        },
      }),
      line({
        type: 'message',
        message: {
          role: 'user',
          content: [
            { type: 'text', text: 'Fix the login bug' },
            { type: 'text', text: '<system-reminder>date</system-reminder>' },
          ],
        },
      }),
      line({
        type: 'message',
        message: {
          role: 'assistant',
          content: [
            { type: 'thinking', thinking: 'hmm' },
            { type: 'text', text: 'Looking at auth.ts' },
            { type: 'tool_use', id: 't', name: 'Read', input: {} },
          ],
        },
      }),
      line({
        type: 'message',
        message: { role: 'user', content: [{ type: 'tool_result', content: 'file' }] },
      }),
      line({ type: 'todo_state', todos: [] }),
      'not json',
      line({ type: 'message', message: { role: 'user', content: '不对，是 session.ts' } }),
    ].join('\n')
    const turns = parseTranscript(jsonl)
    expect(turns).toEqual([
      { role: 'user', text: 'Fix the login bug' },
      { role: 'assistant', text: 'Looking at auth.ts' },
      { role: 'user', text: '不对，是 session.ts' },
    ])
    expect(userPromptCount(turns)).toBe(2)
  })
})

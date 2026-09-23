import { describe, expect, test } from 'vitest'
import { createdFileDiff, parseDiffResult, parseStatusResult, permissionDetail } from './tool-calls'
import type { ToolCall } from './transcript'

describe('parseStatusResult', () => {
  test("reads Create's bare success object", () => {
    expect(parseStatusResult('{"success":true,"file_path":"/w/a.ts"}')).toEqual({
      success: true,
      message: null,
    })
    expect(parseStatusResult('{"success":false,"error":"EACCES: permission denied"}')).toEqual({
      success: false,
      message: 'EACCES: permission denied',
    })
  })

  test('anything with more to say is left as it came', () => {
    expect(parseStatusResult('{"success":true,"stdout":"hi"}')).toBeNull()
    expect(parseStatusResult('[Process exited with code 0]')).toBeNull()
    expect(parseStatusResult('{"success":"yes"}')).toBeNull()
    expect(parseStatusResult('{"success": not json')).toBeNull()
  })
})

describe('parseDiffResult', () => {
  test("reads the Daemon's Edit result into lines with both numbers", () => {
    const result = parseDiffResult(
      JSON.stringify({
        success: true,
        file_path: '/w/a.ts',
        diffLines: [
          { type: 'unchanged', content: 'a', lineNumber: { old: 1, new: 1 } },
          { type: 'removed', content: 'b', lineNumber: { old: 2 } },
          { type: 'added', content: 'c', lineNumber: { new: 2 } },
          { type: 'added', content: 'd', lineNumber: { new: 3 } },
        ],
      }),
    )
    expect(result).toEqual({
      added: 2,
      removed: 1,
      lines: [
        { type: 'unchanged', content: 'a', old: 1, new: 1 },
        { type: 'removed', content: 'b', old: 2, new: null },
        { type: 'added', content: 'c', old: null, new: 2 },
        { type: 'added', content: 'd', old: null, new: 3 },
      ],
    })
  })

  test('other results are left alone', () => {
    expect(parseDiffResult('[Process exited with code 0]')).toBeNull()
    expect(parseDiffResult('{"success":true}')).toBeNull()
    expect(parseDiffResult('{"diffLines": not json')).toBeNull()
  })
})

describe('createdFileDiff', () => {
  const call = (name: string, input: Record<string, unknown>): ToolCall =>
    ({ use: { type: 'tool_use', id: 'c1', name, input }, result: null }) as unknown as ToolCall

  test("shows a Create's content as numbered added lines, ignoring the final newline", () => {
    expect(createdFileDiff(call('Create', { file_path: 'a.ts', content: 'one\ntwo\n' }))).toEqual({
      lines: [
        { type: 'added', content: 'one', old: null, new: 1 },
        { type: 'added', content: 'two', old: null, new: 2 },
      ],
      added: 2,
      removed: 0,
    })
  })

  test('is null for other tools and for a Create without content', () => {
    expect(createdFileDiff(call('Edit', { content: 'x' }))).toBeNull()
    expect(createdFileDiff(call('Create', { file_path: 'a.ts' }))).toBeNull()
  })
})

describe('permissionDetail', () => {
  test('shows the full command the Daemon worked out, before the raw input', () => {
    expect(permissionDetail({ fullCommand: 'npm test -- --watch' }, { command: 'npm test' })).toBe(
      '$ npm test -- --watch',
    )
  })

  test('shows the file an edit touches', () => {
    expect(permissionDetail({ filePath: '/w/a.ts' }, { file_path: '/w/a.ts' })).toBe('/w/a.ts')
  })

  test('falls back to the input: its command, else all of it', () => {
    expect(permissionDetail(undefined, { command: 'ls' })).toBe('$ ls')
    expect(permissionDetail(null, { url: 'https://x' })).toBe('{"url":"https://x"}')
  })
})

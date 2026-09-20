import { describe, expect, test } from 'vitest'
import { parseDiffResult, parseStatusResult } from './tool-activity'

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

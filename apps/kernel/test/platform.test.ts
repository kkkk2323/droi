import { describe, expect, test } from 'bun:test'
import { codingTools, findGitBash, gitBashPaths, shellChoice } from '../src/extensions/coding.ts'

/** A Windows install as the environment presents it, with the Git Bash that is there, if any. */
function windowsEnv(present: boolean) {
  const env = {
    ProgramFiles: 'C:\\Program Files',
    'ProgramFiles(x86)': 'C:\\Program Files (x86)',
    LOCALAPPDATA: 'C:\\Users\\clive\\AppData\\Local',
  }
  const exists = (path: string) =>
    present && path.toLowerCase() === 'c:\\program files\\git\\bin\\bash.exe'
  return { env, exists }
}

describe('the platform tool layer', () => {
  test('macOS and Linux get bash and never the PowerShell tool', () => {
    for (const platform of ['darwin', 'linux'] as const) {
      const choice = shellChoice({ platform, env: {}, exists: () => false })
      expect(choice.kind).toBe('bash')
      expect(choice.shellPath).toBeUndefined()
      const tools = codingTools({ platform, env: {}, exists: () => false })
      expect(tools.names).toContain('bash')
      expect(tools.names).not.toContain('powershell')
    }
  })

  test('Windows without Git Bash gets powershell and no bash at all', () => {
    const { env, exists } = windowsEnv(false)
    const tools = codingTools({ platform: 'win32', env, exists })
    expect(tools.names).toContain('powershell')
    expect(tools.names).not.toContain('bash')
    // Nothing may point the environment at a shell: a bash tool here would run WSL.
    expect(tools.shellPath).toBeUndefined()
  })

  test('Windows with Git Bash gets bash with its path given explicitly', () => {
    const { env, exists } = windowsEnv(true)
    const tools = codingTools({ platform: 'win32', env, exists })
    expect(tools.names).toContain('bash')
    expect(tools.names).toContain('powershell')
    expect(tools.shellPath?.toLowerCase()).toBe('c:\\program files\\git\\bin\\bash.exe')
  })

  test('a bash.exe in System32 (WSL) is not Git Bash', () => {
    const { env } = windowsEnv(false)
    const exists = (path: string) => {
      // Only the WSL entry point is present, which is what the kernel must never choose.
      return path.toLowerCase() === 'c:\\windows\\system32\\bash.exe'
    }
    expect(findGitBash('win32', env, exists)).toBeUndefined()
    expect(codingTools({ platform: 'win32', env, exists }).names).not.toContain('bash')
  })

  test('the search covers the usual install roots, in order', () => {
    const paths = gitBashPaths({
      ProgramFiles: 'C:\\Program Files',
      LOCALAPPDATA: 'C:\\Users\\c\\AppData\\Local',
    })
    expect(paths[0]).toBe('C:\\Program Files\\Git\\bin\\bash.exe')
    expect(paths).toContain('C:\\Users\\c\\AppData\\Local\\Programs\\Git\\bin\\bash.exe')
    // No PATH lookup: a bash on PATH may be WSL.
    expect(paths.every((path) => path.endsWith('\\Git\\bin\\bash.exe'))).toBe(true)
  })

  test('the extension carries the reading tools and a section for each platform', () => {
    const mac = codingTools({ platform: 'darwin' })
    expect(mac.names).toEqual(['read', 'write', 'edit', 'bash', 'grep', 'find', 'ls'])
    const windows = codingTools({ platform: 'win32', ...windowsEnv(false) })
    expect(windows.names).toEqual(['read', 'write', 'edit', 'powershell', 'grep', 'find', 'ls'])
    expect(mac.extension.sections?.map((section) => section.key)).toEqual(['environment', 'cwd'])
  })
})

import { execFile, execSync } from 'child_process'
import { access, constants } from 'fs/promises'
import type { EditorInfo } from '../shared/protocol'

interface EditorDef {
  id: string
  name: string
  // macOS .app bundle name(s) to look for
  appNames?: string[]
  // CLI command to check / use
  command?: string
}

const KNOWN_EDITORS: EditorDef[] = [
  { id: 'vscode', name: 'VS Code', appNames: ['Visual Studio Code.app', 'Visual Studio Code - Insiders.app'], command: 'code' },
  { id: 'cursor', name: 'Cursor', appNames: ['Cursor.app'], command: 'cursor' },
  { id: 'antigravity', name: 'Antigravity', appNames: ['Antigravity.app'], command: 'antigravity' },
  { id: 'windsurf', name: 'Windsurf', appNames: ['Windsurf.app'], command: 'windsurf' },
  { id: 'zed', name: 'Zed', appNames: ['Zed.app'], command: 'zed' },
  { id: 'idea', name: 'IntelliJ IDEA', appNames: ['IntelliJ IDEA.app', 'IntelliJ IDEA CE.app'], command: 'idea' },
  { id: 'webstorm', name: 'WebStorm', appNames: ['WebStorm.app'], command: 'webstorm' },
  { id: 'sublime', name: 'Sublime Text', appNames: ['Sublime Text.app'], command: 'subl' },
  { id: 'finder', name: 'Finder' },
  { id: 'terminal', name: 'Terminal', appNames: ['Terminal.app'] },
  { id: 'iterm', name: 'iTerm', appNames: ['iTerm.app', 'iTerm2.app'] },
  { id: 'ghostty', name: 'Ghostty', appNames: ['Ghostty.app'] },
  { id: 'warp', name: 'Warp', appNames: ['Warp.app'] },
]

export interface EditorsDeps {
  platform: NodeJS.Platform
  commandExists: (cmd: string) => Promise<boolean>
  canOpenMacApp: (appName: string) => Promise<boolean>
  openPath: (dir: string) => Promise<void>
  openWithCommand: (cmd: string, args: string[]) => Promise<boolean>
  openWithMacApp: (appName: string, dir: string) => Promise<boolean>
}

function getMacOpenName(appName: string): string {
  return appName.endsWith('.app') ? appName.slice(0, -'.app'.length) : appName
}

async function commandExists(cmd: string): Promise<boolean> {
  try {
    const path = execSync(`which ${cmd}`, { encoding: 'utf-8', timeout: 3000 }).trim()
    if (!path) return false
    await access(path, constants.X_OK)
    return true
  } catch {
    return false
  }
}

async function canOpenMacApp(appName: string): Promise<boolean> {
  const openName = getMacOpenName(appName)
  return new Promise((resolve) => {
    execFile('open', ['-Ra', openName], { timeout: 5000 }, (err) => {
      resolve(!err)
    })
  })
}

async function openWithCommand(cmd: string, args: string[]): Promise<boolean> {
  return new Promise((resolve) => {
    execFile(cmd, args, { timeout: 10000 }, (err) => {
      resolve(!err)
    })
  })
}

async function openWithMacApp(appName: string, dir: string): Promise<boolean> {
  const openName = getMacOpenName(appName)
  return new Promise((resolve) => {
    execFile('open', ['-a', openName, dir], { timeout: 10000 }, (err) => {
      resolve(!err)
    })
  })
}

async function openPath(dir: string): Promise<void> {
  const { shell } = await import('electron')
  await shell.openPath(dir)
}

const REAL_DEPS: EditorsDeps = {
  platform: process.platform,
  commandExists,
  canOpenMacApp,
  openPath,
  openWithCommand,
  openWithMacApp,
}

export async function __detectInstalledEditors(deps: EditorsDeps): Promise<EditorInfo[]> {
  const results: EditorInfo[] = []
  const isDarwin = deps.platform === 'darwin'

  await Promise.all(
    KNOWN_EDITORS.map(async (def) => {
      // Finder is always available on macOS
      if (def.id === 'finder') {
        results.push({ id: def.id, name: def.name })
        return
      }

      let found = false

      // Check macOS apps via LaunchServices
      if (isDarwin && def.appNames) {
        for (const appName of def.appNames) {
          if (await deps.canOpenMacApp(appName)) {
            found = true
            break
          }
        }
      }

      // Fallback: check CLI command
      if (!found && def.command) {
        found = await deps.commandExists(def.command)
      }

      if (found) {
        results.push({ id: def.id, name: def.name, command: def.command })
      }
    })
  )

  // Sort: keep the order from KNOWN_EDITORS
  const orderMap = new Map(KNOWN_EDITORS.map((e, i) => [e.id, i]))
  results.sort((a, b) => (orderMap.get(a.id) ?? 999) - (orderMap.get(b.id) ?? 999))

  return results
}

export async function detectInstalledEditors(): Promise<EditorInfo[]> {
  return __detectInstalledEditors(REAL_DEPS)
}

export async function __openWithEditor(deps: EditorsDeps, dir: string, editorId: string): Promise<void> {
  if (editorId === 'finder') {
    await deps.openPath(dir)
    return
  }

  const def = KNOWN_EDITORS.find((e) => e.id === editorId)
  if (!def) {
    await deps.openPath(dir)
    return
  }

  // Prefer CLI when installed (e.g. `code .`), but fall back to opening the .app on macOS.
  if (def.command && await deps.commandExists(def.command)) {
    const ok = await deps.openWithCommand(def.command, [dir])
    if (ok) return
  }

  if (deps.platform === 'darwin' && def.appNames?.length) {
    for (const appName of def.appNames) {
      const ok = await deps.openWithMacApp(appName, dir)
      if (ok) return
    }
  }

  await deps.openPath(dir)
}

export async function openWithEditor(dir: string, editorId: string): Promise<void> {
  await __openWithEditor(REAL_DEPS, dir, editorId)
}

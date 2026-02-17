import test from 'node:test'
import assert from 'node:assert/strict'

import { __detectInstalledEditors, __openWithEditor } from '../src/main/editors.ts'
import type { EditorsDeps } from '../src/main/editors.ts'

function createDeps(overrides: Partial<EditorsDeps> = {}): EditorsDeps {
  return {
    platform: 'darwin',
    commandExists: async () => false,
    canOpenMacApp: async () => false,
    openPath: async () => {},
    openWithCommand: async () => false,
    openWithMacApp: async () => false,
    ...overrides,
  }
}

test('detectInstalledEditors only includes apps that can be opened', async () => {
  const editors = await __detectInstalledEditors(createDeps({
    platform: 'darwin',
    canOpenMacApp: async (appName) => appName === 'Visual Studio Code.app',
  }))

  assert.ok(editors.some((e) => e.id === 'finder'))
  assert.ok(editors.some((e) => e.id === 'vscode'))
  assert.ok(!editors.some((e) => e.id === 'iterm'))
})

test('openWithEditor falls back to opening the macOS app when CLI is missing', async () => {
  const calls: string[] = []
  await __openWithEditor(createDeps({
    platform: 'darwin',
    commandExists: async (cmd) => {
      calls.push(`commandExists:${cmd}`)
      return false
    },
    openWithMacApp: async (appName, dir) => {
      calls.push(`openWithMacApp:${appName}:${dir}`)
      return true
    },
    openPath: async (dir) => {
      calls.push(`openPath:${dir}`)
    },
  }), '/repo', 'vscode')

  assert.ok(calls.some((c) => c.startsWith('openWithMacApp:Visual Studio Code.app')))
  assert.ok(!calls.some((c) => c.startsWith('openPath:')))
})

test('openWithEditor falls back to opening the macOS app when CLI fails', async () => {
  const calls: string[] = []
  await __openWithEditor(createDeps({
    platform: 'darwin',
    commandExists: async () => true,
    openWithCommand: async (cmd, args) => {
      calls.push(`openWithCommand:${cmd}:${args.join(',')}`)
      return false
    },
    openWithMacApp: async (appName, dir) => {
      calls.push(`openWithMacApp:${appName}:${dir}`)
      return true
    },
  }), '/repo', 'vscode')

  assert.ok(calls.some((c) => c.startsWith('openWithCommand:code:')))
  assert.ok(calls.some((c) => c.startsWith('openWithMacApp:Visual Studio Code.app')))
})

test('openWithEditor ultimately falls back to openPath when no handler works', async () => {
  const calls: string[] = []
  await __openWithEditor(createDeps({
    platform: 'darwin',
    commandExists: async () => false,
    openWithMacApp: async () => false,
    openPath: async (dir) => {
      calls.push(`openPath:${dir}`)
    },
  }), '/repo', 'vscode')

  assert.deepEqual(calls, ['openPath:/repo'])
})

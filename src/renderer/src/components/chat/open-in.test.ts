import { describe, expect, test } from 'vitest'
import { preferredApp } from './open-in'

const app = (id: string) => ({ id, label: id, icon: null })

describe('preferredApp', () => {
  test('keeps the remembered app while it is installed', () => {
    expect(preferredApp([app('vscode'), app('finder')], 'vscode')?.id).toBe('vscode')
  })

  test('falls back to Finder, then to the first app', () => {
    expect(preferredApp([app('vscode'), app('finder')], 'zed')?.id).toBe('finder')
    expect(preferredApp([app('vscode'), app('cursor')], null)?.id).toBe('vscode')
    expect(preferredApp([], 'vscode')).toBeNull()
  })
})

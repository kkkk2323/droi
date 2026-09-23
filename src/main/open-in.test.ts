import { describe, expect, test } from 'vitest'
import { locateOpenInApps } from './open-in'

const exists = (present: string[]) => (path: string) => present.includes(path)

describe('locateOpenInApps', () => {
  const base = { platform: 'darwin' as const, homedir: '/Users/me' }

  test('lists installed apps in catalog order, wherever the bundle lives', () => {
    const found = locateOpenInApps({
      ...base,
      exists: exists([
        '/System/Library/CoreServices/Finder.app',
        '/Users/me/Applications/Cursor.app',
        '/Applications/Visual Studio Code.app',
        '/System/Applications/Utilities/Terminal.app',
      ]),
    })
    expect(found.map((app) => [app.id, app.appPath])).toEqual([
      ['vscode', '/Applications/Visual Studio Code.app'],
      ['cursor', '/Users/me/Applications/Cursor.app'],
      ['finder', '/System/Library/CoreServices/Finder.app'],
      ['terminal', '/System/Applications/Utilities/Terminal.app'],
    ])
  })

  test('takes the first bundle name found for an app that ships under several', () => {
    const found = locateOpenInApps({ ...base, exists: exists(['/Applications/Zed Preview.app']) })
    expect(found).toEqual([{ id: 'zed', label: 'Zed', appPath: '/Applications/Zed Preview.app' }])
  })

  test('is empty off macOS', () => {
    expect(locateOpenInApps({ ...base, platform: 'linux', exists: () => true })).toEqual([])
  })
})

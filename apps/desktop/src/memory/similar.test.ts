import { describe, expect, it } from 'vitest'
import { similarEntries, similarityTokens } from './similar'
import type { MemoryEntry } from './store'

const entry = (id: string, text: string): MemoryEntry => ({
  id,
  scope: 'project',
  workspace: '/Users/dev/app',
  category: 'convention',
  day: '2026-10-03',
  text,
})

const MEMORY = [
  entry(
    'release1',
    'Release procedure: bump "version" in apps/desktop/package.json, add a CHANGELOG.md section, commit "chore(release): X.Y.Z", tag vX.Y.Z and push main and the tag.',
  ),
  entry(
    'vitest01',
    'Unit tests sit next to the code as *.test.ts and run with vitest from the root.',
  ),
  entry(
    'wsl00001',
    'win.myhome is WSL2 Ubuntu with systemd; the multica daemon runs as a user service.',
  ),
  entry('cover001', '小红书视频封面用 4:3，关键内容放在中间安全区。'),
  entry('voice001', '中文讲解视频的配音用 Gemini 的 Puck 声音，语速 1.25 倍。'),
]

describe('similarityTokens', () => {
  it('keeps Latin words of two or more characters and splits CJK runs into bigrams', () => {
    expect(similarityTokens('Use pnpm, a tool')).toEqual(['use', 'pnpm', 'tool'])
    expect(similarityTokens('视频封面')).toEqual(['视频', '频封', '封面'])
    expect(similarityTokens('用 x')).toEqual(['用'])
  })
})

describe('similarEntries', () => {
  it('finds an entry that says the same thing in other words', () => {
    const found = similarEntries(
      'Releasing Droi: a "chore(release): X.Y.Z" commit bumps apps/desktop/package.json version and prepends a CHANGELOG.md entry, then tag vX.Y.Z and push main plus the tag.',
      MEMORY,
    )
    expect(found.map((e) => e.id)).toEqual(['release1'])
  })

  it('finds a Chinese entry about the same subject', () => {
    expect(
      similarEntries('小红书封面改成 3:4 竖版，关键内容仍放中间。', MEMORY).map((e) => e.id),
    ).toEqual(['cover001'])
  })

  it('finds nothing for an unrelated fact', () => {
    expect(similarEntries('The Phone App is pinned to Expo SDK 57.', MEMORY)).toEqual([])
    expect(similarEntries('', MEMORY)).toEqual([])
    expect(similarEntries('anything', [])).toEqual([])
  })

  it('returns at most the limit, best first', () => {
    const copies = [
      entry('a', 'pnpm check runs lint'),
      entry('b', 'pnpm check runs lint and format'),
    ]
    const found = similarEntries('pnpm check runs lint', copies, { limit: 1 })
    expect(found.map((e) => e.id)).toEqual(['a'])
  })
})

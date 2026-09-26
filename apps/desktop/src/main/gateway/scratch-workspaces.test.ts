import { afterEach, beforeEach, describe, expect, test } from 'vitest'
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { basename, dirname, join } from 'node:path'
import { createScratchFolders } from './scratch-workspaces'

let home: string
let root: string
let trashed: string[]

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'droi-scratch-'))
  root = join(home, '.droi', 'chats')
  trashed = []
})

afterEach(() => rmSync(home, { recursive: true, force: true }))

const folders = () =>
  createScratchFolders({
    root: () => root,
    moveToTrash: async (path) => {
      trashed.push(path)
      rmSync(path, { recursive: true, force: true })
    },
    now: () => new Date(2026, 8, 26, 23, 30),
  })

describe('Scratch folders', () => {
  test('create makes a dated folder of its own under the Scratch folder, creating the root', async () => {
    const scratch = folders()
    const first = await scratch.create()
    const second = await scratch.create()
    expect(dirname(first)).toBe(root)
    expect(basename(first)).toMatch(/^2026-09-26-[0-9a-f]{6}$/)
    expect(second).not.toBe(first)
    expect(existsSync(first) && existsSync(second)).toBe(true)
  })

  test('create follows a changed Scratch folder', async () => {
    const scratch = folders()
    root = join(home, 'elsewhere')
    expect(dirname(await scratch.create())).toBe(root)
  })

  test('trash moves a folder with anything in it to the Trash; a folder already gone is fine', async () => {
    const scratch = folders()
    const path = await scratch.create()
    writeFileSync(join(path, '.DS_Store'), '')
    await scratch.trash(path)
    expect(trashed).toEqual([path])
    await scratch.trash(path)
    expect(trashed).toEqual([path])
  })

  test('trash simply removes an empty folder, leaving nothing in the Trash', async () => {
    const scratch = folders()
    const path = await scratch.create()
    await scratch.trash(path)
    expect(existsSync(path)).toBe(false)
    expect(trashed).toEqual([])
  })

  test('restore recreates a missing folder and leaves an existing one alone', async () => {
    const scratch = folders()
    const path = await scratch.create()
    writeFileSync(join(path, 'notes.md'), 'x')
    await scratch.restore(path)
    expect(existsSync(join(path, 'notes.md'))).toBe(true)
    await scratch.trash(path)
    await scratch.restore(path)
    expect(existsSync(path)).toBe(true)
  })

  test('refuses any path that is not a Scratch Workspace directly inside the Scratch folder', async () => {
    const scratch = folders()
    const project = join(home, 'projects', 'app')
    mkdirSync(project, { recursive: true })
    const made = await scratch.create()
    const refused = [
      project,
      root,
      join(root, 'notes'),
      join(made, 'nested'),
      join(root, '..', basename(made)),
      `${made}/../../../projects/app`,
      'relative/2026-09-26-abcdef',
    ]
    for (const path of refused) {
      await expect(scratch.trash(path), path).rejects.toThrow(/not a Scratch Workspace/)
      await expect(scratch.restore(path), path).rejects.toThrow(/not a Scratch Workspace/)
    }
    expect(trashed).toEqual([])
    expect(existsSync(project)).toBe(true)
  })
})

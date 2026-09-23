import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, describe, expect, test } from 'vitest'
import { builtinSoundPath, readSoundAsDataUrl } from './alert-sounds'

const dir = mkdtempSync(join(tmpdir(), 'droi-sounds-'))
afterAll(() => rmSync(dir, { recursive: true, force: true }))

describe('builtinSoundPath', () => {
  test('only the two CLI sounds, under ~/.factory/sounds', () => {
    expect(builtinSoundPath('/Users/me/.factory', 'fx-ok01')).toBe(
      '/Users/me/.factory/sounds/fx-ok01.wav',
    )
    expect(builtinSoundPath('/Users/me/.factory', '../../etc/passwd')).toBeNull()
  })
})

describe('readSoundAsDataUrl', () => {
  test('reads an audio file as a typed data: URL', async () => {
    const path = join(dir, 'ding.WAV')
    writeFileSync(path, Buffer.from('RIFF'))
    expect(await readSoundAsDataUrl(path)).toBe(
      `data:audio/wav;base64,${Buffer.from('RIFF').toString('base64')}`,
    )
  })

  test('refuses relative paths, other file types, empty and missing files', async () => {
    const text = join(dir, 'notes.txt')
    writeFileSync(text, 'hello')
    const empty = join(dir, 'empty.mp3')
    writeFileSync(empty, '')
    expect(await readSoundAsDataUrl('ding.wav')).toBeNull()
    expect(await readSoundAsDataUrl(text)).toBeNull()
    expect(await readSoundAsDataUrl(empty)).toBeNull()
    expect(await readSoundAsDataUrl(join(dir, 'missing.wav'))).toBeNull()
    expect(await readSoundAsDataUrl(42)).toBeNull()
  })
})

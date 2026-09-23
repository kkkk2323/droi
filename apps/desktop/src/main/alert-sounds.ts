// Reading alert sounds for the Local Client, which is served over HTTP by the
// Gateway and cannot load file:// URLs itself.
import { readFile, stat } from 'node:fs/promises'
import { extname, isAbsolute, join } from 'node:path'
import { BUILTIN_SOUNDS, type BuiltinSound } from '../shared/alerts'

/** Factory's cap for a custom sound. */
export const MAX_SOUND_BYTES = 25 * 1024 * 1024

const AUDIO_TYPES: Record<string, string> = {
  '.wav': 'audio/wav',
  '.mp3': 'audio/mpeg',
  '.m4a': 'audio/mp4',
  '.aac': 'audio/aac',
  '.ogg': 'audio/ogg',
  '.flac': 'audio/flac',
  '.aif': 'audio/aiff',
  '.aiff': 'audio/aiff',
}

export const SOUND_FILE_EXTENSIONS = Object.keys(AUDIO_TYPES).map((ext) => ext.slice(1))

export function builtinSoundPath(factoryHome: string, name: unknown): string | null {
  return BUILTIN_SOUNDS.includes(name as BuiltinSound)
    ? join(factoryHome, 'sounds', `${String(name)}.wav`)
    : null
}

/** The file as a data: URL, or null unless it is an absolute path to a small enough audio file. */
export async function readSoundAsDataUrl(path: unknown): Promise<string | null> {
  if (typeof path !== 'string' || !isAbsolute(path)) return null
  const type = AUDIO_TYPES[extname(path).toLowerCase()]
  if (!type) return null
  try {
    const entry = await stat(path)
    if (!entry.isFile() || entry.size === 0 || entry.size > MAX_SOUND_BYTES) return null
    return `data:${type};base64,${(await readFile(path)).toString('base64')}`
  } catch {
    return null
  }
}

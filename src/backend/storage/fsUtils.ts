import { mkdir, writeFile, rename, unlink } from 'fs/promises'
import { dirname } from 'path'

export async function ensureDir(path: string): Promise<void> {
  await mkdir(path, { recursive: true })
}

export async function atomicWriteFile(path: string, contents: string): Promise<void> {
  await ensureDir(dirname(path))
  const tmp = `${path}.tmp-${Date.now()}-${Math.random().toString(16).slice(2)}`
  await writeFile(tmp, contents, 'utf-8')
  try {
    await rename(tmp, path)
  } catch {
    try {
      await unlink(path)
    } catch {
      // ignore
    }
    await rename(tmp, path)
  }
}


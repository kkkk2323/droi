// Scratch Workspaces (ADR 0008): folders the Gateway makes under the Scratch
// folder for Sessions started without a Workspace. The Gateway only ever
// touches a folder it could have made, directly inside the Scratch folder.
import { mkdir, readdir, realpath, rmdir } from 'node:fs/promises'
import { randomBytes } from 'node:crypto'
import { basename, dirname, isAbsolute, join, resolve } from 'node:path'

export interface ScratchFolders {
  /** A new, empty folder; its path is where the Session starts. */
  create(): Promise<string>
  /**
   * Moves the folder to the Trash, or removes it when empty (an abandoned
   * Draft Session's, or a conversation that never wrote a file). A folder
   * already gone is left at that.
   */
  trash(path: string): Promise<void>
  /** Recreates the folder when it is gone, so an unarchived Session opens again. */
  restore(path: string): Promise<void>
}

export interface ScratchFoldersOptions {
  /** Read on every call: the Scratch folder is a setting the user can change. */
  root: () => string
  moveToTrash: (path: string) => Promise<void>
  now?: () => Date
}

const NAME = /^\d{4}-\d{2}-\d{2}-[0-9a-f]{6}$/

/** A path the Gateway will not touch: anything but a Scratch Workspace. */
export class NotAScratchWorkspace extends Error {
  constructor(path: string) {
    super(`${path} is not a Scratch Workspace`)
  }
}

export function createScratchFolders(options: ScratchFoldersOptions): ScratchFolders {
  const now = options.now ?? (() => new Date())

  // Resolved through symlinks on both sides, so `..` or a link cannot lead out.
  const checked = async (path: string): Promise<string> => {
    const root = options.root()
    const inside =
      isAbsolute(path) &&
      NAME.test(basename(path)) &&
      (await Promise.all([realpath(root), realpath(dirname(resolve(path)))]).then(
        ([base, parent]) => base === parent,
        () => false,
      ))
    if (!inside) throw new NotAScratchWorkspace(path)
    return join(root, basename(path))
  }

  return {
    async create() {
      const root = options.root()
      await mkdir(root, { recursive: true })
      for (;;) {
        const path = join(root, `${day(now())}-${randomBytes(3).toString('hex')}`)
        try {
          await mkdir(path)
          return path
        } catch (error) {
          if ((error as NodeJS.ErrnoException).code !== 'EEXIST') throw error
        }
      }
    },
    async trash(path) {
      const target = await checked(path)
      const entries = await readdir(target).catch(() => null)
      if (entries === null) return
      if (entries.length === 0) await rmdir(target)
      else await options.moveToTrash(target)
    },
    async restore(path) {
      await mkdir(await checked(path), { recursive: true })
    },
  }
}

function day(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

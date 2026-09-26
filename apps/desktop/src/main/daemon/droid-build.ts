// Which `droid` file a Daemon was started from. The droid CLI replaces its
// executable when it updates itself; a running Daemon keeps the old build, and
// with it the old model catalog, while the Sessions it starts afterwards run
// the new file. Only a new Daemon picks the update up.
import { statSync, type Stats } from 'node:fs'

export interface DroidBuild {
  path: string
  ino: number
  mtimeMs: number
}

type Stat = (path: string) => Pick<Stats, 'ino' | 'mtimeMs'>

export function droidBuildOf(path: string, stat: Stat = statSync): DroidBuild | null {
  try {
    const { ino, mtimeMs } = stat(path)
    return { path, ino, mtimeMs }
  } catch {
    return null
  }
}

/** True once the file at the Daemon's path is no longer the one it started from. */
export function isDroidReplaced(running: DroidBuild | null, stat: Stat = statSync): boolean {
  if (!running) return false
  // Missing mid-update: there is nothing newer to start yet.
  const now = droidBuildOf(running.path, stat)
  return now !== null && (now.ino !== running.ino || now.mtimeMs !== running.mtimeMs)
}

import { execFileSync } from 'node:child_process'
import { basename, dirname, resolve } from 'node:path'

/**
 * The repository a directory belongs to: the working tree's root, or the directory itself when it is
 * not inside one.
 *
 * The sidebar groups conversations by repository, so a worktree's sessions belong to the same group as
 * the main checkout's. The answer is taken once, when a conversation is created or its `cwd` changes,
 * and stored in the index, because `git rev-parse` is a process launch and a list of two hundred rows
 * must not pay for it.
 *
 * Every failure answers "not a repository", which is also what a bare `git init`-less directory is: a
 * missing `git`, a directory that no longer exists, a timeout. A wrong grouping is a nuisance; a thrown
 * error would fail the create that asked.
 */
export function repoRootOf(directory: string): string {
  const absolute = resolve(directory)
  const cached = cache.get(absolute)
  if (cached !== undefined) return cached
  let root = absolute
  try {
    const printed = execFileSync(
      'git',
      ['rev-parse', '--path-format=absolute', '--git-common-dir'],
      {
        cwd: absolute,
        encoding: 'utf8',
        stdio: ['ignore', 'pipe', 'ignore'],
        timeout: 2000,
        // A repository this process may not read is still a repository; git says so on stderr, not by exit.
        env: { ...process.env, LC_ALL: 'C' },
      },
    ).trim()
    // The common `.git` directory is shared by a repository and every worktree of it, so its parent is the
    // main checkout: a worktree's conversations then belong to the repository, not to a group of their own.
    // Anything else (a bare repository, a submodule) keeps its own `--show-toplevel`.
    const common = resolve(printed)
    root = basename(common) === '.git' ? dirname(common) : (toplevel(absolute) ?? absolute)
  } catch {
    // Not a working tree, or no git: the directory is its own group. Not cached: a failure now, on a volume
    // that was not mounted yet, must not decide the answer for ever.
    return root
  }
  cache.set(absolute, root)
  return root
}

/** The working tree's own root, for the cases where the common directory is not a `.git` inside one. */
function toplevel(directory: string): string | undefined {
  try {
    const printed = execFileSync('git', ['rev-parse', '--show-toplevel'], {
      cwd: directory,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
      timeout: 2000,
      env: { ...process.env, LC_ALL: 'C' },
    }).trim()
    return printed === '' ? undefined : resolve(printed)
  } catch {
    return undefined
  }
}

/** Remembers the answer per directory: a list, a create and an import of one workspace pay once. */
const cache = new Map<string, string>()

/** A test seam: the cache must not leak one test's temporary directories into another's assertions. */
export function forgetRepoRoots(): void {
  cache.clear()
}

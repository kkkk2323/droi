import { execFile } from 'node:child_process'
import { basename, dirname, resolve } from 'node:path'

/**
 * The repository a directory belongs to: the main checkout's root, or the directory itself when git
 * says it is not inside one. `undefined` when git could not be asked — no `git`, a directory that does
 * not exist (yet: a volume mounted later), a timeout — so the caller stores nothing and asks again later.
 *
 * The sidebar groups conversations by repository, so a worktree's sessions belong to the same group as
 * the main checkout's. The answer is taken once, when a conversation is created or its `cwd` changes,
 * and stored in the index, because `git rev-parse` is a process launch and a list of two hundred rows
 * must not pay for it. It runs asynchronously: a slow git (a network drive, a virus scanner) must not stop
 * the kernel answering everyone else.
 */
export function repoRootOf(directory: string): Promise<string | undefined> {
  const absolute = resolve(directory)
  const known = cache.get(absolute)
  if (known !== undefined) return known
  const asked = ask(absolute).then((root) => {
    // Only an answer is remembered: a failure now must not decide the answer for ever.
    if (root === undefined) cache.delete(absolute)
    return root
  })
  cache.set(absolute, asked)
  return asked
}

async function ask(absolute: string): Promise<string | undefined> {
  const common = await git(absolute, ['rev-parse', '--path-format=absolute', '--git-common-dir'])
  if (common.kind === 'failed') return undefined
  if (common.kind === 'not-a-repository') return absolute
  // The common `.git` directory is shared by a repository and every worktree of it, so its parent is the
  // main checkout: a worktree's conversations then belong to the repository, not to a group of their own.
  // Anything else (a bare repository, a submodule) keeps its own `--show-toplevel`.
  const dir = resolve(common.out)
  if (basename(dir) === '.git') return dirname(dir)
  const top = await git(absolute, ['rev-parse', '--show-toplevel'])
  return top.kind === 'ok' && top.out !== '' ? resolve(top.out) : absolute
}

type GitAnswer = { kind: 'ok'; out: string } | { kind: 'not-a-repository' } | { kind: 'failed' }

function git(cwd: string, args: string[]): Promise<GitAnswer> {
  return new Promise((done) => {
    execFile(
      'git',
      args,
      {
        cwd,
        encoding: 'utf8',
        timeout: 2000,
        windowsHide: true,
        env: { ...process.env, LC_ALL: 'C', GIT_TERMINAL_PROMPT: '0' },
      },
      (err, stdout, stderr) => {
        if (err === null) return done({ kind: 'ok', out: stdout.trim() })
        // git answered, and the answer is "no": the directory is its own group. Anything else — git missing,
        // the directory missing, a timeout — is a question that could not be asked.
        const code = (err as { code?: unknown }).code
        if (code === 128 && /not a git repository/i.test(stderr)) {
          return done({ kind: 'not-a-repository' })
        }
        done({ kind: 'failed' })
      },
    )
  })
}

/** The answer per directory: a list, a create and an import of one workspace pay once. */
const cache = new Map<string, Promise<string | undefined>>()

/** A test seam: the cache must not leak one test's temporary directories into another's assertions. */
export function forgetRepoRoots(): void {
  cache.clear()
}

import { execFile } from 'node:child_process'
import { existsSync } from 'node:fs'
import { lstat, readdir, readFile, stat } from 'node:fs/promises'
import { basename, isAbsolute, join, resolve, sep } from 'node:path'
import { defineTool } from '@earendil-works/pi-durable'
import type { ToolExecutionApi, ToolRegistration } from '@earendil-works/pi-durable'
import { arr, bool, int, obj, opt, str } from '@earendil-works/pi-schema'

/**
 * `grep`, `find` and `ls`: searching and listing files, for scripts. Each answers a script an array to filter,
 * count or slice in JavaScript, where a shell answers text that has to be piped through `grep`, `awk` and `head`
 * first, and on Windows there is no `awk` to pipe it through. They are read-only, so the approval policy lets them
 * run where it lets `read` (policy/decide.ts), and a script's card shows each as what it is.
 *
 * They are callable from scripts only (`callers: ['tools']`): a request offers the model none of them, so a
 * conversation pays nothing for them until a script calls one. Their names and arguments are pi-coding-agent's.
 *
 * Inside a git work tree the files are git's: tracked and untracked, without what it ignores. Elsewhere, and under a
 * path git ignores all of (a search of `node_modules/x` names what it wants), a walk that skips `.git` and
 * `node_modules` below it.
 */

/** The most files one search looks at. */
const MAX_FILES = 50_000
/** A file larger than this is not searched. */
const MAX_FILE_BYTES = 2 * 1024 * 1024
/** A matching line is cut to this many characters. */
const LINE_CHARS = 500
/** Directories a walk outside git skips. */
const SKIPPED = new Set(['.git', 'node_modules'])

type Listing = { files: string[]; truncated: boolean }

const text = (value: string, isError = false) => ({
  output: [{ type: 'text' as const, text: value }],
  ...(isError ? { isError: true as const } : {}),
})

/** The call's working directory, which a relative `path` is resolved against. */
function cwdOf(api: ToolExecutionApi): string {
  const env = api.env
  if (env === undefined) throw new Error('No execution environment')
  return env.cwd
}

/** `given` against `cwd`, or `cwd` itself. */
function target(cwd: string, given: string | undefined): string {
  return given === undefined || given === '' ? cwd : isAbsolute(given) ? given : resolve(cwd, given)
}

/** A path as a result shows it: under the `path` the call gave, so `read` takes it as it is. */
function shown(given: string | undefined, rel: string): string {
  const posix = rel.split(sep).join('/')
  return given === undefined || given === '' || given === '.'
    ? posix
    : join(given, posix).split(sep).join('/')
}

/** The files under `root`, relative to it with `/` separators. */
async function filesUnder(root: string): Promise<Listing> {
  const fromGit = await gitFiles(root)
  // A folder git ignores (node_modules, a build's output) has no files of git's: a search that names it means it.
  if (fromGit !== undefined && fromGit.length > 0) {
    const files = fromGit.slice(0, MAX_FILES)
    return { files, truncated: fromGit.length > MAX_FILES }
  }
  return walk(root)
}

/** git's files under `root`, or undefined outside a work tree or without git. */
function gitFiles(root: string): Promise<string[] | undefined> {
  return new Promise((done) => {
    execFile(
      'git',
      ['-C', root, 'ls-files', '-z', '--cached', '--others', '--exclude-standard'],
      { maxBuffer: 256 * 1024 * 1024, windowsHide: true },
      (error, stdout) => {
        if (error !== null) return done(undefined)
        const seen = new Set<string>()
        for (const file of stdout.split('\0')) if (file !== '') seen.add(file)
        done([...seen])
      },
    )
  })
}

async function walk(root: string): Promise<Listing> {
  const files: string[] = []
  const queue = ['']
  while (queue.length > 0) {
    const dir = queue.shift()!
    let entries
    try {
      entries = await readdir(join(root, dir), { withFileTypes: true })
    } catch {
      continue
    }
    entries.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))
    for (const entry of entries) {
      const rel = dir === '' ? entry.name : `${dir}/${entry.name}`
      if (entry.isDirectory()) {
        if (!SKIPPED.has(entry.name)) queue.push(rel)
      } else if (entry.isFile()) {
        if (files.length >= MAX_FILES) return { files, truncated: true }
        files.push(rel)
      }
    }
  }
  return { files, truncated: false }
}

/**
 * Whether `file` (relative, `/` separators) matches `pattern`: a pattern with a `/` is matched against the whole
 * path, one without against the file's name, as `*.ts` finds TypeScript at any depth.
 */
function matcher(pattern: string): (file: string) => boolean {
  const glob = new Bun.Glob(pattern)
  return pattern.includes('/')
    ? (file) => glob.match(file)
    : (file) => glob.match(file.slice(file.lastIndexOf('/') + 1))
}

function escape(pattern: string): string {
  return pattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

type Match = { path: string; line: number; text: string }

const grep = defineTool({
  name: 'grep',
  description:
    'Search file contents with a JavaScript regular expression, line by line. Inside a git work tree it searches the files git knows of, without the ignored ones, unless `path` is a folder git ignores (node_modules, a build folder), which is searched as it is; binary files and files over 2 MB are skipped. Resolves to { matches: [{ path, line, text }], truncated }, to filter in the script.',
  parameters: obj({
    pattern: str({
      description:
        'Search pattern: a JavaScript regular expression, or a literal string with literal',
    }),
    path: opt(str({ description: 'Directory or file to search (default: the working directory)' })),
    glob: opt(
      str({ description: "Only files matching this glob, e.g. '*.ts' or 'src/**/*.spec.ts'" }),
    ),
    ignoreCase: opt(bool({ description: 'Case-insensitive search (default: false)' })),
    literal: opt(bool({ description: 'Treat the pattern as a literal string (default: false)' })),
    limit: opt(int({ minimum: 1, description: 'Most matches to return (default: 100)' })),
  }),
  structuredOutputSchema: obj({
    matches: arr(obj({ path: str(), line: int(), text: str() })),
    truncated: bool({
      description:
        'Whether the limit, or the most files one search looks at, cut the matches short',
    }),
  }),
  replay: 'safe',
  callers: ['tools'],
  async execute(args, api) {
    const cwd = cwdOf(api)
    const root = target(cwd, args.path)
    let regex: RegExp
    try {
      regex = new RegExp(
        args.literal === true ? escape(args.pattern) : args.pattern,
        args.ignoreCase === true ? 'i' : '',
      )
    } catch (error) {
      return text(`Invalid pattern: ${(error as Error).message}`, true)
    }
    let info
    try {
      info = await stat(root)
    } catch {
      return text(`No such file or directory: ${args.path ?? root}`, true)
    }
    const listing: Listing = info.isFile()
      ? { files: [''], truncated: false }
      : await filesUnder(root)
    const include = args.glob === undefined || info.isFile() ? undefined : matcher(args.glob)
    const limit = args.limit ?? 100
    const matches: Match[] = []
    let truncated = listing.truncated
    const files = include === undefined ? listing.files : listing.files.filter(include)
    // Read a few at a time, in order, so the matches come in the order of the files.
    const BATCH = 16
    search: for (let i = 0; i < files.length; i += BATCH) {
      const batch = files.slice(i, i + BATCH)
      const contents = await Promise.all(
        batch.map((file) => readSmall(file === '' ? root : join(root, file))),
      )
      for (let j = 0; j < batch.length; j++) {
        const content = contents[j]
        if (content === undefined) continue
        const lines = content.split('\n')
        for (let n = 0; n < lines.length; n++) {
          const line = lines[n]!.replace(/\r$/, '')
          if (!regex.test(line)) continue
          if (matches.length >= limit) {
            truncated = true
            break search
          }
          const file = batch[j]!
          matches.push({
            path: file === '' ? (args.path ?? basename(root)) : shown(args.path, file),
            line: n + 1,
            text: line.length > LINE_CHARS ? `${line.slice(0, LINE_CHARS)}…` : line,
          })
        }
      }
    }
    const lines = matches.map((match) => `${match.path}:${match.line}: ${match.text}`)
    if (lines.length === 0) lines.push('No matches.')
    if (truncated)
      lines.push(
        `[${matches.length} matches shown: raise limit, or narrow the pattern, path or glob]`,
      )
    return { ...text(lines.join('\n')), structuredOutput: { matches, truncated } }
  },
})

/** A text file's content, or undefined for one too large, binary or unreadable. */
async function readSmall(path: string): Promise<string | undefined> {
  try {
    const info = await stat(path)
    if (!info.isFile() || info.size > MAX_FILE_BYTES) return undefined
    const bytes = await readFile(path)
    if (bytes.subarray(0, 8000).includes(0)) return undefined
    return bytes.toString('utf8')
  } catch {
    return undefined
  }
}

const find = defineTool({
  name: 'find',
  description:
    "Find files by glob. A pattern without '/' matches file names at any depth ('*.ts'); one with '/' matches the path ('src/**/*.spec.ts'). Inside a git work tree only the files git knows of, without the ignored ones, unless `path` is a folder git ignores. Resolves to { paths, truncated }.",
  parameters: obj({
    pattern: str({ description: "Glob pattern, e.g. '*.ts', '**/*.json' or 'src/**/*.spec.ts'" }),
    path: opt(str({ description: 'Directory to search in (default: the working directory)' })),
    limit: opt(int({ minimum: 1, description: 'Most paths to return (default: 1000)' })),
  }),
  structuredOutputSchema: obj({
    paths: arr(str()),
    truncated: bool({
      description: 'Whether the limit, or the most files one search looks at, cut the paths short',
    }),
  }),
  replay: 'safe',
  callers: ['tools'],
  async execute(args, api) {
    const root = target(cwdOf(api), args.path)
    try {
      if (!(await stat(root)).isDirectory())
        return text(`Not a directory: ${args.path ?? root}`, true)
    } catch {
      return text(`No such directory: ${args.path ?? root}`, true)
    }
    const listing = await filesUnder(root)
    const match = matcher(args.pattern)
    const limit = args.limit ?? 1000
    // git lists a tracked file that was deleted since; only what is there is found.
    const found = listing.files.filter((file) => match(file) && existsSync(join(root, file)))
    const truncated = listing.truncated || found.length > limit
    const paths = found.slice(0, limit).map((file) => shown(args.path, file))
    const lines = paths.length === 0 ? ['No files found.'] : [...paths]
    if (truncated)
      lines.push(`[${paths.length} paths shown: raise limit, or narrow the pattern or path]`)
    return { ...text(lines.join('\n')), structuredOutput: { paths, truncated } }
  },
})

type Entry = { name: string; type: 'file' | 'directory' | 'symlink' | 'other'; size: number }

const ls = defineTool({
  name: 'ls',
  description:
    'List a directory, dotfiles included, sorted by name. Resolves to { entries: [{ name, type, size }], truncated }; type is file, directory, symlink or other, and size is in bytes.',
  parameters: obj({
    path: opt(str({ description: 'Directory to list (default: the working directory)' })),
    limit: opt(int({ minimum: 1, description: 'Most entries to return (default: 500)' })),
  }),
  structuredOutputSchema: obj({
    entries: arr(obj({ name: str(), type: str(), size: int() })),
    truncated: bool(),
  }),
  replay: 'safe',
  callers: ['tools'],
  async execute(args, api) {
    const dir = target(cwdOf(api), args.path)
    let names: string[]
    try {
      names = await readdir(dir)
    } catch {
      return text(`Not a directory: ${args.path ?? dir}`, true)
    }
    names.sort((a, b) => (a < b ? -1 : a > b ? 1 : 0))
    const limit = args.limit ?? 500
    const entries: Entry[] = []
    for (const name of names.slice(0, limit)) {
      try {
        const info = await lstat(join(dir, name))
        const type = info.isSymbolicLink()
          ? 'symlink'
          : info.isDirectory()
            ? 'directory'
            : info.isFile()
              ? 'file'
              : 'other'
        entries.push({ name, type, size: info.isFile() ? info.size : 0 })
      } catch {
        // Gone since it was listed.
      }
    }
    const truncated = names.length > limit
    const lines = entries.map((entry) =>
      entry.type === 'directory' ? `${entry.name}/` : entry.name,
    )
    if (lines.length === 0) lines.push('(empty directory)')
    if (truncated) lines.push(`[${entries.length} of ${names.length} entries shown: raise limit]`)
    return { ...text(lines.join('\n')), structuredOutput: { entries, truncated } }
  },
})

/** `grep`, `find` and `ls`, for the coding extension. */
export function searchTools(): ToolRegistration[] {
  return [grep, find, ls] as unknown as ToolRegistration[]
}

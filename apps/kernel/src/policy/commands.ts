/**
 * Whether a command line runs something on the destructive list, under the `safe` autonomy level.
 *
 * Matched by command position, never by substring: `rm` must not match `npm run format` or `perform`, or
 * `safe` would ask about everyday commands. The line is cut into the commands it runs (`;`, `&&`, `||`,
 * `|`, `&`, a newline, `$(` and a backtick), each command's leading assignments and wrappers are skipped,
 * and its first word — the first two for the two-word entries — decides. A shell given a string (`sh -c`,
 * `eval`) has that string judged the same way, and a pipe into a shell always asks: what it runs is whatever
 * came down the pipe.
 *
 * A command the list does not name does not ask. That is the promise of `safe`, written where a person
 * picks it; whoever wants every command asked about picks `ask`.
 */

/** One word that decides on its own. PowerShell's names are here too, compared without case. */
const ONE = new Set([
  'rm',
  'rmdir',
  'mv',
  'dd',
  'truncate',
  'mkfs',
  'shred',
  'chmod',
  'chown',
  'kill',
  'pkill',
  'killall',
  'shutdown',
  'reboot',
  'sudo',
  'remove-item',
  'ri',
  'del',
  'erase',
  'rd',
  'move-item',
  'mi',
  'move',
  'stop-process',
  'spps',
  'clear-content',
  'format-volume',
  'restart-computer',
  'stop-computer',
])

/** Two words that decide together. */
const TWO = new Set(['git push', 'git reset', 'git clean', 'npm publish', 'pip install'])

/** Wrappers that run the command after them, skipped with their options. */
const WRAPPERS = new Set([
  'env',
  'command',
  'nohup',
  'time',
  'nice',
  'stdbuf',
  'xargs',
  'exec',
  'timeout',
])
/** A wrapper's options that take the next word as their value. */
const VALUED = new Set([
  '-n',
  '-I',
  '-L',
  '-P',
  '-s',
  '-d',
  '-E',
  '-u',
  '-C',
  '-a',
  '-k',
  '--arg-file',
  '--delimiter',
  '--max-args',
  '--max-procs',
  '--max-lines',
  '--max-chars',
  '--kill-after',
  '--signal',
])

/**
 * The options before a two-word entry's verb that take the next word as their value: `git -C repo push` is a
 * push, and the model writes it that way when told to name the repository.
 */
const BEFORE_VERB: Record<string, Set<string>> = {
  git: new Set([
    '-C',
    '-c',
    '--git-dir',
    '--work-tree',
    '--namespace',
    '--exec-path',
    '--config-env',
  ]),
  npm: new Set(['-C', '--prefix', '-w', '--workspace']),
  pip: new Set(['--python', '--log', '--proxy', '--cache-dir']),
}

/** Words that open a clause rather than name a program: `if x; then rm y; fi` runs `rm`. */
const KEYWORDS = new Set(['{', '}', '!', 'if', 'then', 'else', 'elif', 'do', 'while', 'until'])

const SHELLS = new Set(['sh', 'bash', 'zsh', 'dash', 'ksh', 'fish', 'pwsh', 'powershell', 'cmd'])

export function destructive(line: string, depth = 0): boolean {
  if (depth > 4) return true
  for (const { text, piped } of commandsOf(line)) {
    const words = wordsOf(text)
    let i = 0
    while (i < words.length && KEYWORDS.has(words[i]!)) i++
    while (i < words.length && /^[A-Za-z_][A-Za-z0-9_]*=/.test(words[i]!)) i++
    while (i < words.length && WRAPPERS.has(commandName(words[i]!))) {
      const wrapper = commandName(words[i]!)
      i++
      while (i < words.length && words[i]!.startsWith('-')) {
        i += VALUED.has(words[i]!) ? 2 : 1
      }
      // `timeout 5 rm x`: the duration comes before the command.
      if (wrapper === 'timeout') i++
      while (i < words.length && /^[A-Za-z_][A-Za-z0-9_]*=/.test(words[i]!)) i++
    }
    if (i >= words.length) continue
    const first = commandName(words[i]!)
    if (SHELLS.has(first)) {
      // A shell reading the pipe runs whatever came down it.
      if (piped) return true
      const script = shellScript(first, words.slice(i + 1))
      if (script !== undefined && destructive(script, depth + 1)) return true
      continue
    }
    if (first === 'eval') {
      if (destructive(words.slice(i + 1).join(' '), depth + 1)) return true
      continue
    }
    if (first === 'find' && findRuns(words.slice(i + 1), depth)) return true
    if (ONE.has(first) || first.startsWith('mkfs.')) return true
    const program = /^pip\d/.test(first) ? 'pip' : first
    const verb = verbOf(program, words.slice(i + 1))
    if (verb !== undefined && TWO.has(`${program} ${verb}`)) return true
  }
  return false
}

/**
 * The command line a shell is given to run, or undefined when it runs a file or reads its input. The flag
 * can sit among others (`bash -lc`, `sh -ec`); `cmd` runs the rest of its line after `/c` or `/k`; an
 * encoded PowerShell command cannot be read, so it is taken as one that asks.
 */
function shellScript(shell: string, args: string[]): string | undefined {
  if (shell === 'cmd') {
    const at = args.findIndex((word) => /^\/[ck]$/i.test(word))
    return at === -1 ? undefined : args.slice(at + 1).join(' ')
  }
  for (let at = 0; at < args.length; at++) {
    const word = args[at]!
    const lower = word.toLowerCase()
    if (shell === 'pwsh' || shell === 'powershell') {
      if (/^-e(c|ncodedcommand)?$/.test(lower)) return 'rm encoded'
      if (lower === '-c' || lower === '-command') return args.slice(at + 1).join(' ')
      continue
    }
    if (word === '--') return undefined
    if (/^-[A-Za-z]*c[A-Za-z]*$/.test(word)) {
      return args.slice(at + 1).find((next) => !next.startsWith('-'))
    }
    if (!word.startsWith('-')) return undefined
  }
  return undefined
}

/** `find` deletes with `-delete`, and runs a program with `-exec`, `-execdir`, `-ok` and `-okdir`. */
function findRuns(args: string[], depth: number): boolean {
  for (let at = 0; at < args.length; at++) {
    const word = args[at]!
    if (word === '-delete') return true
    if (!/^-(exec|execdir|ok|okdir)$/.test(word)) continue
    const end = args.findIndex((next, k) => k > at && (next === ';' || next === '+'))
    const program = args.slice(at + 1, end === -1 ? undefined : end).join(' ')
    if (destructive(program, depth + 1)) return true
  }
  return false
}

/** The verb of a two-word entry: the first word after the program that is neither an option nor its value. */
function verbOf(program: string, args: string[]): string | undefined {
  const valued = BEFORE_VERB[program]
  if (valued === undefined) return args[0]?.toLowerCase()
  for (let at = 0; at < args.length; at++) {
    const word = args[at]!
    if (!word.startsWith('-')) return word.toLowerCase()
    if (valued.has(word)) at++
  }
  return undefined
}

/** The program a word names: `/bin/rm` and `\\rm` are `rm`, and PowerShell's names have no case. */
function commandName(word: string): string {
  const bare = word.replace(/^\\/, '')
  const base = bare.split(/[\\/]/).pop() ?? bare
  return base.replace(/\.exe$/i, '').toLowerCase()
}

/**
 * The commands a line runs, in order, and whether each reads a pipe. Single quotes hold everything; a
 * substitution (`$(…)`, a backtick) runs even inside double quotes, so it is cut out there too.
 */
function commandsOf(line: string): { text: string; piped: boolean }[] {
  const out: { text: string; piped: boolean }[] = []
  let current = ''
  let piped = false
  let quote: "'" | '"' | undefined
  const cut = (nextPiped: boolean) => {
    if (current.trim() !== '') out.push({ text: current, piped })
    current = ''
    piped = nextPiped
  }
  for (let i = 0; i < line.length; i++) {
    const char = line[i]!
    const next = line[i + 1]
    if (quote === "'") {
      if (char === "'") quote = undefined
      current += char
      continue
    }
    if (char === '\\' && next !== undefined) {
      current += char + next
      i++
      continue
    }
    if (char === '$' && next === '(') {
      cut(false)
      i++
      continue
    }
    if (char === '`') {
      cut(false)
      continue
    }
    if (quote === '"') {
      if (char === '"') quote = undefined
      current += char
      continue
    }
    if (char === "'" || char === '"') {
      quote = char
      current += char
      continue
    }
    if (char === '(' || char === ')') {
      cut(false)
      continue
    }
    if (char === ';' || char === '\n') {
      cut(false)
      continue
    }
    if (char === '&') {
      if (next === '&') i++
      cut(false)
      continue
    }
    if (char === '|') {
      if (next === '|') {
        i++
        cut(false)
        continue
      }
      // `|&` pipes stderr too: still a pipe.
      if (next === '&') i++
      cut(true)
      continue
    }
    current += char
  }
  cut(false)
  return out
}

/** A command's words, quotes taken off. */
function wordsOf(text: string): string[] {
  const words: string[] = []
  let current = ''
  let started = false
  let quote: string | undefined
  for (let i = 0; i < text.length; i++) {
    const char = text[i]!
    if (quote !== undefined) {
      if (char === quote) quote = undefined
      else current += char
    } else if (char === "'" || char === '"') {
      quote = char
      started = true
    } else if (char === '\\' && i + 1 < text.length) {
      current += text[++i]
      started = true
    } else if (/\s/.test(char)) {
      if (started) words.push(current)
      current = ''
      started = false
    } else {
      current += char
      started = true
    }
  }
  if (started) words.push(current)
  return words
}

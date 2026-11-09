import { existsSync } from 'node:fs'
import { open } from 'node:fs/promises'
import { win32 } from 'node:path'
import { ToolTask, defineExtension, hook, section } from '@earendil-works/pi-durable'
import type {
  Extension,
  ToolExecutionResult,
  ToolHookCall,
  ToolRegistration,
} from '@earendil-works/pi-durable'
import { bool, num, obj, opt, str } from '@earendil-works/pi-schema'
import type { JsonValue } from '@earendil-works/chord'
import {
  createBashTool,
  createEditTool,
  createPowerShellTool,
  createReadTool,
  createWriteTool,
} from '@earendil-works/pi-durable/tools'
import type { ImageProcessor } from '@earendil-works/pi-durable/tools'
import { searchTools } from './search.ts'

/**
 * Droi's coding tools, chosen for the platform the kernel runs on.
 *
 * Pi's `NodeExecutionEnv` has no PowerShell branch on Windows: a string command looks for bash, and
 * its search ends at `C:\Windows\System32\bash.exe`, which is WSL. That "works but is wrong" — the
 * working directory becomes `/mnt/c/...`, HOME becomes `/home/...`, git and ssh keys change, and each
 * call takes 4–7 s instead of 58 ms. So on Windows the kernel offers `powershell`, and offers `bash`
 * only when Git Bash is really installed, with its path given explicitly.
 */
export type Platform = 'darwin' | 'linux' | 'win32'

export type CodingTools = {
  extension: Extension
  /** The shell the environment must use for `bash`, or undefined when there is no bash tool. */
  shellPath?: string
  /** The tools offered, by name, for `harness.info`. */
  names: string[]
}

/** Git Bash, in the places a Windows install puts it. WSL's `bash.exe` is deliberately not here. */
export function gitBashPaths(env: NodeJS.ProcessEnv = process.env): string[] {
  const candidates: string[] = []
  const roots = [
    env['ProgramFiles'],
    env['ProgramFiles(x86)'],
    env['ProgramW6432'],
    env['LOCALAPPDATA'] === undefined ? undefined : win32.join(env['LOCALAPPDATA'], 'Programs'),
  ]
  for (const root of roots) {
    if (root === undefined || root === '') continue
    candidates.push(win32.join(root, 'Git', 'bin', 'bash.exe'))
  }
  return candidates
}

/**
 * The first Git Bash that exists, or undefined. Only an explicit install counts: falling back to a
 * `bash` on PATH can land on WSL, which is the failure this function exists to prevent.
 */
export function findGitBash(
  platform: Platform = process.platform as Platform,
  env: NodeJS.ProcessEnv = process.env,
  exists: (path: string) => boolean = existsSync,
): string | undefined {
  if (platform !== 'win32') return undefined
  for (const candidate of gitBashPaths(env)) {
    if (exists(candidate)) return candidate
  }
  return undefined
}

export type CodingOptions = {
  platform?: Platform
  env?: NodeJS.ProcessEnv
  exists?: (path: string) => boolean
  /** Prepares images `read` returns. Without one, only images within Pi's default size pass. */
  images?: ImageProcessor
  /** Ends the extension's name, so a test can install several. */
  name?: string
}

export type ShellChoice = { kind: 'bash' | 'powershell'; shellPath?: string }

/** Which tools this platform offers, and the shell the environment needs for them. */
export function shellChoice(options: CodingOptions = {}): ShellChoice {
  const platform = options.platform ?? (process.platform as Platform)
  if (platform !== 'win32') return { kind: 'bash' }
  const bash = findGitBash(platform, options.env, options.exists)
  return bash === undefined ? { kind: 'powershell' } : { kind: 'bash', shellPath: bash }
}

/** The rule about reaching the filesystem, in the words of the tool that actually exists here. */
function filesystemRule(platform: Platform): string {
  if (platform === 'win32') {
    return [
      'Use `read`, `write`, and `edit` for files. Use `powershell` to run commands; it starts PowerShell',
      'directly, so paths are Windows paths (`C:\\...`) and there is no POSIX shell in between.',
    ].join(' ')
  }
  return [
    'Use `read`, `write`, and `edit` for files. Use `bash` to run commands; each call starts a fresh',
    'shell in the working directory, so `cd` does not carry over: give paths from there, or absolute ones,',
    'rather than chaining `cd … &&`.',
  ].join(' ')
}

/** The most of a shell command's output a script receives: pi-coding-agent's limit for codemode scripts. */
export const SCRIPT_OUTPUT_BYTES = 1024 * 1024

/**
 * What a script that calls `bash` or `powershell` receives. Pi's own schema says `output` is the tail the model
 * sees (2000 lines or 50 KB), which is what made a script filter in the shell: whatever came before the tail was gone
 * by the time it reached JavaScript. A script gets up to {@link SCRIPT_OUTPUT_BYTES} instead ({@link scriptOutput}).
 */
const shellOutputForScripts = obj(
  {
    output: str({
      description:
        'Combined stdout and stderr, up to 1 MiB: longer output keeps its first and last 512 KiB around an omission marker',
    }),
    truncated: bool({ description: 'Whether `output` omits part of the command output' }),
    fullOutputPath: opt(
      str({ description: 'File with the complete output, when it was longer than the model sees' }),
    ),
    exitCode: num(),
  },
  { open: true },
)

/**
 * What a script is told of a shell call, in place of Pi's "Output is truncated to last 2000 lines or 50KB", which is
 * what the model sees of a call it makes itself, not what a script gets: told that, a model filters in the shell
 * before the tail cuts it, and a pipe into `head` or `grep` answers the pipe's exit code, not the command's. In
 * Droid's words for its Script tool.
 */
export const SHELL_IN_SCRIPTS =
  "A call the model makes itself sees the last 2000 lines or 50KB of the output. A script's call resolves for any exit code with `exitCode` and `output`, up to 1 MiB: filter, slice or count `output` in JavaScript, keeping the exit code. When output may be larger, redirect it to a file in the same command (`cmd > <file> 2>&1`) and read or grep that file in the same script; do not pipe it into `head`, `tail` or `grep`, which hides the command's exit code. Run one command per call: independent commands are separate calls in one `Promise.allSettled`, never joined with `&&` or `;`, so each answers its own exit code."

/** A shell tool whose structured output is described as {@link scriptOutput} makes it, and says so. */
function forScripts(tool: ToolRegistration): ToolRegistration {
  const description = tool.description.replace(
    /Output is truncated to last [^.]*\.(?: If truncated, full output is saved to a temp file\.)?/,
    SHELL_IN_SCRIPTS,
  )
  return { ...tool, description, structuredOutputSchema: shellOutputForScripts }
}

/**
 * A script's `bash` or `powershell` call answers the whole output, up to {@link SCRIPT_OUTPUT_BYTES}, as
 * pi-coding-agent's do (1ff5b6fdd): Pi keeps the tail the model sees, and spills the complete output to a file once
 * it is longer, so a spilled call's output is read back from that file. A call the model made itself is left as it
 * is: the model reads its `output`, and only a script reads the structured value.
 */
async function scriptOutput(
  call: ToolHookCall,
  result: ToolExecutionResult,
): Promise<ToolExecutionResult | undefined> {
  if (call.parent === undefined || (call.name !== 'bash' && call.name !== 'powershell'))
    return undefined
  const value = result.structuredOutput as { [key: string]: JsonValue } | undefined
  if (value === undefined || value === null || typeof value['fullOutputPath'] !== 'string')
    return undefined
  const full = await headAndTail(value['fullOutputPath'], SCRIPT_OUTPUT_BYTES).catch(
    () => undefined,
  )
  if (full === undefined) return undefined
  return { ...result, structuredOutput: { ...value, output: full.text, truncated: full.truncated } }
}

/** A file as text, or its first and last `max / 2` bytes around a marker when it is longer. */
export async function headAndTail(
  path: string,
  max: number,
): Promise<{ text: string; truncated: boolean }> {
  const file = await open(path, 'r')
  try {
    const size = (await file.stat()).size
    if (size <= max) return { text: (await file.readFile()).toString('utf8'), truncated: false }
    const headBytes = Math.floor(max / 2)
    const tailBytes = max - headBytes
    const head = Buffer.alloc(headBytes)
    const tail = Buffer.alloc(tailBytes)
    await file.read(head, 0, headBytes, 0)
    await file.read(tail, 0, tailBytes, size - tailBytes)
    // Cut at characters: the head drops an incomplete last one, the tail skips continuation bytes.
    const headText = new TextDecoder().decode(head, { stream: true })
    let start = 0
    while (start < tail.length && (tail[start]! & 0xc0) === 0x80) start++
    const omitted = size - headBytes - tailBytes
    const tailText = tail.subarray(start).toString('utf8')
    return {
      text: `${headText}\n\n[... ${omitted} bytes omitted ...]\n\n${tailText}`,
      truncated: true,
    }
  } finally {
    await file.close()
  }
}

export function codingTools(options: CodingOptions = {}): CodingTools {
  const platform = options.platform ?? (process.platform as Platform)
  const choice = shellChoice(options)
  const tools: ToolRegistration[] = [
    createReadTool(
      options.images === undefined ? {} : { images: options.images },
    ) as ToolRegistration,
    createWriteTool() as ToolRegistration,
    createEditTool() as ToolRegistration,
  ]
  if (choice.kind === 'bash' && choice.shellPath !== undefined) {
    tools.push(forScripts(createBashTool() as ToolRegistration))
    tools.push(forScripts(createPowerShellTool() as ToolRegistration))
  } else if (choice.kind === 'bash') {
    tools.push(forScripts(createBashTool() as ToolRegistration))
  } else {
    // No Git Bash: only PowerShell. Offering `bash` here would run WSL.
    tools.push(forScripts(createPowerShellTool() as ToolRegistration))
  }
  tools.push(...searchTools())
  const extension = defineExtension({
    name: options.name ?? 'coding',
    tools,
    hooks: [hook(ToolTask, { afterTool: scriptOutput })],
    sections: [
      section('environment', () =>
        [
          `You are running as the Kernel of Droi, on ${platform === 'win32' ? 'Windows' : platform === 'darwin' ? 'macOS' : 'Linux'}.`,
          filesystemRule(platform),
          'Prefer the smallest change that solves the problem.',
        ].join('\n'),
      ),
      section('cwd', ({ agent }) => (agent.cwd === undefined ? undefined : `cwd: ${agent.cwd}`)),
    ],
  })
  return {
    extension,
    shellPath: choice.shellPath,
    names: tools.map((tool) => tool.name),
  }
}

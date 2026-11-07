import { existsSync } from 'node:fs'
import { win32 } from 'node:path'
import { defineExtension, section } from '@earendil-works/pi-durable'
import type { Extension, ToolRegistration } from '@earendil-works/pi-durable'
import {
  createBashTool,
  createEditTool,
  createPowerShellTool,
  createReadTool,
  createWriteTool,
} from '@earendil-works/pi-durable/tools'
import type { ImageProcessor } from '@earendil-works/pi-durable/tools'

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
    'shell in the working directory, so `cd` does not carry over unless the command chains it.',
  ].join(' ')
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
    tools.push(createBashTool() as ToolRegistration)
    tools.push(createPowerShellTool() as ToolRegistration)
  } else if (choice.kind === 'bash') {
    tools.push(createBashTool() as ToolRegistration)
  } else {
    // No Git Bash: only PowerShell. Offering `bash` here would run WSL.
    tools.push(createPowerShellTool() as ToolRegistration)
  }
  const extension = defineExtension({
    name: options.name ?? 'coding',
    tools,
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

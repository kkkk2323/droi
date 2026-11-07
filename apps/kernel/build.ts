import { $ } from 'bun'
import { cpSync, mkdirSync, readdirSync, rmSync, statSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Build the kernel for every platform Droi ships, into `apps/native/assets/kernel/<platform>/`.
 *
 * The targets are written out, never inferred: `release.yml` builds the Windows and Linux packages on
 * Ubuntu, so "build where you are" would produce a Linux binary for a Windows installer. `--target` is
 * what makes a cross build possible at all, and Bun does not cross-compile on its own.
 *
 * `--compile` embeds the source and the vendored Photon wasm beside the executable (`import … with {
 * type: 'file' }` gives its path at runtime). A cross build is where that bites: `createNodePhotonImages()`
 * cannot resolve its package inside a single-file binary, so images fail only in a cross product. The
 * image smoke in `test/images.test.ts` is what catches that, and `smoke.ts` runs the same check against a
 * built binary.
 */
const targets = [
  { target: 'bun-darwin-arm64', platform: 'darwin-arm64' },
  { target: 'bun-darwin-x64', platform: 'darwin-x64' },
  { target: 'bun-windows-x64', platform: 'windows-x64', executable: 'kernel.exe' },
  { target: 'bun-linux-x64', platform: 'linux-x64' },
  { target: 'bun-linux-arm64', platform: 'linux-arm64' },
] as const

/**
 * Four targets, as the design fixes them: the macOS universal build takes arm64, Windows x64, and the
 * two Linux architectures the headless `droi host` needs.
 */
export const releaseTargets = targets

const root = new URL('.', import.meta.url).pathname
const entry = join(root, 'src/main.ts')
const outRoot = join(root, '..', '..', 'apps', 'native', 'assets', 'kernel')

export type BuildOptions = {
  /** Which targets: all of `releaseTargets` unless narrowed. */
  targets?: readonly (typeof targets)[number][]
  /** Where the executables go. */
  out?: string
  minify?: boolean
}

export async function build(
  options: BuildOptions = {},
): Promise<{ platform: string; path: string; bytes: number }[]> {
  const selected = options.targets ?? targets
  const out = options.out ?? outRoot
  const built: { platform: string; path: string; bytes: number }[] = []
  for (const entryTarget of selected) {
    const dir = join(out, entryTarget.platform)
    mkdirSync(dir, { recursive: true })
    const executable = join(dir, 'executable' in entryTarget ? entryTarget.executable : 'kernel')
    await $`bun build --compile --target=${entryTarget.target} ${options.minify === false ? '' : '--minify'} --outfile ${executable} ${entry}`.quiet()
    // The wasm lands beside the executable; keep it in the same directory the binary looks in.
    for (const file of readdirSync(dir)) {
      if (file.endsWith('.wasm')) continue
    }
    built.push({
      platform: entryTarget.platform,
      path: executable,
      bytes: statSync(executable).size,
    })
  }
  return built
}

if (import.meta.main) {
  const only = process.argv.slice(2)
  const selected =
    only.length === 0
      ? targets
      : targets.filter(
          (entryTarget) => only.includes(entryTarget.platform) || only.includes(entryTarget.target),
        )
  if (selected.length === 0) {
    process.stderr.write(
      `unknown target: ${only.join(', ')}\nknown: ${targets.map((t) => t.platform).join(', ')}\n`,
    )
    process.exit(2)
  }
  rmSync(join(outRoot, '.tmp'), { recursive: true, force: true })
  const built = await build({ targets: selected })
  for (const artifact of built) {
    process.stdout.write(
      `${artifact.platform}  ${(artifact.bytes / 1_000_000).toFixed(1)} MB  ${artifact.path}\n`,
    )
  }
}

export { cpSync }

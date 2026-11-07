// Runs the production lock against real files, pausing at the one instant R4-2 is about: after the
// stale guard has been judged, and before it is unlinked.
import * as fs from 'node:fs'
import { join } from 'node:path'

const [root, role] = process.argv.slice(2) as [string, string]
const path = join(root, 'kernel.lock')
const source = fs
  .readFileSync(new URL('../src/lock.ts', import.meta.url), 'utf8')
  .replace(/^import .*$/gm, '')
  .replace(/\bexport /g, '')
const code = new Bun.Transpiler({ loader: 'ts' }).transformSync(source)

function pause(label: string): void {
  fs.writeFileSync(join(root, `${label}.paused`), '')
  const deadline = Date.now() + 5000
  const sleeper = new Int32Array(new SharedArrayBuffer(4))
  while (!fs.existsSync(join(root, `${label}.go`))) {
    if (Date.now() > deadline) throw new Error(`${label} schedule timed out`)
    Atomics.wait(sleeper, 0, 0, 5)
  }
}

let paused = false
const take = new Function(
  'readFileSync',
  'renameSync',
  'rmSync',
  'statSync',
  'writeFileSync',
  'process',
  code + '; return takeLock',
)(
  fs.readFileSync,
  fs.renameSync,
  (target: string, options: unknown) => {
    // The first guard unlink is this starter's `clearGuard`. Hold there, and let the parent install a
    // replacement guard — exactly the interleaving an unreviewed second starter would produce.
    if (role === 'A' && target === `${path}.guard` && !paused) {
      paused = true
      pause('A-before-guard-unlink')
    }
    return fs.rmSync(target, options as never)
  },
  fs.statSync,
  fs.writeFileSync,
  process,
)

try {
  take(path)
  fs.writeFileSync(join(root, `${role}.won`), String(process.pid))
  console.log(JSON.stringify({ role, won: true, pid: process.pid }))
} catch (cause) {
  fs.writeFileSync(
    join(root, `${role}.refused`),
    String(cause instanceof Error ? cause.message : cause),
  )
  console.log(JSON.stringify({ role, won: false, pid: process.pid }))
}

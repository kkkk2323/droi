// Builds what every Fake Kernel needs once, before the suite: the Gateway (apps/native/internal/phonegateway),
// with the Go the repository pins. The Kernel itself is DROI_KERNEL_BIN or its source under Bun
// (fake-kernel.ts).
import { execFileSync } from 'node:child_process'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { GATEWAY_BIN } from './fake-kernel/fake-kernel'

export default function globalSetup(): void {
  const repo = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
  execFileSync('go', ['build', '-o', GATEWAY_BIN, './apps/native/internal/phonegateway'], {
    cwd: repo,
    stdio: 'inherit',
  })
}

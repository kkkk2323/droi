// The Desktop Shell's real Memory controller, run in the test process against
// the Fake Daemon and handed to the stand-in Shell in the page, so the Memory
// Sessions the Client starts are the Shell's own.
import type { Page } from '@playwright/test'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { FakeDaemon } from '../fake-daemon/fake-daemon'
import {
  createMemoryController,
  type MemoryController,
} from '../../apps/desktop/src/main/memory/memory-controller'
import { createMemorySessionRunner } from '../../apps/desktop/src/main/memory/memory-session'
import {
  openMemoryStore,
  type Category,
  type MemorySlot,
} from '../../apps/desktop/src/memory/store'

const PROMPTS = fileURLToPath(new URL('../../apps/desktop/resources/memory', import.meta.url))

export interface ShellMemory {
  controller: MemoryController
  memoryDir: string
  dispose(): void
}

export async function exposeShellMemory(
  page: Page,
  daemon: FakeDaemon,
  seed: Array<{ slot: MemorySlot; category: Category; text: string }>,
): Promise<ShellMemory> {
  const memoryDir = mkdtempSync(join(tmpdir(), 'droi-e2e-memory-'))
  const store = openMemoryStore(memoryDir)
  for (const { slot, category, text } of seed) store.add(slot, category, text)
  store.close()
  const controller = createMemoryController({
    memoryDir,
    defaultsDir: PROMPTS,
    runner: () => createMemorySessionRunner({ url: daemon.url, token: daemon.token }),
    modelId: () => 'glm-5.3-flash',
    onChange: () => {},
  })
  await page.exposeFunction('droiShellMemory', (method: string, arg: unknown) =>
    method === 'overview'
      ? controller.overview()
      : controller.consolidate(typeof arg === 'string' ? arg : null),
  )
  return {
    controller,
    memoryDir,
    dispose() {
      controller.stop()
      rmSync(memoryDir, { recursive: true, force: true })
    },
  }
}

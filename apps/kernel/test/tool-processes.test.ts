import { expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'
import { context } from '../src/session/service.ts'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'

/** Wait until a pid is gone, or answer false with what is still there. */
async function gone(pid: number, within = 5000): Promise<boolean> {
  const deadline = Date.now() + within
  for (;;) {
    try {
      process.kill(pid, 0)
    } catch {
      return true
    }
    if (Date.now() > deadline) return false
    await Bun.sleep(25)
  }
}

test.skipIf(process.platform === 'win32')(
  'a background tool process does not outlive the kernel that started it',
  async () => {
    // Pi runs a tool shell `detached`, so a background process it leaves is in the shell's own group and
    // outside the kernel's. A kernel that closes without ending those groups leaves a `sleep 100` behind,
    // reparented to PID 1 — a leak a user sees as a fan that never stops.
    for (const command of [
      'sleep 100 & echo $! > PLACE',
      'nohup sleep 100 > /dev/null 2>&1 & echo $! > PLACE',
    ]) {
      const data = tempData()
      const faux = fauxCatalog()
      const kernel = await start({ dir: data.dir, faux })
      const client = new Client(kernel.port)
      try {
        const pidFile = join(data.dir, 'tool.pid')
        const { conversationId: id } = await client.call('conversation.create', {
          agent: { model: faux.model },
        })
        faux.faux.setResponses([
          fauxAssistantMessage(
            fauxToolCall('bash', { command: command.replace('PLACE', pidFile) }),
            {
              stopReason: 'toolUse',
            },
          ),
          fauxAssistantMessage('started it'),
        ])
        await client.call('conversation.submit', { id, input: 'leave something running' })
        await (await kernel.harness.conversation(id as never, context))!.waitForIdle(context)

        let pid = 0
        for (const deadline = Date.now() + 10_000; pid === 0;) {
          try {
            pid = Number(readFileSync(pidFile, 'utf8').trim())
          } catch {
            // Not written yet.
          }
          if (pid !== 0) break
          if (Date.now() > deadline) throw new Error(`the tool never wrote its pid: ${command}`)
          await Bun.sleep(25)
        }
        expect(pid).toBeGreaterThan(0)

        await kernel.stop()
        expect(await gone(pid), `${command} left pid ${pid} running`).toBe(true)
      } finally {
        client.close()
        await kernel.stop()
        data.cleanup()
      }
    }
  },
  120_000,
)

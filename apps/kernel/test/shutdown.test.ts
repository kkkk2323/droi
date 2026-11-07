import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { spawn } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import { fakeKernelConfig, startFakeProvider } from './fake-provider.ts'
import { loadConfig, parseArgs, providersFromEnv } from '../src/config.ts'

let data: ReturnType<typeof tempData>
let faux: Faux

beforeEach(() => {
  data = tempData()
  faux = fauxCatalog()
})

afterEach(() => {
  data.cleanup()
})

describe('the provider override', () => {
  test('DROI_KERNEL_PROVIDERS replaces the providers and is validated', () => {
    const providers = providersFromEnv({
      DROI_KERNEL_PROVIDERS: JSON.stringify([
        {
          id: 'fake',
          baseUrl: 'http://127.0.0.1:1/v1',
          api: 'openai-completions',
          models: [{ id: 'fake-1', contextWindow: 1000, maxTokens: 100 }],
        },
      ]),
    })
    expect(providers?.length).toBe(1)
    expect(providers?.[0]?.id).toBe('fake')
    expect(providersFromEnv({})).toBeUndefined()
    expect(() => providersFromEnv({ DROI_KERNEL_PROVIDERS: '[]' })).toThrow('non-empty')
    expect(() => providersFromEnv({ DROI_KERNEL_PROVIDERS: '[{"id":"x"}]' })).toThrow(
      'needs an api',
    )
    expect(() => providersFromEnv({ DROI_KERNEL_PROVIDERS: 'not json' })).toThrow()
  })

  test('the override changes providers and nothing else: the token is still required', async () => {
    const provider = startFakeProvider()
    const dir = tempData()
    const configPath = join(data.dir, 'config.json')
    // The file may carry a provider list and progress settings; the run's own flags stay the flags.
    writeFileSync(configPath, JSON.stringify({ port: 1234, readOnly: true }))
    const env = {
      DROI_KERNEL_PROVIDERS: JSON.stringify(fakeKernelConfig(provider.baseUrl)['providers']),
      DROI_KERNEL_TOKEN: 'from-env',
    }
    const previous = process.env['DROI_KERNEL_PROVIDERS']
    process.env['DROI_KERNEL_PROVIDERS'] = env.DROI_KERNEL_PROVIDERS
    try {
      const loaded = loadConfig(
        parseArgs(['--data', dir.dir, '--port', '0', '--config', configPath, '--read-only']),
        env as never,
      )
      expect(loaded.port).toBe(0)
      // The command line decides these, not the file: an override of providers may not flip them.
      expect(loaded.readOnly).toBe(true)
      const withoutFlag = loadConfig(
        parseArgs(['--data', dir.dir, '--config', configPath]),
        env as never,
      )
      expect(withoutFlag.readOnly).toBe(false)
      expect(loaded.dataDir).toBe(dir.dir)
      expect(loaded.providers[0]?.id).toBe('fake')

      // A kernel started this way answers nothing without the token it was given.
      const kernel = await start({ dir: dir.dir, faux })
      const wrong = new Client(kernel.port, 'not-the-token')
      await expect(wrong.opened).rejects.toThrow()
      wrong.close()
      await kernel.stop()
    } finally {
      if (previous === undefined) delete process.env['DROI_KERNEL_PROVIDERS']
      else process.env['DROI_KERNEL_PROVIDERS'] = previous
      provider.stop()
      dir.cleanup()
    }
  })
})

describe('kernel.shutdown', () => {
  test('answers first, then closes the harness and the storage and exits 0', async () => {
    // A real process, because the point is the exit: the built kernel cannot be told to use a faux
    // model, so the binary under test is this test runner in kernel mode.
    const dir = tempData()
    const provider = startFakeProvider()
    const configPath = join(dir.dir, 'config.json')
    writeFileSync(configPath, JSON.stringify(fakeKernelConfig(provider.baseUrl), null, 2))
    const child = spawn(
      process.execPath,
      [
        new URL('../src/main.ts', import.meta.url).pathname,
        '--data',
        join(dir.dir, 'data'),
        '--port',
        '0',
        '--config',
        configPath,
      ],
      {
        env: {
          ...process.env,
          DROI_KERNEL_TOKEN: 'shutdown-test',
          FAKE_PROVIDER_KEY: 'fake-key',
        },
      },
    )
    let log = ''
    child.stderr.on('data', (chunk: Buffer) => {
      log += chunk.toString()
    })
    try {
      const handshake = await new Promise<{ port: number }>((resolve, reject) => {
        let buffer = ''
        const timer = setTimeout(() => reject(new Error(`no handshake: ${log}`)), 30_000)
        child.stdout.on('data', (chunk: Buffer) => {
          buffer += chunk.toString()
          const line = buffer.split('\n').find((candidate) => candidate.startsWith('{'))
          if (line === undefined) return
          clearTimeout(timer)
          resolve(JSON.parse(line) as { port: number })
        })
      })
      const client = new Client(handshake.port, 'shutdown-test')
      const answer = await client.call('kernel.shutdown')
      expect(answer['status']).toBe('closing')
      const code = await new Promise<number | null>((resolve) =>
        child.on('exit', (value) => resolve(value)),
      )
      expect(code).toBe(0)
      // The lock is gone and the database closed cleanly, so a fresh kernel opens the same directory.
      const again = await start({ dir: join(dir.dir, 'data'), faux })
      expect(again.port).toBeGreaterThan(0)
      await again.stop()
      client.close()
    } finally {
      child.kill('SIGKILL')
      provider.stop()
      dir.cleanup()
    }
  }, 60_000)

  test('a read-only kernel answers reads and refuses writes with -32008', async () => {
    const kernel = await start({ dir: data.dir, faux, overrides: { readOnly: true } })
    const client = new Client(kernel.port)
    expect((await client.call('harness.info'))['version']).toBe('kernel-1.5.0-1')
    const error = await client.fails('conversation.create', {})
    expect(error.code).toBe(-32008)
    client.close()
    await kernel.stop()
  })
})

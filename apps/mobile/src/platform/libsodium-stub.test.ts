import { describe, expect, it } from 'vitest'
import sodium from './libsodium-stub'

type Resolver = (context: object, moduleName: string, platform: string) => { filePath?: string }

describe('libsodium in the Phone App', () => {
  it('Metro gives the SDK the stand-in, which loads under Hermes', async () => {
    const metro = (await import('../../metro.config.js')) as unknown as {
      default: { resolver: { resolveRequest: Resolver } }
    }
    for (const platform of ['ios', 'web']) {
      expect(
        metro.default.resolver.resolveRequest({}, 'libsodium-wrappers-sumo', platform).filePath,
      ).toMatch(/src\/platform\/libsodium-stub\.ts$/)
    }
  })

  it('fails only when the relay waits on it', async () => {
    await expect(sodium.ready).rejects.toThrow('libsodium is not in the Phone App')
  })
})

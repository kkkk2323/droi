import { describe, expect, it } from 'vitest'
import { daemonArgs } from './daemon-args'

describe('daemonArgs', () => {
  it('passes the Runtime Overlay only when Memory is on', () => {
    expect(
      daemonArgs({ port: 4242, liveness: ['--liveness-fd', '3'], runtimeOverlay: null }),
    ).toEqual(['daemon', '--host', '127.0.0.1', '--port', '4242', '--liveness-fd', '3'])
    expect(
      daemonArgs({ port: 4242, liveness: [], runtimeOverlay: '/data/runtime-overlay.json' }),
    ).toEqual([
      'daemon',
      '--host',
      '127.0.0.1',
      '--port',
      '4242',
      '--settings',
      '/data/runtime-overlay.json',
    ])
  })
})

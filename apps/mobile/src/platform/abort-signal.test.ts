import {
  DaemonSessionController,
  LOCAL_MACHINE_ID,
  MachineType,
  MultiSessionStateManager,
} from '@factory/droid-sdk'
import * as polyfill from 'abort-controller'
import { afterEach, describe, expect, it } from 'vitest'
import { installAbortSignalFixes } from './abort-signal'

// What React Native installs as the globals (Libraries/Core/setUpXHR.js); the
// devDependency is pinned to the version react-native depends on.
const ReactNativeAbortController = polyfill.AbortController as unknown as typeof AbortController
const ReactNativeAbortSignal = polyfill.AbortSignal as unknown as typeof AbortSignal
installAbortSignalFixes(ReactNativeAbortController, ReactNativeAbortSignal)

const globals = { AbortController, AbortSignal }

afterEach(() => {
  Object.assign(globalThis, globals)
})

describe("React Native's AbortController", () => {
  it('throws the reason it was aborted with', () => {
    const controller = new ReactNativeAbortController()
    expect(() => controller.signal.throwIfAborted()).not.toThrow()
    let seen: unknown
    controller.signal.addEventListener('abort', () => {
      seen = controller.signal.reason
    })
    const reason = new Error('gone')
    controller.abort(reason)
    expect(seen).toBe(reason)
    expect(() => controller.signal.throwIfAborted()).toThrow(reason)
  })

  it('throws an AbortError when aborted without a reason', () => {
    const controller = new ReactNativeAbortController()
    controller.abort()
    expect(() => controller.signal.throwIfAborted()).toThrow(
      expect.objectContaining({ name: 'AbortError' }),
    )
  })

  it('lets the SDK try to connect, as the Phone App on the iPhone does', async () => {
    Object.assign(globalThis, {
      AbortController: ReactNativeAbortController,
      AbortSignal: ReactNativeAbortSignal,
    })
    const controller = new DaemonSessionController({
      sessionStateManager: new MultiSessionStateManager(),
      config: {
        machineId: LOCAL_MACHINE_ID,
        machineType: MachineType.Local,
        // Nothing listens here: the attempt must fail on the network, not before it.
        url: 'ws://127.0.0.1:1/daemon',
        clientType: 'web',
        getCredential: async () => ({ apiKey: 'test' }),
        connectionTimeoutMs: 1_000,
        requestTimeout: 1_000,
        maxPollAttempts: 1,
        maxReconnectAttempts: 0,
        reconnectInterval: 1_000,
        maxReconnectDelay: 1_000,
        reconnectBackoffFactor: 1,
      },
    })
    const failure = await controller.attemptInitialConnection().then(
      () => null,
      (error: unknown) => error as Error & { cause?: Error },
    )
    controller.disconnect()
    expect(failure).not.toBeNull()
    expect(String(failure?.cause?.message)).not.toMatch(/is not a function/)
  })
})

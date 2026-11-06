import { EventEmitter } from 'node:events'
import { describe, expect, test } from 'vitest'
import type { DaemonConnection } from './connection'
import { LOAD_STATE } from './sdk-enums'
import { createActivityStore } from './use-session-activity'

interface Opened {
  sessionId: string
  workingState: string
  pendingAskUserRequestCount?: number
}

/**
 * A connection whose controller is an EventEmitter: `opened` scripts what each
 * list_opened_sessions answers, `held` the ask-user Prompts the SDK holds, and
 * `loaded` the Sessions this Client has loaded, with their working state.
 */
function standIn(opened: () => Opened[] | Promise<Opened[]>) {
  const held: { sessionId: string }[] = []
  const loaded = new Map<string, string>()
  let reads = 0
  const controller = Object.assign(new EventEmitter(), {
    listOpenedSessions: async () => {
      reads += 1
      return opened()
    },
    getPendingAskUserRequests: () => held,
  })
  const connection = {
    controller,
    sessionState: {
      subscribeToSessionEvents: () => () => {},
      getAllSessionIds: () => [...loaded.keys()],
      getSessionManager: (id: string) =>
        loaded.has(id) ? { getDroidWorkingState: () => loaded.get(id) } : undefined,
      getSessionLoadState: (id: string) => (loaded.has(id) ? LOAD_STATE.loaded : undefined),
    },
    getState: () => ({ status: 'connected' }),
  } as unknown as DaemonConnection
  const store = createActivityStore(connection)
  store.subscribe(() => {})
  const working = (sessionId: string, newState: string) =>
    controller.emit('droidWorkingStateChanged', { sessionId, newState })
  return { store, controller, working, held, loaded, reads: () => reads }
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}

describe('a Queued Question the Daemon listed at connect', () => {
  test('stops counting once another Client answers it and the agent has run', async () => {
    let held = 1
    const { store, working } = standIn(() => [
      { sessionId: 'agent', workingState: 'idle', pendingAskUserRequestCount: held },
    ])
    await settle()
    expect(store.getSnapshot().get('agent')).toBe('needs-input')

    held = 0
    working('agent', 'streaming_assistant_message')
    expect(store.getSnapshot().get('agent')).toBe('working')
    working('agent', 'idle')
    await settle()
    expect(store.getSnapshot().has('agent')).toBe(false)
  })

  test('is found when the agent asks it during a turn and then rests', async () => {
    let held = 0
    const { store, working } = standIn(() => [
      {
        sessionId: 'agent',
        workingState: 'streaming_assistant_message',
        pendingAskUserRequestCount: held,
      },
    ])
    await settle()
    working('agent', 'streaming_assistant_message')
    held = 1
    working('agent', 'idle')
    await settle()
    expect(store.getSnapshot().get('agent')).toBe('needs-input')
  })
})

describe('a Session whose Prompts this Client follows', () => {
  test('a loaded one counts the Prompts the SDK holds, not the listed count', async () => {
    const s = standIn(() => [
      { sessionId: 'agent', workingState: 'idle', pendingAskUserRequestCount: 1 },
    ])
    s.loaded.set('agent', 'idle')
    await settle()
    expect(s.store.getSnapshot().has('agent')).toBe(false)

    s.held.push({ sessionId: 'agent' })
    s.controller.emit('askUserRequested', { sessionId: 'agent' })
    expect(s.store.getSnapshot().get('agent')).toBe('needs-input')
  })

  test('one asked here counts while the SDK holds it, and not after it is resolved', async () => {
    const s = standIn(() => [
      { sessionId: 'agent', workingState: 'idle', pendingAskUserRequestCount: 1 },
    ])
    await settle()
    s.held.push({ sessionId: 'agent' })
    s.controller.emit('askUserRequested', { sessionId: 'agent' })
    expect(s.store.getSnapshot().get('agent')).toBe('needs-input')

    s.held.length = 0
    s.controller.emit('askUserResolved', 'request-1')
    // The listed count no longer speaks for it.
    expect(s.store.getSnapshot().has('agent')).toBe(false)
    const reads = s.reads()
    s.controller.emit('droidWorkingStateChanged', { sessionId: 'agent', newState: 'idle' })
    await settle()
    expect(s.reads()).toBe(reads)
  })
})

describe('reading the counts again after a rest', () => {
  test('one read at a time; a rest during it is read once more after', async () => {
    const answers: Array<ReturnType<typeof deferred<Opened[]>>> = []
    const s = standIn(() => {
      const next = deferred<Opened[]>()
      answers.push(next)
      return next.promise
    })
    answers[0]!.resolve([{ sessionId: 'agent', workingState: 'idle' }])
    await settle()
    expect(s.reads()).toBe(1)

    s.working('agent', 'idle')
    await settle()
    expect(s.reads()).toBe(2)
    s.working('agent', 'streaming_assistant_message')
    s.working('agent', 'idle')
    s.working('agent', 'idle')
    await settle()
    expect(s.reads()).toBe(2)

    // The read in flight predates the last rest; its answer must not be the last word.
    answers[1]!.resolve([{ sessionId: 'agent', workingState: 'idle' }])
    await settle()
    expect(s.reads()).toBe(3)
    answers[2]!.resolve([
      { sessionId: 'agent', workingState: 'idle', pendingAskUserRequestCount: 1 },
    ])
    await settle()
    expect(s.reads()).toBe(3)
    expect(s.store.getSnapshot().get('agent')).toBe('needs-input')
  })
})

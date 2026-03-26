import test from 'node:test'
import assert from 'node:assert/strict'
import { runDroidAndCaptureAssistantText } from '../src/backend/droid/textCapture.ts'

test('runDroidAndCaptureAssistantText resolves on message + turn-end', async () => {
  let listener: ((ev: any) => void) | null = null
  const disposed: string[] = []

  const execManager = {
    onEvent: (cb: (ev: any) => void) => {
      listener = cb
      return () => {
        if (listener === cb) listener = null
      }
    },
    send: async () => {
      queueMicrotask(() => {
        listener?.({
          type: 'message',
          sessionId: 'temp',
          message: {
            type: 'assistant_text_delta',
            text: 'fix: keep turn-end after session rekey',
          },
        })
        listener?.({ type: 'turn-end', sessionId: 'temp', code: 0 })
      })
    },
    cancel: () => {},
    disposeSession: (sid: string) => {
      disposed.push(sid)
    },
  } as any

  const text = await runDroidAndCaptureAssistantText({
    execManager,
    send: {
      sessionId: 'temp',
      machineId: 'm1',
      prompt: 'p',
      cwd: '/tmp',
    },
    timeoutMs: 200,
  })

  assert.equal(text, 'fix: keep turn-end after session rekey')
  assert.deepEqual(disposed, ['temp'])
})

test('runDroidAndCaptureAssistantText cancels active session id on timeout', async () => {
  let listener: ((ev: any) => void) | null = null
  const cancelled: string[] = []
  const disposed: string[] = []

  const execManager = {
    onEvent: (cb: (ev: any) => void) => {
      listener = cb
      return () => {
        if (listener === cb) listener = null
      }
    },
    send: async () => {
      queueMicrotask(() => {})
    },
    cancel: (sid: string) => {
      cancelled.push(sid)
    },
    disposeSession: (sid: string) => {
      disposed.push(sid)
    },
  } as any

  await assert.rejects(
    runDroidAndCaptureAssistantText({
      execManager,
      send: {
        sessionId: 'temp',
        machineId: 'm1',
        prompt: 'p',
        cwd: '/tmp',
      },
      timeoutMs: 30,
    }),
    /Timed out generating text/
  )

  assert.deepEqual(cancelled, ['temp'])
  assert.deepEqual(disposed, ['temp'])
})

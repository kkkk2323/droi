import { expect, test } from 'bun:test'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'
import { context } from '../src/session/service.ts'

/** A scripted answer that takes a while, so a second submission arrives while the first is in flight. */
const slowAnswer =
  (text: string, ms = 1200) =>
  async () => {
    await Bun.sleep(ms)
    return fauxAssistantMessage(text)
  }

type Submission = {
  submissionId: number
  status: string
  entry?: number
  answer?: number
  reason?: string
}

test('submission.get names the entries one submission produced, not a task that answered two', async () => {
  // The correlation a client cannot make for itself: an entry carries the *task* that wrote it, and one task
  // can answer a queued input together with the one after it. "The newest assistant entry" is therefore not
  // "this submission's reply", and a wait built on it accepts a reply that was already in flight.
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create', {
      agent: { model: faux.model },
    })
    faux.faux.setResponses([
      slowAnswer('the first reply'),
      fauxAssistantMessage('the second reply'),
    ])

    const first = await client.call('conversation.submit', { id, input: 'first question' })
    const second = await client.call('conversation.submit', { id, input: 'second question' })

    // While the first reply is still being produced, the second submission has not been placed: it has no
    // entry of its own, so nothing it could be said to have answered.
    const early = (await client.call('submission.get', {
      id,
      submissionId: second['submissionId'],
    })) as Submission
    expect(['queued', 'placed']).toContain(early.status)
    expect(early.answer).toBeUndefined()

    await (await kernel.harness.conversation(id as never, context))!.waitForIdle(context)

    const done = (await client.call('submission.get', {
      id,
      submissionId: second['submissionId'],
    })) as Submission
    expect(done.status).toBe('done')
    // The turn's own boundaries: its user entry, and the reply that answered it.
    expect(done.entry).toBeGreaterThan(0)
    expect(done.answer).toBeGreaterThan(done.entry!)

    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 200,
    })
    const entries = page['items'] as { id: number; kind: string; model?: unknown }[]
    const answer = entries.find((entry) => entry.id === done.answer)
    expect(answer?.kind).toBe('pi.assistant')
    expect(JSON.stringify(answer?.model)).toContain('the second reply')

    // And the first submission knows its own answer, which is the reply that was in flight.
    const firstRecord = (await client.call('submission.get', {
      id,
      submissionId: first['submissionId'],
    })) as Submission
    expect(firstRecord.status).toBe('done')
    expect(firstRecord.answer).toBeGreaterThan(0)
    expect(firstRecord.answer).not.toBe(done.answer)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
}, 60_000)

test('submission.get refuses a submission that belongs to another conversation', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  try {
    const created = await client.call('conversation.create', { agent: { model: faux.model } })
    const other = await client.call('conversation.create', { agent: { model: faux.model } })
    faux.faux.setResponses([fauxAssistantMessage('an answer')])
    const submitted = await client.call('conversation.submit', {
      id: created['conversationId'],
      input: 'hello',
    })
    const refused = await client
      .call('submission.get', {
        id: other['conversationId'],
        submissionId: submitted['submissionId'],
      })
      .then(
        (value) => ({ value, code: 0 }),
        (error: { code: number }) => ({ code: error.code }),
      )
    expect(refused.code).toBe(-32003)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
}, 60_000)

test('submission.get reports no end for a turn that answered nothing', async () => {
  // A client used to read `last` as the end of a cut-short turn. Pi keeps no such value, and an end the Kernel
  // guessed adopted a later turn; so the contract has no field, and the client reads the status instead.
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create', {
      agent: { model: faux.model },
    })
    faux.faux.setResponses([
      async () => {
        await Bun.sleep(3000)
        return fauxAssistantMessage('too late')
      },
      fauxAssistantMessage('second answer'),
    ])
    const first = await client.call('conversation.submit', { id, input: 'first' })
    await Bun.sleep(400)
    await client.call('conversation.abort', { id })
    await (await kernel.harness.conversation(id as never, context))!.waitForIdle(context)

    const aborted = (await client.call('submission.get', {
      id,
      submissionId: first['submissionId'],
    })) as Submission
    expect(aborted.status).toBe('unanswered')
    expect(aborted.reason).toBe('aborted')
    expect(aborted.answer).toBeUndefined()
    // The field is gone from the wire shape, not merely empty, so a client cannot read a guess.
    expect(Object.keys(aborted).sort()).toEqual(['entry', 'reason', 'status', 'submissionId'])

    // A later turn leaves the record as it was: nothing "moves" because nothing was ever stored.
    const seen = JSON.stringify(aborted)
    const second = await client.call('conversation.submit', { id, input: 'second' })
    await (await kernel.harness.conversation(id as never, context))!.waitForIdle(context)
    const later = await client.call('submission.get', { id, submissionId: first['submissionId'] })
    expect(JSON.stringify(later)).toBe(seen)

    // A done submission keeps both ends Pi records.
    const done = (await client.call('submission.get', {
      id,
      submissionId: second['submissionId'],
    })) as Submission
    expect(done.status).toBe('done')
    expect(done.answer).toBeGreaterThan(done.entry!)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
}, 60_000)

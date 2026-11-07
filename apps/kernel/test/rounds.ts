import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import type { Client, Faux } from './helpers.ts'

export type RoundPlan = { rounds: string[]; inputs?: string[] }

export type RoundResult = {
  conversationId: number
  /** What the model was scripted to answer, in order, as it was actually stored. */
  answerTexts: string[]
  /** The user messages the conversation holds, in order. */
  inputs: string[]
}

/**
 * Create the conversations, then run each round: one prompt, wait for its answer. With `concurrent`, every
 * conversation's rounds run at the same time, which is the five-sessions-at-once case.
 *
 * Answers are scripted per conversation, so a mixed-up conversation would show another's text and fail.
 */
export async function clientRounds(
  client: Client,
  faux: Faux,
  plan: { conversations: RoundPlan[]; concurrent?: boolean },
): Promise<RoundResult[]> {
  const ids: number[] = []
  for (const [index] of plan.conversations.entries()) {
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
      kind: 'chat',
    })
    const id = created['conversationId'] as number
    ids.push(id)
    expectDefined(id, index)
  }
  faux.faux.setResponses([])

  const runOne = async (id: number, conversationPlan: RoundPlan): Promise<void> => {
    for (const [round, answer] of conversationPlan.rounds.entries()) {
      faux.faux.appendResponses([fauxAssistantMessage(answer)])
      await client.call('conversation.submit', {
        id,
        input: conversationPlan.inputs?.[round] ?? `round ${round}`,
      })
      await waitForAnswer(client, id, round)
    }
  }

  if (plan.concurrent === true) {
    await Promise.all(ids.map((id, index) => runOne(id, plan.conversations[index]!)))
  } else {
    for (const [index, id] of ids.entries()) await runOne(id, plan.conversations[index]!)
  }

  const results: RoundResult[] = []
  for (const id of ids) {
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 200,
    })
    const entries = page['items'] as {
      kind: string
      model?: { role: string; content: unknown }[]
    }[]
    const answerTexts: string[] = []
    const inputs: string[] = []
    for (const entry of entries) {
      const message = entry.model?.[0]
      if (message === undefined) continue
      const text = textOf(message.content)
      if (entry.kind === 'pi.assistant' && text !== '') answerTexts.push(text)
      if (entry.kind === 'pi.user' && text !== '') inputs.push(text)
    }
    results.push({ conversationId: id, answerTexts, inputs })
  }
  return results
}

function expectDefined(value: number | undefined, index: number): void {
  if (value === undefined) throw new Error(`conversation ${index} was not created`)
}

/** Wait until the conversation holds `count + 1` assistant answers, which is the run having answered. */
async function waitForAnswer(client: Client, id: number, count: number): Promise<void> {
  const deadline = Date.now() + 20_000
  while (Date.now() < deadline) {
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 200,
    })
    const entries = page['items'] as { kind: string }[]
    const answers = entries.filter((entry) => entry.kind === 'pi.assistant').length
    if (answers > count) return
    await Bun.sleep(15)
  }
  throw new Error(`conversation ${id} never answered round ${count}`)
}

function textOf(content: unknown): string {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content
    .map((part) =>
      typeof part === 'object' && part !== null && (part as { type?: string }).type === 'text'
        ? String((part as { text?: unknown }).text ?? '')
        : '',
    )
    .join('')
}

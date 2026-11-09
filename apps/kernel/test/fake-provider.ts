import type { Server } from 'bun'

/**
 * A local provider that speaks OpenAI chat completions, so a test — or a built kernel binary, which
 * cannot be handed a faux provider — can be driven with no network and no key.
 *
 * It answers from a script: each request takes the next step, and a step is either text or one tool
 * call. The reply is streamed as SSE chunks, which is the shape pi-ai parses.
 */
export type Step =
  | { text: string }
  | { toolCall: { name: string; arguments: Record<string, unknown>; id?: string } }

export type FakeProvider = {
  readonly baseUrl: string
  /** The steps still unplayed. */
  pending(): number
  /** The requests received, for a test that checks what was sent. */
  requests(): unknown[]
  /** Add steps to the queue. */
  push(...steps: Step[]): void
  /** Replace the queue: what a measurement wants, so a leftover step cannot shift the next turn. */
  set(steps: Step[]): void
  stop(): void
}

export function startFakeProvider(script: Step[] = []): FakeProvider {
  const steps = [...script]
  const received: unknown[] = []
  let server: Server<undefined> | undefined

  server = Bun.serve({
    port: 0,
    hostname: '127.0.0.1',
    async fetch(request) {
      const url = new URL(request.url)
      if (!url.pathname.endsWith('/chat/completions')) {
        return new Response('not found', { status: 404 })
      }
      const body = (await request.json()) as unknown
      received.push(body)
      const step = steps.shift() ?? { text: 'done' }
      const id = `chatcmpl-${received.length}`
      const chunks: string[] = []
      const send = (delta: unknown, finish: string | null = null) =>
        chunks.push(
          `data: ${JSON.stringify({
            id,
            object: 'chat.completion.chunk',
            model: 'fake-1',
            choices: [{ index: 0, delta, finish_reason: finish }],
          })}\n\n`,
        )
      send({ role: 'assistant', content: '' })
      if ('text' in step) {
        for (const piece of step.text.match(/.{1,40}/gs) ?? [step.text]) send({ content: piece })
      } else {
        send({
          tool_calls: [
            {
              index: 0,
              id: step.toolCall.id ?? `call_${received.length}`,
              type: 'function',
              function: {
                name: step.toolCall.name,
                arguments: JSON.stringify(step.toolCall.arguments),
              },
            },
          ],
        })
      }
      send({}, 'toolCall' in step ? 'tool_calls' : 'stop')
      chunks.push(
        `data: ${JSON.stringify({
          id,
          object: 'chat.completion.chunk',
          choices: [],
          usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
        })}\n\n`,
      )
      chunks.push('data: [DONE]\n\n')
      return new Response(chunks.join(''), {
        headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' },
      })
    },
  })

  return {
    get baseUrl() {
      return `http://127.0.0.1:${server!.port}/v1`
    },
    pending: () => steps.length,
    requests: () => received,
    push: (...added: Step[]) => {
      steps.push(...added)
    },
    set: (next: Step[]) => {
      steps.length = 0
      steps.push(...next)
    },
    stop: () => {
      server?.stop(true)
      server = undefined
    },
  }
}

/** The config file a built kernel reads to talk to a fake provider. */
export function fakeKernelConfig(baseUrl: string): Record<string, unknown> {
  return {
    providers: [
      {
        id: 'fake',
        name: 'Fake provider',
        baseUrl,
        api: 'openai-completions',
        apiKeyEnv: 'FAKE_PROVIDER_KEY',
        models: [
          {
            id: 'fake-1',
            name: 'Fake one',
            contextWindow: 200_000,
            maxTokens: 8_000,
            input: ['text', 'image'],
          },
        ],
      },
    ],
    agent: { provider: 'fake', modelId: 'fake-1' },
  }
}

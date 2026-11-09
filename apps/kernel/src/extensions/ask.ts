import { defineExtension, defineTool } from '@earendil-works/pi-durable'
import type { Extension, ToolRegistration } from '@earendil-works/pi-durable'
import { arr, bool, obj, opt, str } from '@earendil-works/pi-schema'
import type { AskArgs, Asks } from '../ask/asks.ts'

const parameters = obj({
  context: str({
    minLength: 1,
    description: 'One sentence that says what the answers are for, shown above the questions.',
  }),
  blocking: opt(
    bool({
      description:
        'true (the default) waits for the answer. false shows the question and lets you go on; the answer arrives later as a user message.',
    }),
  ),
  questions: arr(
    obj({
      questionId: opt(str({ minLength: 1, description: 'Defaults to q1, q2, … in order.' })),
      question: str({ minLength: 1 }),
      options: opt(
        arr(obj({ id: str({ minLength: 1 }), label: str({ minLength: 1 }) }), {
          description:
            'Choices to offer; the person can always type an answer of their own instead.',
        }),
      ),
      multiSelect: opt(bool()),
    }),
    { minItems: 1 },
  ),
})

/** The `ask_user` tool: a question for the person, answered on any window or phone. */
export function askExtension(asks: () => Asks | undefined): Extension {
  const tool = defineTool({
    name: 'ask_user',
    description: [
      'Ask the user one or more questions when you cannot go on well without their decision.',
      'Prefer options the user can pick; keep questions short. Use blocking: false when you can keep working',
      'and only want their answer later.',
    ].join(' '),
    parameters,
    // A question asked twice is two questions: an interrupted one is not asked again on its own.
    replay: 'unsafe',
    callers: ['model'],
    async execute(args, api, context) {
      const gate = asks()
      if (gate === undefined) {
        return {
          output: [{ type: 'text', text: 'Questions are not available yet.' }],
          isError: true,
        }
      }
      return gate.ask(args as AskArgs, api.conversationId as unknown as number, context)
    },
  })
  return defineExtension({ name: 'asks', tools: [tool as ToolRegistration] })
}

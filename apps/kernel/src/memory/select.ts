import type { Context } from '@earendil-works/chord'
import { AgentDoc } from '@earendil-works/pi-durable'
import type { Conversation, Extension, Harness } from '@earendil-works/pi-durable'

/** The Memory extension's name, as a conversation's extension selection names it. */
export const MEMORY_EXTENSION = 'memory'

/**
 * Before a conversation's turn: give it the Memory extension while Memory is on, and take it away once it is off, so
 * turning Memory on or off reaches a running Kernel at the next turn of every conversation (\`harness.configure\`).
 * Like the MCP servers' selection, it is an edit of the default (\`{add}\`): a conversation whose extensions a client
 * chose outright is left as it was.
 */
export async function selectMemory(
  harness: Harness,
  conversation: Conversation,
  on: boolean,
  context: Context,
): Promise<void> {
  const stored = (await harness.snapshot(AgentDoc, conversation.id, context))?.extensions
  if (Array.isArray(stored)) return
  const added = stored?.add ?? []
  if (added.includes(MEMORY_EXTENSION) === on) return
  const next = on ? [...added, MEMORY_EXTENSION] : added.filter((name) => name !== MEMORY_EXTENSION)
  const named = (names: readonly string[]) => names.map((name) => ({ name }) as Extension)
  await conversation.configure(
    {
      extensions: {
        add: named(next),
        ...(stored?.remove === undefined ? {} : { remove: named(stored.remove) }),
      },
    },
    context,
  )
}

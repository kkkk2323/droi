import { ToolTask, defineExtension, hook } from '@earendil-works/pi-durable'
import type { Extension } from '@earendil-works/pi-durable'
import type { Approvals } from '../policy/approvals.ts'

/**
 * Every tool call, the model's and a script's own, passes the approval policy before Pi records its intent:
 * a call that waits is a call that has not started, and a refused one never runs.
 */
export function approvalExtension(approvals: () => Approvals | undefined): Extension {
  return defineExtension({
    name: 'approvals',
    hooks: [
      hook(ToolTask, {
        beforeTool: async (call, api, context) => {
          const gate = approvals()
          if (gate === undefined) return undefined
          return gate.beforeTool(
            { name: call.name, arguments: call.arguments as Record<string, unknown> },
            api.conversationId as unknown as number,
            context,
          )
        },
      }),
    ],
  })
}

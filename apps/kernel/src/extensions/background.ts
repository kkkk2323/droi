import { isAbsolute, resolve } from 'node:path'
import { defineExtension, defineTool } from '@earendil-works/pi-durable'
import type { Extension, ToolRegistration } from '@earendil-works/pi-durable'
import { num, obj, opt, str } from '@earendil-works/pi-schema'
import type { Background } from '../processes/background.ts'

const parameters = obj({
  command: str({
    minLength: 1,
    description: 'The command to start. It keeps running after this call returns.',
  }),
  cwd: opt(
    str({
      description:
        'Where to start it: absolute, or relative to the working directory. Defaults to it.',
    }),
  ),
})

const structuredOutputSchema = obj({
  processId: str(),
  pid: num(),
  logPath: str({ description: 'The file its stdout and stderr go to' }),
})

/** `bash_background`: a command that outlives the call, such as a dev server or a watcher. */
export function backgroundExtension(background: () => Background | undefined): Extension {
  const tool = defineTool({
    name: 'bash_background',
    description: [
      'Start a long-running command in the background (a dev server, a watcher, a slow build) and return at once',
      'with its processId, pid and logPath. Its output goes to logPath: read that file to see it. Use `bash` for',
      'commands that finish; use this only when the command must keep running while you work.',
    ].join(' '),
    parameters,
    structuredOutputSchema,
    // Starting it again after a restart would run a second copy.
    replay: 'unsafe',
    async execute(args, api) {
      const processes = background()
      if (processes === undefined) {
        return {
          output: [{ type: 'text', text: 'Background processes are not available yet.' }],
          isError: true,
        }
      }
      const base = api.env?.cwd
      const cwd =
        args.cwd === undefined || args.cwd === ''
          ? base
          : isAbsolute(args.cwd) || base === undefined
            ? args.cwd
            : resolve(base, args.cwd)
      const started = await processes.start(
        args.command,
        cwd,
        api.conversationId as unknown as number,
        api.callId,
      )
      return {
        output: [
          {
            type: 'text',
            text: [
              `Started in the background: processId ${started.id}, pid ${started.pid}.`,
              `Output: ${started.logPath}`,
            ].join('\n'),
          },
        ],
        structuredOutput: { processId: started.id, pid: started.pid, logPath: started.logPath },
      }
    },
  })
  return defineExtension({ name: 'background', tools: [tool as ToolRegistration] })
}

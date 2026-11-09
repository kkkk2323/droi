import type { Context, JsonValue } from '@earendil-works/chord'
import {
  LiveDoc,
  NestedCallDoc,
  ToolTask,
  defineDoc,
  defineTask,
  defineTool,
} from '@earendil-works/pi-durable'
import type {
  JsonObject,
  NestedToolExecutionResult,
  NestedToolSlot,
  TaskId,
  ToolExecutionApi,
  ToolExecutionResult,
  ToolRegistration,
} from '@earendil-works/pi-durable'
import type { CodemodeNestedCall } from '@earendil-works/pi-durable/codemode'
import { num, obj, opt, bool } from '@earendil-works/pi-schema'

/**
 * Background scripts: a codemode script that starts with `// @options: {"background": true}` answers at once with a
 * job id, and runs on while the conversation does, as Droid's Script with `startInBackground` and pi's planned
 * `background` option do (earendil-works/pi#11189, "a background script is one of those [background tasks]").
 *
 * The job is a durable task of the conversation, `background`, so neither the turn nor Stop waits on it, and a
 * restart resumes it: it runs pi-durable's own codemode call, whose journal replays what the script was handed
 * before. Its calls are tool tasks of its own, made the way `executeTool()` makes a tool's nested calls, so the
 * approval policy judges each and `pi.live.nestedTools` shows each under the card of the call that started it.
 *
 * When it ends, its result reaches the model: as a message the run in progress takes at its next step, or, when the
 * conversation is idle, as an entry in its context the next turn reads; a person sees it as a note. `codemode_job`
 * reports a job, waits for one, or stops it.
 */
export const CODEMODE_JOB_TASK = 'droi.codemode.job'
export const CODEMODE_JOB_TOOL = 'codemode_job'
/** The entry a job's end writes: its result, for the model and the card. */
export const CODEMODE_JOB_DONE = 'droi.codemode.done'

type JobInput = { code: string; callId: string }
type JobState = { phase: 'run' }
export type JobStatus = 'completed' | 'failed' | 'stopped'
export type JobResult = {
  status: JobStatus
  /** What the model reads: the script's output, as a foreground script's result would have said it. */
  output: string
  /** The card's: the calls the script made, with what they answered. */
  details: JsonValue
}

/** Which job a codemode call started, written in the commit that creates it, so a rerun does not start another. */
const StartedDoc = defineDoc<{ jobId?: number }>({
  kind: 'droi.codemode.started',
  version: 1,
  scope: 'task',
  initial: () => ({}),
  checkpointWhen: () => false,
})

/**
 * The calls a job made, by VM call id: `pi.live.nestedTools` holds a running tool's calls only for its round (a new
 * round or the run's end deletes them), and a job outlives both. What a stop and `codemode_job` read.
 */
type JobCall = {
  name: string
  arguments: JsonObject
  status: 'running' | 'ok' | 'error'
  durationMs?: number
  error?: string
}
const JobCallsDoc = defineDoc<{ calls: Record<string, JobCall> }>({
  kind: 'droi.codemode.job-calls',
  version: 1,
  scope: 'task',
  initial: () => ({ calls: {} }),
  checkpointWhen: () => false,
})

export type JobOptions = {
  /** Pi's codemode tool, which a job runs as it would run in the foreground. */
  codemode: ToolRegistration
  /** Each call's answer into its row, as a foreground script's card has it (codemode.ts). */
  withOutputs: (
    rows: readonly CodemodeNestedCall[],
    read: NestedReader,
    stopped: boolean,
  ) => Promise<JsonValue[]>
  /** Harness.abortTask: `codemode_job {stop}` ends a job no tool owns. */
  abortTask: (id: number) => Promise<void>
}

/** What a script's card is built from: the job's slots, and its calls' answers. */
export type NestedReader = {
  slots: Map<number, NestedToolSlot>
  answer: (id: number) => Promise<NestedToolExecutionResult | undefined>
}

/** `background` taken out of a script's options line: the script as pi-codemode accepts it, and whether it was set. */
export function backgroundOption(code: string): { code: string; background: boolean } {
  const match = /^(\s*)\/\/\s*@options:\s*(\{.*\})\s*$/.exec(code.split('\n', 1)[0] ?? '')
  if (match === null) return { code, background: false }
  let options: Record<string, unknown>
  try {
    options = JSON.parse(match[2]!) as Record<string, unknown>
  } catch {
    return { code, background: false }
  }
  if (typeof options !== 'object' || options === null || !('background' in options)) {
    return { code, background: false }
  }
  const { background, ...rest } = options
  const rest_ = Object.keys(rest).length === 0 ? '' : `// @options: ${JSON.stringify(rest)}`
  // The line stays, emptied, so a stack trace's line numbers still match the script the model wrote.
  return {
    code: rest_ + code.slice(code.indexOf('\n') === -1 ? code.length : code.indexOf('\n')),
    background: background === true,
  }
}

export function codemodeJobs(options: JobOptions) {
  const task = defineTask<JobInput, JobState, JobResult>({
    name: CODEMODE_JOB_TASK,
    version: 1,
    initial: () => ({ phase: 'run' }),
    phases: {
      run: async (job, runtime, context) => {
        const env = await runtime.env(context)
        const callId = job.input.callId
        let sequence = 0
        const api = {
          taskId: runtime.taskId,
          conversationId: runtime.conversationId,
          callId,
          registry: runtime.registry,
          agent: (ctx: Context) => runtime.agent(ctx),
          get models() {
            return runtime.models
          },
          env,
          now: () => runtime.now(),
          output: () => {},
          outputWindow: undefined,
          retainedOutput: () => ({ text: '', truncated: false }),
          diagnostic: () => {},
          details: async () => {},
          commit: async <T>(
            change: (tx: never) => T | Promise<T>,
            ctx: Context,
            commitOptions?: never,
          ) => {
            let result: T | undefined
            await runtime.commit(
              async (tx) => {
                result = await change(tx as never)
                return undefined
              },
              ctx,
              commitOptions,
            )
            return result as T
          },
          memo: runtime.memo.bind(runtime),
          createTask: async () => {
            throw new Error('a background script cannot create tasks')
          },
          getTask: runtime.getTask.bind(runtime),
          waitForTask: runtime.waitForTask.bind(runtime),
          conversation: runtime.conversation.bind(runtime),
          snapshot: runtime.snapshot.bind(runtime),
          snapshotAsOf: (runtime as unknown as { snapshotAsOf: unknown }).snapshotAsOf,
          watchDoc: (runtime as unknown as { watchDoc: unknown }).watchDoc,
          executeTool: async (
            name: string,
            args: JsonObject,
            ctx: Context,
            callOptions: { key?: string; progress?: boolean } = {},
          ): Promise<NestedToolExecutionResult> => {
            const key = callOptions.key ?? String(++sequence)
            const id = await startCall(runtime as never, callId, key, name, args, ctx)
            const settled = await runtime.waitForTask(id, ctx)
            const stored = (
              (await runtime.snapshot(NestedCallDoc, runtime.taskId, key, ctx)) as
                | { result?: NestedToolExecutionResult }
                | undefined
            )?.result
            await runtime.commit(async (tx) => {
              const doc = (await (tx as never as Docs).doc(JobCallsDoc, runtime.taskId)) as {
                calls: Record<string, JobCall>
              }
              const made = doc.calls[key]
              if (made !== undefined) {
                made.status = stored === undefined || stored.isError ? 'error' : 'ok'
                if (stored?.durationMs !== undefined) made.durationMs = stored.durationMs
                const error = stored?.diagnostics
                  .filter((diagnostic) => diagnostic.severity === 'error')
                  .map((diagnostic) => diagnostic.message)
                  .join('\n')
                if (made.status === 'error' && error) made.error = error.slice(0, 500)
              }
              return undefined
            }, ctx)
            if (stored !== undefined) return JSON.parse(JSON.stringify(stored))
            const outcome = settled.state.outcome as { status: string }
            return {
              taskId: id,
              isError: true,
              diagnostics: [
                {
                  severity: 'error',
                  code: outcome.status,
                  message: `Tool ${name} ended without a result`,
                },
              ],
            }
          },
        } as unknown as ToolExecutionApi
        const result = (await options.codemode.execute(
          { code: job.input.code },
          api,
          context,
        )) as ToolExecutionResult
        const rows = ((result.details as { calls?: CodemodeNestedCall[] } | undefined)?.calls ??
          []) as CodemodeNestedCall[]
        const reader = await readerOf(runtime as never, context)
        const calls = await options.withOutputs(rows, reader, false)
        await finish(
          runtime as never,
          context,
          {
            status: result.isError === true ? 'failed' : 'completed',
            output: textOf(result),
            details: { ...(result.details as JsonObject | undefined), calls } as JsonValue,
          },
          callId,
        )
      },
    },
    // Stopped (`codemode_job {stop}`): the calls it made keep what they answered, the rest are cancelled.
    abort: async (job, runtime, context) => {
      const callId = job.input.callId
      const reader = await readerOf(runtime as never, context)
      const made = await jobCalls(runtime as never, runtime.taskId, context)
      const rows = Object.entries(made)
        .filter(([key]) => key.startsWith('c'))
        .map(([key, call]) => rowOf(Number(key.slice(1)), call))
        .filter((row) => Number.isSafeInteger(row.id))
        .sort((a, b) => a.id - b.id)
      const calls = await options.withOutputs(rows, reader, true)
      await finish(
        runtime as never,
        context,
        {
          status: 'stopped',
          output: 'The background script was stopped.',
          details: { calls } as JsonValue,
        },
        callId,
      )
    },
  })

  /** Pi's codemode tool, which starts a background script as a job and runs any other in the foreground. */
  const codemode: ToolRegistration = {
    ...options.codemode,
    execute: async (args, api, context) => {
      const { code, background } = backgroundOption(String((args as { code?: unknown }).code ?? ''))
      if (!background) return options.codemode.execute(args, api, context)
      const jobId = await api.commit(async (tx) => {
        const started = await tx.doc(StartedDoc, api.taskId)
        if (started.jobId !== undefined) return started.jobId
        const id = await tx.createTask(
          task,
          { code, callId: api.callId },
          { ownership: { kind: 'conversation' }, background: true },
        )
        started.jobId = id as unknown as number
        return started.jobId
      }, context)
      return {
        output: [
          {
            type: 'text',
            text: [
              `Started in the background as job ${jobId}. The script runs on while you work; its result comes to you as a`,
              `message when it ends. Call ${CODEMODE_JOB_TOOL} {id: ${jobId}} to see how it goes, wait for it or stop it.`,
            ].join(' '),
          },
        ],
        details: { calls: [], background: { jobId } },
      }
    },
  }

  const jobTool = defineTool({
    name: CODEMODE_JOB_TOOL,
    description: [
      'See how a background codemode script is going, wait for it to end, or stop it. A background script is one',
      'started with `// @options: {"background": true}`; codemode answered its job id.',
    ].join(' '),
    parameters: obj({
      id: num({ description: 'The job id codemode answered' }),
      wait_seconds: opt(
        num({
          description: 'Wait up to this many seconds (at most 600) for the job to end. Default 0.',
        }),
      ),
      stop: opt(bool({ description: 'Stop the job: its calls still running are cancelled.' })),
    }),
    callers: ['model'],
    executionMode: 'parallel',
    async execute(args, api, context) {
      const id = args.id as unknown as TaskId<JobResult>
      const record = await api.getTask(id, context)
      if (
        record === undefined ||
        record.kind !== CODEMODE_JOB_TASK ||
        record.conversationId !== api.conversationId
      ) {
        return {
          output: [{ type: 'text', text: `There is no background script ${args.id} here.` }],
          isError: true,
        }
      }
      if (args.stop === true && record.state.status !== 'terminal') await options.abortTask(args.id)
      const wait = Math.min(Math.max(args.wait_seconds ?? 0, 0), 600) * 1000
      if (args.stop === true || wait > 0) {
        const limit = args.stop === true ? 30_000 : wait
        await Promise.race([
          api.waitForTask(id, context).catch(() => undefined),
          new Promise((resolve) => setTimeout(resolve, limit).unref?.()),
        ])
      }
      const now = await api.getTask(id, context)
      if (now?.state.status === 'terminal') {
        const outcome = now.state.outcome as { status: string; result?: JobResult }
        const result = outcome.result
        return {
          output: [
            {
              type: 'text',
              text: doneText(args.id, result?.status ?? 'failed', result?.output ?? ''),
            },
          ],
          details: { jobId: args.id, status: result?.status ?? 'failed' },
        }
      }
      const calls = Object.values(await jobCalls(api as never, id, context))
      const summary =
        calls.length === 0
          ? 'it has made no calls yet'
          : `its calls so far: ${calls.map((call) => `${call.name} ${call.status}`).join(', ')}`
      return {
        output: [
          { type: 'text', text: `Background script ${args.id} is still running; ${summary}.` },
        ],
        details: { jobId: args.id, status: 'running' },
      }
    },
  })

  return { task, codemode, jobTool: jobTool as ToolRegistration }
}

function doneText(id: number, status: JobStatus | string, output: string): string {
  const how = status === 'completed' ? 'ended' : status === 'stopped' ? 'was stopped' : 'failed'
  return output === ''
    ? `Background script ${id} ${how}.`
    : `Background script ${id} ${how}:\n${output}`
}

/** The model's text of a codemode result: its text items, an image as [image]. */
function textOf(result: ToolExecutionResult): string {
  return (result.output ?? [])
    .map((item) => (item.type === 'text' ? item.text : '[image]'))
    .join('\n')
}

type JobRuntime = {
  taskId: TaskId
  conversationId: never
  commit: (
    change: (tx: never, current: unknown) => unknown,
    context: Context,
    options?: unknown,
  ) => Promise<void>
  snapshot: (doc: unknown, ...args: unknown[]) => Promise<unknown>
  ownedTasks: (context: Context) => Promise<{ id: TaskId; kind: string; input: unknown }[]>
  abortOwned: (id: TaskId, context: Context) => Promise<void>
  conversation: (id: never, context: Context) => Promise<unknown>
}

/**
 * Make nested call `key` of the job, as pi-durable's `startNestedCall` makes a tool's: its tool task, owned by the
 * job, its slot under the card of the call that started the job, and its `NestedCallDoc`, in one commit. A key made
 * before (a rerun after a restart) answers that call's task.
 */
async function startCall(
  runtime: JobRuntime,
  parentCallId: string,
  key: string,
  name: string,
  args: JsonObject,
  context: Context,
): Promise<TaskId> {
  const call = {
    type: 'toolCall' as const,
    id: `${parentCallId}/${key}`,
    name,
    arguments: JSON.parse(JSON.stringify(args)) as JsonObject,
  }
  let id: TaskId | undefined
  await runtime.commit(async (tx: never) => {
    const t = tx as {
      doc: (doc: unknown, ...args: unknown[]) => Promise<Record<string, unknown>>
      task: (id: TaskId) => Promise<{ input?: unknown } | undefined>
      createTask: (task: unknown, input: unknown, options: unknown) => Promise<TaskId>
    }
    const state = (await t.doc(NestedCallDoc, runtime.taskId, key, null)) as { taskId?: TaskId }
    if (state.taskId !== undefined) {
      const input = (await t.task(state.taskId))?.input as
        | { kind?: string; call?: { name?: string; arguments?: unknown } }
        | undefined
      if (
        input?.kind !== 'nested' ||
        input.call?.name !== name ||
        JSON.stringify(input.call.arguments) !== JSON.stringify(call.arguments)
      ) {
        throw new Error(
          `Nested call ${call.id} was already made with another tool or other arguments`,
        )
      }
      id = state.taskId
      return undefined
    }
    id = await t.createTask(
      ToolTask,
      { kind: 'nested', parent: runtime.taskId, parentCallId, key, call },
      { ownership: { kind: 'task', taskId: runtime.taskId } },
    )
    state.taskId = id
    const made = (await t.doc(JobCallsDoc, runtime.taskId)) as { calls: Record<string, JobCall> }
    made.calls[key] = { name, arguments: call.arguments, status: 'running' }
    const live = (await t.doc(LiveDoc, runtime.conversationId)) as { nestedTools?: unknown[] }
    live.nestedTools ??= []
    live.nestedTools.push({
      callId: call.id,
      parentCallId,
      parentTaskId: runtime.taskId,
      name,
      taskId: id,
      arguments: call.arguments,
      status: 'pending',
    })
    return undefined
  }, context)
  return id!
}

/** The arguments each of the job's calls ran with, by VM call id, and a reader of their answers. */
async function readerOf(runtime: JobRuntime, context: Context): Promise<NestedReader> {
  const slots = new Map<number, NestedToolSlot>()
  for (const [key, call] of Object.entries(await jobCalls(runtime, runtime.taskId, context))) {
    const id = Number(key.slice(1))
    if (key.startsWith('c') && Number.isSafeInteger(id)) {
      slots.set(id, { arguments: call.arguments } as unknown as NestedToolSlot)
    }
  }
  return {
    slots,
    answer: async (id) =>
      (
        (await runtime.snapshot(NestedCallDoc, runtime.taskId, `c${id}`, context)) as
          | { result?: NestedToolExecutionResult }
          | undefined
      )?.result,
  }
}

/** Pi's row for a call a job made, as Pi writes it for a script that ends: one still running was cancelled. */
function rowOf(id: number, call: JobCall): CodemodeNestedCall {
  const args = JSON.stringify(call.arguments)
  const status = call.status === 'running' ? 'cancelled' : call.status
  return {
    id,
    name: call.name,
    args: args.length > 200 ? `${args.slice(0, 197)}...` : args,
    status,
    ...(call.durationMs === undefined ? {} : { durationMs: call.durationMs }),
    ...(status === 'error' && call.error ? { error: call.error } : {}),
  } as CodemodeNestedCall
}

type Docs = { doc: (doc: unknown, ...args: unknown[]) => Promise<Record<string, unknown>> }

/** The calls job `id` made, by key. */
async function jobCalls(
  reader: { snapshot: (doc: unknown, ...args: unknown[]) => Promise<unknown> },
  id: unknown,
  context: Context,
): Promise<Record<string, JobCall>> {
  return (
    (
      (await reader.snapshot(JobCallsDoc, id, context)) as
        | { calls?: Record<string, JobCall> }
        | undefined
    )?.calls ?? {}
  )
}

/**
 * End the job: its calls still running are stopped, its slots leave `pi.live`, and in one commit with its outcome
 * its result reaches the conversation — as a message the run in progress takes, or, idle, an entry the next turn reads.
 */
async function finish(
  runtime: JobRuntime,
  context: Context,
  result: JobResult,
  callId: string,
): Promise<void> {
  const owned = (await runtime.ownedTasks(context)).filter(
    (record) => record.kind === ToolTask.definition.name,
  )
  await Promise.all(owned.map((record) => runtime.abortOwned(record.id, context)))
  const jobId = runtime.taskId as unknown as number
  const text = doneText(jobId, result.status, result.output)
  const handle = (await runtime.conversation(runtime.conversationId, context)) as
    | { submit: (tx: never, submission: unknown) => Promise<unknown> }
    | undefined
  await runtime.commit(async (tx: never) => {
    const t = tx as {
      doc: (doc: unknown, ...args: unknown[]) => Promise<Record<string, unknown>>
      appendEntry: (conversationId: never, draft: unknown) => Promise<unknown>
    }
    const live = (await t.doc(LiveDoc, runtime.conversationId)) as {
      run?: unknown
      nestedTools?: NestedToolSlot[]
    }
    if (live.nestedTools !== undefined) {
      for (let i = live.nestedTools.length - 1; i >= 0; i--) {
        if (live.nestedTools[i]!.parentTaskId === runtime.taskId) live.nestedTools.splice(i, 1)
      }
    }
    const data = {
      jobId,
      callId,
      status: result.status,
      output: result.output,
      details: result.details,
    }
    if (live.run !== undefined && handle !== undefined) {
      await handle.submit(tx, {
        type: 'input',
        content: [{ type: 'text', text }],
        whenBusy: 'followUp',
        entry: { kind: CODEMODE_JOB_DONE, data },
      })
    } else {
      await t.appendEntry(runtime.conversationId, {
        kind: CODEMODE_JOB_DONE,
        data,
        model: [{ role: 'user', content: [{ type: 'text', text }], timestamp: Date.now() }],
      })
    }
    return { status: 'terminal', outcome: { status: 'completed', result } }
  }, context)
}

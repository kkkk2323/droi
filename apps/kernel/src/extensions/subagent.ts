import { readFile } from 'node:fs/promises'
import { extname, isAbsolute, resolve } from 'node:path'
import type { Context, JsonValue } from '@earendil-works/chord'
import {
  LiveDoc,
  configure,
  defineDoc,
  defineExtension,
  defineTask,
  defineTool,
  section,
} from '@earendil-works/pi-durable'
import type {
  Agent,
  AgentChange,
  AnyTask,
  Extension,
  ToolExecutionApi,
  ToolRegistration,
} from '@earendil-works/pi-durable'
import type { Models } from '@earendil-works/pi-ai'
import { arr, bool, num, obj, oneOf, opt, str } from '@earendil-works/pi-schema'
import { loadDroids, type Droid } from '../subagents/droids.ts'

/**
 * Subagents, as Factory Droid has them: `task` starts a droid in a conversation of its own and answers what it said;
 * with `run_in_background` it answers at once and the subagent's answer comes later, as a message; `task_output`
 * reads one and `task_stop` stops one. `resume` goes on in a subagent's conversation, which keeps all it did.
 *
 * A subagent's conversation is a child of the call that started it (ADR 0043): pi-durable's task-owned conversation,
 * so stopping the call stops it, the parent is busy until it is done, and a restart finds it again instead of
 * starting another. A background one is owned by a background task of the parent's, which neither the turn nor Stop
 * waits for. Either way the index records its owner, so it stays out of the list. The child has no `task` (no
 * subagents of its own) and no `ask_user` (nobody watches it): blocked, it says so in its answer.
 */
export const SUBAGENT_EXTENSION = 'subagents'
export const TASK_TOOL = 'task'
export const TASK_OUTPUT_TOOL = 'task_output'
export const TASK_STOP_TOOL = 'task_stop'
/** The background subagent's run, a task of the parent's. */
export const SUBAGENT_RUN_TASK = 'droi.subagent.run'
/** The entry a background subagent's end writes into its parent: its answer, for the model and the card. */
export const SUBAGENT_DONE = 'droi.subagent.done'
/** The kind of a subagent's conversation in the index. */
export const SUBAGENT_KIND = 'subagent'

/** What the model reads of an answer at most. */
const ANSWER_LIMIT = 50_000

/** The tools a `read-only` droid goes without: they change files or run commands. */
const WRITING_TOOLS = new Set(['write', 'edit', 'bash', 'powershell', 'bash_background'])

const SUBAGENT_RULES = [
  'You are a subagent: another agent gave you this task and reads only your final message. Work on your own;',
  'nobody can answer a question from you. If you are blocked, stop and say what blocked you. End with a',
  'complete answer: what you found or did, with file paths and the results of what you ran.',
].join(' ')

/** What a subagent's conversation remembers of itself. */
const SubagentDoc = defineDoc<{
  droid?: string
  description?: string
  /** The background run going on in it now, if any: what `task_stop` stops. */
  runTaskId?: number
}>({
  kind: 'droi.subagent',
  version: 1,
  scope: 'conversation',
  history: 'latest',
  fork: 'initial',
  initial: () => ({}),
  checkpointWhen: () => false,
})

/** Which child a `task` call made, and its background run: a rerun after a restart makes neither again. */
const StartedDoc = defineDoc<{ childId?: number; runTaskId?: number }>({
  kind: 'droi.subagent.started',
  version: 1,
  scope: 'task',
  initial: () => ({}),
  checkpointWhen: () => false,
})

export type SubagentStatus = 'running' | 'completed' | 'failed' | 'stopped'

/** What a call's card shows: the child to open, and how it stands. */
export type SubagentDetails = {
  conversationId: number
  subagentType: string
  description: string
  status: SubagentStatus
  background?: boolean
}

export type SubagentOptions = {
  /** Where the droids are read from: `<userDir>/droids`. */
  userDir: () => string
  /**
   * A child was made: the index learns its owner, title and workspace, so it is the parent's and stays out of the
   * list (ADR 0043).
   */
  onChild: (child: {
    id: number
    ownerId: number
    title: string
    workspace?: string
    createdAt: number
  }) => Promise<void>
  /** Harness.abortTask: `task_stop` ends a background run, which no tool owns. */
  abortTask: (id: number) => Promise<void>
}

type RunInput = {
  /** The parent's call that started it, for the card and the entry its end writes. */
  callId: string
  subagentType: string
  description: string
  content: JsonValue
  /** The conversation to go on in, for a resumed one; else the one this task owns. */
  childId?: number
}
type RunResult = { status: SubagentStatus; output: string; conversationId: number }

const parameters = obj({
  subagent_type: str({
    description: 'The droid to run: one the Subagents section lists (do not guess a name)',
  }),
  description: str({ description: 'A short label of the task, three to five words, for the card' }),
  prompt: str({
    description:
      'The whole task: what to do, what you already know, and what to answer with. The subagent sees nothing of this conversation.',
  }),
  image_paths: opt(
    arr(str(), {
      description: 'Image files to attach, by path (absolute or relative to the working directory)',
    }),
  ),
  complexity: opt(
    oneOf(['light', 'medium', 'heavy'], {
      description: "How hard the task is. Accepted for later; the droid's model decides for now.",
    }),
  ),
  run_in_background: opt(
    bool({
      description:
        'Answer at once with the task_id and let it run; its answer comes to you as a message when it ends. Only for work that is independent of what you do next.',
    }),
  ),
  resume: opt(
    str({
      description:
        'A task_id from an earlier task call: go on in that subagent conversation, with all it did, instead of starting a new one. subagent_type is then ignored.',
    }),
  ),
})

/** The subagent tools, their background run, and the section that lists the droids. */
export function subagentExtension(options: SubagentOptions): Extension {
  let self: Extension | undefined

  const run = defineTask<RunInput, { phase: 'run' }, RunResult>({
    name: SUBAGENT_RUN_TASK,
    version: 1,
    initial: () => ({ phase: 'run' }),
    phases: {
      run: async (job, runtime, context) => {
        const childId = job.input.childId ?? (await ownedChild(runtime as never, context))
        const handle = await runtime.conversation(childId as never, context)
        if (handle === undefined) throw new Error(`subagent conversation ${childId} is gone`)
        const submission = await handle.submit(
          {
            type: 'input',
            content: job.input.content as never,
            requestId: `subagent:${runtime.taskId}`,
          },
          context,
        )
        const settled = await submission.wait(context)
        const output = await answerOf(runtime as never, childId, context)
        await finish(runtime as never, context, job.input, {
          status: statusOf(settled),
          output,
          conversationId: childId,
        })
      },
    },
    abort: async (job, runtime, context) => {
      const childId = job.input.childId ?? (await ownedChild(runtime as never, context))
      // A resumed child is not this task's: its run is stopped here, not by the cascade.
      const handle = await runtime.conversation(childId as never, context)
      await handle?.abort(context).catch(() => undefined)
      await finish(runtime as never, context, job.input, {
        status: 'stopped',
        output: await answerOf(runtime as never, childId, context),
        conversationId: childId,
      })
    },
  })

  const task = defineTool({
    name: TASK_TOOL,
    description: [
      'Start a subagent: a droid that does a self-contained task in a fresh context of its own and answers with',
      'its final message, which is all you get back. Give it the whole task in prompt. Use one for work that would',
      'fill this conversation with output you will not need again (searching a codebase, a review, a long run of',
      'commands). Several independent ones run at once when you call task several times in one turn. The',
      'Subagents section lists the droids.',
    ].join(' '),
    parameters,
    // A rerun after a crash finds the child it made and the input it sent, and waits on.
    replay: 'safe',
    executionMode: 'parallel',
    callers: ['model', 'tools'],
    async execute(args, api, context) {
      const content = await promptContent(args.prompt, args.image_paths, api.env?.cwd)
      if (typeof content === 'string') return failed(content)
      if (args.resume !== undefined && args.resume !== '') {
        return resumeChild(args, content, api, context)
      }
      const droids = loadDroids(options.userDir()).droids
      const droid = droids.find((each) => each.name === args.subagent_type)
      if (droid === undefined) {
        return failed(
          `There is no droid ${JSON.stringify(args.subagent_type)}. The droids are: ${droids.map((each) => each.name).join(', ')}.`,
        )
      }
      const parent = await api.agent(context)
      const change = childChange(droid, parent, api, self!)
      if (typeof change === 'string') return failed(change)
      const parentId = api.conversationId as unknown as number
      const createdAt = Date.now()
      const workspace = parent.cwd
      const made = await api.commit(async (tx) => {
        const started = await tx.doc(StartedDoc, api.taskId)
        if (started.childId !== undefined) return { childId: started.childId, fresh: false }
        let owner = api.taskId
        if (args.run_in_background === true) {
          owner = await tx.createTask(
            run,
            {
              callId: api.callId,
              subagentType: droid.name,
              description: args.description,
              content: content as JsonValue,
            },
            { ownership: { kind: 'conversation' }, background: true },
          )
          started.runTaskId = owner as unknown as number
        }
        const created = await tx.createConversation({ ownership: { kind: 'task', taskId: owner } })
        await configure(tx, created.id, change)
        await tx.appendEntry(created.id, {
          kind: 'droi.meta',
          data: {
            kind: SUBAGENT_KIND,
            ownerId: parentId,
            createdAt,
            ...(workspace === undefined ? {} : { workspace }),
          },
        })
        const doc = await tx.doc(SubagentDoc, created.id)
        doc.droid = droid.name
        doc.description = args.description
        if (started.runTaskId !== undefined) doc.runTaskId = started.runTaskId
        started.childId = created.id as unknown as number
        return { childId: started.childId, fresh: true }
      }, context)
      if (made.fresh) {
        await options.onChild({
          id: made.childId,
          ownerId: parentId,
          title: args.description,
          createdAt,
          ...(workspace === undefined ? {} : { workspace }),
        })
      }
      const details: SubagentDetails = {
        conversationId: made.childId,
        subagentType: droid.name,
        description: args.description,
        status: 'running',
        ...(args.run_in_background === true ? { background: true } : {}),
      }
      if (args.run_in_background === true) return startedInBackground(details)
      return runInForeground(details, content, api, context)
    },
  })

  /** `resume`: the same child, another input; in the foreground, or as a background run of the parent's. */
  async function resumeChild(
    args: { resume?: string; description: string; run_in_background?: boolean },
    content: unknown[],
    api: ToolExecutionApi,
    context: Context,
  ) {
    const found = await childOf(api, args.resume!, context)
    if (typeof found === 'string') return failed(found)
    if (await running(api, found.id, context)) {
      return failed(
        `Subagent ${found.id} is still running: wait for it with ${TASK_OUTPUT_TOOL} or stop it with ${TASK_STOP_TOOL} first.`,
      )
    }
    const details: SubagentDetails = {
      conversationId: found.id,
      subagentType: found.droid,
      description: args.description,
      status: 'running',
      ...(args.run_in_background === true ? { background: true } : {}),
    }
    if (args.run_in_background !== true) {
      await api.commit(async (tx) => {
        const doc = await tx.doc(SubagentDoc, found.id as never)
        delete doc.runTaskId
      }, context)
      return runInForeground(details, content, api, context)
    }
    await api.commit(async (tx) => {
      const started = await tx.doc(StartedDoc, api.taskId)
      if (started.runTaskId !== undefined) return
      const id = await tx.createTask(
        run,
        {
          callId: api.callId,
          subagentType: found.droid,
          description: args.description,
          content: content as JsonValue,
          childId: found.id,
        },
        { ownership: { kind: 'conversation' }, background: true },
      )
      started.runTaskId = id as unknown as number
      const doc = await tx.doc(SubagentDoc, found.id as never)
      doc.runTaskId = started.runTaskId
    }, context)
    return startedInBackground(details)
  }

  const output = defineTool({
    name: TASK_OUTPUT_TOOL,
    description: [
      "Read a subagent's answer by the task_id task gave: whether it is still running and what it last said.",
      'With block it waits for it to end, up to timeout_seconds.',
    ].join(' '),
    parameters: obj({
      task_id: str({ description: 'The task_id task answered' }),
      block: opt(bool({ description: 'Wait for it to end. Default true.' })),
      timeout_seconds: opt(
        num({ description: 'How long to wait at most, up to 600. Default 300.' }),
      ),
    }),
    callers: ['model', 'tools'],
    executionMode: 'parallel',
    async execute(args, api, context) {
      const found = await childOf(api, args.task_id, context)
      if (typeof found === 'string') return failed(found)
      if (args.block !== false && (await running(api, found.id, context))) {
        const handle = await api.conversation(found.id as never, context)
        const limit = Math.min(Math.max(args.timeout_seconds ?? 300, 0), 600) * 1000
        await Promise.race([
          handle?.waitForIdle(context).catch(() => undefined),
          new Promise((done) => setTimeout(done, limit).unref?.()),
        ])
      }
      const still = await running(api, found.id, context)
      const answer = await answerOf(api as never, found.id, context)
      const head = still
        ? `Subagent ${found.id} is still running.`
        : `Subagent ${found.id} is not running.`
      return {
        output: [
          {
            type: 'text',
            text:
              answer === ''
                ? head
                : `${head} ${still ? 'So far it said' : 'Its answer'}:\n${answer}`,
          },
        ],
        details: { conversationId: found.id, status: still ? 'running' : 'completed' },
      }
    },
  })

  const stop = defineTool({
    name: TASK_STOP_TOOL,
    description: 'Stop a running subagent by the task_id task gave. What it did so far stays.',
    parameters: obj({ task_id: str({ description: 'The task_id task answered' }) }),
    callers: ['model', 'tools'],
    async execute(args, api, context) {
      const found = await childOf(api, args.task_id, context)
      if (typeof found === 'string') return failed(found)
      if (!(await running(api, found.id, context))) {
        return { output: [{ type: 'text', text: `Subagent ${found.id} is not running.` }] }
      }
      if (found.runTaskId !== undefined) await options.abortTask(found.runTaskId)
      else await (await api.conversation(found.id as never, context))?.abort(context)
      return {
        output: [{ type: 'text', text: `Subagent ${found.id} was stopped.` }],
        details: { conversationId: found.id, status: 'stopped' },
      }
    },
  })

  self = defineExtension({
    name: SUBAGENT_EXTENSION,
    tools: [task, output, stop] as ToolRegistration[],
    tasks: [run as unknown as AnyTask],
    sections: [section('subagents', () => droidsSection(loadDroids(options.userDir()).droids))],
  })
  return self
}

function droidsSection(droids: readonly Droid[]): string {
  return [
    `Subagents: ${TASK_TOOL} {subagent_type, description, prompt} starts one of these droids in a context of its own:`,
    ...droids.map((droid) => `- ${droid.name}: ${droid.description}`),
  ].join('\n')
}

/** The agent a droid's conversation runs: the parent's, with the droid's prompt, model and tools. */
function childChange(
  droid: Droid,
  parent: Agent,
  api: ToolExecutionApi,
  self: Extension,
): AgentChange | string {
  const change: { -readonly [K in keyof AgentChange]: AgentChange[K] } = {
    instructions: [parent.instructions, SUBAGENT_RULES, droid.prompt]
      .filter((part): part is string => part !== undefined && part.trim() !== '')
      .join('\n\n'),
  }
  if (droid.model !== undefined) {
    const model = modelOf(api.models, droid.model)
    if (model === undefined) {
      return `Droid ${droid.name} asks for model ${droid.model}, which no Model Source offers. Change its model in Settings → Droids.`
    }
    change.model = model
  }
  if (droid.reasoningEffort !== undefined) change.thinkingLevel = droid.reasoningEffort as never
  const asks = api.registry.extension('asks')
  change.extensions = { remove: [self, ...(asks === undefined ? [] : [asks])] }
  if (droid.tools !== undefined && droid.tools !== 'all') {
    const allowed = droid.tools
    const keep = (name: string) =>
      allowed === 'read-only' ? !WRITING_TOOLS.has(name) : allowed.includes(name)
    const every = new Map<string, ToolRegistration>()
    for (const tool of [...parent.tools, ...parent.callable]) every.set(tool.name, tool)
    const remove = [...every.values()].filter((tool) => !keep(tool.name))
    if (remove.length > 0) change.tools = { remove }
  }
  return change
}

/** A model by `provider/modelId` or by its id alone, among the models there are. */
function modelOf(models: Models, name: string): { provider: string; modelId: string } | undefined {
  const slash = name.indexOf('/')
  if (slash > 0) {
    const provider = name.slice(0, slash)
    const modelId = name.slice(slash + 1)
    if (models.getModel(provider, modelId) !== undefined) return { provider, modelId }
  }
  const found = models.getModels().find((model) => model.id === name)
  return found === undefined ? undefined : { provider: found.provider, modelId: found.id }
}

const IMAGE_TYPES: Readonly<Record<string, string>> = {
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.webp': 'image/webp',
}

/** The input a subagent gets: the prompt and the images; a string says what is wrong with an image. */
async function promptContent(
  prompt: string,
  paths: readonly string[] | undefined,
  cwd: string | undefined,
): Promise<unknown[] | string> {
  const content: unknown[] = [{ type: 'text', text: prompt }]
  for (const given of paths ?? []) {
    const path = isAbsolute(given) || cwd === undefined ? given : resolve(cwd, given)
    const mimeType = IMAGE_TYPES[extname(path).toLowerCase()]
    if (mimeType === undefined) return `${given} is not a PNG, JPEG, GIF or WebP image.`
    try {
      content.push({ type: 'image', data: (await readFile(path)).toString('base64'), mimeType })
    } catch (cause) {
      return `Cannot read ${given}: ${cause instanceof Error ? cause.message : String(cause)}`
    }
  }
  return content
}

/** Wait for the child's answer in this call, and answer it. */
async function runInForeground(
  details: SubagentDetails,
  content: unknown[],
  api: ToolExecutionApi,
  context: Context,
) {
  await api.details(details as never, context)
  const handle = await api.conversation(details.conversationId as never, context)
  if (handle === undefined) return failed(`Subagent ${details.conversationId} is gone.`)
  let settled: { status: string; reason?: string }
  try {
    const submission = await handle.submit(
      { type: 'input', content: content as never, requestId: `subagent:${api.taskId}` },
      context,
    )
    settled = await submission.wait(context)
  } catch (cause) {
    // Stopped: a resumed child is not this call's, so its run is stopped here.
    await handle.abort(context).catch(() => undefined)
    throw cause
  }
  const answer = await answerOf(api as never, details.conversationId, context)
  const status = statusOf(settled)
  const text =
    status === 'completed'
      ? answer
      : `The subagent ${status === 'stopped' ? 'was stopped' : 'ended without an answer'}.${answer === '' ? '' : ` What it said last:\n${answer}`}`
  return {
    output: [
      {
        type: 'text' as const,
        text: `${text}\n\n(task_id: ${details.conversationId}; pass it as resume to go on in this subagent.)`,
      },
    ],
    details: { ...details, status } as never,
    ...(status === 'completed' ? {} : { isError: true }),
  }
}

/** How a child's input ended: answered, stopped (task_stop or a person aborted its run), or failed. */
function statusOf(settled: { status: string; reason?: string }): SubagentStatus {
  if (settled.status === 'done') return 'completed'
  return settled.reason === 'aborted' ? 'stopped' : 'failed'
}

function startedInBackground(details: SubagentDetails) {
  return {
    output: [
      {
        type: 'text' as const,
        text: [
          `Started in the background as task_id ${details.conversationId}. Its answer comes to you as a message when`,
          `it ends; ${TASK_OUTPUT_TOOL} reads it before then, ${TASK_STOP_TOOL} stops it.`,
        ].join(' '),
      },
    ],
    details: details as never,
  }
}

function failed(text: string) {
  return { output: [{ type: 'text' as const, text }], isError: true }
}

type Reader = {
  commit: (change: (tx: never) => unknown, context: Context) => Promise<unknown>
}

/** A child of this conversation by its task_id, with what it remembers of itself; a string when it is not one. */
async function childOf(
  api: ToolExecutionApi,
  taskId: string,
  context: Context,
): Promise<{ id: number; droid: string; runTaskId?: number } | string> {
  const id = Number(taskId)
  const unknown = `There is no subagent ${JSON.stringify(taskId)} of this conversation.`
  if (!Number.isSafeInteger(id) || id < 1) return unknown
  const record = await api.commit((tx) => tx.conversation(id as never), context)
  if (record?.owner?.conversationId !== api.conversationId) return unknown
  const doc = (await api.snapshot(SubagentDoc, id as never, context)) as
    | { droid?: string; runTaskId?: number }
    | undefined
  let runTaskId = doc?.runTaskId
  if (runTaskId !== undefined) {
    const runTask = await api.getTask(runTaskId as never, context)
    if (runTask === undefined || runTask.state.status === 'terminal') runTaskId = undefined
  }
  return { id, droid: doc?.droid ?? 'worker', ...(runTaskId === undefined ? {} : { runTaskId }) }
}

/** Whether the child's run is going on. */
async function running(api: ToolExecutionApi, id: number, context: Context): Promise<boolean> {
  const live = (await api.snapshot(LiveDoc, id as never, context)) as { run?: unknown } | undefined
  return live?.run !== undefined
}

/** The child this background run owns. */
async function ownedChild(
  runtime: { taskId: unknown; commit: Reader['commit'] },
  context: Context,
): Promise<number> {
  let id: number | undefined
  await runtime.commit(async (tx: never) => {
    const page = await (
      tx as {
        scanConversations: (query: unknown, limit: number) => Promise<{ items: { id: unknown }[] }>
      }
    ).scanConversations({ ownerTaskId: runtime.taskId }, 1)
    id = page.items[0]?.id as number | undefined
    return undefined
  }, context)
  if (id === undefined) throw new Error('the background subagent has no conversation')
  return id
}

/** The child's last answer: the text of its newest assistant message. */
async function answerOf(reader: Reader, id: number, context: Context): Promise<string> {
  let text = ''
  await reader.commit(async (tx: never) => {
    const page = await (
      tx as {
        scanEntries: (
          query: unknown,
          limit: number,
        ) => Promise<{ items: { kind: string; model?: unknown }[] }>
      }
    ).scanEntries({ conversationId: id, order: 'descending' }, 50)
    for (const entry of page.items) {
      if (entry.kind !== 'pi.assistant') continue
      const parts = ((entry.model as { content?: unknown }[] | undefined) ?? []).flatMap(
        (message) => (Array.isArray(message.content) ? message.content : []),
      ) as { type?: string; text?: string }[]
      const said = parts
        .filter((part) => part.type === 'text' && typeof part.text === 'string')
        .map((part) => part.text!)
        .join('\n')
        .trim()
      if (said !== '') {
        text = said
        break
      }
    }
    return undefined
  }, context)
  return text.length > ANSWER_LIMIT
    ? `${text.slice(0, ANSWER_LIMIT)}\n[cut at ${ANSWER_LIMIT} characters]`
    : text
}

type RunRuntime = Reader & {
  taskId: unknown
  conversationId: unknown
  conversation: (id: never, context: Context) => Promise<unknown>
}

/**
 * A background run's end: its answer reaches the parent in one commit with its outcome, as a message the parent's
 * run in progress takes, or, idle, an entry its next turn reads, as a background script's does (codemode-jobs.ts).
 */
async function finish(
  runtime: RunRuntime,
  context: Context,
  input: RunInput,
  result: RunResult,
): Promise<void> {
  const how =
    result.status === 'completed' ? 'ended' : result.status === 'stopped' ? 'was stopped' : 'failed'
  const text = [
    `Background subagent ${result.conversationId} (${input.description}) ${how}.`,
    ...(result.output === '' ? [] : [`Its answer:\n${result.output}`]),
  ].join(' ')
  const parent = (await runtime.conversation(runtime.conversationId as never, context)) as
    | { submit: (tx: never, submission: unknown) => Promise<unknown> }
    | undefined
  await runtime.commit(async (tx: never) => {
    const t = tx as {
      doc: (doc: unknown, ...args: unknown[]) => Promise<Record<string, unknown>>
      appendEntry: (conversationId: unknown, draft: unknown) => Promise<unknown>
    }
    const child = await t.doc(SubagentDoc, result.conversationId)
    if (child['runTaskId'] === (runtime.taskId as unknown as number)) delete child['runTaskId']
    const live = (await t.doc(LiveDoc, runtime.conversationId)) as { run?: unknown }
    const data: SubagentDetails & { callId: string; output: string } = {
      conversationId: result.conversationId,
      subagentType: input.subagentType,
      description: input.description,
      status: result.status,
      background: true,
      callId: input.callId,
      output: result.output,
    }
    if (live.run !== undefined && parent !== undefined) {
      await parent.submit(tx, {
        type: 'input',
        content: [{ type: 'text', text }],
        whenBusy: 'followUp',
        entry: { kind: SUBAGENT_DONE, data },
      })
    } else {
      await t.appendEntry(runtime.conversationId, {
        kind: SUBAGENT_DONE,
        data,
        model: [{ role: 'user', content: [{ type: 'text', text }], timestamp: Date.now() }],
      })
    }
    return { status: 'terminal', outcome: { status: 'completed', result } }
  }, context)
}

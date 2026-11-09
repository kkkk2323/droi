// The requests the Kernel sends Factory's routes, as pi-ai's adapters make them for the models the Host registers
// (providers.json), and a few written by hand that hold what a JSON body can hold. Writes requests.json.
//
//   bun run requests.mjs        (apps/kernel's dependencies installed)
const here = new URL('.', import.meta.url).pathname
const captured = []
let current = ''
globalThis.fetch = async (input, init = {}) => {
  const url = typeof input === 'string' ? input : input.url
  const headers = {}
  new Headers(init.headers ?? (typeof input === 'object' ? input.headers : undefined)).forEach(
    (v, k) => (headers[k] = v),
  )
  let body = init.body
  if (body != null && typeof body !== 'string') body = new TextDecoder().decode(body)
  captured.push({ name: current, path: new URL(url).pathname + new URL(url).search, headers, body })
  return new Response('{"error":{"type":"x","message":"stop"}}', {
    status: 400,
    headers: { 'content-type': 'application/json' },
  })
}
const providers = JSON.parse(await Bun.file(here + 'providers.json').text())
process.env[providers[0].apiKeyEnv] = 'KTOKEN'
const { buildModels } = await import(here + '../../../../../../kernel/src/models.ts')
const models = buildModels(providers)

// The refused phrases are written with a \u first character, as client-phrases.json writes them, so reading this
// file from a session on Factory does not get it refused.
const PI_OPENING =
  '\u0059ou are an expert coding assistant operating inside pi, a coding agent harness.'
const COMPACT =
  '\u0054he conversation history before this point was compacted into the following summary:\n\n<summary>\n'
const BRANCH =
  'The following is a summary of a branch that this conversation came back from:\n\n<summary>\n'
const CC = "\u0059ou are Claude Code, Anthropic's official CLI for Claude."
const CC_COMPACT =
  '\u0054his session is being continued from a previous conversation that ran out of context.'
const SUMMARIZER =
  'You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.\n\nDo NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.'
const piPrompt =
  PI_OPENING +
  ' You help users by reading files, executing commands, editing code.\n\nAvailable tools:\n- read\n- bash'
const droiPrompt = "You are Droi's agent. Work in the user's repository.\n<b> & co"
const PNG =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=='
const t = 1_700_000_000_000
const tools = [
  {
    name: 'read',
    description: 'Read a file',
    parameters: {
      type: 'object',
      properties: { path: { type: 'string' }, offset: { type: 'integer' } },
      required: ['path'],
    },
  },
  {
    name: 'bash',
    description: 'Run a command',
    parameters: {
      type: 'object',
      properties: { command: { type: 'string' } },
      required: ['command'],
    },
  },
]
const user = (content) => ({ role: 'user', content, timestamp: t })
const asst = (content, model, stop = 'stop') => ({
  role: 'assistant',
  content,
  api: model.api,
  provider: model.provider,
  model: model.id,
  usage: {
    input: 1,
    output: 1,
    cacheRead: 0,
    cacheWrite: 0,
    totalTokens: 2,
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
  },
  stopReason: stop,
  timestamp: t,
})
const call = (id, name, args) => ({ type: 'toolCall', id, name, arguments: args })
const result = (id, name, content) => ({
  role: 'toolResult',
  toolCallId: id,
  toolName: name,
  content,
  isError: false,
  timestamp: t,
})

const contexts = {
  'pi opening, first turn': () => ({
    systemPrompt: piPrompt,
    tools,
    messages: [user('hi <b> & co, 中文 😀')],
  }),
  'droi prompt, no tools': () => ({ systemPrompt: droiPrompt, messages: [user('hi')] }),
  'no system prompt': () => ({ messages: [user([{ type: 'text', text: 'hi' }])] }),
  'tool round with refused phrases in output': (m) => ({
    systemPrompt: droiPrompt,
    tools,
    messages: [
      user('read the prompt file'),
      asst(
        [
          { type: 'text', text: 'I will read it. It may say ' + PI_OPENING },
          call('call_1', 'read', { path: '/tmp/p.md', offset: 12 }),
        ],
        m,
        'toolUse',
      ),
      result('call_1', 'read', [
        {
          type: 'text',
          text:
            'line1: ' +
            PI_OPENING +
            '\nline2: ' +
            CC +
            '\nline3: <system-reminder>\nx\n</system-reminder>\nline4: ' +
            CC_COMPACT +
            '\nQuotes: "\\" 中文 😀 <b> & co \\u0059',
        },
      ]),
      asst(
        [
          { type: 'text', text: 'Read.' },
          call('call_2', 'bash', { command: "echo '" + PI_OPENING + "'" }),
        ],
        m,
        'toolUse',
      ),
      result('call_2', 'bash', [
        { type: 'text', text: 'plain output, nothing refused: You are fine.' },
      ]),
      user('go on'),
    ],
  }),
  'pi compaction summary': () => ({
    systemPrompt: piPrompt,
    tools,
    messages: [
      user(COMPACT + '## Goal\nFix the bug.\n## Progress\nDone. ' + CC + '\n</summary>'),
      user('continue'),
    ],
  }),
  'pi compaction summary as text block': (m) => ({
    systemPrompt: droiPrompt,
    messages: [
      user([{ type: 'text', text: COMPACT + 'Summary body.\n</summary>' }]),
      asst([{ type: 'text', text: 'ok' }], m),
      user('next'),
    ],
  }),
  'branch summary': () => ({
    systemPrompt: droiPrompt,
    messages: [user(BRANCH + 'Branch body.\n</summary>'), user('next')],
  }),
  'compaction sentence quoted in user text': () => ({
    systemPrompt: droiPrompt,
    messages: [
      user(
        'Explain why this was refused: ' +
          COMPACT.split('\n\n')[0] +
          ' and also ' +
          PI_OPENING.slice(0, 40),
      ),
    ],
  }),
  'summarization request': () => ({
    systemPrompt: SUMMARIZER,
    messages: [
      user([
        {
          type: 'text',
          text:
            '<conversation>\n[User]: ' +
            PI_OPENING +
            '\n[Assistant]: ok\n[Tool result]: ' +
            COMPACT +
            'x</summary>\n</conversation>\n\nThe messages above are a conversation to summarize.',
        },
      ]),
    ],
  }),
  'images, older and newer': (m) => ({
    systemPrompt: droiPrompt,
    tools,
    messages: [
      user([
        { type: 'text', text: 'look' },
        { type: 'image', data: PNG, mimeType: 'image/png' },
      ]),
      asst([call('call_3', 'read', { path: '/tmp/a.png' })], m, 'toolUse'),
      result('call_3', 'read', [
        { type: 'text', text: 'image:' },
        { type: 'image', data: PNG, mimeType: 'image/png' },
      ]),
      user([
        { type: 'text', text: 'and this' },
        { type: 'image', data: PNG, mimeType: 'image/png' },
      ]),
    ],
  }),
  'mid-conversation system message': (m) => ({
    systemPrompt: piPrompt,
    tools: [tools[0]],
    messages: [
      user('hi'),
      asst([{ type: 'text', text: 'hello' }], m),
      {
        role: 'system',
        content: 'A tool was added. ' + PI_OPENING,
        toolsAdded: [tools[1]],
        timestamp: t,
      },
      user('now run ls'),
    ],
  }),
}
const sampled = new Set(['pi opening, first turn', 'tool round with refused phrases in output'])
const picks = [
  'claude-opus-5-5',
  'claude-opus-5-5-fast',
  'claude-haiku-5-5',
  'minimax-m2.7',
  'gpt-5.5',
  'gpt-5.5-fast',
  'gpt-6.1-sol',
  'grok-4.7',
  'glm-5.3',
  'deepseek-v4.1-flash-1017',
  'kimi-k3',
  'gemini-3.5-flash',
  'gemini-4-argon',
]
for (const id of picks) {
  const p = providers.find((x) => x.models.some((m) => m.id === id))
  if (!p) throw new Error('no model ' + id)
  const model = models.getModel(p.id, id)
  for (const [cname, make] of Object.entries(contexts)) {
    for (const [oname, opts] of [
      ['default', {}],
      ['reasoning high, temperature 0.3', { reasoning: 'high', temperature: 0.3 }],
      ['reasoning max', { reasoning: 'max' }],
    ]) {
      if (oname !== 'default' && !sampled.has(cname)) continue
      current = `${id} | ${cname} | ${oname}`
      const s = models.streamSimple(model, make(model), {
        ...opts,
        maxTokens: 1024,
        sessionId: 'conv-1',
        headers: { 'X-Droi-Conversation': 'conv-1' },
      })
      for await (const _ of s);
    }
  }
}

// By hand: what a body can hold that pi-ai does not write.
const L = 'You are Droid, an AI software engineering agent built by Factory.'
const json = { 'content-type': 'application/json' }
const anth = { ...json, 'x-api-key': 'KTOKEN', 'anthropic-version': '2023-06-01' }
const bear = { ...json, authorization: 'Bearer KTOKEN' }
const goog = { ...json, 'x-goog-api-key': 'KTOKEN' }
const M = '/api/llm/a/v1/messages',
  R = '/api/llm/o/v1/responses',
  CH = '/api/llm/o/v1/chat/completions',
  G = '/api/llm/g/v1/models/gemini-3.5-flash:streamGenerateContent?alt=sse'
const add = (name, path, headers, body) =>
  captured.push({ name: 'by hand | ' + name, path, headers, body })
add(
  'spaces and escapes',
  M,
  anth,
  `{ "model" : "claude-opus-5-5", "messages" : [ {"role":"user","content":"\\u0068i \\/ \\u2028 x"} ], "max_tokens": 1000 }`,
)
add(
  'keys that are array indexes',
  M,
  anth,
  `{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}],"metadata":{"b":1,"10":2,"2":3,"a":4,"01":5,"4294967295":6,"4294967294":7}}`,
)
add(
  'a key given twice',
  M,
  anth,
  `{"model":"claude-opus-5-5","stream":false,"messages":[{"role":"user","content":"hi"}],"stream":true,"context_management":{"x":1}}`,
)
add(
  'line and paragraph separators, control characters',
  M,
  anth,
  `{"model":"claude-opus-5-5","system":"a\u2028b\u2029c\\b\\f\\u0001","messages":[{"role":"user","content":"hi"}]}`,
)
add(
  'system blocks: empty, cached, the line twice',
  M,
  anth,
  JSON.stringify({
    model: 'claude-opus-5-5',
    system: [
      { type: 'text', text: '  \n' },
      { type: 'text', text: 'Rules', cache_control: { type: 'ephemeral' } },
      { type: 'text', text: L },
      { type: 'text', text: L },
    ],
    messages: [{ role: 'user', content: 'hi' }],
  }),
)
add(
  'a system of a byte order mark',
  M,
  anth,
  JSON.stringify({ model: 'claude-opus-5-5', system: '\ufeff', messages: [] }),
)
add(
  'a system of a next line character',
  M,
  anth,
  JSON.stringify({ model: 'claude-opus-5-5', system: '\u0085', messages: [] }),
)
add(
  "pi's opening on a later line of an opened system",
  M,
  anth,
  JSON.stringify({
    model: 'claude-opus-5-5',
    system: L + '\n' + PI_OPENING + ' rest',
    messages: [],
  }),
)
add(
  'a summary block with a cache breakpoint',
  M,
  anth,
  JSON.stringify({
    model: 'claude-opus-5-5',
    messages: [
      {
        role: 'user',
        content: [
          { type: 'text', text: COMPACT + 's\n</summary>', cache_control: { type: 'ephemeral' } },
        ],
      },
    ],
  }),
)
add(
  'count_tokens',
  M + '/count_tokens',
  anth,
  JSON.stringify({
    model: 'claude-opus-5-5',
    system: 'x',
    messages: [{ role: 'user', content: 'hi' }],
  }),
)
add(
  'a model droid does not offer, Messages',
  M,
  anth,
  JSON.stringify({ model: 'claude-new-9', messages: [] }),
)
add(
  'a system message whose content is null',
  CH,
  bear,
  `{"model":"glm-5.3","messages":[{"role":"system","content":null},{"role":"user","content":"hi"}]}`,
)
add(
  'system parts with an image',
  CH,
  bear,
  JSON.stringify({
    model: 'glm-5.3',
    messages: [
      {
        role: 'system',
        content: [
          { type: 'text', text: 'a' },
          { type: 'image_url', image_url: { url: 'data:x' } },
        ],
      },
    ],
  }),
)
add(
  'system text parts, tool parts',
  CH,
  bear,
  JSON.stringify({
    model: 'kimi-k3',
    messages: [
      {
        role: 'system',
        content: [
          { type: 'text', text: 'a' },
          { type: 'text', text: 'b' },
        ],
      },
      { role: 'tool', tool_call_id: '1', content: [{ type: 'text', text: PI_OPENING }] },
    ],
  }),
)
add(
  'a model droid does not offer, chat',
  CH,
  bear,
  JSON.stringify({ model: 'new-chat-model', messages: [{ role: 'user', content: 'hi' }] }),
)
add('no messages', CH, bear, `{"model":"glm-5.3","messages":[]}`)
add(
  'no instructions, sampled',
  R,
  bear,
  `{"model":"gpt-5.5","input":"hi","temperature":0.2,"top_p":1}`,
)
add(
  'opened instructions, sampled',
  R,
  bear,
  JSON.stringify({ instructions: L, model: 'gpt-5.5', input: [], top_p: 1 }),
)
add(
  'null instructions, refused tool output',
  R,
  bear,
  `{"model":"grok-4.7","instructions":null,"input":[{"type":"function_call_output","call_id":"c","output":${JSON.stringify(PI_OPENING)}}]}`,
)
add('fast by its tier', R, bear, `{"model":"gpt-5.5","service_tier":"priority","input":"hi"}`)
add(
  'fast by its speed, betas already there',
  M,
  { ...anth, 'anthropic-beta': 'a,b' },
  `{"model":"claude-opus-5-5","speed":"fast","messages":[]}`,
)
add(
  'Gemini: a string system instruction',
  G,
  goog,
  JSON.stringify({
    contents: [{ role: 'user', parts: [{ text: 'hi' }] }],
    systemInstruction: 'Be brief.',
  }),
)
add(
  'Gemini: no system instruction, a stream field',
  G,
  goog,
  JSON.stringify({ stream: true, contents: [], generationConfig: { temperature: 1 } }),
)
add(
  'Gemini: parts with another field, refused function output',
  G,
  goog,
  JSON.stringify({
    systemInstruction: { role: 'user', parts: [{ text: 'a', thought: true }, { text: 'b' }] },
    contents: [
      {
        role: 'user',
        parts: [{ functionResponse: { name: 'r', response: { output: PI_OPENING } } }],
      },
    ],
  }),
)
add(
  'Gemini: a model droid does not offer',
  '/api/llm/g/v1/models/gemini-9-new:generateContent',
  goog,
  `{"contents":[]}`,
)
add(
  'a large integer and a float',
  M,
  anth,
  `{"model":"claude-opus-5-5","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"r","input":{"n":12345678901234567890,"f":1.5,"e":1e-7}}]}]}`,
)

await Bun.write(here + 'requests.json', JSON.stringify(captured, null, 1) + '\n')
console.log(captured.length, 'requests')

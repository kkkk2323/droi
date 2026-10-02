// Sessions where the agent worked through the Script tool: one whose run
// outlived its first minute and was picked up by WaitForScript, one that
// failed, and a reply that draws rich output with <json-render>. The nested
// calls are shaped as a real Daemon (droid 0.231) sends them. Shared by the
// web Client's and the Phone App's tests.
import {
  assistantMessage,
  session,
  toolCallMessage,
  toolResultMessage,
  userMessage,
  type MessageFixture,
} from './scenario'

const cwd = '/Users/dev/acme-web'
const LOG = '/Users/dev/.factory/artifacts/scripts/s1/toolu_script.log'

export const SCRIPT_SOURCE = [
  "const pkg = await tools.Read({ file_path: 'package.json' })",
  'text(pkg)',
  "const tests = await tools.Execute({ summary: 'Run the tests', command: 'pnpm test', riskLevel: 'low' })",
  'text(tests)',
  "const build = await tools.Execute({ summary: 'Build the app', command: 'pnpm build', riskLevel: 'low' })",
  'text(build)',
].join('\n')

/** A call the Script made: user-only, tagged with the run it belongs to. */
export function nestedCallMessage(
  runId: string,
  id: string,
  name: string,
  input: Record<string, unknown>,
): MessageFixture {
  return {
    ...toolCallMessage(id, name, input),
    visibility: 'user_only',
    content: [
      { type: 'tool_use', id, name, input, scriptExecution: { runId, outerToolUseId: runId } },
    ],
  }
}

export function toolResultBlocks(toolUseId: string, texts: string[]): MessageFixture {
  return {
    ...toolResultMessage(toolUseId, ''),
    content: [
      { type: 'tool_result', toolUseId, content: texts.map((text) => ({ type: 'text', text })) },
    ],
  }
}

export function scriptScenario() {
  const run = 'toolu_script'
  const audit = session('Audit the build', cwd, [
    userMessage('Check the package, run the tests, then build'),
    toolCallMessage(run, 'Script', {
      script: SCRIPT_SOURCE,
      inputs: { notes: 'Build with the release flags.' },
    }),
    nestedCallMessage(run, `${run}-1`, 'Read', { file_path: 'package.json' }),
    toolResultMessage(`${run}-1`, '{ "name": "acme-web" }'),
    nestedCallMessage(run, `${run}-3`, 'Execute', {
      summary: 'Run the tests',
      command: 'pnpm test',
      riskLevel: 'low',
    }),
    toolResultMessage(run, `{"toolCallId":"${run}","status":"running"}`),
    toolResultMessage(`${run}-3`, '42 passed\n\n[Process exited with code 0]'),
    assistantMessage('The tests are still running; waiting for them.'),
    toolCallMessage('toolu_wait', 'WaitForScript', { toolCallId: run }),
    nestedCallMessage(run, `${run}-5`, 'Execute', {
      summary: 'Build the app',
      command: 'pnpm build',
      riskLevel: 'low',
    }),
    toolResultMessage(`${run}-5`, 'build ok\n\n[Process exited with code 0]'),
    toolResultBlocks('toolu_wait', [
      '{ "name": "acme-web" }\n42 passed\nbuild ok',
      `[Script completed · 3 calls · 120 B in sandbox · 48 B emitted (40%) · retained: r1 Read package.json, r2 Execute Run the tests, r3 Execute Build the app · log: ${LOG}]`,
      `{"toolCallId":"${run}","status":"completed","result":null}`,
    ]),
    toolCallMessage('toolu_broken', 'Script', {
      script: 'const notes = await tools.Read({ file_path: missing })',
    }),
    toolResultBlocks('toolu_broken', [
      '[Script failed at 1:43 · 0 calls completed · 0 B emitted · log: /tmp/broken.log]',
      '{"toolCallId":"toolu_broken","status":"failed","error":"ReferenceError: missing is not defined"}',
    ]),
    assistantMessage('Tests pass and the build is green.'),
  ])
  const report = session('Release report', cwd, [
    userMessage('Summarize the release'),
    assistantMessage(
      [
        'Here is where the release stands.',
        '',
        `<json-render>${JSON.stringify(RELEASE_REPORT)}</json-render>`,
        '',
        'Ship once the last check is green.',
        '',
        '```html',
        '<json-render>{"root":"quoted"}</json-render>',
        '```',
      ].join('\n'),
    ),
  ])
  return { audit, report, sessions: [audit, report] }
}

export const RELEASE_REPORT = {
  root: 'card',
  elements: {
    card: {
      type: 'Card',
      props: { title: 'Release 1.26' },
      children: ['status', 'table', 'progress', 'bars'],
    },
    status: {
      type: 'StatusLine',
      props: { text: 'All checks passed', status: 'success' },
      children: [],
    },
    table: {
      type: 'Table',
      props: {
        columns: [
          { header: 'Package', key: 'name' },
          { header: 'Version', key: 'version' },
        ],
        rows: [
          { name: 'droi', version: '1.26.0' },
          { name: '@droi/mobile', version: '0.4.0' },
        ],
      },
      children: [],
    },
    progress: { type: 'ProgressBar', props: { progress: 0.75, label: 'Rollout' }, children: [] },
    bars: {
      type: 'BarChart',
      props: {
        data: [
          { label: 'macOS', value: 60 },
          { label: 'iPhone', value: 40 },
        ],
        showPercentage: true,
      },
      children: [],
    },
  },
}

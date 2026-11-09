// Writes apps/native/pkg/kernel/testdata/view_ops.json: views edited the way a running turn edits them, with
// the ops chord's tracker made for each edit, so the Go client's ApplyOps is checked against the real producer.
// Run from the repository root: bun apps/kernel/scripts/view-ops-fixture.ts && pnpm format
import { track } from '@earendil-works/chord/delta'

type Case = { name: string; base: unknown; ops: unknown; want: unknown }

const entry = (id: number, text: string) => ({
  id,
  conversationId: 7,
  kind: 'pi.assistant',
  model: [{ role: 'assistant', content: [{ type: 'text', text }] }],
  data: { usage: { input: 1794203798247, output: 2 } },
})

const base = () => ({
  conversation: { id: 7 },
  entries: [entry(1, 'one'), entry(2, 'two'), entry(3, 'three')],
  docs: {
    'pi.live': { generation: { message: { content: [{ type: 'text', text: 'Hel' }] } }, tools: [] },
    'pi.usage': { input: 10, output: 3 },
  },
  extra: { kept: false },
})

const edits: Array<[string, (s: any) => void]> = [
  [
    'append streamed text',
    (s) => (s.docs['pi.live'].generation.message.content[0].text += 'lo, 世界 👋'),
  ],
  ['push an entry', (s) => s.entries.push(entry(4, 'four'))],
  [
    'slide the window',
    (s) => {
      s.entries.shift()
      s.entries.push(entry(4, 'four'), entry(5, 'five'))
    },
  ],
  ['set a doc', (s) => (s.docs['pi.agent'] = { model: 'glm', thinkingLevel: 'high' })],
  ['delete a doc', (s) => delete s.docs['pi.usage']],
  ['change a number in a doc', (s) => (s.docs['pi.usage'].output = 99)],
  [
    'add a tool slot',
    (s) => s.docs['pi.live'].tools.push({ callId: 'c1', name: 'read', output: '' }),
  ],
  ['set the before cursor', (s) => (s.before = { id: 1 })],
  ['change a key View does not keep', (s) => (s.extra.kept = true)],
  ['edit inside an entry', (s) => (s.entries[1].data.usage.output = 5)],
  ['reverse the entries', (s) => s.entries.reverse()],
  [
    'many edits at once',
    (s) => {
      s.docs['pi.live'].generation.message.content[0].text += 'p'
      s.docs['pi.live'].generation.message.content.push({ type: 'thinking', thinking: 'hm' })
      s.entries.splice(1, 1)
      s.conversation.parent = { conversationId: 3, at: 9 }
    },
  ],
]

const cases: Case[] = []
for (const [name, edit] of edits) {
  const start = base()
  const tracker = track(structuredClone(start))
  const change = tracker.beginChange()
  edit(change.state)
  const prepared = change.prepare()
  tracker.adopt(prepared)
  cases.push({ name, base: start, ops: prepared.ops, want: tracker.value })
}

// Several pushes in a row, as a stream folds them: each case's base is the last one's result.
{
  const tracker = track(structuredClone(base()))
  let previous: unknown = base()
  for (const piece of ['lo', ' wor', 'ld']) {
    const change = tracker.beginChange()
    ;(change.state as any).docs['pi.live'].generation.message.content[0].text += piece
    const prepared = change.prepare()
    tracker.adopt(prepared)
    cases.push({
      name: 'stream ' + JSON.stringify(piece),
      base: previous,
      ops: prepared.ops,
      want: tracker.value,
    })
    previous = structuredClone(tracker.value)
  }
}

await Bun.write(
  'apps/native/pkg/kernel/testdata/view_ops.json',
  JSON.stringify(cases, null, 1) + '\n',
)
console.log(cases.length, 'cases')
for (const c of cases) console.log(c.name, JSON.stringify(c.ops).slice(0, 160))

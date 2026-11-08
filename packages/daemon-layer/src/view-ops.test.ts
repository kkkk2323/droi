import { applyImmutable, track } from '@earendil-works/chord/delta'
import { describe, expect, it } from 'vitest'
import { applyOps, OpError } from './view-ops'

// A small deterministic random source, so a failure replays.
function random(seed: number) {
  let s = seed >>> 0
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0
    return s / 2 ** 32
  }
}

type Doc = {
  entries: { id: number; text: string; tags: string[] }[]
  docs: Record<string, unknown>
}

describe('applyOps', () => {
  it("turns one view into the next exactly as the Kernel's tracker does", () => {
    for (let seed = 1; seed <= 60; seed++) {
      const next = random(seed)
      const tracker = track<Doc>({
        entries: [{ id: 1, text: 'a', tags: [] }],
        docs: { live: { n: 0 } },
      })
      let replica: Doc = JSON.parse(JSON.stringify(tracker.value))
      for (let step = 0; step < 40; step++) {
        const change = tracker.beginChange()
        const state = change.state
        const roll = next()
        if (roll < 0.25) state.entries.push({ id: step + 2, text: 'new', tags: ['x'] })
        else if (roll < 0.45 && state.entries.length > 0) {
          const e = state.entries[Math.floor(next() * state.entries.length)]!
          e.text += ' more'
        } else if (roll < 0.55 && state.entries.length > 2) state.entries.splice(0, 2)
        else if (roll < 0.65 && state.entries.length > 1) state.entries.reverse()
        else if (roll < 0.75)
          state.docs['live'] = { n: step, run: step % 2 === 0 ? null : { taskId: step } }
        else if (roll < 0.85) delete state.docs['live']
        else if (roll < 0.95 && state.entries.length > 0) state.entries[0]!.tags.unshift('t' + step)
        else state.entries.sort((a, b) => b.id - a.id)
        const prepared = change.prepare()
        tracker.adopt(prepared)
        // The wire carries the ops as JSON.
        const ops = JSON.parse(JSON.stringify(prepared.ops))
        const before = JSON.stringify(replica)
        const applied = applyOps(replica, ops)
        expect(JSON.stringify(replica)).toBe(before)
        expect(applied).toEqual(
          JSON.parse(JSON.stringify(applyImmutable(prepared.base, prepared.ops))),
        )
        expect(applied).toEqual(JSON.parse(JSON.stringify(tracker.value)))
        replica = applied
      }
    }
  })

  it('keeps what an op did not touch, and replaces the root on r', () => {
    const view = { entries: [{ id: 1 }], docs: { a: { x: 1 } } }
    const next = applyOps(view, [['s', ['docs', 'a', 'x'], 2]])
    expect(next.entries).toBe(view.entries)
    expect(next.docs).not.toBe(view.docs)
    expect(view.docs.a.x).toBe(1)
    expect(applyOps(view, [['r', { other: true }]])).toEqual({ other: true })
  })

  it('appends to and trims a string', () => {
    const view = { text: 'hello' }
    expect(
      applyOps(view, [
        ['a', ['text'], ' world'],
        ['t', ['text'], 6],
      ]),
    ).toEqual({ text: 'world' })
  })

  it('refuses an op the view it holds cannot take', () => {
    expect(() => applyOps({ entries: [] }, [['s', ['entries', 3], 1]])).toThrow(OpError)
    expect(() => applyOps({ a: 1 }, [['a', ['a'], 'x']])).toThrow(OpError)
    expect(() => applyOps({}, [['s', ['__proto__', 'polluted'], true]])).toThrow(OpError)
    expect(() => applyOps({}, [['q', ['x']]])).toThrow(OpError)
    expect(({} as Record<string, unknown>)['polluted']).toBeUndefined()
  })
})

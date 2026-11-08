import { AUTONOMIES, type Autonomy, type Policy, type Rule } from './decide.ts'
import { invalidParams } from '../protocol/rpc.ts'

/** A change `policy.set` carries: a field left out keeps what was there. */
export type PolicyChange = { autonomy?: Autonomy; rules?: Rule[] }

type Own = { autonomy?: Autonomy; rules: Rule[] }

/**
 * The kernel's copy of the Host's approval policy: a default, and what each conversation has of its own.
 *
 * Nothing here is stored: the Host owns the policy, pushes it after every connect, and stores what
 * `policy.changed` says. Until the first push nothing above `ask` is let through (`ready`): Pi resumes an
 * interrupted run by itself after a restart, which can come before the Host has said anything.
 */
export class PolicyStore {
  #default: Policy = { autonomy: 'ask', rules: [] }
  #defaultSet = false
  readonly #own = new Map<number, Own>()
  readonly #ready: Promise<void>
  #markReady!: () => void
  #isReady = false
  readonly #onChanged: (id: number | null, policy: Policy) => void

  constructor(options: {
    onChanged: (id: number | null, policy: Policy) => void
    /** A policy known at start (a test, a config file): ready at once. */
    initial?: Policy
    /** How long to wait for the Host's first push before settling at `ask`. */
    waitMs?: number
  }) {
    this.#onChanged = options.onChanged
    this.#ready = new Promise((resolve) => {
      this.#markReady = () => {
        this.#isReady = true
        resolve()
      }
    })
    if (options.initial !== undefined) {
      this.#default = clone(options.initial)
      this.#defaultSet = true
      this.#markReady()
    } else {
      const timer = setTimeout(() => this.#markReady(), options.waitMs ?? 30_000)
      timer.unref?.()
    }
  }

  /** Resolves at the first push, or when waiting for one has run out. */
  ready(): Promise<void> {
    return this.#ready
  }

  /** The default as the Host last set it; undefined before it did. */
  defaultPolicy(): Policy | undefined {
    return this.#defaultSet ? clone(this.#default) : undefined
  }

  /** What a conversation's calls are judged by: its own level, else the default's, and both sets of rules. */
  effective(id: number): Policy {
    const own = this.#own.get(id)
    return {
      autonomy: own?.autonomy ?? this.#default.autonomy,
      rules: [...this.#default.rules, ...(own?.rules ?? [])],
    }
  }

  /** The snapshot of one scope, as `policy.changed` carries it. */
  snapshot(id: number | null): Policy {
    if (id === null) return clone(this.#default)
    const own = this.#own.get(id)
    return { autonomy: own?.autonomy ?? this.#default.autonomy, rules: [...(own?.rules ?? [])] }
  }

  /** What `conversation.open` answers: the snapshot, once the Host has said anything. */
  known(id: number): Policy | undefined {
    return this.#isReady && (this.#defaultSet || this.#own.has(id)) ? this.snapshot(id) : undefined
  }

  /** `policy.set`: apply a change, announce it, answer the whole snapshot. */
  set(id: number | null, change: PolicyChange): Policy {
    if (id === null) {
      if (change.autonomy !== undefined) this.#default.autonomy = change.autonomy
      if (change.rules !== undefined) this.#default.rules = clone(change.rules)
      this.#defaultSet = true
    } else {
      const own = this.#own.get(id) ?? { rules: [] }
      if (change.autonomy !== undefined) own.autonomy = change.autonomy
      if (change.rules !== undefined) own.rules = clone(change.rules)
      this.#own.set(id, own)
    }
    if (!this.#isReady) this.#markReady()
    return this.#changed(id)
  }

  /** An "always" answer: one more rule of that conversation's own, announced like any change. */
  addRule(id: number, rule: Rule): Policy {
    const own = this.#own.get(id) ?? { rules: [] }
    if (!own.rules.some((existing) => sameRule(existing, rule))) own.rules.push(rule)
    this.#own.set(id, own)
    return this.#changed(id)
  }

  #changed(id: number | null): Policy {
    const snapshot = this.snapshot(id)
    this.#onChanged(id, snapshot)
    return snapshot
  }
}

/** `policy.set`'s `policy`, checked. */
export function parsePolicyChange(value: unknown): PolicyChange {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw invalidParams([{ path: 'policy', message: 'an object is required' }])
  }
  const raw = value as Record<string, unknown>
  const change: PolicyChange = {}
  if (raw['autonomy'] !== undefined) {
    if (!AUTONOMIES.includes(raw['autonomy'] as Autonomy)) {
      throw invalidParams([{ path: 'policy.autonomy', message: 'ask, edits, safe or all' }])
    }
    change.autonomy = raw['autonomy'] as Autonomy
  }
  if (raw['rules'] !== undefined) {
    if (!Array.isArray(raw['rules'])) {
      throw invalidParams([{ path: 'policy.rules', message: 'a list of rules is required' }])
    }
    change.rules = raw['rules'].map((rule, index) => parseRule(rule, `policy.rules.${index}`))
  }
  return change
}

function parseRule(value: unknown, path: string): Rule {
  const rule = value as { kind?: unknown; tool?: unknown; path?: unknown } | null
  if (rule?.kind === 'tool' && typeof rule.tool === 'string' && rule.tool !== '') {
    return { kind: 'tool', tool: rule.tool }
  }
  if (rule?.kind === 'file' && typeof rule.path === 'string' && rule.path !== '') {
    return { kind: 'file', path: rule.path }
  }
  throw invalidParams([{ path, message: '{kind:"tool",tool} or {kind:"file",path}' }])
}

function sameRule(a: Rule, b: Rule): boolean {
  return a.kind === 'tool'
    ? b.kind === 'tool' && a.tool === b.tool
    : b.kind === 'file' && a.path === b.path
}

function clone<T>(value: T): T {
  return structuredClone(value)
}

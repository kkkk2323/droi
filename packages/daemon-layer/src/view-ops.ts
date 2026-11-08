// Pi's view ops (chord's delta vocabulary), applied to the view a `"delta"` subscriber
// keeps. The Kernel sends the ops between two of its views; applying them in order to the
// last view gives the next one. A view is never changed in place: every container on an
// op's path is copied first, so a component holding the old view still holds the old view.

export type Path = ReadonlyArray<string | number>

export type Op =
  | readonly ['r', unknown]
  | readonly ['s', Path, unknown]
  | readonly ['d', Path]
  | readonly ['a', Path, string]
  | readonly ['t', Path, number]
  | readonly ['p', Path, number, number, readonly unknown[]]
  | readonly ['m', Path, readonly number[]]

/** An op that cannot be applied to the view held: the view is no longer the Kernel's. */
export class OpError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'OpError'
  }
}

const RESERVED = new Set(['__proto__', 'constructor', 'prototype'])

type Container = Record<string, unknown> | unknown[]

/** `ops` applied to `root` in order; `root` and what it holds are left as they were. */
export function applyOps<T>(root: T, ops: readonly unknown[]): T {
  let value: unknown = root
  // Containers this call copied, which later ops may change in place.
  const owned = new WeakSet<object>()
  const own = (container: Container): Container => {
    if (owned.has(container)) return container
    const copy = Array.isArray(container) ? container.slice() : { ...container }
    owned.add(copy)
    return copy
  }
  // The container at path, copied (with every container above it) so it may be changed.
  const reach = (path: Path): Container => {
    if (!isContainer(value)) throw new OpError('the view is not an object')
    let node = own(value)
    value = node
    for (const segment of path) {
      const child = read(node, segment)
      if (!isContainer(child)) throw new OpError(`no container at ${JSON.stringify(path)}`)
      const copy = own(child)
      write(node, segment, copy)
      node = copy
    }
    return node
  }
  for (const raw of ops) {
    const op = raw as Op
    if (!Array.isArray(op)) throw new OpError('an op is not a tuple')
    if (op[0] === 'r') {
      value = op[1]
      continue
    }
    const path = op[1]
    if (!Array.isArray(path)) throw new OpError('an op has no path')
    checkPath(path)
    switch (op[0]) {
      case 'p': {
        const target = reach(path)
        const [, , index, remove, items] = op
        if (!Array.isArray(target) || index > target.length)
          throw new OpError('a splice past its array')
        target.splice(index, remove, ...items)
        break
      }
      case 'm': {
        const target = reach(path)
        const order = op[2]
        if (!Array.isArray(target) || target.length !== order.length)
          throw new OpError('a reorder of another array')
        const previous = target.slice()
        order.forEach((from, i) => {
          target[i] = previous[from]
        })
        break
      }
      case 's':
      case 'd':
      case 'a':
      case 't': {
        if (path.length === 0) throw new OpError('an op on the root')
        const parent = reach(path.slice(0, -1))
        const key = path[path.length - 1]!
        if (Array.isArray(parent)) {
          if (typeof key !== 'number' || key > parent.length)
            throw new OpError('an index past its array')
        }
        if (op[0] === 's') {
          write(parent, key, op[2])
        } else if (op[0] === 'd') {
          if (Array.isArray(parent)) {
            if ((key as number) >= parent.length) throw new OpError('a delete past its array')
            parent.splice(key as number, 1)
          } else delete parent[key as string]
        } else {
          const current = read(parent, key)
          if (typeof current !== 'string') throw new OpError('a text op on something else')
          write(parent, key, op[0] === 'a' ? current + op[2] : current.slice(op[2]))
        }
        break
      }
      default:
        throw new OpError(`an unknown op: ${String((op as unknown[])[0])}`)
    }
  }
  return value as T
}

function isContainer(value: unknown): value is Container {
  return typeof value === 'object' && value !== null
}

function checkPath(path: Path): void {
  for (const segment of path) {
    if (typeof segment === 'string') {
      if (RESERVED.has(segment)) throw new OpError(`an unsafe path segment: ${segment}`)
    } else if (!Number.isInteger(segment) || segment < 0) {
      throw new OpError(`an unsafe path segment: ${String(segment)}`)
    }
  }
}

function read(container: Container, key: string | number): unknown {
  if (Array.isArray(container)) return typeof key === 'number' ? container[key] : undefined
  return Object.hasOwn(container, key) ? container[key as string] : undefined
}

function write(container: Container, key: string | number, value: unknown): void {
  if (Array.isArray(container)) {
    container[key as number] = value
    return
  }
  // An own data property, never a setter the prototype chain might have.
  Object.defineProperty(container, key, {
    value,
    writable: true,
    enumerable: true,
    configurable: true,
  })
}

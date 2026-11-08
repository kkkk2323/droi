/**
 * A tool whose input schema has anyOf, oneOf or allOf at its root cannot be offered as it is. pi-ai's Anthropic
 * adapter sends only the root's `properties` and `required`, so the model sees a tool that takes nothing; OpenAI's
 * function tools need an object at the root. Such a schema is offered under one argument, `arguments`, and a call's
 * arguments are taken out of it before they reach the server.
 *
 * The relocation of document pointers is ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
 * anthropic-tools.mjs, which does the same per request on Factory's Anthropic route.
 */

export const WRAP_ARGUMENT = 'arguments'

const compositions = ['anyOf', 'oneOf', 'allOf']
const schemaMaps = ['properties', 'patternProperties', '$defs', 'definitions', 'dependentSchemas']
const schemaLists = ['anyOf', 'oneOf', 'allOf', 'prefixItems']
const schemaValues = [
  'additionalProperties',
  'unevaluatedProperties',
  'propertyNames',
  'contains',
  'items',
  'additionalItems',
  'unevaluatedItems',
  'not',
  'if',
  'then',
  'else',
  'contentSchema',
]

type Schema = Record<string, unknown>

const isObject = (v: unknown): v is Schema =>
  v !== null && typeof v === 'object' && !Array.isArray(v)

class RecursiveSchema extends Error {}

// Moves a "#" or "#/…" $ref or $dynamicRef under the argument. A resource with its own $id keeps its references,
// and literal data (const, default, examples) is not visited.
function rebase(schema: unknown, relocate: boolean, legacy: boolean): void {
  if (!isObject(schema)) return
  const id = schema['$id'] ?? (legacy ? schema['id'] : undefined)
  if (typeof id === 'string' && id.split('#')[0]) relocate = false
  if (relocate) {
    // Draft 2019-09's recursive anchor must stay a resource root: moving it would change what it resolves to.
    if (schema['$recursiveAnchor'] === true || Object.hasOwn(schema, '$recursiveRef'))
      throw new RecursiveSchema()
    for (const key of ['$ref', '$dynamicRef']) {
      const ref = schema[key]
      if (typeof ref !== 'string' || !ref.startsWith('#')) continue
      const pointer = decodeURIComponent(ref.slice(1))
      if (pointer === '' || pointer.startsWith('/'))
        schema[key] = `#/properties/${WRAP_ARGUMENT}${ref.slice(1)}`
    }
  }
  const visit = (child: unknown) => rebase(child, relocate, legacy)
  for (const key of schemaMaps) if (isObject(schema[key])) Object.values(schema[key]).forEach(visit)
  for (const key of schemaLists) if (Array.isArray(schema[key])) schema[key].forEach(visit)
  for (const key of schemaValues) {
    const value = schema[key]
    if (Array.isArray(value)) value.forEach(visit)
    else visit(value)
  }
  if (isObject(schema['dependencies'])) Object.values(schema['dependencies']).forEach(visit)
}

/**
 * The parameters to offer for a tool whose input schema is `schema`, and whether they wrap it. A schema with no
 * composition at its root, and one that cannot be moved (a recursive anchor without $id), come back as they are.
 */
export function wrapToolSchema(schema: Schema): { parameters: Schema; wrapped: boolean } {
  if (!compositions.some((key) => Object.hasOwn(schema, key)))
    return { parameters: schema, wrapped: false }
  const moved = structuredClone(schema)
  const draft = typeof moved['$schema'] === 'string' ? moved['$schema'] : ''
  try {
    rebase(moved, true, /draft-0[34]/.test(draft))
  } catch (error) {
    if (error instanceof RecursiveSchema || error instanceof URIError)
      return { parameters: schema, wrapped: false }
    throw error
  }
  return {
    parameters: {
      type: 'object',
      properties: { [WRAP_ARGUMENT]: moved },
      required: [WRAP_ARGUMENT],
      additionalProperties: false,
      ...(moved['$schema'] ? { $schema: moved['$schema'] } : {}),
    },
    wrapped: true,
  }
}

/** A wrapped tool's call arguments as the server's schema says them. */
export function unwrapArguments(args: Record<string, unknown>): Record<string, unknown> {
  const inner = args[WRAP_ARGUMENT]
  return isObject(inner) ? inner : {}
}

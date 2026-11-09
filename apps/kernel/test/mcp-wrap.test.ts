import { describe, expect, test } from 'bun:test'
import { validateToolArguments } from '@earendil-works/pi-ai'
import { unwrapArguments, wrapToolSchema } from '../src/mcp/wrap.ts'

// The request half of @magpie-community/opencode-factory-auth 0.1.42's schema.test.mjs, where the wrapping is done
// for the Kernel: on the tool's definition, not on each request. Pi resolves the wrapped schema's references when it
// checks a call, so the cases also run a call through Pi's own validator.

const object = {
  type: 'object',
  properties: { mode: { const: 'view' }, id: { type: 'string' } },
  required: ['mode', 'id'],
  additionalProperties: false,
}

// accepts reports whether Pi takes a call with args for a tool with these parameters.
function accepts(parameters: Record<string, unknown>, args: Record<string, unknown>): boolean {
  try {
    validateToolArguments(
      { name: 't', description: '', parameters } as never,
      {
        type: 'toolCall',
        id: 'c',
        name: 't',
        arguments: args,
      } as never,
    )
    return true
  } catch {
    return false
  }
}

describe('a tool schema with a composition at its root', () => {
  for (const keyword of ['anyOf', 'oneOf', 'allOf']) {
    test(`${keyword} goes under one argument, and a call's arguments come out of it`, () => {
      const create = {
        type: 'object',
        properties: { mode: { const: 'create' }, name: { type: 'string' } },
        required: ['mode', 'name'],
      }
      const schema = {
        [keyword]:
          keyword === 'allOf'
            ? [object, { properties: { id: { pattern: '^task_' } } }]
            : [object, create],
      }
      const { parameters, wrapped } = wrapToolSchema(schema)
      expect(wrapped).toBe(true)
      expect(parameters).toEqual({
        type: 'object',
        properties: { arguments: schema },
        required: ['arguments'],
        additionalProperties: false,
      })
      expect(accepts(parameters, { arguments: { mode: 'view', id: 'task_1' } })).toBe(true)
      expect(accepts(parameters, { mode: 'view', id: 'task_1' })).toBe(false)
      expect(unwrapArguments({ arguments: { mode: 'view', id: 'task_1' } })).toEqual({
        mode: 'view',
        id: 'task_1',
      })
    })
  }

  test('rebases document pointers, respecting resource ids, anchors and literal data', () => {
    const view = {
      ...object,
      $anchor: 'anchor',
      properties: { ...object.properties, nested: { $ref: '#' } },
    }
    const literal = {
      const: { $ref: '#/$defs/untouched', anyOf: [{ $ref: 'literal' }] },
      default: { $ref: 'default' },
      examples: [{ $ref: 'example' }],
    }
    const schema = {
      $schema: 'https://json-schema.org/draft/2020-12/schema',
      anyOf: [
        { $ref: '#/$defs/view' },
        { $ref: '#%2F$defs%2Fview' },
        { $ref: '#anchor' },
        { $ref: 'https://example.test/other.json' },
      ],
      $defs: {
        view,
        resource: {
          $id: 'child.json',
          $defs: { value: { type: 'string' } },
          anyOf: [{ $ref: '#/$defs/value' }],
        },
        holder: { properties: { literal, list: { items: { $ref: '#/$defs/view' } } } },
      },
    }
    const { parameters } = wrapToolSchema(schema)
    const original = (parameters['properties'] as Record<string, Record<string, any>>)['arguments']!
    expect(parameters['$schema']).toBe(schema.$schema)
    expect(original['anyOf']).toEqual([
      { $ref: '#/properties/arguments/$defs/view' },
      { $ref: '#/properties/arguments%2F$defs%2Fview' },
      schema.anyOf[2],
      schema.anyOf[3],
    ])
    expect(original['$defs'].view.properties.nested.$ref).toBe('#/properties/arguments')
    expect(original['$defs'].resource).toEqual(schema.$defs.resource)
    expect(original['$defs'].holder.properties.literal).toEqual(literal)
    expect(original['$defs'].holder.properties.list.items).toEqual({
      $ref: '#/properties/arguments/$defs/view',
    })
    // the server's own schema is not changed
    expect(schema.anyOf[0]!.$ref).toBe('#/$defs/view')
  })

  test('a schema with an $id keeps its references, a fragment-only one too', () => {
    const v = { type: 'object', properties: { v: { type: 'string' } }, required: ['v'] }
    for (const id of ['https://example.test/tool.json', '#root', '#', '']) {
      const schema = { $id: id, $defs: { view: v }, oneOf: [{ $ref: '#/$defs/view' }] }
      const { parameters, wrapped } = wrapToolSchema(schema)
      expect(wrapped).toBe(true)
      expect((parameters['properties'] as any).arguments).toEqual(schema)
      expect(accepts(parameters, { arguments: { v: 'x' } })).toBe(true)
      expect(accepts(parameters, { arguments: { w: 'x' } })).toBe(false)
    }
  })

  test('Pi checks a call against wrapped schemas MCP servers send', () => {
    const zod = {
      $schema: 'http://json-schema.org/draft-07/schema#',
      definitions: {
        item: { type: 'object', properties: { n: { type: 'number' } }, required: ['n'] },
      },
      anyOf: [
        { $ref: '#/definitions/item' },
        { type: 'object', properties: { all: { const: true } }, required: ['all'] },
      ],
    }
    const recursive = {
      anyOf: [
        { type: 'object', properties: { leaf: { type: 'string' } }, required: ['leaf'] },
        {
          type: 'object',
          properties: { children: { type: 'array', items: { $ref: '#' } } },
          required: ['children'],
        },
      ],
    }
    const zodWrapped = wrapToolSchema(zod).parameters
    expect(accepts(zodWrapped, { arguments: { n: 3 } })).toBe(true)
    expect(accepts(zodWrapped, { arguments: { all: true } })).toBe(true)
    expect(accepts(zodWrapped, { arguments: { n: 'three' } })).toBe(false)
    const tree = wrapToolSchema(recursive).parameters
    expect(
      accepts(tree, { arguments: { children: [{ leaf: 'a' }, { children: [{ leaf: 'b' }] }] } }),
    ).toBe(true)
    expect(accepts(tree, { arguments: { children: [{ nope: 1 }] } })).toBe(false)
  })

  test('a recursive schema without $id is offered as it came rather than relocated wrong', () => {
    for (const schema of [
      { anyOf: [{ $recursiveRef: '#' }], $recursiveAnchor: true },
      { anyOf: [object], $recursiveAnchor: true },
    ]) {
      expect(wrapToolSchema(schema)).toEqual({ parameters: schema, wrapped: false })
    }
  })

  test('a schema whose root has properties, or nested compositions only, is offered as it came', () => {
    for (const schema of [
      // the root's properties already reach every route: the union only says which are required
      {
        type: 'object',
        properties: { id: { type: 'string' }, name: { type: 'string' } },
        anyOf: [{ required: ['id'] }, { required: ['name'] }],
      },
      { type: 'object', properties: { value: { anyOf: [{ type: 'string' }, { type: 'null' }] } } },
      { type: 'object' },
    ]) {
      expect(wrapToolSchema(schema)).toEqual({ parameters: schema, wrapped: false })
    }
  })

  test('a call whose arguments are not an object is refused, not sent as nothing', () => {
    expect(() => unwrapArguments({ arguments: 'hello' })).toThrow('arguments')
    expect(() => unwrapArguments({})).toThrow('arguments')
  })
})

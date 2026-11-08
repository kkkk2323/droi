import { describe, expect, test } from 'bun:test'
import { unwrapArguments, wrapToolSchema } from '../src/mcp/wrap.ts'

// The request half of @magpie-community/opencode-factory-auth 0.1.42's schema.test.mjs, where the wrapping is done
// for the Kernel: on the tool's definition, not on each request.

const object = {
  type: 'object',
  properties: { mode: { const: 'view' }, id: { type: 'string' } },
  required: ['mode', 'id'],
  additionalProperties: false,
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
      expect(unwrapArguments({ arguments: { mode: 'view', id: 'task_1' } })).toEqual({
        mode: 'view',
        id: 'task_1',
      })
    })
  }

  test('rebases document pointers, respecting resource ids, anchors and literal data', () => {
    const schema = {
      $schema: 'https://json-schema.org/draft/2020-12/schema',
      type: 'object',
      anyOf: [
        { $ref: '#/$defs/view' },
        { $ref: '#%2F$defs%2Fview' },
        { $ref: '#anchor' },
        { $ref: 'https://example.test/other.json' },
      ],
      $defs: {
        view: {
          ...object,
          $anchor: 'anchor',
          properties: { ...object.properties, nested: { $ref: '#' } },
        },
        resource: {
          $id: 'child.json',
          $defs: { value: { type: 'string' } },
          anyOf: [{ $ref: '#/$defs/value' }],
        },
      },
      properties: {
        literal: {
          const: { $ref: '#/$defs/untouched', anyOf: [{ $ref: 'literal' }] },
          default: { $ref: 'default' },
          examples: [{ $ref: 'example' }],
        },
        list: { items: { $ref: '#/$defs/view' } },
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
    expect(original['properties'].literal).toEqual(schema.properties.literal)
    expect(original['properties'].list.items).toEqual({ $ref: '#/properties/arguments/$defs/view' })
    // the server's own schema is not changed
    expect(schema.anyOf[0]!.$ref).toBe('#/$defs/view')
  })

  test('keeps a root $id resource and its references intact, but not a fragment-only one', () => {
    const schema = {
      $id: 'https://example.test/tool.json',
      $defs: { view: object },
      oneOf: [{ $ref: '#/$defs/view' }],
    }
    expect((wrapToolSchema(schema).parameters['properties'] as any).arguments).toEqual(schema)
    const fragment = { $id: '#root', $defs: { view: object }, oneOf: [{ $ref: '#/$defs/view' }] }
    expect((wrapToolSchema(fragment).parameters['properties'] as any).arguments.oneOf).toEqual([
      { $ref: '#/properties/arguments/$defs/view' },
    ])
  })

  test('a recursive schema without $id is offered as it came rather than relocated wrong', () => {
    for (const schema of [
      { anyOf: [{ $recursiveRef: '#' }], $recursiveAnchor: true },
      { anyOf: [object], $recursiveAnchor: true },
    ]) {
      expect(wrapToolSchema(schema)).toEqual({ parameters: schema, wrapped: false })
    }
  })

  test('an ordinary schema, and one with nested compositions only, are offered as they came', () => {
    for (const schema of [
      { type: 'object', properties: { value: { anyOf: [{ type: 'string' }, { type: 'null' }] } } },
      { type: 'object' },
    ]) {
      expect(wrapToolSchema(schema)).toEqual({ parameters: schema, wrapped: false })
    }
  })
})

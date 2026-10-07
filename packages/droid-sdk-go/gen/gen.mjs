// Generates the Go protocol package from the zod schemas bundled in
// @factory/droid-sdk, so a new SDK release is one `go generate` away.
//
//   node gen.mjs <exposed chunk.mjs> <methods.json> <out dir>
import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const [chunkPath, methodsPath, outDir] = process.argv.slice(2)
const { __schemas: all, __enums: enums } = await import(pathToFileURL(path.resolve(chunkPath)).href)
const enumNames = new Map() // TS enum object -> its name
for (const [k, v] of Object.entries(enums))
  if (v && typeof v === 'object' && !enumNames.has(v)) enumNames.set(v, k)
const methods = JSON.parse(fs.readFileSync(methodsPath, 'utf8'))
const sdkVersion = process.env.SDK_VERSION ?? 'unknown'

// ---------- naming ----------

const INITIALISMS = {
  Id: 'ID',
  Ids: 'IDs',
  Url: 'URL',
  Urls: 'URLs',
  Api: 'API',
  Mcp: 'MCP',
  Json: 'JSON',
  Http: 'HTTP',
  Https: 'HTTPS',
  Uri: 'URI',
  Cwd: 'Cwd',
  Llm: 'LLM',
  Pr: 'PR',
  Ui: 'UI',
  Os: 'OS',
  Ssh: 'SSH',
  Sha: 'SHA',
  Ttl: 'TTL',
  Ip: 'IP',
  Html: 'HTML',
  Sdk: 'SDK',
  Cli: 'CLI',
  Tui: 'TUI',
  Oauth: 'OAuth',
  Ws: 'WS',
}
function words(s) {
  return String(s)
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1 $2')
    .split(/[^A-Za-z0-9]+/)
    .filter(Boolean)
}
function pascal(s) {
  let out = words(s)
    .map((w) => w[0].toUpperCase() + w.slice(1))
    .map(
      (w) => INITIALISMS[w] ?? (w === w.toUpperCase() && w.length > 1 && /[A-Z]/.test(w) ? w : w),
    )
    .join('')
  if (!out) out = 'X'
  if (/^[0-9]/.test(out)) out = 'V' + out
  return out
}

const taken = new Map() // Go name -> schema
const nameOf = new Map() // schema -> Go name
function claim(schema, want) {
  let name = want
  for (let i = 2; taken.has(name) && taken.get(name) !== schema; i++) name = want + i
  taken.set(name, schema)
  nameOf.set(schema, name)
  return name
}

const varNames = new Map() // schema -> [var names]
for (const [k, v] of Object.entries(all)) {
  if (!v || typeof v !== 'object' || !v._def) continue
  if (!varNames.has(v)) varNames.set(v, [])
  varNames.get(v).push(k)
}
function varName(schema) {
  const names = varNames.get(schema)
  if (!names) return null
  const base = names
    .map((n) => n.replace(/Schema\d*$/, '').replace(/\d+$/, ''))
    .sort((a, b) => a.length - b.length)[0]
  return base ? pascal(base) : null
}

// ---------- zod walking ----------

const tn = (s) => s._def.typeName
function unwrap(s) {
  // Peels wrappers that do not change the JSON shape; reports optionality.
  let optional = false,
    nullable = false
  for (;;) {
    const t = tn(s)
    if (t === 'ZodOptional') {
      optional = true
      s = s._def.innerType
      continue
    }
    if (t === 'ZodDefault') {
      optional = true
      s = s._def.innerType
      continue
    }
    if (t === 'ZodNullable') {
      nullable = true
      s = s._def.innerType
      continue
    }
    if (t === 'ZodEffects') {
      s = s._def.schema
      continue
    }
    if (t === 'ZodBranded') {
      s = s._def.type
      continue
    }
    if (t === 'ZodReadonly' || t === 'ZodCatch') {
      s = s._def.innerType
      continue
    }
    if (t === 'ZodPipeline') {
      s = s._def.in
      continue
    }
    if (t === 'ZodLazy') {
      s = s._def.getter()
      continue
    }
    return { s, optional, nullable }
  }
}
function shapeOf(obj) {
  return typeof obj._def.shape === 'function' ? obj._def.shape() : obj.shape
}
function literalUnionValues(s) {
  const t = tn(s)
  if (t === 'ZodLiteral') return [s._def.value]
  if (t === 'ZodEnum') return [...s._def.values]
  if (t === 'ZodNativeEnum')
    return Object.entries(s._def.values)
      .filter(([k]) => !/^\d+$/.test(k))
      .map(([, v]) => v)
  if (t === 'ZodUnion') {
    const out = []
    for (const o of s._def.options) {
      const u = unwrap(o).s
      const v = literalUnionValues(u)
      if (!v) return null
      out.push(...v)
    }
    return out
  }
  return null
}
function discriminatorOf(s) {
  const t = tn(s)
  if (t === 'ZodDiscriminatedUnion') return s._def.discriminator
  if (t !== 'ZodUnion') return null
  const opts = s._def.options.map((o) => unwrap(o).s)
  if (!opts.every((o) => tn(o) === 'ZodObject')) return null
  for (const key of ['type', 'kind', 'status', 'method', 'success']) {
    const vals = opts.map((o) => {
      const f = shapeOf(o)[key]
      return f && tn(unwrap(f).s) === 'ZodLiteral' ? unwrap(f).s._def.value : undefined
    })
    if (
      (vals.every((v) => typeof v === 'string') || vals.every((v) => typeof v === 'boolean')) &&
      new Set(vals).size === vals.length
    )
      return key
  }
  return null
}
function unionOptions(s) {
  return (tn(s) === 'ZodDiscriminatedUnion' ? [...s._def.options] : s._def.options).map(
    (o) => unwrap(o).s,
  )
}

// ---------- Go emission ----------

const decls = [] // Go source chunks, in order
const emitted = new Set()
const queue = []

function typeRef(schema, hint) {
  // Returns the Go type for a schema, emitting named types as needed.
  const { s } = unwrap(schema)
  const t = tn(s)
  switch (t) {
    case 'ZodString':
    case 'ZodDate':
      return 'string'
    case 'ZodNumber':
      return s._def.checks?.some((c) => c.kind === 'int') ? 'int64' : 'float64'
    case 'ZodBigInt':
      return 'int64'
    case 'ZodBoolean':
      return 'bool'
    case 'ZodAny':
    case 'ZodUnknown':
    case 'ZodTuple':
    case 'ZodNull':
    case 'ZodUndefined':
    case 'ZodVoid':
    case 'ZodNever':
    case 'ZodFunction':
    case 'ZodPromise':
      return 'json.RawMessage'
    case 'ZodArray':
      return '[]' + typeRef(s._def.type, hint + 'Item')
    case 'ZodSet':
      return '[]' + typeRef(s._def.valueType, hint + 'Item')
    case 'ZodRecord':
    case 'ZodMap':
      return 'map[string]' + typeRef(s._def.valueType, hint + 'Value')
    case 'ZodLiteral': {
      const v = s._def.value
      return typeof v === 'string'
        ? 'string'
        : typeof v === 'boolean'
          ? 'bool'
          : typeof v === 'number'
            ? 'float64'
            : 'json.RawMessage'
    }
    case 'ZodEnum':
    case 'ZodNativeEnum':
      return named(s, hint, false, () => emitEnum(s))
    case 'ZodObject':
      return named(s, hint, true, () => emitStruct(s))
    case 'ZodIntersection': {
      const l = unwrap(s._def.left).s,
        r = unwrap(s._def.right).s
      if (tn(l) === 'ZodObject' && tn(r) === 'ZodObject')
        return named(s, hint, true, () => emitStruct(s, { ...shapeOf(l), ...shapeOf(r) }))
      return 'json.RawMessage'
    }
    case 'ZodUnion':
    case 'ZodDiscriminatedUnion': {
      const lits = literalUnionValues(s)
      if (lits && lits.every((v) => typeof v === 'string'))
        return named(s, hint, false, () => emitEnum(s, lits))
      if (lits) return 'json.RawMessage'
      const opts = s._def.options.map((o) => unwrap(o).s)
      const nonNull = opts.filter((o) => !['ZodNull', 'ZodUndefined'].includes(tn(o)))
      if (nonNull.length === 1) return typeRef(nonNull[0], hint)
      if (discriminatorOf(s)) return named(s, hint, true, () => emitUnion(s))
      // string | string[] and other mixes stay raw.
      return 'json.RawMessage'
    }
    default:
      throw new Error('unhandled zod type ' + t + ' at ' + hint)
  }
}

// The same enum, made by z.nativeEnum(E) or z.enum([...]) at every use,
// is one Go type.
const enumByKey = new Map()
function enumKey(s) {
  if (tn(s) === 'ZodNativeEnum') return s._def.values
  const v = literalUnionValues(s)
  return v ? 'enum:' + JSON.stringify([...v].sort()) : null
}

function named(s, hint, isStruct, emit) {
  if (!isStruct) {
    const key = enumKey(s)
    const seen = key && enumByKey.get(key)
    if (seen) return seen
    if (key) {
      const want =
        (tn(s) === 'ZodNativeEnum' &&
          enumNames.get(s._def.values) &&
          pascal(enumNames.get(s._def.values))) ||
        varName(s) ||
        hint
      const name = nameOf.get(s) ?? claim(s, want)
      enumByKey.set(key, name)
      emitted.add(s)
      queue.push(() => decls.push(emit()))
      return name
    }
  }
  let name = nameOf.get(s)
  if (!name) name = claim(s, varName(s) ?? hint)
  if (!emitted.has(s)) {
    emitted.add(s)
    if (isStruct) structNames.add(name)
    queue.push(() => decls.push(emit()))
  }
  return name
}

function goField(key) {
  let f = pascal(key)
  if (key.startsWith('_')) f = 'X' + f
  return f
}
const structNames = new Set()

function describe(s) {
  const d = s.description ?? s._def.description
  return d ? d.replace(/\s+/g, ' ').trim() : ''
}
function comment(text, indent = '') {
  if (!text) return ''
  return text
    .split(/(?<=\.) /)
    .map((l) => `${indent}// ${l}\n`)
    .join('')
}

function emitStruct(s, shape = shapeOf(s)) {
  const name = nameOf.get(s)
  {
    const lines = []
    const used = new Set()
    for (const [key, field] of Object.entries(shape)) {
      const { s: base, optional, nullable } = unwrap(field)
      let f = goField(key)
      while (used.has(f)) f += '_'
      used.add(f)
      let ty = typeRef(field, name + f)
      const ptr =
        (optional || nullable) &&
        (ty === 'bool' || ty === 'int64' || ty === 'float64' || structNames.has(ty))
      if (ptr) ty = '*' + ty
      const tag = optional || nullable ? `json:"${key},omitempty"` : `json:"${key}"`
      lines.push(comment(describe(field) || describe(base), '\t') + `\t${f} ${ty} \`${tag}\``)
    }
    return `${comment(describe(s))}type ${name} struct {\n${lines.join('\n')}\n}\n`
  }
}

function emitEnum(s, values = literalUnionValues(s)) {
  const name = nameOf.get(s)
  {
    const strs = values.every((v) => typeof v === 'string')
    const base = strs ? 'string' : 'float64'
    const consts = values.map(
      (v) => `\t${constName(name + pascal(String(v)))} ${name} = ${JSON.stringify(v)}`,
    )
    return `${comment(describe(s))}type ${name} ${base}\n\nconst (\n${consts.join('\n')}\n)\n`
  }
}

function emitUnion(s) {
  const name = nameOf.get(s)
  const disc = discriminatorOf(s)
  const opts = unionOptions(s)
  {
    const goDisc = goField(disc)
    const cases = []
    for (const o of opts) {
      const v = unwrap(shapeOf(o)[disc]).s._def.value
      const member = typeRef(o, name + pascal(v))
      cases.push({ v, member, c: constName(name + goDisc + pascal(v)) })
    }
    const goKind =
      typeof cases[0].v === 'boolean'
        ? 'bool'
        : typeof cases[0].v === 'number'
          ? 'float64'
          : 'string'
    const consts = cases.map(({ v, c }) => `\t${c} ${goKind} = ${JSON.stringify(v)}`)
    const sw = cases.map(
      ({ c, member }) =>
        `\tcase ${c}:\n\t\tvar v ${member}\n\t\terr := json.Unmarshal(u.Raw, &v)\n\t\treturn &v, err`,
    )
    return `${comment(describe(s))}// ${name} is one of several shapes, told apart by its ${JSON.stringify(disc)} field.
// Value decodes it into the shape that ${goDisc} names.
type ${name} struct {
	${goDisc} ${goKind}
	Raw json.RawMessage
}

const (
${consts.join('\n')}
)

func (u *${name}) UnmarshalJSON(b []byte) error {
	var head struct {
		D ${goKind} \`json:${JSON.stringify(disc)}\`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	u.${goDisc}, u.Raw = head.D, append(json.RawMessage(nil), b...)
	return nil
}

func (u ${name}) MarshalJSON() ([]byte, error) {
	if u.Raw == nil {
		return []byte("null"), nil
	}
	return u.Raw, nil
}

// Value returns a pointer to the shape the ${goDisc} names, or nil and no
// error for a ${goDisc} this version of the protocol does not know.
func (u ${name}) Value() (any, error) {
	switch u.${goDisc} {
${sw.join('\n')}
	}
	return nil, nil
}

// New${name} wraps one of its shapes.
func New${name}(v any) (${name}, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return ${name}{}, err
	}
	var u ${name}
	return u, u.UnmarshalJSON(b)
}
`
  }
}

function constName(want) {
  return claim({}, want)
}

function drain() {
  while (queue.length) queue.shift()()
}

// ---------- roots ----------

const methodEntries = []
for (const m of methods) {
  const reqSchema = all[m.req],
    resSchema = all[m.res]
  if (!reqSchema || !resSchema) throw new Error('missing schema for ' + m.name)
  const base = pascal(m.rpc.replace(/^daemon\./, ''))
  const paramsField = shapeOf(unwrap(reqSchema).s).params
  let paramsType = null
  if (paramsField) {
    const p = unwrap(paramsField).s
    if (tn(p) === 'ZodObject' && Object.keys(shapeOf(p)).length === 0) paramsType = 'struct{}'
    else {
      if (!nameOf.has(p) && tn(p) === 'ZodObject') claim(p, base + 'Params')
      paramsType = typeRef(paramsField, base + 'Params')
    }
  }
  const r = unwrap(resSchema).s
  if (!nameOf.has(r) && tn(r) === 'ZodObject')
    claim(r, varName(r) && !/^Daemon/.test(varName(r)) ? varName(r) : base + 'Result')
  const resultType = typeRef(resSchema, base + 'Result')
  methodEntries.push({
    ...m,
    goName: base,
    base,
    paramsType,
    paramsOptional: paramsField ? unwrap(paramsField).optional : true,
    resultType,
  })
}

// Daemon-initiated requests and notifications, found by their envelopes.
const serverRequests = [],
  notifications = []
for (const [k, v] of Object.entries(all)) {
  if (!v?._def || tn(v) !== 'ZodObject') continue
  const sh = shapeOf(v)
  const type = sh.type && unwrap(sh.type).s,
    method = sh.method && unwrap(sh.method).s
  if (!type || !method || tn(type) !== 'ZodLiteral' || tn(method) !== 'ZodLiteral') continue
  const m = method._def.value
  if (typeof m !== 'string' || !m.startsWith('daemon.') || !sh.params) continue
  const entry = { var: k, method: m, params: sh.params }
  if (type._def.value === 'notification' && !notifications.some((n) => n.method === m))
    notifications.push(entry)
  if (
    type._def.value === 'request' &&
    !methods.some((x) => x.rpc === m) &&
    !serverRequests.some((n) => n.method === m)
  )
    serverRequests.push(entry)
}
for (const n of [...notifications, ...serverRequests]) {
  const base = pascal(n.method.replace(/^daemon\./, ''))
  const p = unwrap(n.params).s
  if (!nameOf.has(p) && tn(p) === 'ZodObject') claim(p, base + 'Params')
  n.goType = typeRef(n.params, base + 'Params')
  n.base = base
  // What the Client answers a daemon request with.
  const res = all['Daemon' + base + 'ResultSchema']
  if (serverRequests.includes(n) && res) {
    const r = unwrap(res).s
    if (!nameOf.has(r)) claim(r, base + 'Response')
    n.responseType = typeRef(res, base + 'Response')
  }
}

// Types the Clients use directly.
for (const k of [
  'FactoryDroidMessageSchema',
  'SessionNotificationPayloadSchema',
  'ContentBlockSchema',
  'DroidWorkingStateSchema',
  'ToolConfirmationDetailsSchema',
  'AskUserResultSchema',
  'PermissionResponseSchema',
]) {
  if (all[k]) typeRef(all[k], pascal(k.replace(/Schema$/, '')))
}
drain()

// ---------- files ----------

const header = `// Code generated by gen/gen.mjs from @factory/droid-sdk ${sdkVersion}. DO NOT EDIT.\n\n`
fs.mkdirSync(outDir, { recursive: true })
const protocolVersion = /var FACTORY_PROTOCOL_VERSION = "([^"]+)"/.exec(
  fs.readFileSync(chunkPath, 'utf8'),
)?.[1]
if (!protocolVersion) throw new Error('FACTORY_PROTOCOL_VERSION not found')
const needsJSON = decls.some((d) => d.includes('json.'))
fs.writeFileSync(
  path.join(outDir, 'zz_types.go'),
  header +
    'package protocol\n\n' +
    (needsJSON ? 'import "encoding/json"\n\n' : '') +
    decls.join('\n'),
)

const timeouts = {
  '3e5': '300 * time.Second',
  COMPACTION_REQUEST_TIMEOUT: '240 * time.Second',
  FILE_TRANSFER_REQUEST_TIMEOUT: '900 * time.Second',
}
const mlines = methodEntries.map((m) => {
  const done = m.done.length ? JSON.stringify(m.done[0]) : '""'
  const to = timeouts[m.timeout] ?? '0'
  return `\t${JSON.stringify(m.rpc)}: {Ack: ${m.ack}, CompletedBy: ${done}, Timeout: ${to}},`
})
const consts = methodEntries.map((m) => `\tMethod${m.base} = ${JSON.stringify(m.rpc)}`)
const nconsts = notifications.map((n) => `\tNotification${n.base} = ${JSON.stringify(n.method)}`)
const rconsts = serverRequests.map((n) => `\tServerRequest${n.base} = ${JSON.stringify(n.method)}`)
const methodsUseJSON = methodEntries.some(
  (m) => m.resultType.startsWith('json.') || (m.paramsType ?? '').startsWith('json.'),
)
fs.writeFileSync(
  path.join(outDir, 'zz_methods.go'),
  header +
    `package protocol

import (
${methodsUseJSON ? '\t"encoding/json"\n' : ''}\t"time"
)

// SDKVersion is the @factory/droid-sdk release the protocol was generated from.
const SDKVersion = ${JSON.stringify(sdkVersion)}

// FactoryProtocolVersion is the protocol version requests carry.
const FactoryProtocolVersion = ${JSON.stringify(protocolVersion)}

// Methods the Client calls on the Daemon.
const (
${consts.join('\n')}
)

// Notifications the Daemon sends.
const (
${nconsts.join('\n')}
)

// Requests the Daemon sends the Client, which it must answer.
const (
${rconsts.join('\n')}
)

// MethodInfo is how a method is answered.
type MethodInfo struct {
	// Ack: the Daemon may answer {"accepted": true} at once and complete the
	// request later with a session notification of type CompletedBy that
	// carries the request's id.
	Ack         bool
	CompletedBy string
	// Timeout replaces the Client's default request timeout when not 0.
	Timeout time.Duration
}

// NewParams returns a pointer to a new value of a method's params type, or
// nil for an unknown method.
func NewParams(method string) any {
	switch method {
${methodEntries
  .filter((m) => m.paramsType && m.paramsType !== 'struct{}')
  .map((m) => `\tcase Method${m.base}:\n\t\treturn new(${m.paramsType})`)
  .join('\n')}
	}
	return nil
}

// NewResult returns a pointer to a new value of a method's result type, or
// nil for an unknown method.
func NewResult(method string) any {
	switch method {
${methodEntries.map((m) => `\tcase Method${m.base}:\n\t\treturn new(${m.resultType})`).join('\n')}
	}
	return nil
}

// Methods describes every method in the table above.
var Methods = map[string]MethodInfo{
${mlines.join('\n')}
}
`,
)

// Typed wrappers on the root package's Client.
const wrappers = methodEntries.map((m) => {
  const doc = `// ${m.goName} calls ${m.rpc}.\n`
  const res = m.resultType.startsWith('json.') ? 'json.RawMessage' : 'protocol.' + m.resultType
  if (!m.paramsType) {
    return `${doc}func (c *Client) ${m.goName}(ctx context.Context) (*${res.replace(/^json\./, 'json.')}, error) {\n\tvar out ${res}\n\treturn &out, c.Call(ctx, protocol.Method${m.base}, nil, &out)\n}\n`
  }
  const p =
    m.paramsType === 'struct{}'
      ? null
      : m.paramsType.startsWith('json.')
        ? 'json.RawMessage'
        : 'protocol.' + m.paramsType
  if (!p)
    return `${doc}func (c *Client) ${m.goName}(ctx context.Context) (*${res}, error) {\n\tvar out ${res}\n\treturn &out, c.Call(ctx, protocol.Method${m.base}, struct{}{}, &out)\n}\n`
  return `${doc}func (c *Client) ${m.goName}(ctx context.Context, params ${p}) (*${res}, error) {\n\tvar out ${res}\n\treturn &out, c.Call(ctx, protocol.Method${m.base}, params, &out)\n}\n`
})
const usesJSON = wrappers.some((w) => w.includes('json.'))
fs.writeFileSync(
  path.join(outDir, '..', 'zz_client_methods.go'),
  header +
    `package droid

import (
	"context"
${usesJSON ? '\t"encoding/json"\n' : ''}
	"${process.env.GO_MODULE}/protocol"
)

${wrappers.join('\n')}`,
)

// A summary for the README and for reviewing what changed between releases.
fs.writeFileSync(
  path.join(outDir, '..', 'gen', 'protocol-summary.txt'),
  `@factory/droid-sdk ${sdkVersion}\n\nmethods (${methodEntries.length}):\n` +
    methodEntries
      .map((m) => `  ${m.rpc}${m.ack ? ' (ack, completed by ' + m.done[0] + ')' : ''}`)
      .join('\n') +
    `\n\nnotifications (${notifications.length}):\n` +
    notifications.map((n) => '  ' + n.method).join('\n') +
    `\n\ndaemon requests (${serverRequests.length}):\n` +
    serverRequests.map((n) => '  ' + n.method).join('\n') +
    '\n',
)
console.log(
  'types',
  decls.length,
  'methods',
  methodEntries.length,
  'notifications',
  notifications.length,
  'server requests',
  serverRequests.length,
)

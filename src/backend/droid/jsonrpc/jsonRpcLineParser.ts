import type { JsonRpcMessage, JsonRpcNotification, JsonRpcRequest, JsonRpcResponse } from './jsonRpcTypes.ts'

export type ParsedJsonRpcLine =
  | { kind: 'message'; message: JsonRpcMessage }
  | { kind: 'stdout'; data: string }

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function isJsonRpcBase(value: unknown): value is Record<string, unknown> & { jsonrpc: '2.0'; factoryApiVersion: '1.0.0' } {
  if (!isObject(value)) return false
  return value.jsonrpc === '2.0' && value.factoryApiVersion === '1.0.0'
}

function tryParseJsonRpcMessage(line: string): JsonRpcMessage | null {
  const trimmed = line.trim()
  if (!trimmed) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    return null
  }
  if (!isJsonRpcBase(parsed)) return null
  const obj = parsed as Record<string, unknown>

  const type = obj.type
  if (type === 'request') {
    if (typeof obj.id !== 'string' || typeof obj.method !== 'string') return null
    const msg: JsonRpcRequest = obj as any
    return msg
  }
  if (type === 'response') {
    if (!(typeof obj.id === 'string' || obj.id === null)) return null
    const msg: JsonRpcResponse = obj as any
    return msg
  }
  if (type === 'notification') {
    if (typeof obj.method !== 'string') return null
    const msg: JsonRpcNotification = obj as any
    return msg
  }
  return null
}

export class JsonRpcLineParser {
  private buffer = ''

  push(chunk: Buffer | string): ParsedJsonRpcLine[] {
    this.buffer += typeof chunk === 'string' ? chunk : chunk.toString()
    const lines = this.buffer.split('\n')
    this.buffer = lines.pop() || ''
    return lines.flatMap((line) => this.parseLine(line))
  }

  flush(): ParsedJsonRpcLine[] {
    if (!this.buffer.trim()) return []
    const out = this.parseLine(this.buffer)
    this.buffer = ''
    return out
  }

  private parseLine(line: string): ParsedJsonRpcLine[] {
    const msg = tryParseJsonRpcMessage(line)
    if (msg) return [{ kind: 'message', message: msg }]
    const trimmed = line.trim()
    if (!trimmed) return []
    return [{ kind: 'stdout', data: trimmed }]
  }
}

/** The JSON-RPC 2.0 envelope the Host and the kernel speak. See protocol/README.md. */

export type RequestId = string | number

export type Request = {
  jsonrpc: '2.0'
  id?: RequestId
  method: string
  params?: unknown
}

export type Success = { jsonrpc: '2.0'; id: RequestId; result: unknown }
export type Failure = {
  jsonrpc: '2.0'
  id: RequestId | null
  error: { code: number; message: string; data?: unknown }
}

/** Every code the contract fixes. Clients branch on these, never on `message`. */
export const Code = {
  parseError: -32700,
  invalidRequest: -32600,
  methodNotFound: -32601,
  invalidParams: -32602,
  internalError: -32603,
  conversationNotFound: -32000,
  conversationBusy: -32001,
  conversationIdle: -32002,
  submissionNotFound: -32003,
  submissionSettled: -32004,
  submissionPlaced: -32005,
  taskNotFound: -32006,
  requestNotFound: -32007,
  readOnly: -32008,
  unsupported: -32009,
  kernelFailed: -32010,
  processNotFound: -32011,
  timeout: -32012,
  conflict: -32013,
  /** `model.complete` could not reach a model: no model configured, or the one named is unknown. */
  modelUnavailable: -32014,
  /** `model.complete` was given arguments its schema does not take. */
  modelSchema: -32015,
  /** The model answered but not with the JSON the schema asked for. */
  modelOutput: -32016,
  /** `model.complete` was cancelled by the caller. */
  modelCancelled: -32017,
  /** The model request failed: a provider error, a timeout, a refusal. */
  modelFailed: -32018,
} as const

export class RpcError extends Error {
  constructor(
    readonly code: number,
    message: string,
    readonly data?: unknown,
  ) {
    super(message)
    this.name = 'RpcError'
  }

  toFailure(id: RequestId | null): Failure {
    const error: Failure['error'] = { code: this.code, message: this.message }
    if (this.data !== undefined) error.data = this.data
    return { jsonrpc: '2.0', id, error }
  }
}

export const notFound = (conversationId: number) =>
  new RpcError(Code.conversationNotFound, 'conversation not found', { conversationId })

export const invalidParams = (issues: { path: string; message: string }[]) =>
  new RpcError(Code.invalidParams, 'invalid params', { issues })

export const unsupported = (capability: string) =>
  new RpcError(Code.unsupported, `${capability} is not implemented in this build`, { capability })

export const conflict = (requestId: string) =>
  new RpcError(Code.conflict, 'requestId was used for another request', { requestId })

/** One notification: a message without an `id`. */
export type Notification = { jsonrpc: '2.0'; method: string; params?: unknown }

export function notification(method: string, params?: unknown): Notification {
  return params === undefined ? { jsonrpc: '2.0', method } : { jsonrpc: '2.0', method, params }
}

/** Parse one text frame into a request, or say why it is not one. */
export function parseRequest(frame: string): Request {
  let value: unknown
  try {
    value = JSON.parse(frame)
  } catch (error) {
    throw new RpcError(Code.parseError, 'the frame is not JSON', {
      detail: error instanceof Error ? error.message : String(error),
    })
  }
  if (typeof value !== 'object' || value === null) {
    throw new RpcError(Code.invalidRequest, 'a request must be a JSON object')
  }
  const request = value as Partial<Request>
  if (request.jsonrpc !== '2.0') {
    throw new RpcError(Code.invalidRequest, 'jsonrpc must be "2.0"')
  }
  if (typeof request.method !== 'string' || request.method === '') {
    throw new RpcError(Code.invalidRequest, 'method must be a non-empty string')
  }
  if (
    request.id !== undefined &&
    typeof request.id !== 'string' &&
    typeof request.id !== 'number'
  ) {
    throw new RpcError(Code.invalidRequest, 'id must be a string or a number')
  }
  return request as Request
}

/** Params as an object: every method takes one, and none requires one. */
export function requestParams(request: Request): Record<string, unknown> {
  const value = request.params
  if (value === undefined || value === null) return {}
  if (typeof value !== 'object' || Array.isArray(value)) {
    throw new RpcError(Code.invalidParams, 'params must be an object')
  }
  return value as Record<string, unknown>
}

export function requiredNumber(params: Record<string, unknown>, key: string): number {
  const value = params[key]
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw invalidParams([{ path: key, message: 'a number is required' }])
  }
  return value
}

export function optionalNumber(params: Record<string, unknown>, key: string): number | undefined {
  const value = params[key]
  if (value === undefined || value === null) return undefined
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw invalidParams([{ path: key, message: 'a number is required' }])
  }
  return value
}

export function optionalString(params: Record<string, unknown>, key: string): string | undefined {
  const value = params[key]
  if (value === undefined || value === null) return undefined
  if (typeof value !== 'string') {
    throw invalidParams([{ path: key, message: 'a string is required' }])
  }
  return value
}

export function optionalBoolean(params: Record<string, unknown>, key: string): boolean | undefined {
  const value = params[key]
  if (value === undefined || value === null) return undefined
  if (typeof value !== 'boolean') {
    throw invalidParams([{ path: key, message: 'a boolean is required' }])
  }
  return value
}

export function requiredObject(
  params: Record<string, unknown>,
  key: string,
): Record<string, unknown> {
  const value = params[key]
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw invalidParams([{ path: key, message: 'an object is required' }])
  }
  return value as Record<string, unknown>
}

export function optionalObject(
  params: Record<string, unknown>,
  key: string,
): Record<string, unknown> | undefined {
  const value = params[key]
  if (value === undefined || value === null) return undefined
  if (typeof value !== 'object' || Array.isArray(value)) {
    throw invalidParams([{ path: key, message: 'an object is required' }])
  }
  return value as Record<string, unknown>
}

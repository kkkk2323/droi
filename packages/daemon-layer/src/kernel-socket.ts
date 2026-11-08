// One WebSocket to the Kernel through the Gateway, speaking JSON-RPC 2.0: requests with
// their answers, and the notifications the Kernel sends between them. A socket that closes
// fails every request still waiting; the connection (connection.ts) makes the next one.

export interface Notification {
  method: string
  params: Record<string, unknown>
}

/** The Kernel refused a request; `code` is what a Client branches on. */
export class KernelError extends Error {
  readonly code: number
  readonly data: unknown
  constructor(code: number, message: string, data?: unknown) {
    super(message)
    this.name = 'KernelError'
    this.code = code
    this.data = data
  }
}

/** The socket went away before the Kernel answered. */
export class SocketClosedError extends Error {
  constructor() {
    super('The connection to the computer closed.')
    this.name = 'SocketClosedError'
  }
}

export const REQUEST_TIMEOUT_MS = 30_000

interface Waiting {
  resolve(value: unknown): void
  reject(error: Error): void
  timer: ReturnType<typeof setTimeout>
}

export interface KernelSocket {
  call<T = Record<string, unknown>>(
    method: string,
    params?: Record<string, unknown>,
    timeoutMs?: number,
  ): Promise<T>
  close(): void
  readonly closed: boolean
}

/**
 * Opens `url`; resolves once the socket is open. `onNotification` hears every notification
 * in order, `onClose` once when it closes, either side.
 */
export function openKernelSocket(
  url: string,
  handlers: { onNotification(note: Notification): void; onClose(): void },
  openTimeoutMs = 10_000,
): Promise<KernelSocket> {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(url)
    const waiting = new Map<string, Waiting>()
    let next = 0
    let opened = false
    let closed = false
    const openTimer = setTimeout(() => {
      if (opened) return
      ws.close()
      finish()
      reject(new Error('The computer did not answer.'))
    }, openTimeoutMs)

    const finish = () => {
      if (closed) return
      closed = true
      clearTimeout(openTimer)
      for (const w of waiting.values()) {
        clearTimeout(w.timer)
        w.reject(new SocketClosedError())
      }
      waiting.clear()
      if (opened) handlers.onClose()
    }

    const socket: KernelSocket = {
      get closed() {
        return closed
      },
      call<T>(method: string, params?: Record<string, unknown>, timeoutMs = REQUEST_TIMEOUT_MS) {
        if (closed) return Promise.reject(new SocketClosedError())
        const id = String(++next)
        return new Promise<T>((res, rej) => {
          const timer = setTimeout(() => {
            waiting.delete(id)
            rej(new Error(`The computer did not answer ${method} in time.`))
          }, timeoutMs)
          waiting.set(id, { resolve: res as (value: unknown) => void, reject: rej, timer })
          ws.send(JSON.stringify({ jsonrpc: '2.0', id, method, ...(params ? { params } : {}) }))
        })
      },
      close() {
        ws.close()
        finish()
      },
    }

    ws.onopen = () => {
      opened = true
      clearTimeout(openTimer)
      resolve(socket)
    }
    ws.onerror = () => {
      if (!opened) {
        finish()
        reject(new Error('The computer refused the connection.'))
      }
    }
    ws.onclose = () => {
      if (!opened) {
        finish()
        reject(new Error('The computer closed the connection.'))
        return
      }
      finish()
    }
    ws.onmessage = (event: MessageEvent) => {
      if (typeof event.data !== 'string') return
      let frame: Record<string, unknown>
      try {
        frame = JSON.parse(event.data) as Record<string, unknown>
      } catch {
        return
      }
      const id = frame['id']
      if (typeof id === 'string' || typeof id === 'number') {
        const w = waiting.get(String(id))
        if (!w) return
        waiting.delete(String(id))
        clearTimeout(w.timer)
        const error = frame['error'] as
          | { code?: number; message?: string; data?: unknown }
          | undefined
        if (error)
          w.reject(
            new KernelError(error.code ?? 0, error.message ?? 'The Kernel refused.', error.data),
          )
        else w.resolve(frame['result'] ?? {})
        return
      }
      const method = frame['method']
      if (typeof method === 'string') {
        const params = (frame['params'] ?? {}) as Record<string, unknown>
        handlers.onNotification({ method, params })
      }
    }
  })
}

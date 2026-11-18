// React Native's AbortController is the abort-controller package (setUpXHR
// installs it), which predates abort(reason), signal.reason and
// signal.throwIfAborted(); Expo adds only AbortSignal.timeout and any.
// @factory/droid-sdk 0.10 calls signal.throwIfAborted() as it opens the
// WebSocket, so without these every connection failed before it began.
export function installAbortSignalFixes(
  Controller: typeof AbortController = AbortController,
  Signal: typeof AbortSignal = AbortSignal,
): void {
  if (!('reason' in Signal.prototype)) {
    const abort = Controller.prototype.abort
    Controller.prototype.abort = function (this: AbortController, reason?: unknown) {
      // Set before abort() so the abort listeners already read it.
      if (!this.signal.aborted) {
        Object.defineProperty(this.signal, 'reason', {
          value: reason === undefined ? abortError() : reason,
          configurable: true,
        })
      }
      abort.call(this)
    }
  }
  if (typeof Signal.prototype.throwIfAborted !== 'function') {
    Object.defineProperty(Signal.prototype, 'throwIfAborted', {
      value: function throwIfAborted(this: AbortSignal) {
        if (this.aborted) throw 'reason' in this ? this.reason : abortError()
      },
      configurable: true,
      writable: true,
    })
  }
}

function abortError(): Error {
  const error = new Error('This operation was aborted')
  error.name = 'AbortError'
  return error
}

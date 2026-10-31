// What the Phone App's bundle has in place of libsodium-wrappers-sumo
// (metro.config.js). @factory/droid-sdk imports it for the secure channel of
// Factory's relay, which the Phone App never opens: it reaches the Daemon
// through the Gateway. The real module throws as it loads under Hermes, which
// has no WebAssembly and no crypto.getRandomValues before the polyfill runs.
const sodium = {
  get ready(): Promise<never> {
    return Promise.reject(new Error('libsodium is not in the Phone App'))
  },
}

export default sodium

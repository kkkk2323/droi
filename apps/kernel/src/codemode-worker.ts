// The worker thread of a codemode script (src/codemode/sandbox.ts). It is its own entrypoint of the compiled
// Kernel, because a single-file executable has no package files to start a worker from.
import '@earendil-works/pi-codemode/worker'

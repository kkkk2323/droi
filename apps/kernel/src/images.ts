import { readFileSync } from 'node:fs'
import { createPhotonImages } from '@earendil-works/pi-durable/images'
import type { ImageProcessor } from '@earendil-works/pi-durable/tools'
import wasmPath from './vendor/photon_rs_bg.wasm' with { type: 'file' }

/**
 * The image processor `read` uses to turn a screenshot into an image a model accepts.
 *
 * The WebAssembly comes from a file the kernel ships (`vendor/photon_rs_bg.wasm`, Apache-2.0, see
 * `vendor/NOTICE.md`), read at runtime — never `createNodePhotonImages()`. A single-file build has no
 * package to resolve `@silvia-odwyer/photon/photon_rs_bg.wasm` from, and the failure only shows in a
 * *cross-compiled* binary: the bytes are inside it, the path lookup is not. A native build hides the
 * bug. `import … with { type: 'file' }` makes `bun build --compile` copy the file beside the executable
 * and hand back its path, which works in both.
 *
 * Photon loads once per process and its WebAssembly memory never shrinks, so the processor is built on
 * **first use**, not at start: decoding a 12-megapixel photo takes about a second and a half and leaves
 * that memory resident. Measured, eager loading costs about 40 MB of idle RSS in a kernel that will never
 * read an image — a conversation that only edits text pays for it anyway. The work runs on the calling
 * thread, as it does inside a worker: measured the same on Windows either way, so a worker would buy
 * nothing but a second copy of the memory.
 */
let processor: ImageProcessor | undefined

export function photonImages(): ImageProcessor {
  return {
    prepare: async (bytes, mimeType, limits) => {
      processor ??= createPhotonImages(readFileSync(wasmPath))
      return processor.prepare(bytes, mimeType, limits)
    },
  }
}

/** For a test that wants a fresh processor rather than the shared one. */
export function newPhotonImages(): ImageProcessor {
  return createPhotonImages(readFileSync(wasmPath))
}

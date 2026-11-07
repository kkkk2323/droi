/// <reference types="bun" />

/** A module of Photon's WebAssembly, imported as a file so a compiled binary can read it back. */
declare module '*.wasm' {
  const path: string
  export default path
}

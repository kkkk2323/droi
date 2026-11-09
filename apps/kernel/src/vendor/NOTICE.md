# Photon

`photon_rs_bg.wasm` is Photon (https://github.com/silvia-odwyer/photon), a Rust image library compiled
to WebAssembly, taken unchanged from `@silvia-odwyer/photon` 0.3.3, which
`@earendil-works/pi-durable` 1.5.0 depends on.

Photon is licensed under the Apache License, Version 2.0. A copy of the license is in
`LICENSE-photon.txt`, and the upstream license text in `LICENSE-photon.md`. A Droi release that ships
this file must carry the notice with it.

The file is committed rather than copied at build time because a single-file `bun build --compile`
binary cannot resolve it from `node_modules`, and because the kernel reads it at runtime through
`import … with { type: 'file' }`.

# QuickJS

`quickjs.wasm` is QuickJS compiled to WebAssembly (WASI), taken unchanged from `quickjs-wasi` 3.6.2, which
`@earendil-works/pi-codemode` 1.5.0 depends on. It runs codemode scripts (`src/codemode/sandbox.ts`).

`quickjs-wasi` is licensed under the MIT License; its license text is in `LICENSE-quickjs-wasi.txt`. A Droi
release that ships this file must carry the notice with it.

It is committed for the same reason as Photon: a single-file binary cannot resolve it from `node_modules`.

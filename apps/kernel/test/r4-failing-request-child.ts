import { Database } from 'bun:sqlite'
import { TOKEN, config, fauxCatalog } from './helpers.ts'
import { startKernel } from '../src/kernel.ts'

// A kernel whose SQLite file is locked by somebody else: its first commit a request makes fails, which
// is Pi's final Storage error. The answer to that request is this test's subject.
const dir = process.argv[2]!
const kernel = await startKernel(config(dir), {
  token: TOKEN,
  models: fauxCatalog().models,
  noLock: true,
})
const outside = new Database(`${dir}/kernel.sqlite`)
outside.exec('PRAGMA journal_mode = WAL')
outside.exec('BEGIN EXCLUSIVE')
setTimeout(() => outside.close(), 10_000)
console.log(JSON.stringify({ port: kernel.port }))
await new Promise(() => undefined)

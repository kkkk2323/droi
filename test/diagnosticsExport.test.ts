import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, readFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { LocalDiagnostics } from '../src/backend/diagnostics/localDiagnostics.ts'

test('LocalDiagnostics export bundle creates a zip and redacts secrets', async () => {
  const baseDir = await mkdtemp(join(tmpdir(), 'droi-export-'))
  const diag = new LocalDiagnostics({ baseDir, enabled: true })

  await diag.append({
    ts: new Date().toISOString(),
    level: 'info',
    scope: 'server',
    event: 'test.event',
    sessionId: 's1',
    data: { url: 'http://127.0.0.1:3001/?authKey=abcdef' },
  })

  const outPath = join(diag.getDiagnosticsDir(), 'bundles', 'bundle.zip')
  await diag.exportToPath({
    outputPath: outPath,
    sessionId: 's1',
    appVersion: '1.0.0',
    appState: { version: 2, machineId: 'm1', localDiagnosticsEnabled: true } as any,
    debugTraceText: 'open http://x/?authKey=abcdef',
  })

  const buf = await readFile(outPath)
  assert.equal(buf.subarray(0, 2).toString('utf8'), 'PK')
  assert.ok(buf.includes(Buffer.from('manifest.json')))
  assert.ok(buf.includes(Buffer.from('settings.json')))
  const text = buf.toString('utf8')
  assert.match(text, /REDACTED/i)
  assert.doesNotMatch(text, /authKey=abcdef/)
})

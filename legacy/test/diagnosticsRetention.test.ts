import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, writeFile, stat, utimes, readdir } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { LocalDiagnostics } from '../src/backend/diagnostics/localDiagnostics.ts'

test('LocalDiagnostics cleanup removes old files and trims by size', async () => {
  const baseDir = await mkdtemp(join(tmpdir(), 'droi-diag-'))
  const diag = new LocalDiagnostics({
    baseDir,
    retention: { maxAgeDays: 1, maxTotalBytes: 1024 },
    enabled: true,
  })

  const dir = diag.getDiagnosticsDir()
  const sessionsDir = join(dir, 'sessions')
  await diag.cleanup()

  const oldFile = join(dir, 'app-2000-01-01.jsonl')
  await writeFile(oldFile, 'old\n')
  const oldTime = new Date('2000-01-01T00:00:00Z')
  await utimes(oldFile, oldTime, oldTime)

  const big1 = join(sessionsDir, 's1-2020-01-01.jsonl')
  const big2 = join(sessionsDir, 's2-2020-01-01.jsonl')
  await writeFile(big1, 'a'.repeat(800))
  await writeFile(big2, 'b'.repeat(800))
  const t1 = new Date('2020-01-01T00:00:00Z')
  const t2 = new Date('2020-01-02T00:00:00Z')
  await utimes(big1, t1, t1)
  await utimes(big2, t2, t2)

  await diag.cleanup()

  const remainingRoot = await readdir(dir).catch(() => [])
  assert.ok(remainingRoot.includes('sessions'))
  await assert.rejects(() => stat(oldFile))

  // Total size cap should evict oldest first among remaining.
  const sFiles = await readdir(sessionsDir).catch(() => [])
  assert.ok(sFiles.length <= 1)
})


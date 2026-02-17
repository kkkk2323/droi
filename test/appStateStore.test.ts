import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createAppStateStore } from '../src/backend/storage/appStateStore.ts'

test('appStateStore migrates v0 to v2 (machineId)', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-app-state-'))
  await writeFile(join(dir, 'app-state.json'), JSON.stringify({
    apiKey: 'fk-abc',
    projects: [{ dir: '/repo', name: 'repo', sessions: [{ id: 'old' }] }],
    activeProjectDir: '/repo',
  }))

  const store = createAppStateStore({ baseDir: dir })
  const loaded = await store.load()
  assert.equal(loaded.version, 2)
  assert.equal(typeof (loaded as any).machineId, 'string')
  assert.ok(String((loaded as any).machineId).length > 0)
  assert.equal(loaded.apiKey, 'fk-abc')
  assert.equal(loaded.activeProjectDir, '/repo')
  assert.deepEqual(loaded.projects, [{ dir: '/repo', name: 'repo' }])
})

test('appStateStore normalizes project settings setupScript', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-app-state-settings-'))
  await writeFile(join(dir, 'app-state.json'), JSON.stringify({
    version: 2,
    machineId: 'm1',
    projectSettings: {
      '/repo': {
        baseBranch: ' main ',
        worktreePrefix: ' droi ',
        setupScript: ' npm install ',
      },
    },
  }))

  const store = createAppStateStore({ baseDir: dir })
  const loaded = await store.load() as any
  assert.equal(loaded.projectSettings['/repo'].baseBranch, 'main')
  assert.equal(loaded.projectSettings['/repo'].worktreePrefix, 'droi')
  assert.equal(loaded.projectSettings['/repo'].setupScript, 'npm install')
})

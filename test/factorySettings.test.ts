import assert from 'node:assert/strict'
import { mkdtemp, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import {
  normalizeMissionModelSettings,
  readCustomModelsFromFactorySettings,
  readMissionModelSettingsFromPath,
  writeMissionModelSettingsToPath,
} from '../src/backend/storage/factorySettings.ts'

test('normalizeMissionModelSettings trims supported fields', () => {
  assert.deepEqual(
    normalizeMissionModelSettings({
      orchestratorModel: ' claude-sonnet-4 ',
      workerModel: ' ',
      validationWorkerModel: 123,
    }),
    {
      orchestratorModel: 'claude-sonnet-4',
      workerModel: undefined,
      validationWorkerModel: undefined,
    },
  )
})

test('writeMissionModelSettingsToPath preserves unrelated factory settings', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-factory-settings-'))
  const filePath = join(dir, 'settings.json')

  await writeFile(
    filePath,
    JSON.stringify(
      {
        customModels: [
          { id: 'custom-1', displayName: 'Custom 1', model: 'foo', provider: 'custom' },
        ],
        theme: 'dark',
      },
      null,
      2,
    ),
  )

  const saved = await writeMissionModelSettingsToPath(filePath, {
    orchestratorModel: ' claude-sonnet-4 ',
    workerModel: ' kimi-k2.5 ',
    validationWorkerModel: ' ',
  })

  assert.deepEqual(saved, {
    orchestratorModel: 'claude-sonnet-4',
    workerModel: 'kimi-k2.5',
    validationWorkerModel: undefined,
  })

  const raw = JSON.parse(await readFile(filePath, 'utf-8'))
  assert.equal(raw.theme, 'dark')
  assert.equal(raw.customModels[0].id, 'custom-1')
  assert.deepEqual(raw.missionModelSettings, {
    orchestratorModel: 'claude-sonnet-4',
    workerModel: 'kimi-k2.5',
  })

  assert.deepEqual(await readMissionModelSettingsFromPath(filePath), saved)
  assert.deepEqual(readCustomModelsFromFactorySettings(raw), [
    { id: 'custom-1', displayName: 'Custom 1', model: 'foo', provider: 'custom' },
  ])
})

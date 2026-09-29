import { describe, expect, it } from 'vitest'
import { mkdtempSync, readFileSync, rmSync, statSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { buildRuntimeOverlay, writeRuntimeOverlay } from './runtime-overlay'

const memory = {
  nodeBinary: '/Applications/Droi.app/Contents/MacOS/Droi',
  serverEntry: '/Applications/Droi.app/Contents/Resources/app.asar/out/main/memory-server.js',
  hookEntry: '/Applications/Droi.app/Contents/Resources/app.asar/out/main/memory-hook.js',
  memoryDir: "/Users/dev/Library/Application Support/droi's/memory",
  approvedAt: '2026-09-29T12:00:00.000Z',
}

const HOOK_COMMAND =
  "ELECTRON_RUN_AS_NODE=1 DROI_MEMORY_DIR='/Users/dev/Library/Application Support/droi'\\''s/memory' '/Applications/Droi.app/Contents/MacOS/Droi' '/Applications/Droi.app/Contents/Resources/app.asar/out/main/memory-hook.js'"

describe('Runtime Overlay', () => {
  it('registers the Memory Server under mcp, pre-approved and loaded before the first turn', () => {
    expect(buildRuntimeOverlay(memory)).toEqual({
      env: { DROI_MEMORY_DIR: memory.memoryDir },
      blockOnMcpLoad: true,
      mcp: {
        mcpServers: {
          'droi-memory': {
            type: 'stdio',
            command: memory.nodeBinary,
            args: [memory.serverEntry],
            env: { ELECTRON_RUN_AS_NODE: '1', DROI_MEMORY_DIR: memory.memoryDir },
          },
        },
        persistentPermissions: {
          servers: { 'droi-memory': { approvedAt: memory.approvedAt, impactLevel: 'high' } },
        },
      },
      mcpAutonomyOverrides: { 'droi-memory': { defaultLevel: 'low' } },
      hooks: {
        SessionStart: [{ hooks: [{ type: 'command', command: HOOK_COMMAND, timeout: 10 }] }],
        UserPromptSubmit: [{ hooks: [{ type: 'command', command: HOOK_COMMAND, timeout: 10 }] }],
        PreCompact: [{ hooks: [{ type: 'command', command: HOOK_COMMAND, timeout: 10 }] }],
        SessionEnd: [{ hooks: [{ type: 'command', command: HOOK_COMMAND, timeout: 10 }] }],
      },
    })
  })

  it('quotes the hook command for the shell', () => {
    const overlay = buildRuntimeOverlay({
      ...memory,
      nodeBinary: '/usr/bin/printenv',
      hookEntry: 'DROI_MEMORY_DIR',
    }) as { hooks: { SessionStart: Array<{ hooks: Array<{ command: string }> }> } }
    const command = overlay.hooks.SessionStart[0]!.hooks[0]!.command
    const run = spawnSync('/bin/sh', ['-c', command], { encoding: 'utf8' })
    expect(run.stdout).toBe(`${memory.memoryDir}\n`)
  })

  it('has no top-level mcpServers, which the Daemon would ignore', () => {
    expect(buildRuntimeOverlay(memory)).not.toHaveProperty('mcpServers')
  })

  it('is written as JSON only the user can read', () => {
    const dir = mkdtempSync(join(tmpdir(), 'droi-overlay-'))
    try {
      const path = join(dir, 'nested', 'runtime-overlay.json')
      writeRuntimeOverlay(path, buildRuntimeOverlay(memory))
      expect(JSON.parse(readFileSync(path, 'utf8'))).toEqual(buildRuntimeOverlay(memory))
      expect(statSync(path).mode & 0o777).toBe(0o600)
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })
})

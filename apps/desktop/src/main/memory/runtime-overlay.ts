// The Runtime Overlay (ADR 0010): the settings file the Desktop Shell hands its
// Daemon with `--settings`. The Daemon merges it for its own process only, so
// Memory is attached to Droi's Daemon and the droid CLI's ~/.factory files are
// never touched. Shape as droid 0.229.0 reads it: MCP under `mcp`, general
// keys such as blockOnMcpLoad at the top level.
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname } from 'node:path'
import { SERVER_NAME } from '../../memory/mcp-server'

export interface MemoryAttachment {
  /** Electron's binary, run as plain Node for the Memory Server and the hook. */
  nodeBinary: string
  serverEntry: string
  memoryDir: string
  /** The permission record's timestamp, as the Daemon writes one when a user approves. */
  approvedAt: string
}

export function buildRuntimeOverlay(memory: MemoryAttachment): Record<string, unknown> {
  const env = { DROI_MEMORY_DIR: memory.memoryDir }
  return {
    env,
    // Otherwise a Session's first turn can start before the Memory Server is connected.
    blockOnMcpLoad: true,
    mcp: {
      mcpServers: {
        [SERVER_NAME]: {
          type: 'stdio',
          command: memory.nodeBinary,
          args: [memory.serverEntry],
          env: { ELECTRON_RUN_AS_NODE: '1', ...env },
        },
      },
      // The Daemon classifies an unknown MCP tool as high impact, so anything
      // lower would make every Session stop and ask before a Memory write.
      persistentPermissions: {
        servers: { [SERVER_NAME]: { approvedAt: memory.approvedAt, impactLevel: 'high' } },
      },
    },
    mcpAutonomyOverrides: { [SERVER_NAME]: { defaultLevel: 'low' } },
  }
}

export function writeRuntimeOverlay(path: string, overlay: Record<string, unknown>): void {
  mkdirSync(dirname(path), { recursive: true })
  writeFileSync(path, JSON.stringify(overlay, null, 2), { mode: 0o600 })
}

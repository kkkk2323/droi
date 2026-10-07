// The Automations a Fake Daemon has: the local ones the real Daemon keeps
// under ~/.factory/automations and runs on their schedules. A run is a
// Session tagged `automation`, as the real one; dispatching one adds it.
import { randomUUID } from 'node:crypto'
import type { MethodHandler, SessionFixture } from './scenario'

export interface AutomationRunFixture {
  sessionId: string
  status: 'success' | 'in_progress' | 'failed'
  startedAt: string
  completedAt?: string
  type?: 'run' | 'create'
}

export interface AutomationFixture {
  id: string
  name: string
  prompt: string
  /** A cron expression in UTC. */
  schedule: string
  /** Defaults to active. */
  status?: 'active' | 'paused' | 'invalid'
  model?: string
  nextRunAt?: string
  lastRunAt?: string
  lastRunStatus?: string
  /** The folder; defaults to /Users/dev/.factory/automations/<id>. */
  path?: string
  runs?: AutomationRunFixture[]
}

// Like the real Daemon: a failed change answers success false and an error.
const failed = (error: string) => ({ success: false, error })
const SCHEDULE = /^(\S+\s+){4}\S+$/

export function automationHandlers(
  input: { automations?: AutomationFixture[] },
  sessions: SessionFixture[],
): Record<string, MethodHandler> {
  const automations = structuredClone(input.automations ?? [])
  const find = (id: unknown) => automations.find((a) => a.id === id)
  const pathOf = (a: AutomationFixture) => a.path ?? `/Users/dev/.factory/automations/${a.id}`
  const list = () => ({
    automations: automations.map((a) => ({
      id: a.id,
      uuid: `uuid-${a.id}`,
      name: a.name,
      prompt: a.prompt,
      schedule: a.schedule,
      status: a.status ?? 'active',
      isValid: a.status !== 'invalid',
      path: pathOf(a),
      machineId: 'local',
      setupState: 'complete',
      ...(a.model ? { model: a.model } : {}),
      ...(a.nextRunAt && a.status !== 'paused' ? { nextRunAt: a.nextRunAt } : {}),
      ...(a.lastRunAt ? { lastRunAt: a.lastRunAt } : {}),
      ...(a.lastRunStatus ? { lastRunStatus: a.lastRunStatus } : {}),
    })),
  })
  const setStatus =
    (status: 'active' | 'paused'): MethodHandler =>
    (params) => {
      const found = find(params['automationId'])
      if (!found) return { ...failed('Automation not found'), automationId: params['automationId'] }
      found.status = status
      return { success: true, automationId: found.id, status }
    }
  return {
    'daemon.list_automations': list,
    'daemon.create_automation': (params) => {
      const id = String(params['id'])
      if (find(id)) return failed('Automation already exists')
      if (!SCHEDULE.test(String(params['schedule']))) return failed('Invalid schedule.')
      automations.push({
        id,
        name: String(params['name']),
        prompt: String(params['instructions'] ?? ''),
        schedule: String(params['schedule']),
        ...(typeof params['model'] === 'string' ? { model: params['model'] } : {}),
        ...(params['paused'] === true ? { status: 'paused' as const } : {}),
      })
      return { success: true, automationId: id }
    },
    'daemon.update_automation': (params) => {
      const found = find(params['automationId'])
      if (!found) return failed('Automation not found')
      if (!SCHEDULE.test(String(params['schedule']))) return failed('Invalid schedule.')
      found.name = String(params['name'])
      found.prompt = String(params['prompt'])
      found.schedule = String(params['schedule'])
      if (typeof params['model'] === 'string') found.model = params['model']
      return { success: true }
    },
    'daemon.pause_automation': setStatus('paused'),
    'daemon.resume_automation': setStatus('active'),
    'daemon.delete_automation': (params) => {
      const found = find(params['automationId'])
      if (!found) return failed('Automation not found')
      automations.splice(automations.indexOf(found), 1)
      return { success: true }
    },
    'daemon.get_automation_history': (params) => {
      const found = find(params['automationId'])
      const runs = [...(found?.runs ?? [])].sort((a, b) => b.startedAt.localeCompare(a.startedAt))
      return {
        automationId: params['automationId'],
        runs: runs.map((r) => ({
          runId: r.sessionId,
          sessionId: r.sessionId,
          automationId: found?.id,
          type: r.type ?? 'run',
          status: r.status,
          startedAt: r.startedAt,
          ...(r.completedAt ? { completedAt: r.completedAt } : {}),
        })),
        totalCount: runs.length,
      }
    },
    // Runs at once, paused or not, and answers with the run's Session.
    'daemon.dispatch_automation_run': (params) => {
      const found = find(params['automationId'])
      if (!found)
        return { success: false, error: { code: 'not_found', message: 'Automation not found' } }
      if (found.runs?.some((r) => r.status === 'in_progress')) {
        return {
          success: false,
          error: {
            code: 'already_running',
            message: 'Automation is already running on this machine',
          },
        }
      }
      const sessionId = randomUUID()
      const now = new Date()
      sessions.unshift({
        sessionId,
        title: `[Automation] ${found.name}`,
        cwd: pathOf(found),
        updatedAt: Math.floor(now.getTime() / 1000),
        messages: [],
        tags: [
          {
            name: 'automation',
            metadata: {
              automationId: found.id,
              automationName: found.name,
              automationUuid: `uuid-${found.id}`,
              triggerSource: 'manual',
              type: 'run',
            },
          },
        ],
      })
      found.runs = [
        ...(found.runs ?? []),
        { sessionId, status: 'in_progress', startedAt: now.toISOString() },
      ]
      found.lastRunAt = now.toISOString()
      return { success: true, sessionId }
    },
  }
}

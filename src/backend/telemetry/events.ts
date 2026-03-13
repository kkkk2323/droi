export type TelemetryEventName =
  | 'app_launched'
  | 'app_closed'
  | 'session_created'
  | 'message_sent'
  | 'turn_completed'
  | 'mission_started'
  | 'mission_completed'
  | 'git_commit'
  | 'git_push'
  | 'git_pr_created'
  | 'slash_command_used'
  | 'skill_used'
  | 'setting_changed'
  | 'update_accepted'
  | 'update_dismissed'
  | 'session_restored'
  | 'startup_metrics'
  | 'app_error'

export interface TelemetryEvent {
  event: TelemetryEventName
  properties?: Record<string, string | number | boolean | undefined>
}

export function messageLengthBucket(length: number): string {
  if (length < 50) return 'short'
  if (length < 500) return 'medium'
  return 'long'
}

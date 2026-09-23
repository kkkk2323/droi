// A Session that handed work to two subagents: an Explorer it waited for,
// which finished, and a Reviewer it started in the background, still running.
// Shared by the web Client's and the Phone App's tests.
import {
  assistantMessage,
  session,
  toolCallMessage,
  toolResultMessage,
  userMessage,
} from './scenario'

const cwd = '/Users/dev/acme-web'
const explorerId = '0c1d2e3f-0000-4000-8000-00000000e001'
const reviewerId = '0c1d2e3f-0000-4000-8000-00000000e002'

export function subagentScenario() {
  const main = session('Plan the release', cwd, [
    userMessage('Map the mobile app, then review the diff'),
    toolCallMessage('toolu_explore', 'Task', {
      subagent_type: 'explorer',
      description: 'Map the mobile app',
      prompt: 'Explore apps/mobile and report its screens.',
    }),
    toolResultMessage('toolu_explore', 'The app has **three screens**: list, session, settings.'),
    toolCallMessage('toolu_review', 'Task', {
      subagent_type: 'reviewer',
      description: 'Review the diff',
      prompt: 'Review the uncommitted changes.',
    }),
    toolResultMessage(
      'toolu_review',
      `Task launched in background.\ntask_id: ${reviewerId}\nsession_id: ${reviewerId}\nsubagent_type: reviewer\ndescription: Review the diff`,
    ),
    assistantMessage('The Explorer is done; the Reviewer is still reading.'),
  ])
  const explorer = session(
    'Explorer: Map the mobile app',
    cwd,
    [
      userMessage('Explore apps/mobile and report its screens.'),
      assistantMessage('The app has three screens: list, session, settings.'),
    ],
    {
      sessionId: explorerId,
      subagent: {
        callingSessionId: main.sessionId,
        callingToolUseId: 'toolu_explore',
        subagentType: 'explorer',
        description: 'Map the mobile app',
        status: 'completed',
        toolUseCount: 12,
        durationMs: 134_000,
      },
    },
  )
  const reviewer = session(
    'Reviewer: Review the diff',
    cwd,
    [userMessage('Review the uncommitted changes.')],
    {
      sessionId: reviewerId,
      subagent: {
        callingSessionId: main.sessionId,
        callingToolUseId: 'toolu_review',
        subagentType: 'reviewer',
        description: 'Review the diff',
        status: 'running',
      },
    },
  )
  const other = session('Fix the login bug', cwd, [userMessage('Why does login fail?')])
  return { main, explorer, reviewer, other, sessions: [main, explorer, reviewer, other] }
}

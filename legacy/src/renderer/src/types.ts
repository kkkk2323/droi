export type {
  ApiKeyEntry,
  ApiKeyUsage,
  DroidClientAPI,
  SlashCommandDef,
  SlashResolveResult,
  SkillDef,
  ChatMessage,
  TextBlock,
  ToolCallBlock,
  AttachmentBlock,
  CommandBlock,
  SkillBlock,
  ThinkingBlock,
  SessionMeta,
  Project,
  ProjectSettings,
  MissionModelSettings,
  PersistedAppState,
  JsonRpcRequest,
  JsonRpcNotification,
  JsonRpcResponse,
  JsonRpcMessage,
  DroidPermissionOption,
  DroidAutonomyLevel,
  DroidSessionNotification,
  SaveSessionRequest,
  LoadSessionResponse,
  GitStatusFile,
  WorkspaceType,
  WorkspaceInfo,
  WorkspaceCreateParams,
  RemoveWorktreeResult,
  PushBranchResult,
  GitToolsInfo,
  GenerateCommitMetaRequest,
  GenerateCommitMetaResult,
  CommitWorkflow,
  CommitWorkflowRequest,
  CommitWorkflowResult,
  WorkflowStepName,
  WorkflowStepStatus,
  WorkflowStepProgress,
  CustomModelDef,
  AvailableModelConfig,
  CreateSessionResult,
  EditorInfo,
  SetupScriptStatus,
  SetupScriptEvent,
  RuntimeLogEntry,
} from '../../shared/protocol'

export type { MissionRuntimeSnapshot } from '../../shared/mission'

// === Constants ===

export const AUTO_LEVELS = [
  { value: 'default', label: 'Spec' },
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
] as const

export function getAutoLevelLabel(level: string): string {
  return AUTO_LEVELS.find((l) => l.value === level)?.label ?? level
}

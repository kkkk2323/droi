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
  EditorInfo,
  SetupScriptStatus,
  SetupScriptEvent,
} from '../../shared/protocol'

// === Constants ===

export type ModelProvider = 'kimi' | 'zhipu' | 'claude' | 'openai' | 'gemini' | 'minimax'

export interface ModelDef {
  value: string
  label: string
  provider: ModelProvider
  multiplier: string
}

export interface ModelGroup {
  label: string
  icon: string
  options: ModelDef[]
}

export const MODEL_GROUPS: ModelGroup[] = [
  {
    label: 'Anthropic',
    icon: 'claude',
    options: [
      { value: 'claude-haiku-4-5-20251001', label: 'Claude Haiku 4.5', provider: 'claude', multiplier: '0.4×' },
      { value: 'claude-sonnet-4-5-20250929', label: 'Claude Sonnet 4.5', provider: 'claude', multiplier: '1.2×' },
      { value: 'claude-sonnet-4-6', label: 'Claude Sonnet 4.6', provider: 'claude', multiplier: '1.2×' },
      { value: 'claude-opus-4-5-20251101', label: 'Claude Opus 4.5', provider: 'claude', multiplier: '2×' },
      { value: 'claude-opus-4-6', label: 'Claude Opus 4.6', provider: 'claude', multiplier: '2×' },
      { value: 'claude-opus-4-6-fast', label: 'Claude Opus 4.6 Fast', provider: 'claude', multiplier: '12×' },
    ],
  },
  {
    label: 'OpenAI',
    icon: 'openai',
    options: [
      { value: 'gpt-5.1', label: 'GPT-5.1', provider: 'openai', multiplier: '0.5×' },
      { value: 'gpt-5.1-codex', label: 'GPT-5.1 Codex', provider: 'openai', multiplier: '0.5×' },
      { value: 'gpt-5.1-codex-max', label: 'GPT-5.1 Codex Max', provider: 'openai', multiplier: '0.5×' },
      { value: 'gpt-5.2', label: 'GPT-5.2', provider: 'openai', multiplier: '0.7×' },
      { value: 'gpt-5.2-codex', label: 'GPT-5.2 Codex', provider: 'openai', multiplier: '0.7×' },
      { value: 'gpt-5.3-codex', label: 'GPT-5.3 Codex', provider: 'openai', multiplier: '0.7×' },
    ],
  },
  {
    label: 'Google',
    icon: 'gemini',
    options: [
      { value: 'gemini-3-flash-preview', label: 'Gemini 3 Flash', provider: 'gemini', multiplier: '0.2×' },
      { value: 'gemini-3-pro-preview', label: 'Gemini 3 Pro', provider: 'gemini', multiplier: '0.8×' },
      { value: 'gemini-3.1-pro-preview', label: 'Gemini 3.1 Pro', provider: 'gemini', multiplier: '0.8×' },
    ],
  },
  {
    label: 'Zhipu',
    icon: 'zhipu',
    options: [
      { value: 'glm-4.7', label: 'GLM-4.7', provider: 'zhipu', multiplier: '0.25×' },
      { value: 'glm-5', label: 'GLM-5', provider: 'zhipu', multiplier: '0.4×' },
    ],
  },
  {
    label: 'Kimi',
    icon: 'kimi',
    options: [
      { value: 'kimi-k2.5', label: 'Kimi K2.5', provider: 'kimi', multiplier: '0.25×' },
    ],
  },
  {
    label: 'MiniMax',
    icon: 'minimax',
    options: [
      { value: 'minimax-m2.5', label: 'MiniMax M2.5', provider: 'minimax', multiplier: '0.12×' },
    ],
  },
]

export const MODELS: readonly ModelDef[] = MODEL_GROUPS.flatMap((g) => g.options)

export const AUTO_LEVELS = [
  { value: 'default', label: 'Spec' },
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
] as const

export function getModelLabel(model: string): string {
  return MODELS.find((m) => m.value === model)?.label ?? model
}

export function getAutoLevelLabel(level: string): string {
  return AUTO_LEVELS.find((l) => l.value === level)?.label ?? level
}

export const MODEL_REASONING_MAP: Record<string, {
  levels: string[]
  default: string
}> = {
  'claude-opus-4-6':        { levels: ['off','low','medium','high','max'], default: 'high' },
  'claude-opus-4-6-fast':   { levels: ['off','low','medium','high','max'], default: 'high' },
  'claude-opus-4-5-20251101': { levels: ['off','low','medium','high'], default: 'off' },
  'claude-sonnet-4-6':      { levels: ['off','low','medium','high'], default: 'off' },
  'claude-sonnet-4-5-20250929': { levels: ['off','low','medium','high'], default: 'off' },
  'claude-haiku-4-5-20251001': { levels: ['off','low','medium','high'], default: 'off' },
  'gpt-5.1':                { levels: ['none','low','medium','high'], default: 'none' },
  'gpt-5.1-codex':          { levels: ['low','medium','high'], default: 'medium' },
  'gpt-5.1-codex-max':      { levels: ['low','medium','high','xhigh'], default: 'medium' },
  'gpt-5.2':                { levels: ['off','low','medium','high','xhigh'], default: 'low' },
  'gpt-5.2-codex':          { levels: ['none','low','medium','high','xhigh'], default: 'medium' },
  'gpt-5.3-codex':          { levels: ['none','low','medium','high','xhigh'], default: 'medium' },
  'gemini-3-pro-preview':   { levels: ['none','low','medium','high'], default: 'high' },
  'gemini-3.1-pro-preview': { levels: ['none','low','medium','high'], default: 'high' },
  'gemini-3-flash-preview': { levels: ['minimal','low','medium','high'], default: 'high' },
  'minimax-m2.5':           { levels: ['low','medium','high'], default: 'high' },
}

export function getModelReasoningLevels(modelId: string): string[] | null {
  return MODEL_REASONING_MAP[modelId]?.levels ?? null
}

export function getModelDefaultReasoning(modelId: string): string {
  return MODEL_REASONING_MAP[modelId]?.default ?? ''
}

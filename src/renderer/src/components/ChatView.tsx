import React, { useCallback, useEffect, useRef, useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso'
import { cn } from '../lib/utils'
import {
  ChevronDown,
  ChevronRight,
  FolderOpen,
  Terminal,
  FileEdit,
  Play,
  Search,
  Globe,
  Loader2,
  Check,
  Paperclip,
  X,
  BookOpen,
  Brain,
} from 'lucide-react'
import type {
  ChatMessage,
  TextBlock,
  ToolCallBlock,
  AttachmentBlock,
  CommandBlock,
  SkillBlock,
} from '@/types'
import type { SessionSetupState, PendingPermissionRequest } from '@/state/appReducer'
import type { DroidPermissionOption } from '@/types'
import { isTodoWriteBlock } from './TodoPanel'
import { isBrowserMode, getApiBase } from '@/droidClient'
import { SpecReviewCard, isExitSpecPermission } from './SpecReviewCard'
import { SessionBootstrapCards } from './SessionBootstrapCards'
import { MarkdownRenderer } from './MarkdownRenderer'

const IMAGE_EXTS = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.svg', '.bmp', '.ico']
function isImageFile(name: string): boolean {
  return IMAGE_EXTS.some((ext) => name.toLowerCase().endsWith(ext))
}
function attachmentSrc(path: string): string {
  if (isBrowserMode()) {
    return `${getApiBase()}/file?path=${encodeURIComponent(path)}`
  }
  return `local-file://${encodeURIComponent(path)}`
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.round(seconds % 60)
  return `${minutes}m ${remainingSeconds}s`
}

function formatWorkingState(state?: string): string {
  switch (state) {
    case 'streaming_assistant_message':
      return 'Generating...'
    case 'executing_tool':
      return 'Running tool...'
    case 'waiting_for_tool_confirmation':
      return 'Waiting for confirmation...'
    case 'compacting_conversation':
      return 'Compacting conversation...'
    default:
      return 'Thinking...'
  }
}

interface ChatViewProps {
  sessionId: string
  messages: ChatMessage[]
  isRunning: boolean
  noProject: boolean
  activeProjectDir?: string
  workingState?: string
  pendingPermissionRequest?: PendingPermissionRequest | null
  pendingSendMessageIds?: Record<string, true>
  setupScript?: SessionSetupState | null
  workspacePrepStatus?: 'running' | 'completed' | null
  onRetrySetupScript?: () => void
  onSkipSetupScript?: () => void
  onRespondPermission?: (params: {
    selectedOption: DroidPermissionOption
    selectedExitSpecModeOptionIndex?: number
    exitSpecModeComment?: string
  }) => void
  onRequestSpecChanges?: () => void
}

function ChatView({
  sessionId,
  messages,
  isRunning,
  noProject,
  activeProjectDir,
  workingState,
  pendingPermissionRequest,
  pendingSendMessageIds = {},
  setupScript = null,
  workspacePrepStatus = null,
  onRetrySetupScript,
  onSkipSetupScript,
  onRespondPermission,
  onRequestSpecChanges,
}: ChatViewProps): React.JSX.Element {
  const virtuosoRef = useRef<VirtuosoHandle>(null)
  const isAtBottomRef = useRef(true)
  const projectName = activeProjectDir
    ? activeProjectDir.split(/[\\/]/).pop() || activeProjectDir
    : ''

  const isInitialRenderRef = useRef(true)
  const prevCountRef = useRef(messages.length)
  useEffect(() => {
    if (isInitialRenderRef.current) {
      isInitialRenderRef.current = false
      prevCountRef.current = messages.length
      return
    }
    const prev = prevCountRef.current
    prevCountRef.current = messages.length
    if (messages.length > prev && isAtBottomRef.current) {
      virtuosoRef.current?.scrollToIndex({ index: 'LAST', behavior: 'smooth' })
    }
  }, [messages.length])

  // Scroll to reveal footer content (Generating indicator or SpecReviewCard)
  // whenever it appears. Virtuoso's followOutput only scrolls to the last
  // message item, but the Footer renders below that and can be half-hidden.
  const lastMsgRole = messages.length > 0 ? messages[messages.length - 1].role : undefined
  const showsGenerating = isRunning && lastMsgRole !== 'assistant'
  const showsExitSpec = isExitSpecPermission(pendingPermissionRequest)
  const hasFooterContent = showsGenerating || showsExitSpec

  useEffect(() => {
    if (!hasFooterContent || !isAtBottomRef.current) return

    // First scroll to last item, then after footer renders, scroll
    // the remaining distance so the footer is fully visible.
    virtuosoRef.current?.scrollToIndex({ index: 'LAST', behavior: 'smooth' })
    const timer = setTimeout(() => {
      virtuosoRef.current?.scrollBy({ top: 99999, behavior: 'smooth' })
    }, 150)
    return () => clearTimeout(timer)
  }, [hasFooterContent, showsGenerating, showsExitSpec])

  if (messages.length === 0) {
    const showBootstrapCards =
      !noProject && Boolean((setupScript && setupScript.status !== 'idle') || workspacePrepStatus)

    return (
      <motion.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.35, ease: [0.16, 1, 0.3, 1] }}
        className="flex flex-1 items-center justify-center px-6"
      >
        <div className="w-full max-w-2xl space-y-5">
          <div className="flex flex-col items-center gap-2 text-center">
            <h2 className="text-base font-medium text-foreground">
              {noProject ? 'Select a Project' : 'What would you like to build?'}
            </h2>
            {!noProject && projectName && (
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground/70">
                <FolderOpen className="size-3" />
                <span className="font-mono">{projectName}</span>
              </p>
            )}
          </div>

          {showBootstrapCards && (
            <SessionBootstrapCards
              workspacePrepStatus={workspacePrepStatus}
              setupScript={setupScript}
              suppressSetupScriptRunningSpinner={workspacePrepStatus === 'running'}
              onRetrySetupScript={onRetrySetupScript}
              onSkipSetupScript={onSkipSetupScript}
            />
          )}
        </div>
      </motion.div>
    )
  }

  const lastMsg = messages[messages.length - 1]
  const isStreamingLast = isRunning && lastMsg?.role === 'assistant'
  const isExitSpec = isExitSpecPermission(pendingPermissionRequest)

  const handleFollowOutput = useCallback((atBottom: boolean) => {
    if (atBottom) return 'smooth' as const
    return false as const
  }, [])

  const handleAtBottomChange = useCallback((atBottom: boolean) => {
    isAtBottomRef.current = atBottom
  }, [])

  const renderItem = useCallback(
    (_index: number, msg: ChatMessage) => {
      const isLast = msg === messages[messages.length - 1]
      return (
        <div className="mx-auto max-w-3xl px-6 overflow-hidden">
          <MessageEntry
            message={msg}
            isStreaming={isLast && isStreamingLast}
            isSessionRunning={isRunning}
            isPendingSend={Boolean(pendingSendMessageIds[msg.id])}
          />
        </div>
      )
    },
    [messages, isStreamingLast, isRunning, pendingSendMessageIds],
  )

  const footer = useCallback(
    () => (
      <div className="mx-auto max-w-3xl px-6 pb-6 overflow-hidden">
        {pendingPermissionRequest && isExitSpec && onRespondPermission && (
          <SpecReviewCard
            request={pendingPermissionRequest}
            onRespond={onRespondPermission}
            onRequestChanges={onRequestSpecChanges || (() => {})}
          />
        )}
        {isRunning && lastMsg?.role !== 'assistant' && (
          <div className="flex items-center gap-2 py-3 text-sm text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" />
            <span>{formatWorkingState(workingState)}</span>
          </div>
        )}
      </div>
    ),
    [
      pendingPermissionRequest,
      isExitSpec,
      onRespondPermission,
      onRequestSpecChanges,
      isRunning,
      lastMsg,
      workingState,
    ],
  )

  return (
    <Virtuoso
      key={sessionId}
      ref={virtuosoRef}
      className="flex-1 chat-scroll-container "
      data={messages}
      itemContent={renderItem}
      computeItemKey={(_index, msg) => msg.id}
      followOutput={handleFollowOutput}
      atBottomStateChange={handleAtBottomChange}
      atBottomThreshold={40}
      initialTopMostItemIndex={messages.length - 1}
      defaultItemHeight={120}
      increaseViewportBy={{ top: 400, bottom: 200 }}
      components={{ Footer: footer }}
      style={{ flex: 1 }}
    />
  )
}

function MessageEntry({
  message,
  isStreaming,
  isSessionRunning,
  isPendingSend,
}: {
  message: ChatMessage
  isStreaming: boolean
  isSessionRunning: boolean
  isPendingSend?: boolean
}) {
  const [previewImage, setPreviewImage] = useState<string | null>(null)
  const blocks = Array.isArray(message.blocks) ? message.blocks : []

  if (message.role === 'user') {
    const cmd = blocks.find((b): b is CommandBlock => b.kind === 'command')
    const skill = blocks.find((b): b is SkillBlock => b.kind === 'skill')
    const text =
      blocks.find((b) => b.kind === 'text')?.kind === 'text'
        ? (blocks.find((b) => b.kind === 'text') as TextBlock).content
        : ''
    const attachments = blocks.filter((b): b is AttachmentBlock => b.kind === 'attachment')
    const imageAttachments = attachments.filter((a) => isImageFile(a.name))
    const fileAttachments = attachments.filter((a) => !isImageFile(a.name))
    return (
      <>
        <div className="flex items-center justify-end gap-2 pb-3 pt-4">
          {isPendingSend && (
            <span className="size-2 animate-pulse rounded-full bg-muted-foreground/50" />
          )}
          <div className="max-w-[85%] rounded-2xl bg-muted px-4 py-2.5">
            {imageAttachments.length > 0 && (
              <div className={cn('flex flex-wrap gap-1.5', text && 'mb-2')}>
                {imageAttachments.map((att, i) => (
                  <div
                    key={i}
                    className="size-16 cursor-pointer overflow-hidden rounded-xl bg-background shadow-sm ring-1 ring-border/40 transition-opacity hover:opacity-80"
                    onClick={() => setPreviewImage(attachmentSrc(att.path))}
                  >
                    <img
                      src={attachmentSrc(att.path)}
                      alt={att.name}
                      className="size-full object-cover"
                      onError={() => {
                        console.warn(
                          `[attachment-image-load-failed] name=${att.name} path=${att.path}`,
                        )
                      }}
                    />
                  </div>
                ))}
              </div>
            )}
            {cmd && (
              <div className="mb-1 flex">
                <span className="inline-flex items-center rounded-full bg-background/70 px-2 py-0.5 text-xs font-medium text-foreground">
                  {cmd.name}
                </span>
              </div>
            )}
            {!cmd && skill && (
              <div className="mb-1 flex">
                <span className="inline-flex items-center gap-1 rounded-full bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-700">
                  <BookOpen className="size-3" />
                  {skill.name}
                </span>
              </div>
            )}
            {text && (
              <p className="whitespace-pre-wrap break-words text-sm text-foreground">{text}</p>
            )}
            {fileAttachments.length > 0 && (
              <div className={cn('flex flex-wrap gap-1.5', text && 'mt-2')}>
                {fileAttachments.map((att, i) => (
                  <span
                    key={i}
                    className="flex items-center gap-1 rounded-md bg-background/60 px-2 py-1 text-xs text-foreground"
                  >
                    <Paperclip className="size-3 text-muted-foreground" />
                    <span className="max-w-[160px] truncate">{att.name}</span>
                  </span>
                ))}
              </div>
            )}
          </div>
        </div>
        <AnimatePresence>
          {previewImage && (
            <ImagePreviewModal src={previewImage} onClose={() => setPreviewImage(null)} />
          )}
        </AnimatePresence>
      </>
    )
  }

  if (message.role === 'error') {
    const text = blocks[0]?.kind === 'text' ? blocks[0].content : ''
    return (
      <div className="my-2 rounded border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive-foreground">
        {message.errorType && <span className="mb-1 block font-medium">{message.errorType}</span>}
        {text}
        {message.errorTimestamp && (
          <span className="mt-1 block text-[10px] opacity-60">
            {new Date(message.errorTimestamp).toLocaleTimeString()}
          </span>
        )}
      </div>
    )
  }

  const visibleBlocks = blocks.filter((b) => !isTodoWriteBlock(b))
  const hasNonThinkingContent = visibleBlocks.some(
    (b) => (b.kind === 'text' && b.content.trim()) || b.kind === 'tool_call',
  )

  return (
    <div className="py-1">
      {visibleBlocks.map((block, i) => {
        if (block.kind === 'thinking') {
          return (
            <ThinkingSection
              key={i}
              content={block.content}
              isStreaming={isStreaming && !hasNonThinkingContent}
              defaultExpanded={!hasNonThinkingContent}
            />
          )
        }
        if (block.kind === 'text') {
          const isLastBlock = i === visibleBlocks.length - 1
          return (
            <AgentText key={i} content={block.content} isStreaming={isStreaming && isLastBlock} />
          )
        }
        if (block.kind === 'tool_call') {
          return <ToolActivity key={i} block={block} isSessionRunning={isSessionRunning} />
        }
        return null
      })}
      {message.endTimestamp && message.timestamp > 0 && (
        <div className="mt-1 text-[11px] text-muted-foreground/80">
          {formatDuration(message.endTimestamp - message.timestamp)}
        </div>
      )}
    </div>
  )
}

function ThinkingSection({
  content,
  isStreaming,
  defaultExpanded,
}: {
  content: string
  isStreaming: boolean
  defaultExpanded: boolean
}) {
  const [expanded, setExpanded] = useState(defaultExpanded)
  const wasExpandedByDefault = useRef(defaultExpanded)

  useEffect(() => {
    if (wasExpandedByDefault.current && !defaultExpanded) {
      setExpanded(false)
      wasExpandedByDefault.current = false
    }
  }, [defaultExpanded])

  if (!content.trim() && !isStreaming) return null

  return (
    <div className="py-0.5">
      <button
        className={cn(
          'flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs transition-colors',
          'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
        )}
        onClick={() => setExpanded(!expanded)}
      >
        {isStreaming ? (
          <Loader2 className="size-3 shrink-0 animate-spin text-muted-foreground" />
        ) : (
          <Brain className="size-3 shrink-0 text-muted-foreground" />
        )}
        <span className="font-medium">Thinking</span>
        {!expanded && (
          <span className="truncate font-mono opacity-40">
            {content.trim().slice(0, 80)}
            {content.trim().length > 80 ? '...' : ''}
          </span>
        )}
        {expanded ? (
          <ChevronDown className="ml-auto size-3 shrink-0 opacity-40" />
        ) : (
          <ChevronRight className="ml-auto size-3 shrink-0 opacity-40" />
        )}
      </button>

      {expanded && (
        <div className="ml-5 mt-1 mb-2">
          <pre className="max-h-64 overflow-y-auto whitespace-pre-wrap break-words rounded-md bg-muted px-3 py-2 text-[11px] leading-5 text-muted-foreground dark:bg-muted/50">
            {content}
          </pre>
        </div>
      )}
    </div>
  )
}

function AgentText({ content, isStreaming }: { content: string; isStreaming: boolean }) {
  if (!content.trim()) return null

  return (
    <MarkdownRenderer className="text-foreground/90" content={content} isStreaming={isStreaming} />
  )
}

function useTick(enabled: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!enabled) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [enabled])
  return now
}

function useElapsedTime(startTimestamp?: number, endTimestamp?: number): string | null {
  const isRunning = startTimestamp !== undefined && endTimestamp === undefined
  const now = useTick(isRunning)
  if (startTimestamp === undefined) return null
  const elapsed = (endTimestamp ?? now) - startTimestamp
  return formatDuration(Math.max(0, elapsed))
}

function parseTaskProgress(progress: string): {
  toolName: string
  status?: string
  details?: string
} | null {
  try {
    const obj = JSON.parse(progress)
    if (obj && typeof obj === 'object' && typeof obj.toolName === 'string') {
      return {
        toolName: obj.toolName,
        status: typeof obj.status === 'string' ? obj.status : undefined,
        details: typeof obj.details === 'string' ? obj.details : undefined,
      }
    }
  } catch {
    // not JSON
  }
  return null
}

function TaskProgressView({ progress, isComplete }: { progress: string; isComplete?: boolean }) {
  const parsed = parseTaskProgress(progress)

  if (!parsed) {
    return (
      <pre className="max-h-48 overflow-y-auto whitespace-pre-wrap break-all rounded-md bg-muted px-3 py-2 text-[11px] leading-5 text-muted-foreground">
        {progress}
      </pre>
    )
  }

  const icon = getToolIcon(parsed.toolName)
  const isExecuting = parsed.status === 'executing' && !isComplete
  const displayStatus = isComplete && parsed.status === 'executing' ? undefined : parsed.status
  return (
    <div className="flex items-center gap-2 rounded-md bg-muted px-3 py-2 text-[11px] text-muted-foreground">
      {isExecuting ? (
        <Loader2 className="size-3 shrink-0 animate-spin" />
      ) : (
        <span className="shrink-0">{icon}</span>
      )}
      <span className="font-medium">{parsed.toolName}</span>
      {displayStatus && (
        <span className="rounded bg-background/60 px-1.5 py-0.5 text-[10px]">{displayStatus}</span>
      )}
      {parsed.details && (
        <span className="truncate font-mono opacity-60">
          {parsed.details.length > 100 ? parsed.details.slice(0, 100) + '...' : parsed.details}
        </span>
      )}
    </div>
  )
}

function ToolActivity({
  block,
  isSessionRunning,
}: {
  block: ToolCallBlock
  isSessionRunning: boolean
}) {
  const [expanded, setExpanded] = useState(false)
  const hasResult = block.result !== undefined
  const icon = getToolIcon(block.toolName)
  const applyPatchPath = block.toolName === 'ApplyPatch' ? getApplyPatchTargetPath(block) : ''
  const summary = applyPatchPath ? '' : getToolSummary(block)
  const isTask = block.toolName === 'Task'
  const elapsed = useElapsedTime(
    isTask ? block.startTimestamp : undefined,
    isTask ? block.endTimestamp : undefined,
  )

  // Show spinner only if no result AND session is still running
  const isLoading = !hasResult && isSessionRunning
  const isSkill = /skill/i.test(block.toolName)
  const skillName = isSkill && block.parameters.skill ? String(block.parameters.skill) : null

  return (
    <div className="py-0.5">
      <button
        className={cn(
          'flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs transition-colors',
          'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
        )}
        onClick={() => setExpanded(!expanded)}
      >
        {isLoading ? (
          <Loader2 className="size-3 shrink-0 animate-spin text-muted-foreground" />
        ) : block.isError ? (
          <span className="size-3 shrink-0 text-destructive-foreground">{icon}</span>
        ) : hasResult ? (
          isSkill ? (
            <span className="size-3 shrink-0 text-amber-500">{icon}</span>
          ) : (
            <Check className="size-3 shrink-0 text-emerald-600" />
          )
        ) : (
          <span className="size-3 shrink-0 text-muted-foreground">{icon}</span>
        )}
        <span className="font-medium">{block.toolName}</span>
        {applyPatchPath && (
          <span className="min-w-0 truncate font-mono opacity-60">{applyPatchPath}</span>
        )}
        {skillName && (
          <span className="ml-1 truncate rounded bg-amber-500/10 px-1.5 py-0.5 font-mono text-[10px] text-amber-700">
            {skillName}
          </span>
        )}
        {summary && <span className="min-w-0 truncate font-mono opacity-60">{summary}</span>}
        {elapsed && (
          <span className="ml-1 shrink-0 tabular-nums text-[10px] opacity-50">{elapsed}</span>
        )}
        {expanded ? (
          <ChevronDown className="ml-auto size-3 shrink-0 opacity-40" />
        ) : (
          <ChevronRight className="ml-auto size-3 shrink-0 opacity-40" />
        )}
      </button>

      {expanded && (
        <div className="ml-5 mt-1 mb-2">
          <ToolExpandedContent block={block} />
        </div>
      )}
    </div>
  )
}

function ToolExpandedContent({ block }: { block: ToolCallBlock }) {
  if (block.toolName === 'Task') {
    return <TaskExpandedView block={block} />
  }

  const isEdit = /edit|create|write|multiedit/i.test(block.toolName)
  const hasProgress = typeof block.progress === 'string' && block.progress.trim().length > 0
  const hasResult = block.result !== undefined
  const applyPatchResult =
    block.toolName === 'ApplyPatch' && hasResult ? parseApplyPatchResult(block.result || '') : null

  return (
    <>
      {isEdit && block.parameters.old_str !== undefined ? (
        <DiffView
          filePath={String(block.parameters.file_path || '')}
          oldStr={String(block.parameters.old_str)}
          newStr={String(block.parameters.new_str ?? '')}
        />
      ) : applyPatchResult ? (
        <ApplyPatchDiffView result={applyPatchResult} />
      ) : block.parameters.command ? (
        <pre className="whitespace-pre-wrap break-all rounded-md bg-zinc-950 px-3 py-2 text-[11px] leading-5 text-zinc-300">
          <span className="text-zinc-500">$ </span>
          {String(block.parameters.command)}
        </pre>
      ) : (
        <pre className="whitespace-pre-wrap break-all rounded-md bg-muted px-3 py-2 text-[11px] leading-5 text-muted-foreground">
          {JSON.stringify(block.parameters, null, 2)}
        </pre>
      )}

      {hasProgress && (
        <pre className="mt-1 max-h-48 overflow-y-auto whitespace-pre-wrap break-all rounded-md bg-muted px-3 py-2 text-[11px] leading-5 text-muted-foreground">
          {block.progress}
        </pre>
      )}

      {hasResult && (!isEdit || block.isError) && !applyPatchResult && (
        <ResultView result={block.result || ''} isError={block.isError} isCode={isEdit} />
      )}
    </>
  )
}

function TaskExpandedView({ block }: { block: ToolCallBlock }) {
  const p = block.parameters
  const subagentType = p.subagent_type ? String(p.subagent_type) : ''
  const description = p.description ? String(p.description) : ''
  const prompt = p.prompt ? String(p.prompt) : ''
  const hasProgress = typeof block.progress === 'string' && block.progress.trim().length > 0
  const hasResult = block.result !== undefined
  const [promptExpanded, setPromptExpanded] = useState(false)
  const promptPreviewLen = 300

  return (
    <div className="space-y-1.5">
      <div className="rounded-md bg-muted px-3 py-2 text-[11px] leading-5 text-muted-foreground">
        {subagentType && (
          <div>
            <span className="text-foreground/70">droid:</span>{' '}
            <span className="font-mono">{subagentType}</span>
          </div>
        )}
        {description && (
          <div>
            <span className="text-foreground/70">task:</span> {description}
          </div>
        )}
        {prompt && (
          <div className="mt-1">
            <span className="text-foreground/70">prompt:</span>{' '}
            <span className="whitespace-pre-wrap break-words">
              {promptExpanded || prompt.length <= promptPreviewLen
                ? prompt
                : prompt.slice(0, promptPreviewLen) + '...'}
            </span>
            {prompt.length > promptPreviewLen && (
              <button
                className="ml-1 text-[10px] text-muted-foreground/80 hover:text-foreground"
                onClick={(e) => {
                  e.stopPropagation()
                  setPromptExpanded(!promptExpanded)
                }}
              >
                {promptExpanded ? 'less' : 'more'}
              </button>
            )}
          </div>
        )}
      </div>

      {hasProgress && <TaskProgressView progress={block.progress!} isComplete={hasResult} />}

      {hasResult && (
        <ResultView result={block.result || ''} isError={block.isError} isCode={false} />
      )}
    </div>
  )
}

type DiffLine = {
  prefix: '+' | '-'
  text: string
}

function SharedDiffView({
  filePath,
  lines,
  previewMaxLines,
  previewMaxChars,
}: {
  filePath: string
  lines: DiffLine[]
  previewMaxLines?: number
  previewMaxChars?: number
}) {
  const [expanded, setExpanded] = useState(false)
  const fileName = filePath.split('/').slice(-2).join('/')
  const combinedLength = lines.reduce((total, line) => total + line.text.length, 0)
  const truncated =
    !expanded &&
    ((previewMaxLines !== undefined && lines.length > previewMaxLines) ||
      (previewMaxChars !== undefined && combinedLength > previewMaxChars))

  let renderedCharCount = 0
  const visibleLines = truncated
    ? lines.filter((line, index) => {
        if (previewMaxLines !== undefined && index >= previewMaxLines) return false
        if (previewMaxChars === undefined) return true
        if (renderedCharCount >= previewMaxChars) return false
        renderedCharCount += line.text.length
        return true
      })
    : lines

  return (
    <div>
      <pre className="whitespace-pre-wrap break-all rounded-md bg-zinc-950 px-3 py-2 text-[11px] leading-5">
        {fileName && <div className="mb-1 text-zinc-500">{fileName}</div>}
        {visibleLines.map((line, i) => (
          <div
            key={`${line.prefix}${i}`}
            className={line.prefix === '-' ? 'text-red-400/80' : 'text-emerald-400/80'}
          >
            <span
              className={cn(
                'select-none',
                line.prefix === '-' ? 'text-red-600/50' : 'text-emerald-600/50',
              )}
            >
              {line.prefix}{' '}
            </span>
            {line.text}
          </div>
        ))}
      </pre>
      {truncated && (
        <button
          className="mt-0.5 text-[10px] text-muted-foreground hover:text-foreground"
          onClick={() => setExpanded(true)}
        >
          Show more
        </button>
      )}
    </div>
  )
}

function DiffView({
  filePath,
  oldStr,
  newStr,
}: {
  filePath: string
  oldStr: string
  newStr: string
}) {
  return (
    <SharedDiffView
      filePath={filePath}
      lines={[
        ...oldStr.split('\n').map((text) => ({ prefix: '-' as const, text })),
        ...newStr.split('\n').map((text) => ({ prefix: '+' as const, text })),
      ]}
    />
  )
}

type ParsedApplyPatchResult = {
  success: boolean
  filePath: string
  diff?: string
  content?: string
}

const APPLY_PATCH_PREVIEW_MAX_LINES = 40
const APPLY_PATCH_PREVIEW_MAX_CHARS = 2400

function parseApplyPatchResult(result: string): ParsedApplyPatchResult | null {
  try {
    const parsed = JSON.parse(result)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null
    if (typeof (parsed as any).success !== 'boolean') return null
    if (typeof (parsed as any).file_path !== 'string') return null
    const diff = typeof (parsed as any).diff === 'string' ? String((parsed as any).diff) : ''
    const content =
      typeof (parsed as any).content === 'string' ? String((parsed as any).content) : ''
    if (!diff && !content) return null
    return {
      success: (parsed as any).success,
      filePath: String((parsed as any).file_path),
      diff: diff || undefined,
      content: content || undefined,
    }
  } catch {
    return null
  }
}

function parseApplyPatchDiffLines(diff: string): DiffLine[] {
  const lines: DiffLine[] = []
  for (const line of diff.split('\n')) {
    if (!line || line.startsWith('*** ') || line.startsWith('@@')) continue
    if (line.startsWith('--- ') || line.startsWith('+++ ')) continue
    if (line.startsWith('-')) lines.push({ prefix: '-', text: line.slice(1) })
    if (line.startsWith('+')) lines.push({ prefix: '+', text: line.slice(1) })
  }
  return lines
}

function ApplyPatchDiffView({ result }: { result: ParsedApplyPatchResult }) {
  const lines = result.diff
    ? parseApplyPatchDiffLines(result.diff)
    : (result.content || '').split('\n').map((text) => ({ prefix: '+' as const, text }))

  return (
    <SharedDiffView
      filePath={result.filePath}
      lines={lines}
      previewMaxChars={APPLY_PATCH_PREVIEW_MAX_CHARS}
      previewMaxLines={APPLY_PATCH_PREVIEW_MAX_LINES}
    />
  )
}

function ResultView({ result, isError }: { result: string; isError?: boolean; isCode: boolean }) {
  const [showFull, setShowFull] = useState(false)
  const maxLen = 500
  const truncated = result.length > maxLen && !showFull
  const display = truncated ? result.slice(0, maxLen) : result

  return (
    <div className="mt-1">
      <pre
        className={cn(
          'max-h-48 overflow-y-auto whitespace-pre-wrap break-all rounded-md px-3 py-2 text-[11px] leading-5',
          isError
            ? 'bg-destructive/5 text-destructive-foreground'
            : 'bg-muted text-muted-foreground',
        )}
      >
        {display}
      </pre>
      {truncated && (
        <button
          className="mt-0.5 text-[10px] text-muted-foreground hover:text-foreground"
          onClick={() => setShowFull(true)}
        >
          Show {result.length - maxLen} more characters...
        </button>
      )}
    </div>
  )
}

function ImagePreviewModal({ src, onClose }: { src: string; onClose: () => void }) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onClose])

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.15 }}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70"
      onClick={onClose}
    >
      <button
        className="absolute top-4 right-4 flex size-9 items-center justify-center rounded-full bg-black/50 text-white transition-colors hover:bg-black/70"
        onClick={onClose}
      >
        <X className="size-5" />
      </button>
      <motion.img
        initial={{ scale: 0.95, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        exit={{ scale: 0.95, opacity: 0 }}
        transition={{ duration: 0.15, ease: [0.16, 1, 0.3, 1] }}
        src={src}
        alt="Preview"
        className="max-h-[85vh] max-w-[90vw] rounded-lg object-contain"
        onClick={(e) => e.stopPropagation()}
      />
    </motion.div>
  )
}

function getToolIcon(toolName: string): React.ReactNode {
  if (/skill/i.test(toolName)) return <BookOpen className="size-3 text-amber-500" />
  if (/edit|create|write|multiedit/i.test(toolName)) return <FileEdit className="size-3" />
  if (/execute/i.test(toolName)) return <Play className="size-3" />
  if (/read|glob|ls/i.test(toolName)) return <Search className="size-3" />
  if (/grep/i.test(toolName)) return <Search className="size-3" />
  if (/fetch|web/i.test(toolName)) return <Globe className="size-3" />
  return <Terminal className="size-3" />
}

function getApplyPatchTargetPath(block: ToolCallBlock): string {
  const result = typeof block.result === 'string' ? parseApplyPatchResult(block.result) : null
  if (result?.filePath) return result.filePath

  const rawInput = typeof block.parameters.input === 'string' ? block.parameters.input : ''
  const match = rawInput.match(/\*\*\* (?:Add|Update) File: (.+)/)
  return match?.[1]?.trim() || ''
}

function getToolSummary(block: ToolCallBlock): string {
  const p = block.parameters
  if (block.toolName === 'Task') {
    const subagentType = p.subagent_type ? String(p.subagent_type) : ''
    const description = p.description ? String(p.description) : ''
    const left = subagentType || ''
    const right = description ? description.trim() : ''
    const summary = right && left ? `${left} • ${right}` : left || right
    return summary.length > 80 ? summary.slice(0, 80) + '...' : summary
  }
  if (p.file_path) return String(p.file_path).split('/').slice(-2).join('/')
  if (p.command) {
    const cmd = String(p.command)
    return cmd.length > 80 ? cmd.slice(0, 80) + '...' : cmd
  }
  if (p.pattern) return `/${p.pattern}/`
  if (p.patterns) return String(p.patterns)
  if (p.query) return String(p.query).slice(0, 60)
  if (p.url) return String(p.url).slice(0, 60)
  return ''
}

export default ChatView

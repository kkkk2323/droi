import React, { useCallback, useEffect, useRef, useState } from 'react'
import { cn } from '../lib/utils'
import { ScrollArea } from './ui/scroll-area'
import {
  ChevronDown, ChevronRight, FileCode, FolderOpen, Terminal,
  FileEdit, Play, Search, Globe, Loader2, Check, Circle, Paperclip, X, BookOpen, AlertTriangle, Brain,
} from 'lucide-react'
import { Streamdown } from 'streamdown'
import type { ChatMessage, TextBlock, ToolCallBlock, AttachmentBlock, CommandBlock, SkillBlock, ThinkingBlock } from '@/types'
import type { SessionSetupState, PendingPermissionRequest } from '@/state/appReducer'
import type { DroidPermissionOption } from '@/types'
import { isTodoWriteBlock } from './TodoPanel'
import { isBrowserMode, getApiBase } from '@/droidClient'
import { SpecReviewCard, isExitSpecPermission } from './SpecReviewCard'

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




interface ChatViewProps {
  messages: ChatMessage[]
  isRunning: boolean
  noProject: boolean
  activeProjectDir?: string
  pendingPermissionRequest?: PendingPermissionRequest | null
  pendingSendMessageIds?: Record<string, true>
  setupScript?: SessionSetupState | null
  onRetrySetupScript?: () => void
  onSkipSetupScript?: () => void
  onRespondPermission?: (params: { selectedOption: DroidPermissionOption }) => void
  onRequestSpecChanges?: () => void
}

function ChatView({
  messages,
  isRunning,
  noProject,
  activeProjectDir,
  pendingPermissionRequest,
  pendingSendMessageIds = {},
  setupScript = null,
  onRetrySetupScript,
  onSkipSetupScript,
  onRespondPermission,
  onRequestSpecChanges,
}: ChatViewProps): React.JSX.Element {
  const bottomRef = useRef<HTMLDivElement>(null)
  const viewportRef = useRef<HTMLDivElement>(null)
  const prevMessagesRef = useRef<ChatMessage[]>([])
  const isAtBottomRef = useRef(true)
  const projectName = activeProjectDir ? activeProjectDir.split(/[\\/]/).pop() || activeProjectDir : ''

  const BOTTOM_THRESHOLD = 40

  const handleScroll = useCallback(() => {
    const el = viewportRef.current
    if (!el) return
    isAtBottomRef.current =
      el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_THRESHOLD
  }, [])

  useEffect(() => {
    const prev = prevMessagesRef.current
    const isSessionSwitch = prev.length === 0 || Math.abs(messages.length - prev.length) > 5
    prevMessagesRef.current = messages

    if (isSessionSwitch) {
      bottomRef.current?.scrollIntoView({ behavior: 'auto' })
      isAtBottomRef.current = true
      return
    }

    if (isAtBottomRef.current) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [messages])

  useEffect(() => {
    if (isExitSpecPermission(pendingPermissionRequest)) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [pendingPermissionRequest])

  if (messages.length === 0) {
    const showSetupCard = !noProject && Boolean(setupScript && setupScript.status !== 'idle')

    return (
      <div className="flex flex-1 items-center justify-center px-6">
        <div className="w-full max-w-2xl space-y-5">
          <div className="flex flex-col items-center gap-3 text-center">
            <div className="flex size-14 items-center justify-center rounded-2xl bg-muted">
              {noProject
                ? <FolderOpen className="size-7 text-muted-foreground" />
                : <Terminal className="size-7 text-muted-foreground" />}
            </div>
            <div>
              <h2 className="text-base font-medium text-foreground">
                {noProject ? 'Select a Project' : 'What would you like to build?'}
              </h2>
              <p className="mt-1 text-sm text-muted-foreground">
                {noProject
                  ? 'Add a project from the sidebar to get started'
                  : 'Describe a task and Droi will execute it'}
              </p>
              {!noProject && projectName && (
                <p className="mt-2 flex items-center justify-center gap-1.5 text-xs text-muted-foreground/70">
                  <FolderOpen className="size-3" />
                  <span className="font-mono">{projectName}</span>
                </p>
              )}
            </div>
          </div>

          {showSetupCard && setupScript && (
            <SetupScriptCard
              setupScript={setupScript}
              onRetry={onRetrySetupScript}
              onSkip={onSkipSetupScript}
            />
          )}
        </div>
      </div>
    )
  }

  // Check if the last assistant message is still being streamed
  const lastMsg = messages[messages.length - 1]
  const isStreamingLast = isRunning && lastMsg?.role === 'assistant'
  const isExitSpec = isExitSpecPermission(pendingPermissionRequest)

  return (
    <ScrollArea className="flex-1" viewportRef={viewportRef} onScroll={handleScroll}>
      <div className="mx-auto max-w-3xl px-6 py-4 overflow-hidden">
        {messages.map((msg, idx) => {
          const isLast = idx === messages.length - 1
          return (
            <MessageEntry
              key={msg.id}
              message={msg}
              isStreaming={isLast && isStreamingLast}
              isSessionRunning={isRunning}
              isPendingSend={Boolean(pendingSendMessageIds[msg.id])}
            />
          )
        })}

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
            <span>Thinking...</span>
          </div>
        )}

        <div ref={bottomRef} />
      </div>
    </ScrollArea>
  )
}

function SetupScriptCard({
  setupScript,
  onRetry,
  onSkip,
}: {
  setupScript: SessionSetupState
  onRetry?: () => void
  onSkip?: () => void
}) {
  const output = String(setupScript.output || '')
  const isDone = setupScript.status === 'completed' || setupScript.status === 'skipped'
  const [expanded, setExpanded] = useState(!isDone)

  useEffect(() => {
    if (isDone) setExpanded(false)
    else setExpanded(true)
  }, [isDone])

  return (
    <div className="mx-auto rounded-xl border border-border bg-card px-4 py-3 shadow-sm">
      <button
        type="button"
        className="flex w-full items-center gap-2 text-left text-sm font-medium"
        onClick={() => setExpanded((v) => !v)}
      >
        {setupScript.status === 'running' && <Loader2 className="size-4 animate-spin text-blue-500" />}
        {setupScript.status === 'failed' && <AlertTriangle className="size-4 text-amber-500" />}
        {setupScript.status === 'completed' && <Check className="size-4 text-emerald-600" />}
        {setupScript.status === 'skipped' && <Circle className="size-4 text-muted-foreground" />}

        <span className="flex-1">
          {setupScript.status === 'running' && 'Running setup script...'}
          {setupScript.status === 'failed' && 'Setup script failed'}
          {setupScript.status === 'completed' && 'Setup script completed'}
          {setupScript.status === 'skipped' && 'Setup script skipped'}
        </span>

        <ChevronDown className={cn(
          "size-3.5 shrink-0 text-muted-foreground transition-transform duration-300",
          expanded ? "rotate-180" : "rotate-0"
        )} />
      </button>

      <div className={cn(
        "grid transition-all duration-300 ease-out",
        expanded ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0"
      )}>
        <div className="overflow-hidden">
          {setupScript.script && (
            <p className="mt-2 text-xs text-muted-foreground">
              <span className="font-mono">{setupScript.script}</span>
            </p>
          )}

          {setupScript.status === 'failed' && setupScript.error && (
            <p className="mt-2 text-xs text-red-600">{setupScript.error}</p>
          )}

          {output.trim() && (
            <pre className="mt-2 max-h-48 overflow-y-auto whitespace-pre-wrap break-all rounded-md bg-zinc-950 px-3 py-2 text-[11px] leading-5 text-zinc-200">
              {output}
            </pre>
          )}

          {setupScript.status === 'failed' && (
            <div className="mt-3 flex items-center gap-2">
              <button
                type="button"
                onClick={(e) => { e.stopPropagation(); onRetry?.() }}
                className="rounded-md bg-foreground px-2.5 py-1 text-xs text-background transition-colors hover:bg-foreground/80"
              >
                Retry
              </button>
              <button
                type="button"
                onClick={(e) => { e.stopPropagation(); onSkip?.() }}
                className="rounded-md border border-border px-2.5 py-1 text-xs text-foreground transition-colors hover:bg-accent"
              >
                Skip
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
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

  if (message.role === 'user') {
    const cmd = message.blocks.find((b): b is CommandBlock => b.kind === 'command')
    const skill = message.blocks.find((b): b is SkillBlock => b.kind === 'skill')
    const text = message.blocks.find((b) => b.kind === 'text')?.kind === 'text'
      ? (message.blocks.find((b) => b.kind === 'text') as TextBlock).content
      : ''
    const attachments = message.blocks.filter((b): b is AttachmentBlock => b.kind === 'attachment')
    const imageAttachments = attachments.filter((a) => isImageFile(a.name))
    const fileAttachments = attachments.filter((a) => !isImageFile(a.name))
    const stateLabel = isPendingSend ? 'Pending' : ''

    return (
      <>
        <div className="flex justify-end pb-3 pt-4">
          <div className="max-w-[85%] rounded-2xl bg-muted px-4 py-2.5">
            {stateLabel && (
              <div className="mb-1 flex justify-end">
                <span
                  className={cn(
                    'inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium',
                    'bg-blue-500/10 text-blue-700'
                  )}
                >
                  {stateLabel}
                </span>
              </div>
            )}
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
                        console.warn(`[attachment-image-load-failed] name=${att.name} path=${att.path}`)
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
            {text && <p className="whitespace-pre-wrap break-words text-sm text-foreground">{text}</p>}
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
        {previewImage && (
          <ImagePreviewModal src={previewImage} onClose={() => setPreviewImage(null)} />
        )}
      </>
    )
  }

  if (message.role === 'error') {
    const text = message.blocks[0]?.kind === 'text' ? message.blocks[0].content : ''
    return (
      <div className="my-2 rounded border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
        {text}
      </div>
    )
  }

  const visibleBlocks = message.blocks.filter((b) => !isTodoWriteBlock(b))
  const hasNonThinkingContent = visibleBlocks.some((b) => (b.kind === 'text' && b.content.trim()) || b.kind === 'tool_call')

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
            <AgentText
              key={i}
              content={block.content}
              isStreaming={isStreaming && isLastBlock}
            />
          )
        }
        if (block.kind === 'tool_call') {
          return <ToolActivity key={i} block={block} isSessionRunning={isSessionRunning} />
        }
        return null
      })}
    </div>
  )
}

function ThinkingSection({ content, isStreaming, defaultExpanded }: { content: string; isStreaming: boolean; defaultExpanded: boolean }) {
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
          'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
        )}
        onClick={() => setExpanded(!expanded)}
      >
        {isStreaming
          ? <Loader2 className="size-3 shrink-0 animate-spin text-violet-500" />
          : <Brain className="size-3 shrink-0 text-violet-500" />}
        <span className="font-medium">Thinking</span>
        {!expanded && (
          <span className="truncate font-mono opacity-40">
            {content.trim().slice(0, 80)}{content.trim().length > 80 ? '...' : ''}
          </span>
        )}
        {expanded
          ? <ChevronDown className="ml-auto size-3 shrink-0 opacity-40" />
          : <ChevronRight className="ml-auto size-3 shrink-0 opacity-40" />}
      </button>

      {expanded && (
        <div className="ml-5 mt-1 mb-2">
          <pre className="max-h-64 overflow-y-auto whitespace-pre-wrap break-words rounded-md bg-violet-50 px-3 py-2 text-[11px] leading-5 text-violet-900/70 dark:bg-violet-950/30 dark:text-violet-300/70">
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
    <div className="prose prose-sm max-w-none text-foreground/90 prose-headings:text-foreground prose-p:leading-relaxed prose-pre:bg-zinc-950 prose-pre:text-zinc-200 prose-pre:overflow-x-auto prose-code:text-foreground prose-code:break-all overflow-hidden break-words">
      <Streamdown>{content}</Streamdown>
    </div>
  )
}

function ToolActivity({ block, isSessionRunning }: { block: ToolCallBlock; isSessionRunning: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const hasResult = block.result !== undefined
  const hasProgress = typeof block.progress === 'string' && block.progress.trim().length > 0
  const icon = getToolIcon(block.toolName)
  const summary = getToolSummary(block)
  const isEdit = /edit|create|write|multiedit/i.test(block.toolName)

  // Show spinner only if no result AND session is still running
  const isLoading = !hasResult && isSessionRunning
  const isSkill = /skill/i.test(block.toolName)
  const skillName = isSkill && block.parameters.skill ? String(block.parameters.skill) : null

  return (
    <div className="py-0.5">
      <button
        className={cn(
          'flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs transition-colors',
          'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
        )}
        onClick={() => setExpanded(!expanded)}
      >
        {isLoading ? (
          <Loader2 className="size-3 shrink-0 animate-spin text-blue-500" />
        ) : block.isError ? (
          <span className="size-3 shrink-0 text-red-500">{icon}</span>
        ) : isSkill ? (
          <span className="size-3 shrink-0 text-amber-500">{icon}</span>
        ) : hasResult ? (
          <Check className="size-3 shrink-0 text-emerald-600" />
        ) : (
          <span className="size-3 shrink-0 text-muted-foreground">{icon}</span>
        )}
        <span className="font-medium">{block.toolName}</span>
        {skillName && <span className="ml-1 truncate rounded bg-amber-50 px-1.5 py-0.5 font-mono text-[10px] text-amber-600">{skillName}</span>}
        {summary && <span className="truncate font-mono opacity-60">{summary}</span>}
        {expanded
          ? <ChevronDown className="ml-auto size-3 shrink-0 opacity-40" />
          : <ChevronRight className="ml-auto size-3 shrink-0 opacity-40" />}
      </button>

      {expanded && (
        <div className="ml-5 mt-1 mb-2">
          {isEdit && block.parameters.old_str !== undefined ? (
            <DiffView
              filePath={String(block.parameters.file_path || '')}
              oldStr={String(block.parameters.old_str)}
              newStr={String(block.parameters.new_str ?? '')}
            />
          ) : block.parameters.command ? (
            <pre className="whitespace-pre-wrap break-all rounded-md bg-zinc-950 px-3 py-2 text-[11px] leading-5 text-zinc-300">
              <span className="text-zinc-500">$ </span>{String(block.parameters.command)}
            </pre>
          ) : (
            <pre className="whitespace-pre-wrap break-all rounded-md bg-zinc-50 px-3 py-2 text-[11px] leading-5 text-zinc-600">
              {JSON.stringify(block.parameters, null, 2)}
            </pre>
          )}

          {hasProgress && (
            <pre className="mt-1 max-h-48 overflow-y-auto whitespace-pre-wrap break-all rounded-md bg-zinc-50 px-3 py-2 text-[11px] leading-5 text-zinc-600">
              {block.progress}
            </pre>
          )}

          {hasResult && (
            <ResultView result={block.result || ''} isError={block.isError} isCode={isEdit} />
          )}
        </div>
      )}
    </div>
  )
}

function DiffView({ filePath, oldStr, newStr }: { filePath: string; oldStr: string; newStr: string }) {
  const fileName = filePath.split('/').slice(-2).join('/')
  return (
    <pre className="whitespace-pre-wrap break-all rounded-md bg-zinc-950 px-3 py-2 text-[11px] leading-5">
      {fileName && <div className="mb-1 text-zinc-500">{fileName}</div>}
      {oldStr.split('\n').map((line, i) => (
        <div key={`o${i}`} className="text-red-400/80">
          <span className="select-none text-red-600/50">- </span>{line}
        </div>
      ))}
      {newStr.split('\n').map((line, i) => (
        <div key={`n${i}`} className="text-emerald-400/80">
          <span className="select-none text-emerald-600/50">+ </span>{line}
        </div>
      ))}
    </pre>
  )
}

function ResultView({ result, isError, isCode }: { result: string; isError?: boolean; isCode: boolean }) {
  const [showFull, setShowFull] = useState(false)
  const maxLen = 500
  const truncated = result.length > maxLen && !showFull
  const display = truncated ? result.slice(0, maxLen) : result

  return (
    <div className="mt-1">
      <pre
        className={cn(
          'max-h-48 overflow-y-auto whitespace-pre-wrap break-all rounded-md px-3 py-2 text-[11px] leading-5',
          isError ? 'bg-red-50 text-red-700' : 'bg-zinc-50 text-zinc-600'
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
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70"
      onClick={onClose}
    >
      <button
        className="absolute top-4 right-4 flex size-9 items-center justify-center rounded-full bg-black/50 text-white transition-colors hover:bg-black/70"
        onClick={onClose}
      >
        <X className="size-5" />
      </button>
      <img
        src={src}
        alt="Preview"
        className="max-h-[85vh] max-w-[90vw] rounded-lg object-contain"
        onClick={(e) => e.stopPropagation()}
      />
    </div>
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

function getToolSummary(block: ToolCallBlock): string {
  const p = block.parameters
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

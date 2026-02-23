import React, { useCallback, useEffect, useRef, useState } from 'react'
import { motion } from 'framer-motion'
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso'
import { cn } from '../lib/utils'
import {
  ChevronDown, ChevronRight, FileCode, FolderOpen, Terminal,
  FileEdit, Play, Search, Globe, Loader2, Check, Paperclip, X, BookOpen, Brain,
} from 'lucide-react'
import { Streamdown } from 'streamdown'
import type { ChatMessage, TextBlock, ToolCallBlock, AttachmentBlock, CommandBlock, SkillBlock, ThinkingBlock } from '@/types'
import type { SessionSetupState, PendingPermissionRequest } from '@/state/appReducer'
import type { DroidPermissionOption } from '@/types'
import { isTodoWriteBlock } from './TodoPanel'
import { isBrowserMode, getApiBase } from '@/droidClient'
import { SpecReviewCard, isExitSpecPermission } from './SpecReviewCard'
import { SessionBootstrapCards } from './SessionBootstrapCards'

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
  workspacePrepStatus?: 'running' | 'completed' | null
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
  workspacePrepStatus = null,
  onRetrySetupScript,
  onSkipSetupScript,
  onRespondPermission,
  onRequestSpecChanges,
}: ChatViewProps): React.JSX.Element {
  const virtuosoRef = useRef<VirtuosoHandle>(null)
  const isAtBottomRef = useRef(true)
  const projectName = activeProjectDir ? activeProjectDir.split(/[\\/]/).pop() || activeProjectDir : ''

  useEffect(() => {
    if (isExitSpecPermission(pendingPermissionRequest)) {
      virtuosoRef.current?.scrollToIndex({ index: 'LAST', behavior: 'smooth' })
    }
  }, [pendingPermissionRequest])

  if (messages.length === 0) {
    const showBootstrapCards = !noProject && Boolean(
      (setupScript && setupScript.status !== 'idle') || workspacePrepStatus,
    )

    return (
      <motion.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.35, ease: [0.16, 1, 0.3, 1] }}
        className="flex flex-1 items-center justify-center px-6"
      >
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

  const renderItem = useCallback((_index: number, msg: ChatMessage) => {
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
  }, [messages, isStreamingLast, isRunning, pendingSendMessageIds])

  const footer = useCallback(() => (
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
          <span>Thinking...</span>
        </div>
      )}
    </div>
  ), [pendingPermissionRequest, isExitSpec, onRespondPermission, onRequestSpecChanges, isRunning, lastMsg])

  return (
    <Virtuoso
      ref={virtuosoRef}
      className="flex-1 chat-scroll-container "
      data={messages}
      itemContent={renderItem}
      computeItemKey={(_index, msg) => msg.id}
      followOutput={handleFollowOutput}
      atBottomStateChange={handleAtBottomChange}
      atBottomThreshold={40}
      initialTopMostItemIndex={messages.length - 1}
      increaseViewportBy={200}
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
                    'bg-muted text-muted-foreground'
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
      <div className="my-2 rounded border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive-foreground">
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
          ? <Loader2 className="size-3 shrink-0 animate-spin text-muted-foreground" />
          : <Brain className="size-3 shrink-0 text-muted-foreground" />}
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
          <Loader2 className="size-3 shrink-0 animate-spin text-muted-foreground" />
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
          isError ? 'bg-destructive/5 text-destructive-foreground' : 'bg-zinc-50 text-zinc-600'
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

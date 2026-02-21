import React, { useState, useRef, useEffect, useCallback, useMemo } from 'react'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from './ui/select'
import { ArrowUp, Square, Plus, X, Paperclip, Image as ImageIcon, BookOpen, Loader2 } from 'lucide-react'
import { AUTO_LEVELS, type CustomModelDef, type SlashCommandDef, type SkillDef, getModelReasoningLevels, getModelDefaultReasoning } from '@/types'
import { getDroidClient, isBrowserMode } from '@/droidClient'
import { KeyUsageIndicator } from './KeyUsageIndicator'
import { TokenUsageIndicator } from './TokenUsageIndicator'
import { McpStatusIndicator } from './McpStatusIndicator'
import { SettingsFlashIndicator } from './SettingsFlashIndicator'
import { ModelSelect } from './ModelSelect'
import { useSlashCommandsQuery, useSkillsQuery } from '@/hooks/useSlashCommands'

const droid = getDroidClient()

const IMAGE_EXTS = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.svg', '.bmp', '.ico']
function isImageFile(name: string): boolean {
  return IMAGE_EXTS.some((ext) => name.toLowerCase().endsWith(ext))
}

export interface Attachment {
  name: string
  path: string
}

function toAttachmentFromPath(path: string): Attachment {
  const parts = path.split(/[\\/]/)
  return { name: parts[parts.length - 1] || path, path }
}

function mergeAttachments(prev: Attachment[], next: Attachment[]): Attachment[] {
  if (next.length === 0) return prev
  const seen = new Set(prev.map((att) => att.path))
  const out = [...prev]
  for (const att of next) {
    if (!att?.path || seen.has(att.path)) continue
    seen.add(att.path)
    out.push(att)
  }
  return out
}

type SendInput = string | { text: string; tag?: { type: 'command' | 'skill'; name: string } }

type SelectedTag =
  | null
  | { type: 'command'; name: string }
  | { type: 'skill'; name: string }

type InputBarDraft = {
  input: string
  attachments: Attachment[]
  selectedTag: SelectedTag
}

const INPUT_BAR_DRAFT_CACHE_MAX = 100
const inputBarDraftCache = new Map<string, InputBarDraft>()

function normalizeDraftKey(raw: string | undefined): string {
  return String(raw || '').trim()
}

function emptyInputBarDraft(): InputBarDraft {
  return { input: '', attachments: [], selectedTag: null }
}

function readInputBarDraft(key: string): InputBarDraft {
  if (!key) return emptyInputBarDraft()
  const found = inputBarDraftCache.get(key)
  if (!found) return emptyInputBarDraft()
  return {
    input: String(found.input || ''),
    attachments: Array.isArray(found.attachments) ? [...found.attachments] : [],
    selectedTag: found.selectedTag ?? null,
  }
}

function writeInputBarDraft(key: string, draft: InputBarDraft): void {
  if (!key) return

  // Refresh LRU position
  if (inputBarDraftCache.has(key)) inputBarDraftCache.delete(key)
  inputBarDraftCache.set(key, {
    input: String(draft.input || ''),
    attachments: Array.isArray(draft.attachments) ? [...draft.attachments] : [],
    selectedTag: draft.selectedTag ?? null,
  })

  while (inputBarDraftCache.size > INPUT_BAR_DRAFT_CACHE_MAX) {
    const iter = inputBarDraftCache.keys().next()
    if (iter.done) break
    inputBarDraftCache.delete(iter.value)
  }
}

type BuiltinCommandDef = { name: string; description: string }

type SlashItem =
  | { type: 'command'; def: SlashCommandDef }
  | { type: 'skill'; def: SkillDef }
  | { type: 'builtin'; def: BuiltinCommandDef }

const BUILTIN_COMMANDS: BuiltinCommandDef[] = [
  { name: 'clear', description: 'Clear session and start fresh' },
  { name: 'reset', description: 'Reset session (alias for /clear)' },
  { name: 'restart', description: 'Restart session (alias for /clear)' },
]

type SlashCacheState = {
  at: number
  projectDir: string
  commands: SlashCommandDef[]
  skills: SkillDef[]
}

const SLASH_CACHE_TTL_MS = 1000
const SLASH_FETCH_DEBOUNCE_MS = 300
const MAX_SLASH_ITEMS_DISPLAY = 24


interface InputBarProps {
  draftKey?: string
  model: string
  autoLevel: string
  reasoningEffort: string
  customModels?: CustomModelDef[]
  onModelChange: (model: string) => void
  onAutoLevelChange: (level: string) => void
  onReasoningEffortChange: (level: string) => void
  onSend: (input: SendInput, attachments: Attachment[]) => void
  onCancel: () => void
  onForceCancel?: () => void
  isCancelling?: boolean
  isRunning: boolean
  disabled?: boolean
  disabledPlaceholder?: string
  activeProjectDir?: string
  onUiDebug?: (message: string) => void
  specChangesMode?: boolean
}

export function InputBar({
  draftKey,
  model, autoLevel, reasoningEffort, customModels, onModelChange, onAutoLevelChange, onReasoningEffortChange,
  onSend, onCancel, onForceCancel, isCancelling, isRunning, disabled, disabledPlaceholder,
  activeProjectDir,
  onUiDebug,
  specChangesMode,
}: InputBarProps) {
  const normalizedDraftKey = normalizeDraftKey(draftKey)
  const canPersistDraft = Boolean(normalizedDraftKey && String(activeProjectDir || '').trim())

  const initialDraftRef = useRef<InputBarDraft | null>(null)
  if (initialDraftRef.current === null) {
    initialDraftRef.current = canPersistDraft ? readInputBarDraft(normalizedDraftKey) : emptyInputBarDraft()
  }
  const initialDraft = initialDraftRef.current

  const [input, setInput] = useState(() => initialDraft.input)
  const [attachments, setAttachments] = useState<Attachment[]>(() => initialDraft.attachments)
  const [selectedTag, setSelectedTag] = useState<SelectedTag>(() => initialDraft.selectedTag)
  const [slashOpen, setSlashOpen] = useState(false)
  const [slashHighlightedIndex, setSlashHighlightedIndex] = useState(0)
  const [isDragOver, setIsDragOver] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const dragCounterRef = useRef(0)
  const slashProjectKey = String(activeProjectDir || '').trim()

  const latestDraftRef = useRef<InputBarDraft>({ input: '', attachments: [], selectedTag: null })
  latestDraftRef.current = { input, attachments, selectedTag }

  useEffect(() => {
    return () => {
      if (!canPersistDraft) return
      writeInputBarDraft(normalizedDraftKey, latestDraftRef.current)
    }
  }, [normalizedDraftKey, canPersistDraft])

  const shouldFetchSlash = Boolean(slashOpen) && !selectedTag && !disabled
  const { data: slashCommands = [], isLoading: slashCmdsLoading, error: slashCmdsError } = useSlashCommandsQuery(slashProjectKey, shouldFetchSlash)
  const { data: skills = [], isLoading: slashSkillsLoading, error: slashSkillsError } = useSkillsQuery(slashProjectKey, shouldFetchSlash)
  const slashLoading = slashCmdsLoading || slashSkillsLoading
  const slashError = slashCmdsError || slashSkillsError
    ? [slashCmdsError && `commands: ${(slashCmdsError as Error).message}`, slashSkillsError && `skills: ${(slashSkillsError as Error).message}`].filter(Boolean).join(' | ')
    : null

  useEffect(() => {
    if (!disabled) textareaRef.current?.focus()
  }, [disabled])

  useEffect(() => {
    if (specChangesMode) textareaRef.current?.focus()
  }, [specChangesMode])

  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 200) + 'px'
  }, [input])

  const slashState = useMemo(() => {
    if (selectedTag) return null
    const trimmed = input.trimStart()
    if (!trimmed.startsWith('/') || trimmed.startsWith('//')) return null
    const after = trimmed.slice(1)
    const m = after.match(/\s/)
    const splitIdx = m ? (m.index ?? -1) : -1
    const name = (splitIdx >= 0 ? after.slice(0, splitIdx) : after).trim()
    const args = splitIdx >= 0 ? after.slice(splitIdx).trim() : ''
    return { name, args }
  }, [input, selectedTag])

  const filteredSlashItems = useMemo<SlashItem[]>(() => {
    if (!slashState) return []
    const q = slashState.name
    const builtinItems: SlashItem[] = (q ? BUILTIN_COMMANDS.filter((c) => c.name.startsWith(q)) : BUILTIN_COMMANDS).map((def) => ({ type: 'builtin' as const, def }))
    const cmdItems: SlashItem[] = (q ? slashCommands.filter((c) => c.name.startsWith(q)) : slashCommands).map((def) => ({ type: 'command' as const, def }))
    const skillItems: SlashItem[] = (q ? skills.filter((s) => s.name.startsWith(q)) : skills).map((def) => ({ type: 'skill' as const, def }))
    return [...builtinItems, ...cmdItems, ...skillItems]
  }, [slashCommands, skills, slashState])

  const visibleSlashItems = useMemo<SlashItem[]>(
    () => filteredSlashItems.slice(0, MAX_SLASH_ITEMS_DISPLAY),
    [filteredSlashItems]
  )

  const getSlashApiBaseForDebug = useCallback((): string => {
    if (
      typeof (window as any).droid?.listSlashCommands === 'function'
      && typeof (window as any).droid?.listSkills === 'function'
    ) return 'ipc'
    const envBase = (import.meta as any)?.env?.VITE_DROID_API_BASE as string | undefined
    return (envBase || 'http://localhost:3001/api').replace(/\/+$/, '')
  }, [])

  useEffect(() => {
    const shouldOpen = Boolean(slashState) && !selectedTag && !disabled
    setSlashOpen(shouldOpen)
  }, [slashState, selectedTag, disabled])

  useEffect(() => {
    if (!slashOpen) return
    setSlashHighlightedIndex(0)
  }, [slashOpen, slashState?.name])

  useEffect(() => {
    if (!slashOpen) return
    if (visibleSlashItems.length === 0) {
      setSlashHighlightedIndex(0)
      return
    }
    setSlashHighlightedIndex((i) => Math.min(i, visibleSlashItems.length - 1))
  }, [slashOpen, visibleSlashItems.length])

  const confirmSelectedItem = useCallback((item: SlashItem) => {
    if (item.type === 'builtin') {
      onSend(`/${item.def.name}`, [])
      setInput('')
      setSlashOpen(false)
      setSlashHighlightedIndex(0)
      return
    }
    const trimmed = input.trimStart()
    const after = trimmed.startsWith('/') ? trimmed.slice(1) : ''
    const m = after.match(/\s/)
    const splitIdx = m ? (m.index ?? -1) : -1
    const args = splitIdx >= 0 ? after.slice(splitIdx).trim() : ''
    setSelectedTag({ type: item.type, name: item.def.name })
    setInput(args)
    setSlashOpen(false)
    setSlashHighlightedIndex(0)
    textareaRef.current?.focus()
  }, [input, onSend])

  const clearTag = useCallback(() => {
    setSelectedTag(null)
    setInput('')
    setAttachments([])
    setSlashOpen(false)
    setSlashHighlightedIndex(0)
    textareaRef.current?.focus()
  }, [])

  const addFilesFromPaths = useCallback(async (paths: string[]) => {
    if (!paths.length) return
    try {
      const projectDir = await droid.getProjectDir()
      if (!projectDir) return
      const saved: Attachment[] = await droid.saveAttachments({ sourcePaths: paths, projectDir })
      if (saved?.length) setAttachments((prev) => mergeAttachments(prev, saved))
    } catch {
      // ignore save errors
    }
  }, [])

  const saveClipboardBlob = useCallback(async (file: File) => {
    const projectDir = await droid.getProjectDir()
    if (!projectDir) return
    const buffer = await file.arrayBuffer()
    const result = await droid.saveClipboardImage({
      data: Array.from(new Uint8Array(buffer)),
      mimeType: file.type,
      projectDir,
      fileName: file.name,
    })
    if (result) setAttachments((prev) => mergeAttachments(prev, [result]))
  }, [])

  const handleAttachClick = useCallback(async () => {
    const paths: string[] | null = await droid.openFile()
    if (!paths?.length) return
    if (isBrowserMode()) {
      setAttachments((prev) => mergeAttachments(prev, paths.map(toAttachmentFromPath)))
      return
    }
    addFilesFromPaths(paths)
  }, [addFilesFromPaths])

  const handleRemoveAttachment = useCallback((index: number) => {
    setAttachments((prev) => prev.filter((_, i) => i !== index))
  }, [])

  const handleDragEnter = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    dragCounterRef.current++
    if (e.dataTransfer.types.includes('Files')) setIsDragOver(true)
  }, [])

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    dragCounterRef.current--
    if (dragCounterRef.current === 0) setIsDragOver(false)
  }, [])

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    e.dataTransfer.dropEffect = 'copy'
  }, [])

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    dragCounterRef.current = 0
    setIsDragOver(false)

    const files = Array.from(e.dataTransfer.files)
    // In Electron, dropped files have a .path property with the absolute filesystem path
    const paths = files.map((f) => (f as any).path as string).filter((p) => typeof p === 'string' && p.length > 0)
    if (paths.length > 0) {
      addFilesFromPaths(paths)
      return
    }
    // Fallback: if no path (e.g. dragged from browser), save as blob
    for (const file of files) {
      saveClipboardBlob(file)
    }
  }, [addFilesFromPaths, saveClipboardBlob])

  const handlePaste = useCallback(async (e: React.ClipboardEvent) => {
    // Approach 1: Check for files with filesystem paths (Cmd+C on a file in Finder)
    const pastedFiles = Array.from(e.clipboardData.files)
    const filePaths = pastedFiles.map((f) => (f as any).path as string).filter((p) => typeof p === 'string' && p.length > 0)
    if (filePaths.length > 0) {
      e.preventDefault()
      addFilesFromPaths(filePaths)
      return
    }

    // Approach 2: Check for clipboard image data (screenshots, copied images)
    const items = Array.from(e.clipboardData.items)
    const imageItems = items.filter((item) => item.type.startsWith('image/'))
    if (imageItems.length === 0) return // let default text paste happen

    e.preventDefault()

    // Read File objects synchronously before any await -- clipboardData becomes stale after event returns
    const blobs = imageItems.map((item) => item.getAsFile()).filter(Boolean) as File[]
    if (blobs.length === 0) return

    for (const blob of blobs) {
      await saveClipboardBlob(blob)
    }
  }, [addFilesFromPaths, saveClipboardBlob])

  const hasDraftToSend = !disabled && (() => {
    if (attachments.length > 0) return true
    if (selectedTag) return true
    const trimmed = input.trim()
    if (!trimmed) return false
    if (trimmed === '/' && slashOpen) return false
    return true
  })()
  const canSend = hasDraftToSend

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (!canSend) return

      if (!selectedTag && slashOpen && slashState && !slashState.name) return

      if (!selectedTag && slashOpen && slashState && slashState.name && visibleSlashItems.length > 0) {
        const idx = Math.min(Math.max(slashHighlightedIndex, 0), visibleSlashItems.length - 1)
        confirmSelectedItem(visibleSlashItems[idx])
        return
      }

      if (selectedTag) {
        onSend({ text: input.trim(), tag: selectedTag }, attachments)
        setSelectedTag(null)
        setInput('')
        setAttachments([])
        return
      }

      onSend(input.trim(), attachments)
      setInput('')
      setAttachments([])
      return
    }

    if (!selectedTag && slashOpen && visibleSlashItems.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSlashHighlightedIndex((i) => Math.min(i + 1, visibleSlashItems.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSlashHighlightedIndex((i) => Math.max(i - 1, 0))
      }
    }
  }

  const handleSubmit = () => {
    if (!canSend) return
    if (!selectedTag && slashOpen && slashState && !slashState.name) return
    if (!selectedTag && slashOpen && slashState && slashState.name && visibleSlashItems.length > 0) {
      const idx = Math.min(Math.max(slashHighlightedIndex, 0), visibleSlashItems.length - 1)
      confirmSelectedItem(visibleSlashItems[idx])
      return
    }

    if (selectedTag) {
      onSend({ text: input.trim(), tag: selectedTag }, attachments)
      setSelectedTag(null)
      setInput('')
      setAttachments([])
      return
    }

    onSend(input.trim(), attachments)
    setInput('')
    setAttachments([])
  }

  return (
	    <footer className="shrink-0 px-4 pb-4">
	      <div
	        className={`mx-auto max-w-3xl rounded-2xl border bg-card shadow-sm  transition-colors ${isDragOver ? 'border-blue-400 bg-blue-50/5' : 'border-border'}`}
        onDragEnter={handleDragEnter}
        onDragLeave={handleDragLeave}
        onDragOver={handleDragOver}
        onDrop={handleDrop}
      >
	        <div className="relative px-4 pt-3 pb-2">
	          {selectedTag && (
	            <div className="mb-2 flex items-center">
	              <span
	                className={selectedTag.type === 'skill'
	                  ? 'inline-flex max-w-full items-center gap-1 rounded-full bg-amber-500/10 px-2 py-1 text-xs font-medium text-amber-700 whitespace-nowrap'
	                  : 'inline-flex max-w-full items-center gap-1 rounded-full bg-muted px-2 py-1 text-xs font-medium text-foreground whitespace-nowrap'}
	              >
	                {selectedTag.type === 'skill' && <BookOpen className="size-3 shrink-0" />}
	                <span className="max-w-[240px] truncate">{selectedTag.name}</span>
	                <button
	                  type="button"
	                  className="ml-0.5 shrink-0 rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
	                  onClick={clearTag}
	                  title="Clear tag"
	                >
	                  <X className="size-3" />
	                </button>
	              </span>
	            </div>
	          )}
	          <div className="flex items-start gap-2">
	            <textarea
	              ref={textareaRef}
	              value={input}
	              onChange={(e) => setInput(e.target.value)}
	              onKeyDown={handleKeyDown}
	              onPaste={handlePaste}
	              onDragOver={handleDragOver}
	              onDragEnter={handleDragEnter}
	              onDragLeave={handleDragLeave}
	              onDrop={handleDrop}
	              placeholder={disabled
                ? (disabledPlaceholder || 'Select a project to start...')
	                  : isDragOver
	                    ? 'Drop files here...'
	                    : selectedTag
	                      ? 'Type tag arguments...'
	                      : specChangesMode
	                        ? 'Describe what to change in the plan...'
	                        : 'Ask anything, / for commands'}
	              disabled={disabled}
	              rows={1}
	              className="w-full resize-none bg-transparent text-sm leading-relaxed text-foreground placeholder:text-muted-foreground focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
	            />
	          </div>

		          {slashOpen && (
		            <div className="absolute left-4 right-4 bottom-full z-20 mb-2 max-h-60 overflow-auto rounded-xl border border-border bg-popover p-1 shadow">
		              {slashLoading && (
		                <div className="px-2 py-2 text-xs text-muted-foreground">
		                  Loading commands…
		                </div>
		              )}
		              {!slashLoading && slashError && (
		                <div className="px-2 py-2 text-xs text-muted-foreground">
		                  Failed to load from {getSlashApiBaseForDebug()}: {slashError}
		                </div>
		              )}
		              {!slashLoading && visibleSlashItems.length === 0 && (
		                <div className="px-2 py-2 text-xs text-muted-foreground">
		                  No commands or skills found (from ~/.factory/commands, ~/.factory/skills, ~/.agents/skills, and &lt;project&gt;/.factory/*)
		                </div>
		              )}
		              {!slashLoading && visibleSlashItems.length > 0 && (
		                <>
		                  {visibleSlashItems.map((item, i) => {
		                    const active = i === slashHighlightedIndex
		                    const isSkill = item.type === 'skill'
		                    const isBuiltin = item.type === 'builtin'
		                    const scope = (item.def as any).scope
		                    const desc = isBuiltin
		                      ? (item.def as BuiltinCommandDef).description
		                      : isSkill
		                        ? (item.def as SkillDef).description
		                        : ((item.def as SlashCommandDef).description || (item.def as SlashCommandDef).argumentHint)
		                    return (
		                      <button
		                        key={`${item.type}:${item.def.name}`}
		                        type="button"
		                        className={`w-full rounded-lg px-2 py-1.5 text-left text-xs transition-colors ${
		                          active ? 'bg-muted text-foreground' : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
		                        }`}
		                        onMouseDown={(e) => {
		                          e.preventDefault()
		                          confirmSelectedItem(item)
		                        }}
		                      >
		                        <div className="flex items-center gap-2">
		                          <span className="font-mono text-[11px] text-foreground">/{item.def.name}</span>
		                          {isBuiltin && (
		                            <span className="rounded bg-violet-500/10 px-1.5 py-0.5 text-[10px] text-violet-600">built-in</span>
		                          )}
		                          {isSkill && (
		                            <span className="rounded bg-amber-500/10 px-1.5 py-0.5 text-[10px] text-amber-700">skill</span>
		                          )}
		                          {!isSkill && !isBuiltin && (
		                            <span className="rounded bg-zinc-500/10 px-1.5 py-0.5 text-[10px] text-zinc-700">command</span>
		                          )}
		                          {scope === 'project' && (
		                            <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] text-emerald-600">project</span>
		                          )}
		                          {scope === 'user' && (
		                            <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] text-blue-600">user</span>
		                          )}
		                        </div>
		                        {desc && (
		                          <div className="mt-0.5 text-[11px] text-muted-foreground">
		                            {desc}
		                          </div>
		                        )}
		                      </button>
		                    )
		                  })}
		                </>
		              )}
		            </div>
		          )}
		        </div>

        {attachments.length > 0 && (
          <div className="flex gap-1.5 overflow-x-auto px-4 pb-2">
            {attachments.map((att, i) => (
              <span
                key={`${att.path}-${i}`}
                className="flex shrink-0 items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs text-foreground"
              >
                {isImageFile(att.name)
                  ? <ImageIcon className="size-3 text-muted-foreground" />
                  : <Paperclip className="size-3 text-muted-foreground" />
                }
                <span className="max-w-[120px] truncate">{att.name}</span>
                <button
                  type="button"
                  className="ml-0.5 rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
                  onClick={() => handleRemoveAttachment(i)}
                >
                  <X className="size-3" />
                </button>
              </span>
            ))}
          </div>
        )}

        <div className="flex items-center justify-between gap-2 px-3 pb-2">
          <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-hidden">
            <button
              type="button"
              className="flex size-7 shrink-0 items-center justify-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              title="Attach file"
              disabled={disabled}
              onClick={handleAttachClick}
            >
              <Plus className="size-4" />
            </button>

            <KeyUsageIndicator className="ml-2" />
            <McpStatusIndicator className="ml-1" />

            <ModelSelect
              value={model}
              onChange={onModelChange}
              customModels={customModels}
              variant="compact"
            />

            {(() => {
              const levels = getModelReasoningLevels(model)
              if (!levels) return null
              const displayValue = reasoningEffort || getModelDefaultReasoning(model) || levels[0]
              return (
                <Select value={displayValue} onValueChange={(v) => v && onReasoningEffortChange(v)}>
                  <SelectTrigger size="sm" className="h-7 w-auto shrink-0 gap-1 rounded-lg border-none bg-transparent px-1.5 text-xs text-muted-foreground shadow-none hover:bg-accent hover:text-foreground">
                    <span className="hidden md:inline">Reasoning: </span><span>{displayValue}</span>
                  </SelectTrigger>
                  <SelectContent side="top">
                    {levels.map((l) => (
                      <SelectItem key={l} value={l}>{l}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )
            })()}

            <Select value={autoLevel} onValueChange={(v) => v && onAutoLevelChange(v)}>
              <SelectTrigger size="sm" className="h-7 w-auto shrink-0 gap-1 rounded-lg border-none bg-transparent px-1.5 text-xs text-muted-foreground shadow-none hover:bg-accent hover:text-foreground">
                <span className="flex flex-1 text-left">{AUTO_LEVELS.find((l) => l.value === autoLevel)?.label ?? autoLevel}</span>
              </SelectTrigger>
              <SelectContent side="top">
                {AUTO_LEVELS.map((l) => (
                  <SelectItem key={l.value} value={l.value}>{l.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>

            <SettingsFlashIndicator className="ml-1" />
          </div>

          <div className="flex shrink-0 items-center gap-2">
            <TokenUsageIndicator />
            {isCancelling ? (
              <button
                type="button"
                onClick={onForceCancel}
                className="flex items-center gap-1.5 rounded-full bg-red-600 px-3 py-1.5 text-xs text-white transition-colors hover:bg-red-700"
                title="Force stop"
              >
                <Loader2 className="size-3 animate-spin" />
                <span>Stopping...</span>
              </button>
            ) : (isRunning || isCancelling) && !hasDraftToSend ? (
              <button
                type="button"
                onClick={onCancel}
                className="flex size-8 items-center justify-center rounded-full bg-foreground text-background transition-colors hover:bg-foreground/80"
              >
                <Square className="size-3.5" />
              </button>
            ) : (
              <button
                type="button"
                onClick={handleSubmit}
                disabled={!canSend}
                className="flex size-8 items-center justify-center rounded-full bg-foreground text-background transition-colors hover:bg-foreground/80 disabled:opacity-30 disabled:cursor-not-allowed"
              >
                <ArrowUp className="size-4" strokeWidth={2.5} />
              </button>
            )}
          </div>
        </div>
      </div>
    </footer>
  )
}

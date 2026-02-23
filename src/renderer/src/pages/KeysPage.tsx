import React, { useState, useRef } from 'react'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  useKeysQuery,
  useRefreshKeysMutation,
  useAddKeysMutation,
  useRemoveKeyMutation,
  useUpdateKeyNoteMutation,
} from '@/hooks/useKeys'

function maskKey(k: string): string {
  if (k.length <= 10) return k.slice(0, 3) + '***'
  return k.slice(0, 6) + '...' + k.slice(-4)
}

function formatNumber(n: number | null): string {
  if (n === null) return '?'
  if (Math.abs(n) >= 1e9) return (n / 1e9).toFixed(1).replace(/\.0$/, '') + 'B'
  if (Math.abs(n) >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M'
  if (Math.abs(n) >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'K'
  return Math.round(n).toString()
}

type SortField = 'percent' | 'quota' | 'expiry'
type SortDir = 'asc' | 'desc'

export function KeysPage() {
  const { data: keys = [], isLoading: loading } = useKeysQuery()
  const refreshMutation = useRefreshKeysMutation()
  const addMutation = useAddKeysMutation()
  const removeMutation = useRemoveKeyMutation()
  const updateNoteMutation = useUpdateKeyNoteMutation()

  const [addOpen, setAddOpen] = useState(false)
  const [newKeyText, setNewKeyText] = useState('')
  const [deleteIndex, setDeleteIndex] = useState<number | null>(null)
  const [editingNote, setEditingNote] = useState<number | null>(null)
  const [noteValue, setNoteValue] = useState('')
  const [sortField, setSortField] = useState<SortField | null>(null)
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const [copied, setCopied] = useState<number | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const noteInputRef = useRef<HTMLInputElement>(null)

  const refreshing = refreshMutation.isPending

  const handleRefresh = () => {
    refreshMutation.mutate()
  }

  const handleAdd = async () => {
    if (!newKeyText.trim()) return
    const lines = newKeyText
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean)
    await addMutation.mutateAsync(lines)
    setNewKeyText('')
    setAddOpen(false)
    refreshMutation.mutate()
  }

  const handleFileImport = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = (ev) => setNewKeyText(ev.target?.result as string)
    reader.readAsText(file)
    e.target.value = ''
  }

  const handleExport = () => {
    if (keys.length === 0) return
    const text = keys.map((k) => k.key).join('\n')
    const blob = new Blob([text], { type: 'text/plain' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 'droi-keys.txt'
    a.click()
    URL.revokeObjectURL(a.href)
  }

  const handleDelete = async () => {
    if (deleteIndex === null) return
    await removeMutation.mutateAsync(deleteIndex)
    setDeleteIndex(null)
  }

  const handleCopy = async (key: string, index: number) => {
    await navigator.clipboard.writeText(key)
    setCopied(index)
    setTimeout(() => setCopied(null), 1500)
  }

  const handleNoteEdit = (index: number, currentNote: string) => {
    setEditingNote(index)
    setNoteValue(currentNote)
    setTimeout(() => noteInputRef.current?.focus(), 50)
  }

  const handleNoteSave = async () => {
    if (editingNote === null) return
    await updateNoteMutation.mutateAsync({ index: editingNote, note: noteValue })
    setEditingNote(null)
  }

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      if (sortDir === 'desc') {
        setSortField(null)
        setSortDir('asc')
        return
      }
      setSortDir('desc')
    } else {
      setSortField(field)
      setSortDir('asc')
    }
  }

  const sorted = [...keys].sort((a, b) => {
    if (!sortField) return 0
    const dir = sortDir === 'asc' ? 1 : -1
    if (sortField === 'percent') {
      const pA = a.usage?.total ? (a.usage.used || 0) / a.usage.total : -1
      const pB = b.usage?.total ? (b.usage.used || 0) / b.usage.total : -1
      return (pA - pB) * dir
    }
    if (sortField === 'quota') {
      return ((a.usage?.used ?? -1) - (b.usage?.used ?? -1)) * dir
    }
    if (sortField === 'expiry') {
      return (a.usage?.expires || '').localeCompare(b.usage?.expires || '') * dir
    }
    return 0
  })

  const totalUsed = keys.reduce((s, k) => s + (k.usage?.used || 0), 0)
  const totalQuota = keys.reduce((s, k) => s + (k.usage?.total || 0), 0)
  const activeKey = keys.find((k) => k.isActive)

  if (loading) {
    return (
      <div className="flex flex-1 flex-col">
        <div className="mx-auto w-full max-w-4xl space-y-6 p-8">
          <div className="space-y-2">
            <Skeleton className="h-6 w-32" />
            <Skeleton className="h-4 w-64" />
          </div>
          <Skeleton className="h-px w-full" />
          <div className="flex gap-3">
            <Skeleton className="h-9 w-28 rounded" />
            <Skeleton className="h-9 w-24 rounded" />
            <Skeleton className="h-9 w-28 rounded" />
          </div>
          <div className="space-y-0 rounded border overflow-hidden">
            <Skeleton className="h-9 w-full" />
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-11 w-full rounded-none border-t" />
            ))}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-6 overflow-hidden p-8">
        <div>
          <h1 className="text-xl font-semibold">API Keys</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Manage multiple API keys with automatic rotation based on expiry date.
          </p>
        </div>

        <Separator />

        {/* Stats bar */}
        <div className="flex flex-wrap items-center gap-3">
          <div className="rounded border px-3 py-1.5 text-sm">
            <span className="text-xs text-muted-foreground uppercase tracking-wider mr-2">
              Active
            </span>
            <span className="font-mono font-bold">#{activeKey ? activeKey.index + 1 : '?'}</span>
          </div>
          <div className="rounded border px-3 py-1.5 text-sm">
            <span className="text-xs text-muted-foreground uppercase tracking-wider mr-2">
              Total
            </span>
            <span className="font-mono font-bold">{keys.length}</span>
          </div>
          <div className="rounded border px-3 py-1.5 text-sm">
            <span className="text-xs text-muted-foreground uppercase tracking-wider mr-2">
              Usage
            </span>
            <span className="font-mono font-bold">
              {formatNumber(totalUsed)}
              <span className="text-muted-foreground mx-0.5">/</span>
              <span className="text-muted-foreground">{formatNumber(totalQuota)}</span>
            </span>
          </div>

          <div className="ml-auto flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={handleRefresh} disabled={refreshing}>
              {refreshing ? 'Refreshing...' : 'Refresh'}
            </Button>
            <Button variant="outline" size="sm" onClick={handleExport} disabled={keys.length === 0}>
              Export
            </Button>
            <Button size="sm" onClick={() => setAddOpen(true)}>
              + Add Keys
            </Button>
          </div>
        </div>

        {/* Keys table */}
        {keys.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
            <p className="text-sm">No API keys configured</p>
            <p className="mt-1 text-xs">Click &quot;+ Add Keys&quot; to get started.</p>
          </div>
        ) : (
          <ScrollArea className="min-h-0 flex-1 rounded border">
            <table className="w-full min-w-0 text-sm">
              <thead>
                <tr className="border-b bg-muted/30">
                  <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground w-10">
                    #
                  </th>
                  <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
                    Key
                  </th>
                  <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
                    Note
                  </th>
                  <th
                    className="px-3 py-2 text-right text-xs font-medium text-muted-foreground cursor-pointer select-none hover:text-foreground"
                    onClick={() => handleSort('percent')}
                  >
                    % {sortField === 'percent' && (sortDir === 'asc' ? '↑' : '↓')}
                  </th>
                  <th
                    className="px-3 py-2 text-right text-xs font-medium text-muted-foreground cursor-pointer select-none hover:text-foreground"
                    onClick={() => handleSort('quota')}
                  >
                    Quota {sortField === 'quota' && (sortDir === 'asc' ? '↑' : '↓')}
                  </th>
                  <th
                    className="px-3 py-2 text-center text-xs font-medium text-muted-foreground cursor-pointer select-none hover:text-foreground"
                    onClick={() => handleSort('expiry')}
                  >
                    Expiry {sortField === 'expiry' && (sortDir === 'asc' ? '↑' : '↓')}
                  </th>
                  <th className="px-3 py-2 text-right text-xs font-medium text-muted-foreground w-24">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((entry) => {
                  const percent = entry.usage?.total
                    ? Math.round(((entry.usage.used || 0) / entry.usage.total) * 100)
                    : 0
                  const isHigh = percent >= 80 && percent < 100
                  const isExhausted = percent >= 100
                  const isInvalid = entry.usage?.error?.startsWith('http_')
                  const bgColor = isExhausted
                    ? 'rgba(239,68,68,0.08)'
                    : isHigh
                      ? 'rgba(245,158,11,0.08)'
                      : 'transparent'

                  return (
                    <tr
                      key={entry.index}
                      className="border-b last:border-0 transition-colors hover:bg-muted/20"
                      style={{
                        background: entry.usage?.total
                          ? `linear-gradient(to right, ${bgColor} ${Math.min(percent, 100)}%, transparent ${Math.min(percent, 100)}%)`
                          : undefined,
                      }}
                    >
                      <td className="px-3 py-2 font-mono text-muted-foreground">
                        <span className="flex items-center gap-1.5">
                          {entry.isActive && (
                            <span className="inline-block h-2 w-2 rounded-full bg-emerald-500" />
                          )}
                          {entry.index + 1}
                        </span>
                      </td>
                      <td className="px-3 py-2">
                        <button
                          className="font-mono text-sm hover:underline cursor-pointer"
                          onClick={() => handleCopy(entry.key, entry.index)}
                          title="Click to copy"
                        >
                          {maskKey(entry.key)}
                          {copied === entry.index && (
                            <span className="ml-2 text-xs text-emerald-500">copied</span>
                          )}
                        </button>
                      </td>
                      <td className="px-3 py-2">
                        {editingNote === entry.index ? (
                          <Input
                            ref={noteInputRef}
                            value={noteValue}
                            onChange={(e) => setNoteValue(e.target.value)}
                            onBlur={handleNoteSave}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') handleNoteSave()
                              if (e.key === 'Escape') setEditingNote(null)
                            }}
                            className="h-7 text-xs"
                            placeholder="Note..."
                          />
                        ) : (
                          <button
                            className="text-xs text-muted-foreground hover:text-foreground cursor-pointer"
                            onClick={() => handleNoteEdit(entry.index, entry.note)}
                          >
                            {entry.note || '-'}
                          </button>
                        )}
                      </td>
                      <td className="px-3 py-2 text-right font-mono text-muted-foreground">
                        {entry.usage?.total ? `${percent}%` : '-'}
                      </td>
                      <td className="px-3 py-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                        {entry.usage?.total ? (
                          <>
                            {formatNumber(entry.usage.used || 0)}
                            <span className="mx-0.5 text-muted-foreground/50">/</span>
                            {formatNumber(entry.usage.total)}
                          </>
                        ) : (
                          '-'
                        )}
                      </td>
                      <td className="px-3 py-2 text-center">
                        {isInvalid ? (
                          <Badge variant="destructive" className="text-xs">
                            INVALID
                          </Badge>
                        ) : (
                          <span className="text-sm text-muted-foreground">
                            {entry.usage?.expires || '-'}
                          </span>
                        )}
                      </td>
                      <td className="px-3 py-2 text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 text-xs text-muted-foreground hover:text-destructive"
                          onClick={() => setDeleteIndex(entry.index)}
                        >
                          Delete
                        </Button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </ScrollArea>
        )}

        {/* Add keys dialog */}
        <AlertDialog open={addOpen} onOpenChange={setAddOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Add API Keys</AlertDialogTitle>
              <AlertDialogDescription>
                Enter your Factory API keys below (one per line).
              </AlertDialogDescription>
            </AlertDialogHeader>

            <div className="space-y-3">
              <Textarea
                placeholder={'fk-xxx\nfk-yyy\nfk-zzz'}
                value={newKeyText}
                onChange={(e) => setNewKeyText(e.target.value)}
                rows={6}
                className="font-mono text-sm"
              />

              <input
                type="file"
                accept=".txt"
                ref={fileInputRef}
                onChange={handleFileImport}
                className="hidden"
              />

              <div>
                <Button variant="outline" size="sm" onClick={() => fileInputRef.current?.click()}>
                  Import from File
                </Button>
              </div>
            </div>

            <AlertDialogFooter>
              <AlertDialogCancel disabled={addMutation.isPending}>Cancel</AlertDialogCancel>
              <AlertDialogAction
                onClick={handleAdd}
                disabled={addMutation.isPending || !newKeyText.trim()}
              >
                {addMutation.isPending ? 'Adding...' : 'Add Keys'}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        {/* Delete confirmation */}
        <AlertDialog
          open={deleteIndex !== null}
          onOpenChange={(open) => !open && setDeleteIndex(null)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete this key?</AlertDialogTitle>
              <AlertDialogDescription>
                This will permanently remove key #{deleteIndex !== null ? deleteIndex + 1 : ''} from
                your configuration.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction onClick={handleDelete}>Delete</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </div>
  )
}

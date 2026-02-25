import { useState, useCallback } from 'react'
import { ChevronDown, FolderOpen } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuGroup,
  DropdownMenuLabel,
} from '@/components/ui/dropdown-menu'
import { getDroidClient } from '@/droidClient'
import type { EditorInfo } from '@/types'
import { useEditorsQuery } from '@/hooks/useEditors'
import { EDITOR_ICON_MAP } from '@/components/EditorIcons'

interface OpenInEditorButtonProps {
  dir: string
}

export function OpenInEditorButton({ dir }: OpenInEditorButtonProps) {
  const { data: editors = [] } = useEditorsQuery()
  const [defaultEditor, setDefaultEditor] = useState<EditorInfo | null>(null)
  const [open, setOpen] = useState(false)

  const saved = typeof window !== 'undefined' ? localStorage.getItem('droi:defaultEditor') : null
  const resolvedDefault = saved ? editors.find((e) => e.id === saved) : null
  const effectiveDefault = defaultEditor || resolvedDefault || editors[0] || null

  const handleOpen = useCallback(
    (editor: EditorInfo) => {
      localStorage.setItem('droi:defaultEditor', editor.id)
      setDefaultEditor(editor)
      getDroidClient()
        .openWithEditor({ dir, editorId: editor.id })
        .catch(() => {
          getDroidClient()
            .openInEditor({ dir })
            .catch(() => {})
        })
      setOpen(false)
    },
    [dir],
  )

  const handleDefaultClick = useCallback(() => {
    if (effectiveDefault) {
      getDroidClient()
        .openWithEditor({ dir, editorId: effectiveDefault.id })
        .catch(() => {
          getDroidClient()
            .openInEditor({ dir })
            .catch(() => {})
        })
    } else {
      getDroidClient()
        .openInEditor({ dir })
        .catch(() => {})
    }
  }, [dir, effectiveDefault])

  if (editors.length === 0) {
    return (
      <button
        type="button"
        className="flex size-7 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        title="Open in Finder"
        onClick={() => getDroidClient().openInEditor({ dir })}
      >
        <FolderOpen className="size-4" />
      </button>
    )
  }

  return (
    <div className="flex items-center rounded-lg border border-border overflow-hidden">
      <button
        type="button"
        className="flex items-center gap-1 px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        title={`Open in ${effectiveDefault?.name || 'editor'}`}
        onClick={handleDefaultClick}
      >
        {effectiveDefault && (() => {
          const Icon = EDITOR_ICON_MAP[effectiveDefault.id]
          return Icon ? <Icon className="size-3.5" /> : <FolderOpen className="size-3.5" />
        })()}
        <span className="text-xs font-medium">Open</span>
      </button>
      <DropdownMenu open={open} onOpenChange={setOpen}>
        <DropdownMenuTrigger
          render={<button type="button" />}
          className="flex items-center px-1 py-0.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground border-l border-border"
        >
          <ChevronDown className="size-3" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" sideOffset={6}>
          <DropdownMenuGroup>
            <DropdownMenuLabel>Open in</DropdownMenuLabel>
            {editors.map((editor) => {
              const Icon = EDITOR_ICON_MAP[editor.id]
              return (
                <DropdownMenuItem
                  key={editor.id}
                  className="gap-2"
                  onClick={() => handleOpen(editor)}
                >
                  {Icon ? <Icon className="size-4" /> : <FolderOpen className="size-4" />}
                  <span>{editor.name}</span>
                </DropdownMenuItem>
              )
            })}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

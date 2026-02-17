import { Badge } from './ui/badge'
import { Bot } from 'lucide-react'
import { getModelLabel } from '@/types'
import { useModel } from '@/store'

interface StatusBarProps {
  version: string
}

export function StatusBar({ version }: StatusBarProps) {
  const model = useModel()
  const modelLabel = getModelLabel(model)

  return (
    <div className="flex flex-1 items-center justify-between gap-4">
      <div className="flex items-center gap-2">
        <Bot className="size-4 text-primary" />
        <span className="text-sm font-semibold text-foreground">Droi</span>
        <Badge variant="outline" className="px-1.5 py-0 font-mono text-[10px]">
          v{version}
        </Badge>
      </div>
      <div className="flex items-center gap-2">
        <Badge variant="secondary" className="px-1.5 py-0 text-[10px]">
          {modelLabel}
        </Badge>
      </div>
    </div>
  )
}

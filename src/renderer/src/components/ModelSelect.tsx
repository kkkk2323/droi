import React from 'react'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectGroup,
  SelectLabel,
  SelectSeparator,
  SelectValue,
} from './ui/select'
import { Sparkles } from 'lucide-react'
import { Kimi, Zhipu, Claude, OpenAI, Gemini } from '@lobehub/icons'
import { MODEL_GROUPS, type ModelProvider, type CustomModelDef } from '@/types'

export type ModelSelectVariant = 'compact' | 'default'

export interface ModelSelectProps {
  value: string
  onChange: (value: string) => void
  customModels?: CustomModelDef[]
  variant?: ModelSelectVariant
  className?: string
  placeholder?: string
}

const providerIcon: Record<ModelProvider, React.FC<{ size?: number | string }>> = {
  kimi: (props) => <Kimi {...props} />,
  zhipu: (props) => <Zhipu.Color {...props} />,
  claude: (props) => <Claude.Color {...props} />,
  openai: (props) => <OpenAI {...props} />,
  gemini: (props) => <Gemini.Color {...props} />,
  minimax: (props) => <Sparkles {...props} />,
}

function ModelIcon({ provider, size = 14 }: { provider: ModelProvider; size?: number }) {
  const Icon = providerIcon[provider]
  return <Icon size={size} />
}

function getCurrentModelInfo(value: string, customModels?: CustomModelDef[]) {
  // Find in built-in models
  for (const group of MODEL_GROUPS) {
    const found = group.options.find((m) => m.value === value)
    if (found) {
      return { label: found.label, provider: found.provider, multiplier: found.multiplier }
    }
  }
  // Find in custom models
  const custom = customModels?.find((m) => m.id === value)
  if (custom) {
    return { label: custom.displayName, provider: null, multiplier: null }
  }
  return { label: value, provider: null, multiplier: null }
}

export function ModelSelect({
  value,
  onChange,
  customModels,
  variant = 'default',
  className,
  placeholder,
}: ModelSelectProps) {
  const { label, provider, multiplier } = getCurrentModelInfo(value, customModels)

  if (variant === 'compact') {
    return (
      <Select value={value} onValueChange={(v) => v && onChange(v)}>
        <SelectTrigger
          size="sm"
          className={`h-7 w-auto shrink-0 gap-1.5 rounded-lg border-none bg-transparent px-2 text-xs text-muted-foreground shadow-none hover:bg-accent hover:text-foreground ${className || ''}`}
        >
          {provider && <ModelIcon provider={provider} size={14} />}
          <span className="hidden md:flex flex-1 text-left">{label}</span>
        </SelectTrigger>
        <SelectContent className="min-w-[260px]" side="top">
          {customModels && customModels.length > 0 && (
            <>
              <SelectGroup>
                <SelectLabel className="flex items-center gap-1.5 px-2">Custom</SelectLabel>
                {customModels.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    <span className="flex w-full items-center gap-2">
                      <span className="flex-1">{m.displayName}</span>
                    </span>
                  </SelectItem>
                ))}
              </SelectGroup>
              <SelectSeparator />
            </>
          )}
          {MODEL_GROUPS.map((group) => (
            <SelectGroup key={group.label}>
              <SelectLabel className="flex items-center gap-1.5 px-2">
                <ModelIcon provider={group.options[0]?.provider} size={12} />
                {group.label}
              </SelectLabel>
              {group.options.map((m) => (
                <SelectItem key={m.value} value={m.value}>
                  <span className="flex w-full items-center gap-2">
                    <span className="flex-1">{m.label}</span>
                    <span className="ml-2 text-[10px] text-muted-foreground">{m.multiplier}</span>
                  </span>
                </SelectItem>
              ))}
            </SelectGroup>
          ))}
        </SelectContent>
      </Select>
    )
  }

  // Default variant - for Settings page
  return (
    <Select value={value} onValueChange={(v) => v && onChange(v)}>
      <SelectTrigger className={`w-full justify-between ${className || ''}`}>
        <SelectValue placeholder={placeholder}>
          <span className="flex items-center gap-2">
            {provider && <ModelIcon provider={provider} size={14} />}
            <span>{label}</span>
            {multiplier && (
              <span className="ml-2 text-[10px] text-muted-foreground">{multiplier}</span>
            )}
          </span>
        </SelectValue>
      </SelectTrigger>
      <SelectContent className="min-w-[260px]">
        {customModels && customModels.length > 0 && (
          <>
            <SelectGroup>
              <SelectLabel className="px-2">Custom</SelectLabel>
              {customModels.map((m) => (
                <SelectItem key={m.id} value={m.id}>
                  {m.displayName}
                </SelectItem>
              ))}
            </SelectGroup>
            <SelectSeparator />
          </>
        )}
        {MODEL_GROUPS.map((group) => (
          <SelectGroup key={group.label}>
            <SelectLabel className="flex items-center gap-1.5 px-2">
              <ModelIcon provider={group.options[0]?.provider} size={12} />
              {group.label}
            </SelectLabel>
            {group.options.map((m) => (
              <SelectItem key={m.value} value={m.value}>
                <span className="flex w-full items-center gap-2">
                  <span className="flex-1">{m.label}</span>
                  <span className="ml-2 text-[10px] text-muted-foreground">{m.multiplier}</span>
                </span>
              </SelectItem>
            ))}
          </SelectGroup>
        ))}
      </SelectContent>
    </Select>
  )
}

export { ModelIcon }

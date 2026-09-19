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
import { Claude, OpenAI, Gemini } from '@lobehub/icons'
import type { AvailableModelConfig, CustomModelDef } from '@/types'
import {
  buildRuntimeModelCatalog,
  findRuntimeModelOption,
  type ModelProviderIcon,
} from '@/lib/modelCatalog'

export type ModelSelectVariant = 'compact' | 'default'

export interface ModelSelectProps {
  value: string
  onChange: (value: string) => void
  availableModels?: AvailableModelConfig[]
  customModels?: CustomModelDef[]
  variant?: ModelSelectVariant
  className?: string
  placeholder?: string
  disabled?: boolean
  loading?: boolean
}

const providerIcon: Record<ModelProviderIcon, React.FC<{ size?: number | string }>> = {
  claude: (props) => <Claude.Color {...props} />,
  openai: (props) => <OpenAI {...props} />,
  gemini: (props) => <Gemini.Color {...props} />,
  factory: (props) => <Sparkles {...props} />,
  xai: (props) => <Sparkles {...props} />,
  custom: (props) => <Sparkles {...props} />,
  unknown: (props) => <Sparkles {...props} />,
}

function ModelIcon({ provider, size = 14 }: { provider: ModelProviderIcon; size?: number }) {
  const Icon = providerIcon[provider]
  return <Icon size={size} />
}

export function ModelSelect({
  value,
  onChange,
  availableModels,
  customModels,
  variant = 'default',
  className,
  placeholder,
  disabled = false,
  loading = false,
}: ModelSelectProps) {
  const groups = buildRuntimeModelCatalog({ availableModels, customModels })
  const current = findRuntimeModelOption(value, { availableModels, customModels })
  const effectiveDisabled = disabled || loading || groups.length === 0
  const label = loading ? 'Loading models...' : current?.label || value
  const provider = current?.providerIcon ?? null
  const multiplier = current?.multiplier ?? null
  const effectivePlaceholder = loading ? 'Loading models...' : placeholder

  if (variant === 'compact') {
    return (
      <Select value={value} onValueChange={(v) => v && onChange(v)} disabled={effectiveDisabled}>
        <SelectTrigger
          data-testid="model-select-trigger"
          size="sm"
          disabled={effectiveDisabled}
          className={`h-7 w-auto shrink-0 gap-1.5 rounded-lg border-none bg-transparent px-2 text-xs text-muted-foreground shadow-none hover:bg-accent hover:text-foreground ${className || ''}`}
        >
          {provider && <ModelIcon provider={provider} size={14} />}
          <span className="hidden md:flex flex-1 text-left">{label}</span>
        </SelectTrigger>
        <SelectContent className="min-w-[260px]" side="top">
          {groups.map((group, index) => (
            <React.Fragment key={group.label}>
              <SelectGroup>
                <SelectLabel className="flex items-center gap-1.5 px-2">
                  <ModelIcon provider={group.providerIcon} size={12} />
                  {group.label}
                </SelectLabel>
                {group.options.map((m) => (
                  <SelectItem key={m.value} value={m.value}>
                    <span className="flex w-full items-center gap-2">
                      <span className="flex-1">{m.label}</span>
                      {m.multiplier && (
                        <span className="ml-2 text-[10px] text-muted-foreground">
                          {m.multiplier}
                        </span>
                      )}
                    </span>
                  </SelectItem>
                ))}
              </SelectGroup>
              {index < groups.length - 1 && <SelectSeparator />}
            </React.Fragment>
          ))}
        </SelectContent>
      </Select>
    )
  }

  // Default variant - for Settings page
  return (
    <Select value={value} onValueChange={(v) => v && onChange(v)} disabled={effectiveDisabled}>
      <SelectTrigger
        disabled={effectiveDisabled}
        className={`w-full justify-between ${className || ''}`}
      >
        <SelectValue placeholder={effectivePlaceholder}>
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
        {groups.map((group, index) => (
          <React.Fragment key={group.label}>
            <SelectGroup>
              <SelectLabel className="flex items-center gap-1.5 px-2">
                <ModelIcon provider={group.providerIcon} size={12} />
                {group.label}
              </SelectLabel>
              {group.options.map((m) => (
                <SelectItem key={m.value} value={m.value}>
                  <span className="flex w-full items-center gap-2">
                    <span className="flex-1">{m.label}</span>
                    {m.multiplier && (
                      <span className="ml-2 text-[10px] text-muted-foreground">{m.multiplier}</span>
                    )}
                  </span>
                </SelectItem>
              ))}
            </SelectGroup>
            {index < groups.length - 1 && <SelectSeparator />}
          </React.Fragment>
        ))}
      </SelectContent>
    </Select>
  )
}

export { ModelIcon }

import { useState, type ReactNode } from 'react'
import { X } from 'lucide-react'
import { AUTONOMY_LABELS, EFFORT_LABELS } from '@droi/daemon-layer/model-choices'
import {
  AUTONOMY_DESCRIPTIONS,
  COMPACTION_LIMITS,
  CURRENT_MODEL,
  DEFAULT_COMPACTION_LIMIT,
  INTERACTION_MODES,
  SUBAGENT_TIERS,
  pickableModels,
  reasoningChoices,
  specSaveChoice,
  specSavePaths,
  tokenLimitLabel,
  withSubagentTier,
  type SessionDefaultsPatch,
  type SessionDefaultsView,
  type SubagentTier,
} from '@droi/daemon-layer/session-defaults'
import { useSessionDefaultsEditor } from '@droi/daemon-layer/use-session-defaults'
import { ModelPicker } from '@/components/chat/model-picker'
import { Button } from '@/components/ui/button'
import { Select, type SelectOption } from '@/components/ui/select'
import { SettingRow, Switch, settingInputClass } from '@/components/ui/setting-row'

const SAME_AS_MAIN = '__same_as_main__'
const MODEL_DEFAULT = '__model_default__'
const INHERIT = 'inherit'
const TIER_LABELS: Record<SubagentTier, string> = {
  light: 'Light task',
  medium: 'Medium task',
  heavy: 'Heavy task',
}
const ORG_MANAGED = 'Set by your organization.'

type Update = (patch: SessionDefaultsPatch) => void

/**
 * What every new Session starts with, after the Factory App's Session Defaults
 * page. The Daemon keeps them in ~/.factory/settings.json, which the droid CLI
 * and the Factory App read too; it works from any Client. `systemPrompt`,
 * `scratchFolder` and `notice` are the Desktop Shell's own rows, present in
 * the Local Client only.
 */
export function SessionDefaultsTab({
  systemPrompt,
  scratchFolder,
  notice,
}: {
  systemPrompt: ReactNode
  scratchFolder?: ReactNode
  notice?: ReactNode
}) {
  const { defaults, update, error } = useSessionDefaultsEditor()
  const save: Update = (patch) => void update(patch)
  return (
    <>
      <p className="-mt-2 mb-1 text-[13px] text-muted-foreground">
        Shared with the droid CLI and the Factory App on this computer. Existing Sessions keep their
        settings.
      </p>
      {notice}
      {error ? (
        <p role="alert" className="text-sm text-destructive-foreground">
          Session defaults did not load or save: {error}
        </p>
      ) : null}
      {!defaults ? (
        <p className="text-sm text-muted-foreground">Loading session defaults…</p>
      ) : (
        <>
          <General defaults={defaults} save={save} />
          <SpecMode defaults={defaults} save={save} />
          <Compaction defaults={defaults} save={save} />
          <Subagents defaults={defaults} save={save} />
        </>
      )}
      {/* The Shell's own setting; it does not wait for the Daemon. */}
      <Section title="System prompt">{systemPrompt}</Section>
      {scratchFolder ? <Section title="Without a workspace">{scratchFolder}</Section> : null}
    </>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  if (!children) return null
  return (
    <section aria-label={title} className="flex flex-col gap-3">
      <h3 className="mt-3 px-1 text-xs font-medium text-muted-foreground">{title}</h3>
      {children}
    </section>
  )
}

type Props = { defaults: SessionDefaultsView; save: Update }

function modelChoices(defaults: SessionDefaultsView, routers = true) {
  return pickableModels(defaults.models, { routers })
}

function effortOptions(efforts: string[]): SelectOption[] {
  return efforts.map((value) => ({ value, label: EFFORT_LABELS[value] ?? value }))
}

function General({ defaults, save }: Props) {
  const { locked } = defaults
  const efforts = reasoningChoices(defaults.models, defaults.modelId, defaults.reasoningEffort)
  return (
    <Section title="General">
      <SettingRow
        title="Default model"
        description={locked.has('modelId') ? ORG_MANAGED : undefined}
        control={
          <ModelPicker
            field
            label="Default model"
            value={defaults.modelId}
            disabled={locked.has('modelId')}
            onChange={(modelId) => {
              // A level the new model lacks would be refused; start from its first.
              const supported = reasoningChoices(defaults.models, modelId, null)
              const effort = defaults.reasoningEffort
              save({
                modelId,
                ...(effort && supported.includes(effort) ? {} : { reasoningEffort: supported[0] }),
              })
            }}
            models={modelChoices(defaults)}
          />
        }
      />
      <SettingRow
        title="Default reasoning level"
        description={locked.has('reasoningEffort') ? ORG_MANAGED : undefined}
        control={
          <Select
            label="Default reasoning level"
            value={defaults.reasoningEffort ?? ''}
            disabled={locked.has('reasoningEffort')}
            onChange={(reasoningEffort) => save({ reasoningEffort })}
            options={effortOptions(efforts)}
          />
        }
      />
      <SettingRow
        title="Default interaction mode"
        description="Spec plans with you before it changes anything."
        control={
          <Select
            label="Default interaction mode"
            value={defaults.interactionMode}
            disabled={locked.has('interactionMode')}
            onChange={(interactionMode) => save({ interactionMode })}
            options={[...INTERACTION_MODES]}
          />
        }
      />
      <SettingRow
        title="Default autonomy level"
        description={
          AUTONOMY_DESCRIPTIONS[defaults.autonomyLevel ?? 'off'] ??
          'How much Droid may do without asking for approval.'
        }
        control={
          <Select
            label="Default autonomy level"
            value={defaults.autonomyLevel ?? 'off'}
            disabled={locked.has('autonomyLevel')}
            onChange={(autonomyLevel) => save({ autonomyLevel })}
            options={defaults.availableAutonomyLevels.map((level) => ({
              value: level,
              label: AUTONOMY_LABELS[level] ?? level,
            }))}
          />
        }
      />
    </Section>
  )
}

function SpecMode({ defaults, save }: Props) {
  const specModel = defaults.specModeModelId ?? defaults.modelId
  const efforts = reasoningChoices(
    defaults.models,
    specModel,
    defaults.specModeReasoningEffort ?? defaults.reasoningEffort,
  )
  const paths = specSavePaths(defaults.userFactoryDir)
  const choice = specSaveChoice(defaults.specSaveDir, paths)
  const [custom, setCustom] = useState(choice === 'custom')
  const shown = custom ? 'custom' : choice
  return (
    <Section title="Spec mode">
      <SettingRow
        title="Spec mode model"
        control={
          <ModelPicker
            field
            label="Spec mode model"
            value={defaults.specModeModelId ?? SAME_AS_MAIN}
            disabled={defaults.locked.has('specModeModelId')}
            onChange={(value) =>
              save(
                value === SAME_AS_MAIN
                  ? { specModeModelId: null, specModeReasoningEffort: null }
                  : { specModeModelId: value, specModeReasoningEffort: null },
              )
            }
            extras={[{ value: SAME_AS_MAIN, label: 'Same as main' }]}
            models={modelChoices(defaults)}
          />
        }
      />
      <SettingRow
        title="Spec mode reasoning level"
        control={
          <Select
            label="Spec mode reasoning level"
            value={defaults.specModeReasoningEffort ?? SAME_AS_MAIN}
            disabled={defaults.locked.has('specModeReasoningEffort')}
            onChange={(value) =>
              save({ specModeReasoningEffort: value === SAME_AS_MAIN ? null : value })
            }
            options={[{ value: SAME_AS_MAIN, label: 'Same as main' }, ...effortOptions(efforts)]}
          />
        }
      />
      <SettingRow
        title="Spec save folder"
        description={
          shown === 'user'
            ? `Specs go to ${paths.user}.`
            : shown === 'project'
              ? `Specs go to the project's ${paths.project}, or ${paths.user} outside a project.`
              : 'Specs from every Session go to this folder.'
        }
        control={
          <Select
            label="Spec save folder"
            value={shown}
            disabled={defaults.locked.has('specSaveDir')}
            onChange={(value) => {
              setCustom(value === 'custom')
              if (value === 'user') save({ specSaveDir: null })
              if (value === 'project') save({ specSaveDir: paths.project })
            }}
            options={[
              { value: 'user', label: 'User home' },
              { value: 'project', label: 'Project' },
              { value: 'custom', label: 'Custom folder' },
            ]}
          />
        }
      >
        {shown === 'custom' ? (
          <CustomFolder
            key={defaults.specSaveDir ?? ''}
            stored={choice === 'custom' ? (defaults.specSaveDir ?? '') : ''}
            onSave={(specSaveDir) => save({ specSaveDir })}
          />
        ) : null}
      </SettingRow>
    </Section>
  )
}

function CustomFolder({ stored, onSave }: { stored: string; onSave: (path: string) => void }) {
  const [value, setValue] = useState(stored)
  return (
    <form
      className="flex gap-2"
      onSubmit={(event) => {
        event.preventDefault()
        if (value.trim()) onSave(value.trim())
      }}
    >
      <input
        aria-label="Spec folder path"
        placeholder="/Users/you/specs"
        value={value}
        spellCheck={false}
        autoCapitalize="off"
        onChange={(event) => setValue(event.target.value)}
        className={`${settingInputClass} font-mono`}
      />
      <Button type="submit" variant="outline" disabled={!value.trim() || value.trim() === stored}>
        Save folder
      </Button>
    </form>
  )
}

const limitOptions = (current: number | null): SelectOption[] =>
  [...new Set([...(current ? [current] : []), ...COMPACTION_LIMITS])]
    .sort((a, b) => a - b)
    .map((tokens) => ({ value: String(tokens), label: tokenLimitLabel(tokens) }))

function Compaction({ defaults, save }: Props) {
  const overrides = defaults.compactionTokenLimitPerModel
  const label = (id: string) => defaults.models.find((m) => m.id === id)?.label ?? id
  const addable = modelChoices(defaults).filter((m) => !(m.id in overrides))
  return (
    <Section title="Compaction">
      <SettingRow
        title="Compact automatically"
        description="New Sessions compact their history once it passes the token limit."
        control={
          <Switch
            aria-label="Compact automatically"
            checked={defaults.compactionThresholdCheckEnabled}
            disabled={defaults.locked.has('compactionThresholdCheckEnabled')}
            onCheckedChange={(compactionThresholdCheckEnabled) =>
              save({ compactionThresholdCheckEnabled })
            }
          />
        }
      />
      <SettingRow
        title="Compaction token limit"
        control={
          <Select
            label="Compaction token limit"
            value={String(defaults.compactionTokenLimit ?? DEFAULT_COMPACTION_LIMIT)}
            disabled={defaults.locked.has('compactionTokenLimit')}
            onChange={(value) => save({ compactionTokenLimit: Number(value) })}
            options={limitOptions(defaults.compactionTokenLimit)}
          />
        }
      />
      <SettingRow
        title="Limits for specific models"
        description="Override the token limit for a model."
      >
        <ul aria-label="Model compaction limits" className="flex flex-col gap-2">
          {Object.entries(overrides).map(([modelId, tokens]) => (
            <li key={modelId} className="flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate text-sm">{label(modelId)}</span>
              <Select
                label={`${label(modelId)} compaction limit`}
                value={String(tokens)}
                disabled={defaults.locked.has('compactionTokenLimitPerModel')}
                onChange={(value) =>
                  save({
                    compactionTokenLimitPerModel: { ...overrides, [modelId]: Number(value) },
                  })
                }
                options={limitOptions(tokens)}
              />
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                aria-label={`Remove the ${label(modelId)} limit`}
                disabled={defaults.locked.has('compactionTokenLimitPerModel')}
                onClick={() => {
                  const next = { ...overrides }
                  delete next[modelId]
                  save({ compactionTokenLimitPerModel: next })
                }}
              >
                <X aria-hidden />
              </Button>
            </li>
          ))}
          <li>
            <ModelPicker
              field
              label="Add a model limit"
              value={null}
              disabled={addable.length === 0 || defaults.locked.has('compactionTokenLimitPerModel')}
              onChange={(modelId) =>
                save({
                  compactionTokenLimitPerModel: {
                    ...overrides,
                    [modelId]: defaults.compactionTokenLimit ?? DEFAULT_COMPACTION_LIMIT,
                  },
                })
              }
              models={addable}
            />
          </li>
        </ul>
      </SettingRow>
      <SettingRow
        title="Compaction model"
        description="The model that writes the summary."
        control={
          <ModelPicker
            field
            label="Compaction model"
            value={defaults.compactionModel}
            disabled={defaults.locked.has('compactionModel')}
            onChange={(compactionModel) => save({ compactionModel })}
            extras={[{ value: CURRENT_MODEL, label: 'Current model' }]}
            models={modelChoices(defaults, false)}
          />
        }
      />
    </Section>
  )
}

function Subagents({ defaults, save }: Props) {
  const settings = defaults.subagentModelSettings
  return (
    <Section title="Subagents">
      <SettingRow
        title="Subagent autonomy level"
        control={
          <Select
            label="Subagent autonomy level"
            value={defaults.subagentAutonomyLevel}
            disabled={defaults.locked.has('subagentAutonomyLevel')}
            onChange={(subagentAutonomyLevel) => save({ subagentAutonomyLevel })}
            options={[
              { value: INHERIT, label: 'Inherit (calling session)' },
              ...defaults.availableAutonomyLevels.map((level) => ({
                value: level,
                label: AUTONOMY_LABELS[level] ?? level,
              })),
            ]}
          />
        }
      />
      <SettingRow
        title="Task models"
        description="The model and reasoning level for subagents started with light, medium or heavy complexity."
      >
        <div className="flex flex-col gap-2">
          {SUBAGENT_TIERS.map((tier) => {
            const model = settings[`${tier}Model`] ?? null
            const effort = settings[`${tier}ReasoningEffort`] ?? null
            return (
              <div key={tier} className="flex flex-wrap items-center gap-2">
                <span className="w-28 shrink-0 text-sm">{TIER_LABELS[tier]}</span>
                <ModelPicker
                  field
                  label={`${TIER_LABELS[tier]} model`}
                  value={model ?? INHERIT}
                  disabled={defaults.locked.has(`subagent.${tier}Model`)}
                  className="w-full min-w-40 flex-1"
                  onChange={(value) =>
                    save({
                      subagentModelSettings: withSubagentTier(settings, tier, {
                        model: value === INHERIT ? null : value,
                      }),
                    })
                  }
                  extras={[{ value: INHERIT, label: 'Inherit (calling session)' }]}
                  models={modelChoices(defaults)}
                />
                {model !== null ? (
                  <Select
                    label={`${TIER_LABELS[tier]} reasoning level`}
                    value={effort ?? MODEL_DEFAULT}
                    disabled={defaults.locked.has(`subagent.${tier}ReasoningEffort`)}
                    onChange={(value) =>
                      save({
                        subagentModelSettings: withSubagentTier(settings, tier, {
                          reasoningEffort: value === MODEL_DEFAULT ? null : value,
                        }),
                      })
                    }
                    options={[
                      { value: MODEL_DEFAULT, label: 'Model default' },
                      ...effortOptions(reasoningChoices(defaults.models, model, effort)),
                    ]}
                  />
                ) : null}
              </div>
            )
          })}
        </div>
      </SettingRow>
    </Section>
  )
}

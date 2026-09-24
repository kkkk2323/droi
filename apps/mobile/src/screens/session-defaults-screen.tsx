// What every new Session on the selected computer starts with, as on the web
// Client's Settings page. The Daemon keeps them in that computer's
// ~/.factory/settings.json, shared with the droid CLI and the Factory App.
import { useConnectionState } from '@droi/daemon-layer/connection-context'
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
import { useState } from 'react'
import { ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { ListPicker, ListSection, ListSwitch } from '../ui/list'
import { Button, Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

const SAME_AS_MAIN = '__same_as_main__'
const MODEL_DEFAULT = '__model_default__'
const INHERIT = 'inherit'
const TIER_LABELS: Record<SubagentTier, string> = {
  light: 'Light task',
  medium: 'Medium task',
  heavy: 'Heavy task',
}

type Option = { value: string; label: string }
type Save = (patch: SessionDefaultsPatch) => void

export function SessionDefaultsScreen({ computerName }: { computerName: string }) {
  const colors = useColors()
  const connection = useConnectionState()
  const { defaults, update, error } = useSessionDefaultsEditor()
  const save: Save = (patch) => void update(patch)

  return (
    <ScrollView
      style={{ backgroundColor: colors.background }}
      contentContainerStyle={styles.content}
      keyboardShouldPersistTaps="handled"
    >
      <Text tone="muted" size="sm" style={styles.intro}>
        For new Sessions on {computerName}. Shared with the droid CLI and the Factory App there;
        existing Sessions keep their settings.
      </Text>
      {error ? (
        <Text role="alert" size="sm" style={{ color: colors.destructiveForeground }}>
          Session defaults did not load or save: {error}
        </Text>
      ) : null}
      {!defaults ? (
        <Text tone="muted" size="sm">
          {connection.status === 'connected'
            ? 'Loading session defaults…'
            : `Connecting to ${computerName}…`}
        </Text>
      ) : (
        <Sections defaults={defaults} save={save} />
      )}
    </ScrollView>
  )
}

function models(defaults: SessionDefaultsView, routers = true): Option[] {
  return pickableModels(defaults.models, { routers }).map((m) => ({ value: m.id, label: m.label }))
}

function efforts(values: string[]): Option[] {
  return values.map((value) => ({ value, label: EFFORT_LABELS[value] ?? value }))
}

function limits(current: number | null): Option[] {
  return [...new Set([...(current ? [current] : []), ...COMPACTION_LIMITS])]
    .sort((a, b) => a - b)
    .map((tokens) => ({ value: String(tokens), label: tokenLimitLabel(tokens) }))
}

function Sections({ defaults, save }: { defaults: SessionDefaultsView; save: Save }) {
  const { locked } = defaults
  const editable = (key: string, onChange: (value: string) => void) =>
    locked.has(key) ? undefined : onChange
  const specModel = defaults.specModeModelId ?? defaults.modelId
  const paths = specSavePaths(defaults.userFactoryDir)
  const specChoice = specSaveChoice(defaults.specSaveDir, paths)
  const [customFolder, setCustomFolder] = useState(specChoice === 'custom')
  const shownFolder = customFolder ? 'custom' : specChoice
  const overrides = defaults.compactionTokenLimitPerModel
  const label = (id: string) => defaults.models.find((m) => m.id === id)?.label ?? id
  const subagents = defaults.subagentModelSettings

  return (
    <>
      <ListSection
        title="General"
        footer={
          locked.size > 0
            ? 'Settings your organization manages cannot be changed here.'
            : `${AUTONOMY_LABELS[defaults.autonomyLevel ?? 'off']}: ${AUTONOMY_DESCRIPTIONS[defaults.autonomyLevel ?? 'off']?.toLowerCase()}.`
        }
      >
        <ListPicker
          label="Default model"
          value={defaults.modelId ?? ''}
          options={models(defaults)}
          onChange={editable('modelId', (modelId) => {
            const supported = reasoningChoices(defaults.models, modelId, null)
            const effort = defaults.reasoningEffort
            save({
              modelId,
              ...(effort && supported.includes(effort) ? {} : { reasoningEffort: supported[0] }),
            })
          })}
        />
        <ListPicker
          label="Reasoning level"
          value={defaults.reasoningEffort ?? ''}
          options={efforts(
            reasoningChoices(defaults.models, defaults.modelId, defaults.reasoningEffort),
          )}
          onChange={editable('reasoningEffort', (reasoningEffort) => save({ reasoningEffort }))}
        />
        <ListPicker
          label="Interaction mode"
          value={defaults.interactionMode}
          options={INTERACTION_MODES}
          onChange={editable('interactionMode', (interactionMode) => save({ interactionMode }))}
        />
        <ListPicker
          label="Autonomy"
          value={defaults.autonomyLevel ?? 'off'}
          options={defaults.availableAutonomyLevels.map((level) => ({
            value: level,
            label: AUTONOMY_LABELS[level] ?? level,
          }))}
          onChange={editable('autonomyLevel', (autonomyLevel) => save({ autonomyLevel }))}
        />
      </ListSection>

      <ListSection
        title="Spec mode"
        footer={
          shownFolder === 'user'
            ? `Specs go to ${paths.user}.`
            : shownFolder === 'project'
              ? `Specs go to the project's ${paths.project}, or ${paths.user} outside a project.`
              : 'Specs from every Session go to this folder.'
        }
      >
        <ListPicker
          label="Spec model"
          value={defaults.specModeModelId ?? SAME_AS_MAIN}
          options={[{ value: SAME_AS_MAIN, label: 'Same as main' }, ...models(defaults)]}
          onChange={editable('specModeModelId', (value) =>
            save({
              specModeModelId: value === SAME_AS_MAIN ? null : value,
              specModeReasoningEffort: null,
            }),
          )}
        />
        <ListPicker
          label="Spec reasoning level"
          value={defaults.specModeReasoningEffort ?? SAME_AS_MAIN}
          options={[
            { value: SAME_AS_MAIN, label: 'Same as main' },
            ...efforts(
              reasoningChoices(
                defaults.models,
                specModel,
                defaults.specModeReasoningEffort ?? defaults.reasoningEffort,
              ),
            ),
          ]}
          onChange={editable('specModeReasoningEffort', (value) =>
            save({ specModeReasoningEffort: value === SAME_AS_MAIN ? null : value }),
          )}
        />
        <ListPicker
          label="Spec folder"
          value={shownFolder}
          options={[
            { value: 'user', label: 'User home' },
            { value: 'project', label: 'Project' },
            { value: 'custom', label: 'Custom folder' },
          ]}
          onChange={editable('specSaveDir', (value) => {
            setCustomFolder(value === 'custom')
            if (value === 'user') save({ specSaveDir: null })
            if (value === 'project') save({ specSaveDir: paths.project })
          })}
        />
      </ListSection>
      {shownFolder === 'custom' ? (
        <CustomFolder
          key={defaults.specSaveDir ?? ''}
          stored={specChoice === 'custom' ? (defaults.specSaveDir ?? '') : ''}
          onSave={(specSaveDir) => save({ specSaveDir })}
        />
      ) : null}

      <ListSection
        title="Compaction"
        footer="New Sessions compact their history once it passes the token limit."
      >
        <ListSwitch
          label="Compact automatically"
          value={defaults.compactionThresholdCheckEnabled}
          disabled={locked.has('compactionThresholdCheckEnabled')}
          onChange={(compactionThresholdCheckEnabled) => save({ compactionThresholdCheckEnabled })}
        />
        <ListPicker
          label="Token limit"
          value={String(defaults.compactionTokenLimit ?? DEFAULT_COMPACTION_LIMIT)}
          options={limits(defaults.compactionTokenLimit)}
          onChange={editable('compactionTokenLimit', (value) =>
            save({ compactionTokenLimit: Number(value) }),
          )}
        />
        <ListPicker
          label="Compaction model"
          value={defaults.compactionModel}
          options={[{ value: CURRENT_MODEL, label: 'Current model' }, ...models(defaults, false)]}
          onChange={editable('compactionModel', (compactionModel) => save({ compactionModel }))}
        />
      </ListSection>
      <ListSection title="Limits for specific models">
        {Object.entries(overrides).map(([modelId, tokens]) => (
          <ListPicker
            key={modelId}
            label={label(modelId)}
            value={String(tokens)}
            options={[{ value: 'remove', label: 'Remove this limit' }, ...limits(tokens)]}
            onChange={editable('compactionTokenLimitPerModel', (value) => {
              const next = { ...overrides }
              if (value === 'remove') delete next[modelId]
              else next[modelId] = Number(value)
              save({ compactionTokenLimitPerModel: next })
            })}
          />
        ))}
        <ListPicker
          label="Add a model limit"
          value=""
          options={models(defaults).filter((m) => !(m.value in overrides))}
          onChange={editable('compactionTokenLimitPerModel', (modelId) =>
            save({
              compactionTokenLimitPerModel: {
                ...overrides,
                [modelId]: defaults.compactionTokenLimit ?? DEFAULT_COMPACTION_LIMIT,
              },
            }),
          )}
        />
      </ListSection>

      <ListSection
        title="Subagents"
        footer="The model and reasoning level for subagents started with light, medium or heavy complexity."
      >
        <ListPicker
          label="Subagent autonomy"
          value={defaults.subagentAutonomyLevel}
          options={[
            { value: INHERIT, label: 'Inherit' },
            ...defaults.availableAutonomyLevels.map((level) => ({
              value: level,
              label: AUTONOMY_LABELS[level] ?? level,
            })),
          ]}
          onChange={editable('subagentAutonomyLevel', (subagentAutonomyLevel) =>
            save({ subagentAutonomyLevel }),
          )}
        />
        {SUBAGENT_TIERS.flatMap((tier) => {
          const model = subagents[`${tier}Model`] ?? null
          const effort = subagents[`${tier}ReasoningEffort`] ?? null
          const rows = [
            <ListPicker
              key={`${tier}-model`}
              label={`${TIER_LABELS[tier]} model`}
              value={model ?? INHERIT}
              options={[{ value: INHERIT, label: 'Inherit' }, ...models(defaults)]}
              onChange={editable(`subagent.${tier}Model`, (value) =>
                save({
                  subagentModelSettings: withSubagentTier(subagents, tier, {
                    model: value === INHERIT ? null : value,
                  }),
                }),
              )}
            />,
          ]
          if (model !== null) {
            rows.push(
              <ListPicker
                key={`${tier}-effort`}
                label={`${TIER_LABELS[tier]} reasoning`}
                value={effort ?? MODEL_DEFAULT}
                options={[
                  { value: MODEL_DEFAULT, label: 'Model default' },
                  ...efforts(reasoningChoices(defaults.models, model, effort)),
                ]}
                onChange={editable(`subagent.${tier}ReasoningEffort`, (value) =>
                  save({
                    subagentModelSettings: withSubagentTier(subagents, tier, {
                      reasoningEffort: value === MODEL_DEFAULT ? null : value,
                    }),
                  }),
                )}
              />,
            )
          }
          return rows
        })}
      </ListSection>
    </>
  )
}

function CustomFolder({ stored, onSave }: { stored: string; onSave: (path: string) => void }) {
  const colors = useColors()
  const [value, setValue] = useState(stored)
  const trimmed = value.trim()
  return (
    <View style={styles.folder}>
      <TextInput
        aria-label="Spec folder path"
        placeholder="/Users/you/specs"
        placeholderTextColor={colors.mutedForeground}
        value={value}
        onChangeText={setValue}
        autoCapitalize="none"
        autoCorrect={false}
        style={[
          styles.input,
          { borderColor: colors.input, color: colors.foreground, backgroundColor: colors.card },
        ]}
      />
      <Button
        label="Save folder"
        variant="secondary"
        disabled={!trimmed || trimmed === stored}
        onPress={() => onSave(trimmed)}
      />
    </View>
  )
}

const styles = StyleSheet.create({
  content: { padding: space.lg, gap: space.xl },
  intro: { paddingHorizontal: space.xs },
  folder: { gap: space.sm, marginTop: -space.md },
  input: {
    borderWidth: 1,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    height: 44,
    fontFamily: fonts.mono,
    fontSize: fontSize.sm,
  },
})

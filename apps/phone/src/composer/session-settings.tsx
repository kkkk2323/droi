// Model, reasoning effort and autonomy in the composer's bottom row, each
// opening a sheet. The model sheet has the web Client's brand rail, search
// and favourites (kept on this phone).
import { favoriteModels, usePreference } from '@droi/daemon-layer/local-preference'
import { BRAND_LABELS, brandOf, type Brand } from '@droi/daemon-layer/model-brand'
import {
  AUTONOMY_LABELS,
  EFFORT_LABELS,
  brandsOf,
  visibleModels,
  type PickerFilter,
} from '@droi/daemon-layer/model-choices'
import {
  AUTONOMY_LEVELS,
  useSessionSettings,
  useSessionSettingsActions,
  type SessionSettingsView,
} from '@droi/daemon-layer/use-session-settings'
import { ChevronDown, ShieldCheck, Star } from 'lucide-react-native'
import { useState, type ReactNode } from 'react'
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { BrandIcon } from '../ui/brand-icon'
import { Text } from '../ui/primitives'
import { Sheet, SheetOption } from '../ui/sheet'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SessionSettingsBar({ sessionId }: { sessionId: string }) {
  const settings = useSessionSettings(sessionId)
  const actions = useSessionSettingsActions(sessionId)
  return (
    <SettingsControls
      settings={settings}
      onModel={(value) => void actions.setModel(value)}
      onReasoningEffort={(value) => void actions.setReasoningEffort(value)}
      onAutonomyLevel={(value) => void actions.setAutonomyLevel(value)}
      error={actions.error}
    />
  )
}

type Open = 'model' | 'effort' | 'autonomy' | null

export function SettingsControls({
  settings,
  onModel,
  onReasoningEffort,
  onAutonomyLevel,
  error = null,
}: {
  settings: SessionSettingsView
  onModel: (modelId: string) => void
  onReasoningEffort: (effort: string) => void
  onAutonomyLevel: (level: string) => void
  error?: string | null
}) {
  const colors = useColors()
  const [open, setOpen] = useState<Open>(null)
  const model = settings.models.find((m) => m.id === settings.modelId)
  const efforts =
    model?.reasoningEfforts ?? (settings.reasoningEffort ? [settings.reasoningEffort] : [])
  const close = () => setOpen(null)

  return (
    <View style={styles.wrap}>
      {error ? (
        <Text role="alert" size="xs" style={{ color: colors.destructiveForeground }}>
          {error}
        </Text>
      ) : null}
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={styles.row}
      >
        <Pill
          label="Model"
          icon={
            model ? (
              <BrandIcon
                brand={brandOf(model.id, model.provider)}
                size={12}
                color={colors.mutedForeground}
              />
            ) : undefined
          }
          value={model?.label ?? settings.modelId ?? '…'}
          onPress={() => setOpen('model')}
        />
        {efforts.length > 0 ? (
          <Pill
            label="Reasoning effort"
            value={EFFORT_LABELS[settings.reasoningEffort ?? ''] ?? settings.reasoningEffort ?? '…'}
            onPress={() => setOpen('effort')}
          />
        ) : null}
        <Pill
          label="Autonomy"
          icon={<ShieldCheck size={12} color={colors.mutedForeground} strokeWidth={1.75} />}
          value={AUTONOMY_LABELS[settings.autonomyLevel ?? ''] ?? settings.autonomyLevel ?? '…'}
          onPress={() => setOpen('autonomy')}
        />
      </ScrollView>
      <ModelSheet
        visible={open === 'model'}
        settings={settings}
        onClose={close}
        onPick={(id) => {
          close()
          if (id !== settings.modelId) onModel(id)
        }}
      />
      <Sheet visible={open === 'effort'} title="Reasoning effort" onClose={close}>
        <View role="radiogroup" aria-label="Reasoning effort">
          {efforts.map((effort) => (
            <SheetOption
              key={effort}
              label={EFFORT_LABELS[effort] ?? effort}
              checked={effort === settings.reasoningEffort}
              onPress={() => {
                close()
                onReasoningEffort(effort)
              }}
            />
          ))}
        </View>
      </Sheet>
      <Sheet visible={open === 'autonomy'} title="Autonomy" onClose={close}>
        <View role="radiogroup" aria-label="Autonomy">
          {AUTONOMY_LEVELS.map((level) => (
            <SheetOption
              key={level}
              label={AUTONOMY_LABELS[level] ?? level}
              checked={level === settings.autonomyLevel}
              onPress={() => {
                close()
                onAutonomyLevel(level)
              }}
            />
          ))}
        </View>
      </Sheet>
    </View>
  )
}

function Pill({
  label,
  value,
  icon,
  onPress,
}: {
  label: string
  value: string
  icon?: ReactNode
  onPress: () => void
}) {
  const colors = useColors()
  return (
    <Pressable
      role="button"
      aria-label={label}
      aria-haspopup="dialog"
      onPress={onPress}
      style={({ pressed }) => [styles.pill, pressed ? { backgroundColor: colors.accent } : null]}
    >
      {icon}
      <Text size="xs" tone="muted" numberOfLines={1}>
        {value}
      </Text>
      <ChevronDown size={12} color={colors.mutedForeground} />
    </Pressable>
  )
}

function ModelSheet({
  visible,
  settings,
  onClose,
  onPick,
}: {
  visible: boolean
  settings: SessionSettingsView
  onClose: () => void
  onPick: (modelId: string) => void
}) {
  const colors = useColors()
  const [filter, setFilter] = useState<PickerFilter>('all')
  const [query, setQuery] = useState('')
  const [favorites, setFavorites] = usePreference(favoriteModels)
  const rows = visibleModels(settings.models, favorites, filter, query)
  const choose = (next: PickerFilter) => setFilter(filter === next ? 'all' : next)
  const toggleFavorite = (id: string) =>
    setFavorites(favorites.includes(id) ? favorites.filter((f) => f !== id) : [...favorites, id])

  return (
    <Sheet visible={visible} title="Choose a model" onClose={onClose}>
      <TextInput
        role="searchbox"
        aria-label="Search models"
        placeholder="Search models"
        placeholderTextColor={colors.mutedForeground}
        value={query}
        onChangeText={setQuery}
        autoCapitalize="none"
        autoCorrect={false}
        style={[styles.search, { backgroundColor: colors.card, color: colors.foreground }]}
      />
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        role="toolbar"
        aria-label="Filter models"
        contentContainerStyle={styles.rail}
      >
        {(['favorites', ...brandsOf(settings.models)] as PickerFilter[]).map((option) => (
          <Pressable
            key={option}
            role="button"
            aria-label={
              option === 'favorites'
                ? 'Favorites'
                : BRAND_LABELS[option as keyof typeof BRAND_LABELS]
            }
            aria-pressed={filter === option}
            onPress={() => choose(option)}
            style={[
              styles.chip,
              { backgroundColor: filter === option ? colors.primary : colors.card },
            ]}
          >
            {option === 'favorites' ? (
              <Star
                size={12}
                color={filter === option ? colors.primaryForeground : colors.foreground}
                fill={filter === option ? colors.primaryForeground : 'transparent'}
                strokeWidth={1.75}
              />
            ) : (
              <BrandIcon
                brand={option as Brand}
                size={12}
                color={filter === option ? colors.primaryForeground : colors.foreground}
              />
            )}
            <Text
              size="xs"
              weight="medium"
              style={{ color: filter === option ? colors.primaryForeground : colors.foreground }}
            >
              {option === 'favorites'
                ? 'Favorites'
                : BRAND_LABELS[option as keyof typeof BRAND_LABELS]}
            </Text>
          </Pressable>
        ))}
      </ScrollView>
      <ScrollView role="list" aria-label="Models" style={styles.models}>
        {rows.map((row) => {
          const starred = favorites.includes(row.id)
          return (
            <View key={row.id} role="listitem" style={styles.modelRow}>
              <Pressable
                role="radio"
                aria-checked={row.id === settings.modelId}
                aria-label={row.label}
                disabled={row.disabled}
                onPress={() => onPick(row.id)}
                style={({ pressed }) => [
                  styles.model,
                  {
                    backgroundColor:
                      pressed || row.id === settings.modelId ? colors.accent : undefined,
                    opacity: row.disabled ? 0.5 : 1,
                  },
                ]}
              >
                <Text weight={row.id === settings.modelId ? 'medium' : 'regular'}>{row.label}</Text>
                <View style={styles.brandLine}>
                  <BrandIcon brand={row.brand} size={12} color={colors.mutedForeground} />
                  <Text size="xs" tone="muted">
                    {row.provider === null ? 'Router' : BRAND_LABELS[row.brand]}
                  </Text>
                </View>
              </Pressable>
              <Pressable
                role="button"
                aria-label={`${starred ? 'Unstar' : 'Star'} ${row.label}`}
                aria-pressed={starred}
                hitSlop={8}
                onPress={() => toggleFavorite(row.id)}
                style={styles.star}
              >
                <Star
                  size={16}
                  color={starred ? colors.attention : colors.mutedForeground}
                  fill={starred ? colors.attention : 'transparent'}
                  strokeWidth={1.75}
                />
              </Pressable>
            </View>
          )
        })}
      </ScrollView>
    </Sheet>
  )
}

const styles = StyleSheet.create({
  wrap: { flexShrink: 1, gap: space.xs },
  row: { alignItems: 'center', gap: 2 },
  pill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    height: 30,
    paddingHorizontal: space.sm,
    borderRadius: radius.md,
    maxWidth: 180,
  },
  search: {
    marginHorizontal: space.lg,
    height: 40,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    fontFamily: fonts.sans,
    fontSize: fontSize.base,
  },
  rail: { gap: space.xs, paddingHorizontal: space.lg, paddingVertical: space.sm },
  chip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    height: 28,
    paddingHorizontal: space.md,
    borderRadius: radius.full,
  },
  brandLine: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  models: { flexGrow: 0 },
  modelRow: { flexDirection: 'row', alignItems: 'center', paddingRight: space.lg },
  model: {
    flex: 1,
    minHeight: 48,
    justifyContent: 'center',
    paddingHorizontal: space.lg,
    marginLeft: space.sm,
    borderRadius: radius.lg,
  },
  star: { padding: space.sm },
})

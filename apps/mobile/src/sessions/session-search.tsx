// Search across every Session on the computer, from the drawer. The Daemon
// does the searching (use-session-search.ts); while a query is typed the
// results take the place of the Workspace groups.
import { useSessionSearch, type SessionSearchHit } from '@droi/daemon-layer/use-session-search'
import { Search, X } from 'lucide-react-native'
import { Pressable, StyleSheet, TextInput, View } from 'react-native'
import { Spinner } from '../ui/activity'
import { Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SessionSearchBox({
  query,
  onChange,
}: {
  query: string
  onChange: (query: string) => void
}) {
  const colors = useColors()
  return (
    <View style={[styles.box, { backgroundColor: colors.sidebarAccent }]}>
      <Search size={16} color={colors.mutedForeground} strokeWidth={1.75} />
      <TextInput
        role="searchbox"
        aria-label="Search sessions"
        placeholder="Search sessions"
        placeholderTextColor={colors.mutedForeground}
        value={query}
        onChangeText={onChange}
        autoCapitalize="none"
        autoCorrect={false}
        returnKeyType="search"
        clearButtonMode="never"
        style={[styles.input, { color: colors.foreground }]}
      />
      {query ? (
        <Pressable
          role="button"
          aria-label="Clear search"
          hitSlop={8}
          onPress={() => onChange('')}
          style={styles.clear}
        >
          <X size={14} color={colors.mutedForeground} strokeWidth={1.75} />
        </Pressable>
      ) : null}
    </View>
  )
}

export function SessionSearchResults({
  query,
  selectedId,
  onSelect,
}: {
  query: string
  selectedId: string | null
  onSelect: (sessionId: string) => void
}) {
  const colors = useColors()
  const search = useSessionSearch(query)
  if (!search.active) {
    return (
      <Text tone="muted" size="xs" style={styles.note}>
        Type a little more to search.
      </Text>
    )
  }
  if (search.error) {
    return (
      <Text role="alert" size="xs" style={[styles.note, { color: colors.destructiveForeground }]}>
        {search.error.message}
      </Text>
    )
  }
  if (!search.hits) {
    return (
      <View style={[styles.note, styles.row]}>
        <Spinner size={12} color={colors.mutedForeground} />
        <Text tone="muted" size="xs">
          Searching…
        </Text>
      </View>
    )
  }
  if (search.hits.length === 0) {
    return (
      <Text tone="muted" size="xs" style={styles.note}>
        No sessions match.
      </Text>
    )
  }
  return (
    <View role="region" aria-label="Search results" aria-busy={search.isSearching}>
      <View role="list">
        {search.hits.map((hit) => (
          <View key={hit.sessionId} role="listitem">
            <SearchHitRow
              hit={hit}
              selected={hit.sessionId === selectedId}
              onSelect={() => onSelect(hit.sessionId)}
            />
          </View>
        ))}
      </View>
    </View>
  )
}

function SearchHitRow({
  hit,
  selected,
  onSelect,
}: {
  hit: SessionSearchHit
  selected: boolean
  onSelect: () => void
}) {
  const colors = useColors()
  return (
    <Pressable
      role="button"
      aria-current={selected ? 'page' : undefined}
      onPress={onSelect}
      style={({ pressed }) => [
        styles.hit,
        selected || pressed ? { backgroundColor: colors.sidebarAccent } : null,
      ]}
    >
      <Text size="sm" numberOfLines={1}>
        {hit.title}
      </Text>
      {hit.snippet.length > 0 ? (
        <Text tone="muted" size="xs" numberOfLines={2}>
          {hit.snippet.map((run) =>
            run.match ? (
              <Text key={run.offset} size="xs" weight="medium" style={{ color: colors.foreground }}>
                {run.text}
              </Text>
            ) : (
              run.text
            ),
          )}
        </Text>
      ) : null}
    </Pressable>
  )
}

const styles = StyleSheet.create({
  box: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    marginHorizontal: space.sm,
    marginBottom: space.xs,
    paddingHorizontal: space.sm,
    height: 36,
    borderRadius: radius.lg,
  },
  input: {
    flex: 1,
    height: 36,
    fontFamily: fonts.sans,
    fontSize: fontSize.sm,
  },
  clear: { padding: space.xs },
  note: { paddingHorizontal: space.sm, paddingVertical: space.xs },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  hit: {
    gap: 2,
    paddingHorizontal: space.sm,
    paddingVertical: space.xs,
    borderRadius: radius.lg,
  },
})

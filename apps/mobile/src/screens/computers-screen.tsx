// The Paired Computers, to rename, re-address or remove, and to add another.
import { usePreference } from '@droi/daemon-layer/local-preference'
import { useRouter } from 'expo-router'
import { ScrollView, StyleSheet } from 'react-native'
import { pairedComputers } from '../computers/store'
import { ListRow, ListSection } from '../ui/list'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function ComputersScreen() {
  const colors = useColors()
  const router = useRouter()
  const [computers] = usePreference(pairedComputers)
  return (
    <ScrollView
      style={{ backgroundColor: colors.background }}
      contentContainerStyle={styles.content}
    >
      {computers.length > 0 ? (
        <ListSection>
          {computers.map((computer) => (
            <ListRow
              key={computer.id}
              label={computer.name}
              value={computer.address.replace(/^https?:\/\//, '')}
              onPress={() =>
                router.push({ pathname: '/computer/[id]', params: { id: computer.id } })
              }
            />
          ))}
        </ListSection>
      ) : null}
      <ListSection>
        <ListRow label="Add a computer" onPress={() => router.push('/pair')} />
      </ListSection>
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  content: { padding: space.lg, gap: space.xl },
})

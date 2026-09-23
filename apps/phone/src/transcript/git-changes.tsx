// The Session's branch and uncommitted line counts in the header; tapping
// opens the changed files. Absent when the Workspace is not a Git repository.
import type { ChangedFile, GitChanges } from '@droi/daemon-layer/use-git-changes'
import { GitBranch } from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, ScrollView, StyleSheet, View } from 'react-native'
import { Text } from '../ui/primitives'
import { Sheet } from '../ui/sheet'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function GitChangesButton({ changes }: { changes: GitChanges | null }) {
  const colors = useColors()
  const [open, setOpen] = useState(false)
  if (!changes) return null
  const dirty = changes.files.length > 0
  const count = changes.files.length
  return (
    <>
      <Pressable
        role="button"
        aria-label={
          dirty
            ? `Branch ${changes.branch}, ${count} changed ${plural(count, 'file')}`
            : `Branch ${changes.branch}, no changes`
        }
        aria-haspopup="dialog"
        onPress={() => setOpen(true)}
        style={({ pressed }) => [
          styles.button,
          pressed ? { backgroundColor: colors.accent } : null,
        ]}
      >
        <GitBranch size={13} color={colors.mutedForeground} strokeWidth={1.75} />
        <Text tone="muted" size="xs" numberOfLines={1} style={styles.branch}>
          {changes.branch}
        </Text>
        {dirty ? <Counts additions={changes.additions} deletions={changes.deletions} /> : null}
      </Pressable>
      <Sheet
        visible={open}
        title={dirty ? `${count} uncommitted ${plural(count, 'file')}` : 'Working tree clean'}
        onClose={() => setOpen(false)}
      >
        {dirty ? (
          <ScrollView role="list" aria-label="Changed files" style={styles.list}>
            {changes.files.map((file) => (
              <FileRow key={file.path} file={file} />
            ))}
          </ScrollView>
        ) : null}
      </Sheet>
    </>
  )
}

function Counts({ additions, deletions }: { additions: number; deletions: number }) {
  const colors = useColors()
  return (
    <Text size="xs" mono>
      <Text size="xs" mono style={{ color: colors.success }}>
        +{additions}
      </Text>{' '}
      <Text size="xs" mono style={{ color: colors.destructiveForeground }}>
        −{deletions}
      </Text>
    </Text>
  )
}

const STATUS_LETTER: Record<string, string> = {
  added: 'A',
  modified: 'M',
  deleted: 'D',
  renamed: 'R',
  copied: 'C',
  untracked: 'A',
}

function FileRow({ file }: { file: ChangedFile }) {
  const colors = useColors()
  const slash = file.path.lastIndexOf('/')
  const dir = slash >= 0 ? file.path.slice(0, slash + 1) : ''
  const name = slash >= 0 ? file.path.slice(slash + 1) : file.path
  return (
    <View role="listitem" style={styles.file}>
      <Text
        aria-label={file.status}
        size="xs"
        mono
        style={[
          styles.status,
          {
            color:
              file.status === 'deleted' ? colors.destructiveForeground : colors.mutedForeground,
          },
        ]}
      >
        {STATUS_LETTER[file.status] ?? file.status.charAt(0).toUpperCase()}
      </Text>
      <View style={styles.name}>
        <Text size="sm" mono numberOfLines={1}>
          {name}
        </Text>
        {dir ? (
          <Text size="xs" mono tone="muted" numberOfLines={1}>
            {dir}
          </Text>
        ) : null}
      </View>
      <Counts additions={file.additions} deletions={file.deletions} />
    </View>
  )
}

function plural(count: number, word: string): string {
  return count === 1 ? word : `${word}s`
}

const styles = StyleSheet.create({
  button: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    height: 30,
    paddingHorizontal: space.sm,
    borderRadius: radius.md,
    maxWidth: 200,
  },
  branch: { flexShrink: 1 },
  list: { flexGrow: 0, paddingHorizontal: space.sm },
  file: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 44,
    paddingHorizontal: space.sm,
  },
  status: { width: 14, textAlign: 'center' },
  name: { flex: 1, minWidth: 0 },
})

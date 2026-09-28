// The Session's skills and MCP Servers, behind a wrench in the header: the
// phone's take on the web Client's panel, in a sheet with two tabs.
import { Wrench } from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, ScrollView, StyleSheet, View } from 'react-native'
import { Text } from '../ui/primitives'
import { Sheet } from '../ui/sheet'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { McpList } from './mcp-list'
import { SkillsList } from './skills-list'

type Tab = 'skills' | 'mcp'

const TABS: Array<{ id: Tab; label: string }> = [
  { id: 'skills', label: 'Skills' },
  { id: 'mcp', label: 'MCP servers' },
]

export function ToolsButton({ sessionId }: { sessionId: string }) {
  const colors = useColors()
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState<Tab>('skills')
  return (
    <>
      <Pressable
        role="button"
        aria-label="Skills and MCP servers"
        aria-haspopup="dialog"
        onPress={() => setOpen(true)}
        style={({ pressed }) => [
          styles.button,
          pressed ? { backgroundColor: colors.accent } : null,
        ]}
      >
        <Wrench size={15} color={colors.mutedForeground} strokeWidth={1.75} />
      </Pressable>
      <Sheet visible={open} title="Skills and MCP servers" onClose={() => setOpen(false)}>
        <View role="tablist" aria-label="Skills and MCP servers" style={styles.tabs}>
          {TABS.map(({ id, label }) => (
            <Pressable
              key={id}
              role="tab"
              aria-selected={tab === id}
              onPress={() => setTab(id)}
              style={[styles.tab, tab === id ? { backgroundColor: colors.accent } : null]}
            >
              <Text
                size="sm"
                weight={tab === id ? 'medium' : 'regular'}
                tone={tab === id ? 'default' : 'muted'}
              >
                {label}
              </Text>
            </Pressable>
          ))}
        </View>
        <ScrollView
          role="tabpanel"
          style={styles.panel}
          contentContainerStyle={styles.panelContent}
          keyboardShouldPersistTaps="handled"
        >
          {open ? (
            tab === 'skills' ? (
              <SkillsList sessionId={sessionId} />
            ) : (
              <McpList sessionId={sessionId} />
            )
          ) : null}
        </ScrollView>
      </Sheet>
    </>
  )
}

const styles = StyleSheet.create({
  button: {
    width: 30,
    height: 30,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.md,
  },
  tabs: {
    flexDirection: 'row',
    gap: space.xs,
    paddingHorizontal: space.lg,
    paddingBottom: space.sm,
  },
  tab: {
    height: 30,
    paddingHorizontal: space.md,
    borderRadius: radius.md,
    justifyContent: 'center',
  },
  panel: { flexGrow: 0 },
  panelContent: { paddingHorizontal: space.lg, paddingBottom: space.md, gap: space.md },
})

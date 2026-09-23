// The transcript: opens on the latest message, follows streamed output while
// the reader is at the bottom, stays put once they scroll up, and offers the
// way back down. Same rules as the web Client's list, on a FlatList.
import type { FactoryDroidMessage } from '@factory/droid-sdk'
import {
  buildTranscript,
  turnEndIds,
  workingLabel,
  type TranscriptEntry,
} from '@droi/daemon-layer/transcript'
import { ArrowDown } from 'lucide-react-native'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  FlatList,
  Pressable,
  StyleSheet,
  View,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from 'react-native'
import { BouncingDots } from '../ui/activity'
import { Text } from '../ui/primitives'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { MessageEntry } from './message-entry'

/** How far above the bottom, in points, still counts as reading the latest output. */
const FOLLOW_THRESHOLD = 120
/** A height change this soon after a touch in the list is the reader's own (a row opening). */
const USER_RESIZE_WINDOW_MS = 500

const NO_EARLIER: ReadonlyArray<readonly FactoryDroidMessage[]> = []

export function TranscriptView({
  messages,
  earlier = NO_EARLIER,
  workingState,
  lead = null,
  scrollToEndKey = 0,
}: {
  messages: readonly FactoryDroidMessage[]
  /** The Sessions this one continues after compactions, oldest first, shown above it. */
  earlier?: ReadonlyArray<readonly FactoryDroidMessage[]>
  workingState: string
  lead?: ReactNode
  /** Bumped on send: the list goes to the end wherever it was. */
  scrollToEndKey?: number
}) {
  const colors = useColors()
  const list = useRef<FlatList<TranscriptEntry>>(null)
  const following = useRef(true)
  const geometry = useRef({ offset: 0, viewport: 0 })
  const lastTouched = useRef(0)
  const [atBottom, setAtBottom] = useState(true)

  const parts = [...earlier, messages].map((part) => buildTranscript(part))
  const entries = parts.flat()
  const last = entries[entries.length - 1]
  const running = workingState !== 'idle'
  const streamingId =
    workingState === 'streaming_assistant_message' && last?.role === 'assistant' ? last.id : null
  const ends = turnEndIds(entries, running)
  // Where each continued Session starts, below the one it continues.
  const boundaryIds = new Set(parts.slice(1).flatMap((part) => (part[0] ? [part[0].id] : [])))
  const activity = workingLabel(workingState)

  // Not FlatList.scrollToEnd: it measures the end from cell layouts that lag
  // behind a streaming row's growth. The content height is always current;
  // an offset past the end stops at the end.
  const scrollToEnd = (animated: boolean) =>
    list.current?.scrollToOffset({ offset: Number.MAX_SAFE_INTEGER / 2, animated })

  useEffect(() => {
    if (!scrollToEndKey) return
    following.current = true
    const end = () =>
      list.current?.scrollToOffset({ offset: Number.MAX_SAFE_INTEGER / 2, animated: true })
    end()
    // The sent message lands a moment after the send.
    const timer = setTimeout(end, 150)
    return () => clearTimeout(timer)
  }, [scrollToEndKey])

  const onScroll = (event: NativeSyntheticEvent<NativeScrollEvent>) => {
    const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent
    const bottom =
      contentSize.height - contentOffset.y - layoutMeasurement.height <= FOLLOW_THRESHOLD
    // Only the reader moving up leaves the bottom. Content growing under the
    // viewport also fires scroll events (the browser's scroll anchoring,
    // rows rendering in), and those must not stop the following.
    if (bottom) following.current = true
    else if (contentOffset.y < geometry.current.offset - 1) following.current = false
    geometry.current = { offset: contentOffset.y, viewport: layoutMeasurement.height }
    setAtBottom(following.current)
  }

  const onContentSizeChange = (_width: number, height: number) => {
    if (!following.current) return
    if (Date.now() - lastTouched.current < USER_RESIZE_WINDOW_MS) {
      // A row the reader opened grows in place; follow again only from the bottom.
      const { offset, viewport } = geometry.current
      following.current = height - offset - viewport <= FOLLOW_THRESHOLD
      return
    }
    scrollToEnd(false)
  }

  const onLayout = (event: LayoutChangeEvent) => {
    geometry.current.viewport = event.nativeEvent.layout.height
  }

  if (entries.length === 0 && !lead) {
    return (
      <View style={styles.empty}>
        <Text tone="muted">What should Droid work on?</Text>
      </View>
    )
  }

  return (
    <View
      style={styles.fill}
      onTouchStart={() => {
        lastTouched.current = Date.now()
      }}
    >
      <FlatList
        ref={list}
        role="log"
        aria-label="Transcript"
        data={entries}
        keyExtractor={(entry) => entry.id}
        renderItem={({ item }) => (
          <>
            {boundaryIds.has(item.id) ? <Boundary /> : null}
            <MessageEntry
              entry={item}
              isStreaming={item.id === streamingId}
              showTime={ends.has(item.id)}
            />
          </>
        )}
        ListHeaderComponent={<View style={styles.header}>{lead}</View>}
        ListFooterComponent={
          <View role="status" aria-label="Session activity" style={styles.activity}>
            {activity ? (
              <>
                <BouncingDots color={colors.mutedForeground} />
                <Text tone="muted" size="sm">
                  {activity}
                </Text>
              </>
            ) : null}
          </View>
        }
        contentContainerStyle={styles.content}
        onScroll={onScroll}
        scrollEventThrottle={16}
        onContentSizeChange={onContentSizeChange}
        onLayout={onLayout}
        keyboardDismissMode="interactive"
        initialNumToRender={20}
        windowSize={11}
      />
      {!atBottom ? (
        <Pressable
          role="button"
          aria-label="Scroll to latest"
          onPress={() => {
            following.current = true
            scrollToEnd(true)
          }}
          style={[styles.down, { backgroundColor: colors.background, borderColor: colors.border }]}
        >
          <ArrowDown size={18} color={colors.foreground} strokeWidth={1.75} />
        </Pressable>
      ) : null}
    </View>
  )
}

function Boundary() {
  const colors = useColors()
  return (
    <View role="separator" aria-label="Context compacted here" style={styles.boundary}>
      <View style={[styles.line, { backgroundColor: colors.border }]} />
      <Text tone="muted" size="xs" style={styles.boundaryText}>
        Context compacted here; the conversation continues in a new session
      </Text>
      <View style={[styles.line, { backgroundColor: colors.border }]} />
    </View>
  )
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  empty: { flex: 1, alignItems: 'center', justifyContent: 'center' },
  content: { paddingHorizontal: space.lg },
  header: { paddingTop: space.md },
  activity: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 28,
    paddingBottom: space.lg,
  },
  down: {
    position: 'absolute',
    bottom: space.md,
    alignSelf: 'center',
    width: 36,
    height: 36,
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: 'center',
    justifyContent: 'center',
  },
  boundary: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    paddingVertical: space.md,
  },
  line: { flex: 1, height: StyleSheet.hairlineWidth },
  boundaryText: { flexShrink: 1, textAlign: 'center' },
})

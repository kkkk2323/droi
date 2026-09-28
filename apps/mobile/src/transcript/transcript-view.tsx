// The transcript: opens on the latest message, follows streamed output while
// the reader is at the bottom, stays put once they scroll up, and offers the
// way back down.
//
// The list is inverted: row 0 is the newest and scroll offset 0 is the bottom
// of the screen. A chat has one fixed point, the latest message above the
// composer, and an inverted list puts the scroll origin there. It opens on the
// latest message with no scroll at all, the keyboard shrinking the viewport
// leaves it where it is, and history coming in lands at the far end, out of
// sight. Output that grows below the reader is kept from moving what they read
// by maintainVisibleContentPosition (the browser's scroll anchoring on the
// web), which holds the row they are on; that is why a turn is split into a
// row per block.
import {
  turnEndIds,
  workingLabel,
  type TranscriptBlock,
  type TranscriptEntry,
} from '@droi/daemon-layer/transcript'
import { ArrowDown } from 'lucide-react-native'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  FlatList,
  Pressable,
  StyleSheet,
  View,
  type GestureResponderEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from 'react-native'
import { BouncingDots } from '../ui/activity'
import { Text } from '../ui/primitives'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { AssistantBlock, UserMessage } from './message-entry'

/** How far above the bottom, in points, still counts as reading the latest output. */
const FOLLOW_THRESHOLD = 120
/** A height change this soon after a tap in the list is the reader's own (a row opening). */
const TAP_RESIZE_WINDOW_MS = 500
/** A touch that moves less than this, in points, is a tap rather than a scroll. */
const TAP_SLOP = 10

const NO_EARLIER: ReadonlyArray<readonly TranscriptEntry[]> = []

interface Row {
  key: string
  entry: TranscriptEntry
  /** An assistant turn's block; null for the user's message. */
  block: TranscriptBlock | null
  first: boolean
  last: boolean
}

/** The rows newest first, as the inverted list takes them. */
function rowsOf(entries: readonly TranscriptEntry[]): Row[] {
  const rows: Row[] = []
  for (let e = entries.length - 1; e >= 0; e--) {
    const entry = entries[e]!
    if (entry.role === 'user') {
      rows.push({ key: entry.id, entry, block: null, first: true, last: true })
      continue
    }
    for (let b = entry.blocks.length - 1; b >= 0; b--) {
      const block = entry.blocks[b]!
      rows.push({
        key: block.id,
        entry,
        block,
        first: b === 0,
        last: b === entry.blocks.length - 1,
      })
    }
  }
  return rows
}

export function TranscriptView({
  transcript,
  earlier = NO_EARLIER,
  workingState,
  lead = null,
  scrollToEndKey = 0,
}: {
  transcript: readonly TranscriptEntry[]
  /** The Sessions this one continues after compactions, oldest first, shown above it. */
  earlier?: ReadonlyArray<readonly TranscriptEntry[]>
  workingState: string
  lead?: ReactNode
  /** Bumped on send: the list goes to the end wherever it was. */
  scrollToEndKey?: number
}) {
  const colors = useColors()
  const list = useRef<FlatList<Row>>(null)
  const offset = useRef(0)
  const contentHeight = useRef(0)
  const touchY = useRef(0)
  const lastTap = useRef(0)
  const [atLatest, setAtLatest] = useState(true)

  const parts = [...earlier, transcript]
  const entries = parts.flat()
  const rows = rowsOf(entries)
  const last = entries[entries.length - 1]
  const running = workingState !== 'idle'
  const streamingId =
    workingState === 'streaming_assistant_message' && last?.role === 'assistant' ? last.id : null
  const ends = turnEndIds(entries, running)
  // Where each continued Session starts, below the one it continues.
  const boundaryIds = new Set(parts.slice(1).flatMap((part) => (part[0] ? [part[0].id] : [])))
  const activity = workingLabel(workingState)

  const toLatest = (animated: boolean) => list.current?.scrollToOffset({ offset: 0, animated })

  useEffect(() => {
    if (!scrollToEndKey) return
    const latest = () => list.current?.scrollToOffset({ offset: 0, animated: true })
    latest()
    // The sent message lands a moment after the send.
    const timer = setTimeout(latest, 150)
    return () => clearTimeout(timer)
  }, [scrollToEndKey])

  const onScroll = (event: NativeSyntheticEvent<NativeScrollEvent>) => {
    offset.current = event.nativeEvent.contentOffset.y
    setAtLatest(offset.current <= FOLLOW_THRESHOLD)
  }

  // Nothing here follows the output: offset 0 is the bottom, so a reader
  // there sees what arrives without a scroll.
  const onContentSizeChange = (_width: number, height: number) => {
    const grown = height - contentHeight.current
    contentHeight.current = height
    if (grown === 0 || Date.now() - lastTap.current >= TAP_RESIZE_WINDOW_MS) return
    // A row the reader opened grows away from the bottom, up the screen;
    // scrolling by as much keeps the row under their finger.
    list.current?.scrollToOffset({ offset: Math.max(0, offset.current + grown), animated: false })
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
      // Watches presses without taking them: a press that stays put is a tap
      // (which may open a row), one that moves is a scroll.
      onStartShouldSetResponderCapture={(event: GestureResponderEvent) => {
        touchY.current = event.nativeEvent.pageY
        lastTap.current = Date.now()
        return false
      }}
      onMoveShouldSetResponderCapture={(event: GestureResponderEvent) => {
        if (Math.abs(event.nativeEvent.pageY - touchY.current) >= TAP_SLOP) lastTap.current = 0
        return false
      }}
    >
      <FlatList
        ref={list}
        role="log"
        aria-label="Transcript"
        inverted
        data={rows}
        keyExtractor={(row) => row.key}
        renderItem={({ item }) => (
          <>
            {item.first && boundaryIds.has(item.entry.id) ? <Boundary /> : null}
            {item.block ? (
              <AssistantBlock
                block={item.block}
                turnStreaming={item.entry.id === streamingId}
                first={item.first}
                last={item.last}
                isError={item.last && item.entry.isError}
                time={item.last && ends.has(item.entry.id) ? item.entry.createdAt : 0}
              />
            ) : (
              <UserMessage entry={item.entry} />
            )}
          </>
        )}
        // Swapped: an inverted list draws its header at the bottom, after the
        // latest row, and its footer at the top, before the oldest.
        ListHeaderComponent={
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
        ListFooterComponent={
          <View
            style={styles.lead}
            // What is tapped here brings rows in at the far end, where they
            // move nothing; no row opened.
            onStartShouldSetResponderCapture={() => {
              lastTap.current = 0
              return false
            }}
          >
            {lead}
          </View>
        }
        contentContainerStyle={styles.content}
        onScroll={onScroll}
        scrollEventThrottle={16}
        onContentSizeChange={onContentSizeChange}
        keyboardDismissMode="interactive"
        // Holds the row being read while rows below it grow or arrive, and
        // carries a reader at the bottom along with them.
        maintainVisibleContentPosition={{
          minIndexForVisible: 0,
          autoscrollToTopThreshold: FOLLOW_THRESHOLD,
        }}
        initialNumToRender={20}
        windowSize={11}
      />
      {!atLatest ? (
        <Pressable
          role="button"
          aria-label="Scroll to latest"
          onPress={() => toLatest(true)}
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
  // A conversation shorter than the screen starts at the top, as it reads;
  // inverted, the far end of the content is the top.
  content: { flexGrow: 1, justifyContent: 'flex-end', paddingHorizontal: space.lg },
  lead: { paddingTop: space.md },
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

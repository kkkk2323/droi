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
//
// A row the reader opens grows the other way, up the screen, and the list
// scrolls by as much to keep the tapped header in place. The scroll is the
// contentOffset prop rather than a command: the prop lands in the same native
// update as the row's new size, while a command from JS would arrive a frame
// or more after the row had already moved. (Fold measures the growth first.)
import {
  turnEnds,
  workingLabel,
  type TranscriptBlock,
  type TranscriptEntry,
} from '@droi/daemon-layer/transcript'
import { ArrowDown } from 'lucide-react-native'
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import {
  FlatList,
  Platform,
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
import { OnGrow } from './fold'
import { AssistantBlock, UserMessage } from './message-entry'

/** How far above the bottom, in points, still counts as reading the latest output. */
const FOLLOW_THRESHOLD = 120

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
  const viewport = useRef(0)
  /** The content's height, or the viewport's when the conversation is shorter. */
  const contentHeight = useRef(0)
  /** What a conversation shorter than the screen leaves empty below it. */
  const slack = useRef(0)
  const [pin, setPin] = useState<{ x: number; y: number }>()
  const lastPin = useRef(0)
  const [atLatest, setAtLatest] = useState(true)

  const parts = [...earlier, transcript]
  const entries = parts.flat()
  const rows = rowsOf(entries)
  const last = entries[entries.length - 1]
  const running = workingState !== 'idle'
  const streamingId =
    workingState === 'streaming_assistant_message' && last?.role === 'assistant' ? last.id : null
  const ends = turnEnds(entries, running)
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

  // Nothing follows the output: offset 0 is the bottom, so a reader there
  // sees what arrives without a scroll. The sizes are kept for grow.
  const onContentSizeChange = (_width: number, height: number) => {
    contentHeight.current = height
  }
  const onLayout = (event: LayoutChangeEvent) => {
    viewport.current = event.nativeEvent.layout.height
  }

  // A fold in a row opened or closed: scroll by as much, in the same commit.
  const grow = useCallback((by: number) => {
    let y = Math.max(0, offset.current + by)
    if (Platform.OS !== 'web') {
      // A UIScrollView takes an offset past its end as given, where the
      // browser clamps; a conversation short of the screen has no room to
      // scroll until it outgrows the screen.
      const content = contentHeight.current - slack.current + by
      y = Math.min(y, Math.max(0, content - viewport.current))
    }
    if (y === offset.current) return
    // The prop reaches native only when it changes.
    if (y === lastPin.current) y += 1
    offset.current = y
    lastPin.current = y
    setPin({ x: 0, y })
  }, [])

  // The web build ignores contentOffset; scrolling here, before the browser
  // paints the commit, is as seamless.
  useLayoutEffect(() => {
    if (Platform.OS === 'web' && pin) {
      list.current?.scrollToOffset({ offset: pin.y, animated: false })
    }
  }, [pin])

  if (entries.length === 0 && !lead) {
    return (
      <View style={styles.empty}>
        <Text tone="muted">What should Droid work on?</Text>
      </View>
    )
  }

  return (
    <OnGrow.Provider value={grow}>
      <View style={styles.fill}>
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
                  turnEnd={item.last ? (ends.get(item.entry.id) ?? null) : null}
                />
              ) : (
                <UserMessage entry={item.entry} />
              )}
            </>
          )}
          // Swapped: an inverted list draws its header at the bottom, after the
          // latest row, and its footer at the top, before the oldest.
          ListHeaderComponent={
            <View
              role="status"
              aria-label="Session activity"
              style={styles.activity}
              // Sits at the far end of the header, so its position in it is
              // the room a short conversation leaves; 0 once it fills the screen.
              onLayout={(event) => {
                slack.current = event.nativeEvent.layout.y
              }}
            >
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
          // A conversation shorter than the screen starts at the top, as it
          // reads: the header, at the inverted list's bottom, takes up the rest.
          ListHeaderComponentStyle={styles.header}
          ListFooterComponent={<View style={styles.lead}>{lead}</View>}
          contentContainerStyle={styles.content}
          contentOffset={pin}
          onLayout={onLayout}
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
            style={[
              styles.down,
              { backgroundColor: colors.background, borderColor: colors.border },
            ]}
          >
            <ArrowDown size={18} color={colors.foreground} strokeWidth={1.75} />
          </Pressable>
        ) : null}
      </View>
    </OnGrow.Provider>
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
  content: { flexGrow: 1, paddingHorizontal: space.lg },
  header: { flexGrow: 1, justifyContent: 'flex-end' },
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

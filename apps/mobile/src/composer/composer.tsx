// The composer: text (kept per Session as a draft), Send while the Session
// rests, Queue and Cancel while a turn runs. Holding Queue hands the message
// to the running turn instead, like ⌘↩ in the web Client.
import { attachmentUrl, type ImageAttachment } from '@droi/daemon-layer/attachments'
import { loadDraft, saveDraft } from '@droi/daemon-layer/drafts'
import {
  filterSlashItems,
  pickedSlashItem,
  slashQuery,
  type SlashItem,
} from '@droi/daemon-layer/use-slash-items'
import type { QueuePlacement } from '@droi/daemon-layer/use-turn'
import { ArrowUp, ListPlus, Plus, Sparkles, Square, SquareSlash, X } from 'lucide-react-native'
import { useState, type ReactNode } from 'react'
import { Image, Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { ImageSourceError, pickImages, type ImageSource } from '../platform/images'
import { IconButton } from '../ui/primitives'
import { Text } from '../ui/primitives'
import { useTextScale } from '../ui/text-scale'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

const NO_ITEMS: SlashItem[] = []

/** The composer card's corner radius; the shelf above it sits inside the corners. */
export const COMPOSER_RADIUS = radius.xl + 4

export interface Submission {
  text: string
  images: ImageAttachment[]
  placement?: QueuePlacement
}

export function Composer({
  isRunning,
  disabled,
  onSend,
  onCancel,
  error,
  draftKey,
  placeholder = 'Ask anything',
  sendLabel = 'Send',
  allowEmpty = false,
  slashItems = NO_ITEMS,
  accessory,
}: {
  isRunning: boolean
  disabled: boolean
  onSend: (submission: Submission) => void
  onCancel: () => void
  error: string | null
  /** Keeps what is typed while the user is away from this Session. */
  draftKey?: string
  placeholder?: string
  sendLabel?: string
  /** Let Send fire with nothing typed (starting a Session without a first message). */
  allowEmpty?: boolean
  /** Commands and skills offered when the message starts with "/". */
  slashItems?: SlashItem[]
  /** Controls in the composer's bottom row, before the send button. */
  accessory?: ReactNode
}) {
  const colors = useColors()
  const scale = useTextScale()
  const [draft] = useState(() => (draftKey ? loadDraft(draftKey) : { text: '', images: [] }))
  const [text, setText] = useState(draft.text)
  const [images, setImages] = useState<ImageAttachment[]>(draft.images)
  const [caret, setCaret] = useState(draft.text.length)
  const [adding, setAdding] = useState(false)
  const [imageError, setImageError] = useState<string | null>(null)

  const update = (nextText: string, nextImages = images) => {
    setText(nextText)
    setImages(nextImages)
    if (draftKey) saveDraft(draftKey, { text: nextText, images: nextImages })
  }

  const addImages = async (source: ImageSource) => {
    setAdding(false)
    setImageError(null)
    try {
      const added = await pickImages(source)
      if (added.length > 0) update(text, [...images, ...added])
    } catch (cause) {
      setImageError(
        cause instanceof ImageSourceError ? cause.message : 'The image could not be read.',
      )
    }
  }

  // A picked command or skill shows as a tag; the input edits what follows it.
  const picked = pickedSlashItem(text, slashItems)
  const prefix = picked ? text.slice(0, text.length - picked.rest.length) : ''
  const query = slashQuery(text, caret)
  const suggestions = query === null ? NO_ITEMS : filterSlashItems(slashItems, query)
  const accept = (item: SlashItem) => {
    const next = `/${item.name} `
    update(next)
    setCaret(next.length)
  }

  const canSend = !disabled && (allowEmpty || text.trim() !== '' || images.length > 0)
  const submit = (placement?: QueuePlacement) => {
    if (!canSend) return
    onSend({ text, images, ...(placement ? { placement } : {}) })
    update('', [])
  }

  return (
    <View style={styles.wrap}>
      {error || imageError ? (
        <Text
          role="alert"
          size="xs"
          style={[styles.error, { color: colors.destructiveForeground }]}
        >
          {error ?? imageError}
        </Text>
      ) : null}
      {suggestions.length > 0 ? (
        <View
          role="menu"
          aria-label="Commands and skills"
          style={[
            styles.suggestions,
            { borderColor: colors.border, backgroundColor: colors.popover },
          ]}
        >
          {suggestions.map((item) => (
            <Pressable
              key={item.name}
              role="menuitem"
              onPress={() => accept(item)}
              style={({ pressed }) => [
                styles.option,
                pressed ? { backgroundColor: colors.accent } : null,
              ]}
            >
              <View style={styles.optionHead}>
                <Text size="sm" mono weight="medium">
                  /{item.name}
                </Text>
                {item.argumentHint ? (
                  <Text size="xs" tone="muted" mono numberOfLines={1} style={styles.shrink}>
                    {item.argumentHint}
                  </Text>
                ) : null}
                <Text size="xs" tone="muted" style={styles.kind}>
                  {item.kind === 'skill' ? 'Skill' : 'Command'}
                </Text>
              </View>
              {item.description ? (
                <Text size="xs" tone="muted" numberOfLines={1}>
                  {item.description}
                </Text>
              ) : null}
            </Pressable>
          ))}
        </View>
      ) : null}
      <View
        role="form"
        aria-label="Message composer"
        style={[styles.card, { borderColor: colors.border, backgroundColor: colors.background }]}
      >
        {images.length > 0 ? (
          <ScrollView
            horizontal
            role="list"
            aria-label="Attachments"
            contentContainerStyle={styles.attachments}
          >
            {images.map((image) => (
              <View key={image.id} role="listitem" style={styles.attachment}>
                <Image
                  source={{ uri: attachmentUrl(image) }}
                  accessibilityLabel={image.name}
                  alt={image.name}
                  style={[styles.thumb, { borderColor: colors.border }]}
                />
                <Pressable
                  role="button"
                  aria-label={`Remove ${image.name}`}
                  hitSlop={6}
                  onPress={() =>
                    update(
                      text,
                      images.filter((i) => i.id !== image.id),
                    )
                  }
                  style={[styles.removeImage, { backgroundColor: colors.foreground }]}
                >
                  <X size={10} color={colors.background} strokeWidth={3} />
                </Pressable>
              </View>
            ))}
          </ScrollView>
        ) : null}
        <View style={styles.inputRow}>
          {picked ? (
            <SlashTag
              item={picked.item}
              onRemove={() => {
                update(picked.rest)
                setCaret(picked.rest.length)
              }}
            />
          ) : null}
          <TextInput
            aria-label="Message"
            placeholder={isRunning ? 'Queue a message' : (picked?.item.argumentHint ?? placeholder)}
            placeholderTextColor={colors.mutedForeground}
            value={text.slice(prefix.length)}
            onChangeText={(next) => {
              update(prefix + next)
              // Typing leaves the caret at the end; a selection event corrects
              // it when the edit was elsewhere (web builds send none on input).
              setCaret(prefix.length + next.length)
            }}
            onSelectionChange={(event) => setCaret(prefix.length + event.nativeEvent.selection.end)}
            onKeyPress={(event) => {
              // Backspace at the very start takes the tag away.
              if (picked && event.nativeEvent.key === 'Backspace' && caret === prefix.length) {
                update(picked.rest)
                setCaret(0)
              }
            }}
            multiline
            editable={!disabled}
            style={[
              styles.input,
              picked ? styles.inputAfterTag : null,
              {
                color: colors.foreground,
                fontSize: styles.input.fontSize * scale,
                lineHeight: styles.input.lineHeight * scale,
              },
            ]}
          />
        </View>
        {adding ? (
          <View
            role="menu"
            aria-label="Add image"
            style={[styles.menu, { borderTopColor: colors.border }]}
          >
            {(
              [
                ['library', 'Photo library'],
                ['camera', 'Take photo'],
                ['clipboard', 'Paste image'],
              ] as const
            ).map(([source, label]) => (
              <Pressable
                key={source}
                role="menuitem"
                onPress={() => void addImages(source)}
                style={({ pressed }) => [
                  styles.menuItem,
                  pressed ? { backgroundColor: colors.accent } : null,
                ]}
              >
                <Text size="sm">{label}</Text>
              </Pressable>
            ))}
          </View>
        ) : null}
        <View style={styles.bar}>
          <IconButton
            label="Add image"
            icon={Plus}
            expanded={adding}
            onPress={() => setAdding(!adding)}
          />
          {accessory}
          <View style={styles.spacer} />
          {isRunning ? (
            <>
              <Pressable
                role="button"
                aria-label="Cancel"
                onPress={onCancel}
                style={[styles.round, { backgroundColor: colors.secondary }]}
              >
                <Square size={12} color={colors.foreground} fill={colors.foreground} />
              </Pressable>
              <Pressable
                role="button"
                aria-label="Queue"
                accessibilityHint="Hold to hand the message to the running turn instead"
                disabled={!canSend}
                onPress={() => submit('end_of_loop')}
                onLongPress={() => submit('end_of_turn')}
                style={[
                  styles.round,
                  { backgroundColor: colors.primary, opacity: canSend ? 1 : 0.4 },
                ]}
              >
                <ListPlus size={16} color={colors.primaryForeground} strokeWidth={2} />
              </Pressable>
            </>
          ) : (
            <Pressable
              role="button"
              aria-label={sendLabel}
              aria-disabled={!canSend}
              disabled={!canSend}
              onPress={() => submit()}
              style={[
                styles.round,
                { backgroundColor: colors.primary, opacity: canSend ? 1 : 0.4 },
              ]}
            >
              <ArrowUp size={16} color={colors.primaryForeground} strokeWidth={2.25} />
            </Pressable>
          )}
        </View>
      </View>
    </View>
  )
}

/** The command or skill the message will run, so a pick is not mistaken for plain text. */
function SlashTag({ item, onRemove }: { item: SlashItem; onRemove: () => void }) {
  const colors = useColors()
  const kind = item.kind === 'skill' ? 'Skill' : 'Command'
  const Icon = item.kind === 'skill' ? Sparkles : SquareSlash
  return (
    <View
      role="group"
      aria-label={`${kind} ${item.name}`}
      style={[styles.tag, { backgroundColor: `${colors.working}1a` }]}
    >
      <Icon size={13} color={colors.working} strokeWidth={2} />
      <Text
        size="sm"
        weight="medium"
        numberOfLines={1}
        style={[styles.shrink, { color: colors.working }]}
      >
        {item.name}
      </Text>
      <Pressable
        role="button"
        aria-label={`Remove ${kind.toLowerCase()} ${item.name}`}
        hitSlop={8}
        onPress={onRemove}
        style={styles.tagRemove}
      >
        <X size={12} color={colors.working} strokeWidth={2.25} />
      </Pressable>
    </View>
  )
}

const styles = StyleSheet.create({
  wrap: { gap: space.xs, paddingHorizontal: space.md },
  error: { paddingHorizontal: space.xs },
  card: {
    // A full point, like the web Client's 1px border; a hairline is a third
    // of that on an iPhone and all but disappears.
    borderWidth: 1,
    borderRadius: COMPOSER_RADIUS,
    paddingTop: space.sm,
  },
  inputRow: { flexDirection: 'row', alignItems: 'flex-start' },
  tag: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    height: 26,
    maxWidth: '50%',
    marginLeft: space.md,
    marginTop: 2,
    paddingLeft: 6,
    borderRadius: radius.md,
  },
  tagRemove: { paddingHorizontal: 5, height: 26, justifyContent: 'center' },
  inputAfterTag: { paddingLeft: space.sm },
  input: {
    flex: 1,
    minHeight: 40,
    maxHeight: 160,
    paddingHorizontal: space.md,
    paddingVertical: space.xs,
    fontFamily: fonts.sans,
    fontSize: fontSize.base,
    lineHeight: 21,
  },
  bar: { flexDirection: 'row', alignItems: 'center', gap: space.sm, padding: space.sm },
  spacer: { flex: 1 },
  suggestions: {
    borderWidth: 1,
    borderRadius: radius.lg,
    paddingVertical: space.xs,
  },
  option: { paddingHorizontal: space.md, paddingVertical: 6, gap: 2 },
  optionHead: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  shrink: { flexShrink: 1 },
  kind: { marginLeft: 'auto' },
  attachments: { gap: space.sm, paddingHorizontal: space.md, paddingBottom: space.xs },
  // The scroller clips its content, so the remove button's overhang is room
  // inside the chip rather than a negative offset.
  attachment: { position: 'relative', paddingTop: 6, paddingRight: 6 },
  thumb: { width: 56, height: 56, borderRadius: radius.md, borderWidth: 1 },
  removeImage: {
    position: 'absolute',
    top: 0,
    right: 0,
    width: 18,
    height: 18,
    borderRadius: 9,
    alignItems: 'center',
    justifyContent: 'center',
  },
  menu: {
    borderTopWidth: StyleSheet.hairlineWidth,
    marginTop: space.xs,
    paddingVertical: space.xs,
  },
  menuItem: { paddingHorizontal: space.md, minHeight: 36, justifyContent: 'center' },
  round: {
    width: 32,
    height: 32,
    borderRadius: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
})

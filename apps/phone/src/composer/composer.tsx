// The composer: text (kept per Session as a draft), Send while the Session
// rests, Queue and Cancel while a turn runs. Holding Queue hands the message
// to the running turn instead, like ⌘↩ in the web Client.
import { attachmentUrl, type ImageAttachment } from '@droi/daemon-layer/attachments'
import { loadDraft, saveDraft } from '@droi/daemon-layer/drafts'
import { filterSlashItems, slashQuery, type SlashItem } from '@droi/daemon-layer/use-slash-items'
import type { QueuePlacement } from '@droi/daemon-layer/use-turn'
import { ArrowUp, ListPlus, Plus, Square, X } from 'lucide-react-native'
import { useState, type ReactNode } from 'react'
import { Image, Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { ImageSourceError, pickImages, type ImageSource } from '../platform/images'
import { IconButton } from '../ui/primitives'
import { Text } from '../ui/primitives'
import { useTextScale } from '../ui/text-scale'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

const NO_ITEMS: SlashItem[] = []

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
        <TextInput
          aria-label="Message"
          placeholder={isRunning ? 'Queue a message' : placeholder}
          placeholderTextColor={colors.mutedForeground}
          value={text}
          onChangeText={(next) => {
            update(next)
            // Typing leaves the caret at the end; a selection event corrects
            // it when the edit was elsewhere (web builds send none on input).
            setCaret(next.length)
          }}
          onSelectionChange={(event) => setCaret(event.nativeEvent.selection.end)}
          multiline
          editable={!disabled}
          style={[
            styles.input,
            {
              color: colors.foreground,
              fontSize: styles.input.fontSize * scale,
              lineHeight: styles.input.lineHeight * scale,
            },
          ]}
        />
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

const styles = StyleSheet.create({
  wrap: { gap: space.xs, paddingHorizontal: space.md },
  error: { paddingHorizontal: space.xs },
  card: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.xl + 4,
    paddingTop: space.sm,
  },
  input: {
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
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.lg,
    paddingVertical: space.xs,
  },
  option: { paddingHorizontal: space.md, paddingVertical: 6, gap: 2 },
  optionHead: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  shrink: { flexShrink: 1 },
  kind: { marginLeft: 'auto' },
  attachments: { gap: space.sm, paddingHorizontal: space.md, paddingBottom: space.xs },
  attachment: { position: 'relative' },
  thumb: { width: 56, height: 56, borderRadius: radius.md, borderWidth: StyleSheet.hairlineWidth },
  removeImage: {
    position: 'absolute',
    top: -4,
    right: -4,
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

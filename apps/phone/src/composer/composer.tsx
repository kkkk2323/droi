// The composer: text (kept per Session as a draft), Send while the Session
// rests, Queue and Cancel while a turn runs. Holding Queue hands the message
// to the running turn instead, like ⌘↩ in the web Client.
import type { ImageAttachment } from '@droi/daemon-layer/attachments'
import { loadDraft, saveDraft } from '@droi/daemon-layer/drafts'
import type { QueuePlacement } from '@droi/daemon-layer/use-turn'
import { ArrowUp, ListPlus, Square } from 'lucide-react-native'
import { useState, type ReactNode } from 'react'
import { Pressable, StyleSheet, TextInput, View } from 'react-native'
import { Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

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
  /** Controls in the composer's bottom row, before the send button. */
  accessory?: ReactNode
}) {
  const colors = useColors()
  const [draft] = useState(() => (draftKey ? loadDraft(draftKey) : { text: '', images: [] }))
  const [text, setText] = useState(draft.text)
  const [images, setImages] = useState<ImageAttachment[]>(draft.images)

  const update = (nextText: string, nextImages = images) => {
    setText(nextText)
    setImages(nextImages)
    if (draftKey) saveDraft(draftKey, { text: nextText, images: nextImages })
  }

  const canSend = !disabled && (allowEmpty || text.trim() !== '' || images.length > 0)
  const submit = (placement?: QueuePlacement) => {
    if (!canSend) return
    onSend({ text, images, ...(placement ? { placement } : {}) })
    update('', [])
  }

  return (
    <View style={styles.wrap}>
      {error ? (
        <Text
          role="alert"
          size="xs"
          style={[styles.error, { color: colors.destructiveForeground }]}
        >
          {error}
        </Text>
      ) : null}
      <View
        role="form"
        aria-label="Message composer"
        style={[styles.card, { borderColor: colors.border, backgroundColor: colors.background }]}
      >
        <TextInput
          aria-label="Message"
          placeholder={isRunning ? 'Queue a message' : placeholder}
          placeholderTextColor={colors.mutedForeground}
          value={text}
          onChangeText={(next) => update(next)}
          multiline
          editable={!disabled}
          style={[styles.input, { color: colors.foreground }]}
        />
        <View style={styles.bar}>
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
  round: {
    width: 32,
    height: 32,
    borderRadius: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
})

// A live OMP engine session opened through the computer's Gateway HTTP API.
import {
  abortOmpSession,
  getOmpSession,
  promptOmpSession,
  type OmpMessage,
  type OmpSession,
} from '@droi/daemon-layer/omp'
import { useDaemonConnection } from '@droi/daemon-layer/connection-context'
import { Stack } from 'expo-router'
import { useCallback, useEffect, useState } from 'react'
import {
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { Spinner } from '../ui/activity'
import { Button, Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function OmpSessionScreen({ sessionId }: { sessionId: string }) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const { gateway } = useDaemonConnection()
  const [session, setSession] = useState<OmpSession | null>(null)
  const [messages, setMessages] = useState<OmpMessage[]>([])
  const [text, setText] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    if (!gateway.token) {
      setError('Missing pairing token')
      setLoading(false)
      return
    }
    try {
      const got = await getOmpSession(gateway.url, gateway.token, sessionId)
      setSession(got.session)
      setMessages(got.messages)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [gateway, sessionId])

  useEffect(() => {
    void refresh()
    const t = setInterval(() => void refresh(), 1500)
    return () => clearInterval(t)
  }, [refresh])

  const send = async () => {
    const message = text.trim()
    if (!message || !gateway.token || busy) return
    setBusy(true)
    setText('')
    try {
      await promptOmpSession(gateway.url, gateway.token, sessionId, message)
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const stop = async () => {
    if (!gateway.token) return
    try {
      await abortOmpSession(gateway.url, gateway.token, sessionId)
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <KeyboardAvoidingView
      style={[styles.fill, { backgroundColor: colors.background }]}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      keyboardVerticalOffset={88}
    >
      <Stack.Screen
        options={{
          title: session?.title || 'OMP session',
          headerRight: () =>
            session?.working ? (
              <Pressable onPress={() => void stop()} accessibilityRole="button">
                <Text size="sm" tone="muted">
                  Stop
                </Text>
              </Pressable>
            ) : null,
        }}
      />
      {loading ? (
        <View style={styles.center}>
          <Spinner size={16} color={colors.mutedForeground} />
        </View>
      ) : (
        <ScrollView
          style={styles.fill}
          contentContainerStyle={{ padding: space.md, paddingBottom: space.lg, gap: space.md }}
        >
          <View style={[styles.badge, { backgroundColor: colors.secondary }]}>
            <Text size="xs" weight="semibold">
              OMP
            </Text>
            {session?.model ? (
              <Text size="xs" tone="muted">
                {session.model}
              </Text>
            ) : null}
          </View>
          {messages.length === 0 ? (
            <Text tone="muted" size="sm">
              Send a message to start.
            </Text>
          ) : null}
          {messages.map((m) => (
            <MessageBubble key={m.id} message={m} />
          ))}
          {error ? (
            <Text size="sm" style={{ color: colors.destructiveForeground }}>
              {error}
            </Text>
          ) : null}
        </ScrollView>
      )}
      <View
        style={[
          styles.composer,
          {
            borderTopColor: colors.border,
            backgroundColor: colors.card,
            paddingBottom: Math.max(insets.bottom, space.sm),
          },
        ]}
      >
        <TextInput
          value={text}
          onChangeText={setText}
          placeholder="Message OMP…"
          placeholderTextColor={colors.mutedForeground}
          multiline
          style={[
            styles.input,
            {
              color: colors.foreground,
              borderColor: colors.input,
              backgroundColor: colors.background,
              fontFamily: fonts.sans,
            },
          ]}
        />
        <Button
          label="Send"
          onPress={() => void send()}
          disabled={!text.trim() || busy}
          variant="primary"
        />
      </View>
    </KeyboardAvoidingView>
  )
}

function MessageBubble({ message }: { message: OmpMessage }) {
  const colors = useColors()
  if (message.role === 'user') {
    return (
      <View style={[styles.user, { backgroundColor: colors.primary + '14' }]}>
        <Text size="sm">{message.text}</Text>
      </View>
    )
  }
  if (message.role === 'tool') {
    return (
      <View style={[styles.tool, { borderColor: colors.border, backgroundColor: colors.card }]}>
        <Text size="sm" weight="semibold">
          {message.toolName || 'tool'}
        </Text>
        {message.toolArgs ? (
          <Text size="xs" tone="muted" style={{ fontFamily: fonts.mono }}>
            {message.toolArgs}
          </Text>
        ) : null}
        {message.toolOut ? (
          <Text size="xs" style={{ fontFamily: fonts.mono, color: colors.foreground }}>
            {message.toolOut}
          </Text>
        ) : null}
      </View>
    )
  }
  return (
    <View style={styles.assistant}>
      {message.thinking ? (
        <Text size="xs" tone="muted" style={{ marginBottom: 4 }}>
          {message.thinking}
        </Text>
      ) : null}
      <Text size="sm" style={message.isError ? { color: colors.destructiveForeground } : undefined}>
        {message.text}
      </Text>
    </View>
  )
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center' },
  badge: {
    alignSelf: 'flex-start',
    flexDirection: 'row',
    gap: space.sm,
    paddingHorizontal: space.sm,
    paddingVertical: 4,
    borderRadius: radius.md,
  },
  user: {
    alignSelf: 'flex-end',
    maxWidth: '85%',
    padding: space.sm,
    borderRadius: radius.lg,
  },
  assistant: { alignSelf: 'stretch', gap: 4 },
  tool: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.md,
    padding: space.sm,
    gap: 4,
  },
  composer: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: space.md,
    paddingTop: space.sm,
    gap: space.sm,
  },
  input: {
    minHeight: 44,
    maxHeight: 120,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    fontSize: fontSize.sm,
  },
})

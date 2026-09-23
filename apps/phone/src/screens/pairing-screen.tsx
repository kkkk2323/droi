// First launch, or a computer to add: pair the Phone App with Droi on a
// computer by pasting the pairing link its Settings show.
import { useRouter } from 'expo-router'
import { useState } from 'react'
import { ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { PairingError, resolvePairing } from '../computers/pairing'
import { savePairing } from '../computers/store'
import { PAIRING_TOKEN_QUERY } from '../connection/computer-connection'
import { keychain } from '../platform/keychain'
import { appQueryClient } from '../query-client'
import { Button, Heading, Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function PairingScreen() {
  const colors = useColors()
  const router = useRouter()
  const insets = useSafeAreaInsets()
  const [link, setLink] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const pair = async () => {
    setBusy(true)
    setError(null)
    try {
      const result = await resolvePairing(link)
      await savePairing(result, keychain)
      await appQueryClient.invalidateQueries({ queryKey: [PAIRING_TOKEN_QUERY, result.id] })
    } catch (cause) {
      setError(cause instanceof PairingError ? cause.message : String(cause))
      setBusy(false)
    }
  }

  return (
    <ScrollView
      style={{ backgroundColor: colors.background }}
      contentContainerStyle={[
        styles.content,
        { paddingTop: insets.top + space.xl, paddingBottom: insets.bottom + space.xl },
      ]}
      keyboardShouldPersistTaps="handled"
    >
      <Heading>Pair with a computer</Heading>
      <Text tone="muted">
        On your computer, open Droi → Settings → Remote Access, turn it on and copy the pairing link
        shown there.
      </Text>
      <TextInput
        aria-label="Pairing link"
        placeholder="http://192.168.1.10:41417/#pair=…"
        placeholderTextColor={colors.mutedForeground}
        value={link}
        onChangeText={setLink}
        autoCapitalize="none"
        autoCorrect={false}
        keyboardType="url"
        returnKeyType="go"
        onSubmitEditing={() => void pair()}
        style={[
          styles.input,
          {
            borderColor: colors.input,
            color: colors.foreground,
            backgroundColor: colors.background,
          },
        ]}
      />
      {error ? (
        <Text role="alert" size="sm" style={{ color: colors.destructiveForeground }}>
          {error}
        </Text>
      ) : null}
      <Button label="Pair" busy={busy} disabled={!link.trim()} onPress={() => void pair()} />
      <Text tone="muted" size="sm">
        To reach the computer away from home too, use its Tailscale name in the link (for example
        http://my-mac.tailnet.ts.net:41417); it works on the home network as well.
      </Text>
      <View style={styles.footer}>
        <Button label="Settings" variant="ghost" onPress={() => router.push('/settings')} />
      </View>
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: space.xl, gap: space.md },
  input: {
    borderWidth: 1,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    height: 44,
    fontFamily: fonts.mono,
    fontSize: fontSize.sm,
  },
  footer: { flexDirection: 'row', justifyContent: 'flex-end', marginTop: space.lg },
})

// Pair the Phone App with Droi on a computer: scan the QR code its Settings
// show, or paste the pairing link. On first launch this is the whole app.
import { useRouter } from 'expo-router'
import { useState } from 'react'
import { ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { usePairing } from '../computers/use-pairing'
import { backToMain } from '../navigation'
import { Button, Heading, Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function PairingScreen({ firstLaunch }: { firstLaunch: boolean }) {
  const colors = useColors()
  const router = useRouter()
  const insets = useSafeAreaInsets()
  const [link, setLink] = useState('')
  const { pair, busy, error } = usePairing(() => backToMain(router))

  return (
    <ScrollView
      style={{ backgroundColor: colors.background }}
      contentContainerStyle={[
        styles.content,
        {
          paddingTop: (firstLaunch ? insets.top : 0) + space.xl,
          paddingBottom: insets.bottom + space.xl,
        },
      ]}
      keyboardShouldPersistTaps="handled"
    >
      <Heading>Pair with a computer</Heading>
      <Text tone="muted">
        On your computer, open Droi → Settings → Remote Access and turn it on. Then scan the QR code
        shown there, or paste its pairing link.
      </Text>
      <Button label="Scan QR code" onPress={() => router.push('/scan')} />
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
        onSubmitEditing={() => void pair(link)}
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
      <Button
        label="Pair"
        variant="secondary"
        busy={busy}
        disabled={!link.trim()}
        onPress={() => void pair(link)}
      />
      <Text tone="muted" size="sm">
        To reach the computer away from home too, change the address to its Tailscale name (for
        example my-mac.tailnet.ts.net) after pairing; it works on the home network as well.
      </Text>
      {firstLaunch ? (
        <View style={styles.footer}>
          <Button label="Settings" variant="ghost" onPress={() => router.push('/settings')} />
        </View>
      ) : null}
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

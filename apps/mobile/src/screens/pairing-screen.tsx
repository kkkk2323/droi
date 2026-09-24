// Pair the Phone App with Droi on a computer: scan the QR code its Settings
// show, or paste the pairing link. On first launch this is the whole app.
import { useRouter } from 'expo-router'
import { ScrollView, StyleSheet, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { PairingForm } from '../computers/pairing-form'
import { backToMain } from '../navigation'
import { Button, Heading, Text } from '../ui/primitives'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function PairingScreen({ firstLaunch }: { firstLaunch: boolean }) {
  const colors = useColors()
  const router = useRouter()
  const insets = useSafeAreaInsets()

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
      <PairingForm onPaired={() => backToMain(router)} />
      <Text tone="muted" size="sm">
        To reach the computer away from home too, use a name that reaches it from anywhere, such as
        a Tailscale or Surge Ponte name: set it as the pairing address in Droi before scanning, or
        change the computer’s address here after pairing.
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
  footer: { flexDirection: 'row', justifyContent: 'flex-end', marginTop: space.lg },
})

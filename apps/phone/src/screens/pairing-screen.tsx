// First launch, or a computer to add: how to pair the Phone App with Droi on
// a computer.
import { useRouter } from 'expo-router'
import { ScrollView, StyleSheet, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { Button, Heading, Text } from '../ui/primitives'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function PairingScreen() {
  const colors = useColors()
  const router = useRouter()
  const insets = useSafeAreaInsets()
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
        On your computer, open Droi → Settings → Remote Access, turn it on and use the pairing link
        or QR code shown there.
      </Text>
      <View style={styles.footer}>
        <Button label="Settings" variant="ghost" onPress={() => router.push('/settings')} />
      </View>
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: space.xl, gap: space.md },
  footer: { flexDirection: 'row', justifyContent: 'flex-end', marginTop: space.lg },
})

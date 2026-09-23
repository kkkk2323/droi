// Scan the QR code or paste the pairing link: used to pair a new computer
// and to pair a known one again after its Pairing Token was reset.
import { useRouter } from 'expo-router'
import { useState } from 'react'
import { StyleSheet, TextInput, View } from 'react-native'
import { Button, Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { usePairing } from './use-pairing'

export function PairingForm({ onPaired }: { onPaired: () => void }) {
  const colors = useColors()
  const router = useRouter()
  const [link, setLink] = useState('')
  const { pair, busy, error } = usePairing(onPaired)
  return (
    <View style={styles.form}>
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
    </View>
  )
}

const styles = StyleSheet.create({
  form: { gap: space.md },
  input: {
    borderWidth: 1,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    height: 44,
    fontFamily: fonts.mono,
    fontSize: fontSize.sm,
  },
})

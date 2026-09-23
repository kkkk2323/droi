// Scanning the pairing QR code. A code that does not pair says why and the
// camera keeps looking.
import { useRouter } from 'expo-router'
import { StyleSheet, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { usePairing } from '../computers/use-pairing'
import { backToMain } from '../navigation'
import { QrScanner } from '../platform/qr-scanner'
import { Text } from '../ui/primitives'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function ScanScreen() {
  const colors = useColors()
  const router = useRouter()
  const insets = useSafeAreaInsets()
  const { pair, busy, error } = usePairing(() => backToMain(router))

  return (
    <View style={[styles.fill, { backgroundColor: colors.background }]}>
      <QrScanner paused={busy} onScanned={(text) => void pair(text)} />
      <View style={[styles.footer, { paddingBottom: insets.bottom + space.lg }]}>
        {error ? (
          <Text role="alert" size="sm" style={{ color: colors.destructiveForeground }}>
            {error}
          </Text>
        ) : (
          <Text tone="muted" size="sm">
            {busy ? 'Pairing…' : 'Point the camera at the QR code in Droi → Settings.'}
          </Text>
        )}
      </View>
    </View>
  )
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  footer: { padding: space.lg },
})

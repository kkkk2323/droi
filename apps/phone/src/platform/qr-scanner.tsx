// The camera, looking for the pairing QR code the Desktop Shell shows.
import { CameraView, useCameraPermissions } from 'expo-camera'
import { Linking, StyleSheet, View } from 'react-native'
import { Button, Text } from '../ui/primitives'
import { space } from '../ui/theme'

export function QrScanner({
  onScanned,
  paused,
}: {
  onScanned: (text: string) => void
  paused: boolean
}) {
  const [permission, requestPermission] = useCameraPermissions()
  if (!permission) return null
  if (!permission.granted) {
    return (
      <View style={styles.ask}>
        <Text tone="muted">Droi needs the camera to read the pairing code on your computer.</Text>
        {permission.canAskAgain ? (
          <Button label="Allow camera" onPress={() => void requestPermission()} />
        ) : (
          <Button label="Open Settings" onPress={() => void Linking.openSettings()} />
        )}
      </View>
    )
  }
  return (
    <CameraView
      style={styles.camera}
      facing="back"
      barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
      onBarcodeScanned={paused ? undefined : ({ data }) => onScanned(data)}
    />
  )
}

const styles = StyleSheet.create({
  camera: { flex: 1 },
  ask: { flex: 1, justifyContent: 'center', padding: space.xl, gap: space.md },
})

// Web stand-in for the camera: the tests "scan" a code by calling
// window.droiStandIns.scan(text).
import { useEffect, useRef } from 'react'
import { StyleSheet, View } from 'react-native'
import { Text } from '../ui/primitives'
import { standIns } from './stand-ins'

export function QrScanner({
  onScanned,
  paused,
}: {
  onScanned: (text: string) => void
  paused: boolean
}) {
  const latest = useRef({ onScanned, paused })
  useEffect(() => {
    latest.current = { onScanned, paused }
  })
  useEffect(() => {
    const scope = standIns()
    scope.scan = (text) => {
      if (!latest.current.paused) latest.current.onScanned(text)
    }
    return () => {
      scope.scan = null
    }
  }, [])
  return (
    <View style={styles.fill}>
      <Text tone="muted">Camera stand-in</Text>
    </View>
  )
}

const styles = StyleSheet.create({
  fill: { flex: 1, alignItems: 'center', justifyContent: 'center' },
})

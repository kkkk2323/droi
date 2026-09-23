// Settings: for now the app's version and how long its signature has left.
import { useQuery } from '@tanstack/react-query'
import Constants from 'expo-constants'
import { ScrollView, StyleSheet, View } from 'react-native'
import { daysLeft } from '../lib/signature'
import { signatureExpiry } from '../platform/signature'
import { Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SettingsScreen() {
  const colors = useColors()
  const expiry = useQuery({ queryKey: ['signature-expiry'], queryFn: signatureExpiry })
  const version = Constants.expoConfig?.version ?? 'unknown'

  let signature = '…'
  if (expiry.isSuccess) {
    if (!expiry.data) {
      signature = 'Not signed for a device'
    } else {
      const days = daysLeft(expiry.data, new Date())
      signature = days === 0 ? 'Expired' : days === 1 ? '1 day left' : `${days} days left`
    }
  }

  return (
    <ScrollView
      style={{ backgroundColor: colors.background }}
      contentContainerStyle={styles.content}
    >
      <Text tone="muted" size="sm" weight="medium" accessibilityRole="header">
        About
      </Text>
      <View style={[styles.group, { backgroundColor: colors.card }]}>
        <Row label="Version" value={version} />
        <View style={[styles.divider, { backgroundColor: colors.border }]} />
        <Row label="Signature" value={signature} />
      </View>
      <Text tone="muted" size="sm">
        The app is signed with a free Apple account for seven days. Run pnpm install:phone on the
        computer with the iPhone connected to sign and install it again.
      </Text>
    </ScrollView>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <View style={styles.row} accessible accessibilityLabel={`${label}: ${value}`}>
      <Text>{label}</Text>
      <Text tone="muted">{value}</Text>
    </View>
  )
}

const styles = StyleSheet.create({
  content: { padding: space.lg, gap: space.sm },
  group: { borderRadius: radius.lg, overflow: 'hidden' },
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: space.lg,
    minHeight: 44,
  },
  divider: { height: StyleSheet.hairlineWidth, marginLeft: space.lg },
})

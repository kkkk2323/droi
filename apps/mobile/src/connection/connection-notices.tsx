// What the main screen shows when the connection is not simply up: the
// computer is no longer paired, it cannot be reached, it is reconnecting, or
// it runs another Droi version than the one this app was built from.
import { useConnectionState } from '@droi/daemon-layer/connection-context'
import { GATEWAY_META_PATH, type GatewayMeta } from '@droi/daemon-layer/gateway'
import { createStringPreference, usePreference } from '@droi/daemon-layer/local-preference'
import { useQuery } from '@tanstack/react-query'
import Constants from 'expo-constants'
import { X } from 'lucide-react-native'
import type { ReactNode } from 'react'
import { ScrollView, StyleSheet, View } from 'react-native'
import { PulsingDot, Spinner } from '../ui/activity'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { PairingForm } from '../computers/pairing-form'
import type { PairedComputer } from '../computers/store'
import { versionMismatch } from '../lib/version'
import { Heading, IconButton, Text } from '../ui/primitives'
import { ScreenHeader } from '../ui/screen-header'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

const HEADER_HEIGHT = 44

export function ConnectionGate({
  computer,
  drawerOpen,
  onOpenDrawer,
  children,
}: {
  computer: PairedComputer
  drawerOpen: boolean
  onOpenDrawer: () => void
  children: ReactNode
}) {
  const state = useConnectionState()
  if (state.status === 'unpaired' || state.status === 'unreachable') {
    return (
      <View style={styles.fill}>
        <ScreenHeader title={computer.name} drawerOpen={drawerOpen} onOpenDrawer={onOpenDrawer} />
        {state.status === 'unpaired' ? (
          <NoLongerPaired computer={computer} />
        ) : (
          <Unreachable computer={computer} />
        )}
      </View>
    )
  }
  return (
    <View style={styles.fill}>
      {children}
      <Notices computer={computer} reconnecting={state.status === 'reconnecting'} />
    </View>
  )
}

function NoLongerPaired({ computer }: { computer: PairedComputer }) {
  return (
    <ScrollView contentContainerStyle={styles.panel} keyboardShouldPersistTaps="handled">
      <Heading>{computer.name} is no longer paired</Heading>
      <Text tone="muted">
        Its Pairing Token was reset in Droi on the computer. Pair again from Droi → Settings →
        Remote Access; the phone keeps this computer and its name.
      </Text>
      <PairingForm onPaired={() => {}} />
    </ScrollView>
  )
}

function Unreachable({ computer }: { computer: PairedComputer }) {
  const colors = useColors()
  return (
    <ScrollView contentContainerStyle={styles.panel}>
      <Heading>Can’t reach {computer.name}</Heading>
      <Text tone="muted">The phone is trying</Text>
      <Text mono size="sm" style={[styles.address, { backgroundColor: colors.muted }]}>
        {computer.address}
      </Text>
      <View role="list" aria-label="What to check" style={styles.checks}>
        <Check>
          Remote Access is on in Droi → Settings on the computer. With it off the computer does not
          listen on the network at all.
        </Check>
        <Check>
          The iPhone is on the same network as the computer, or Tailscale is on for both.
        </Check>
        <Check>The computer is awake and Droi is running.</Check>
      </View>
      <View style={styles.retrying}>
        <Spinner size={14} color={colors.mutedForeground} />
        <Text tone="muted" size="sm">
          Trying again…
        </Text>
      </View>
    </ScrollView>
  )
}

function Check({ children }: { children: ReactNode }) {
  return (
    <View role="listitem" style={styles.check}>
      <Text tone="muted">•</Text>
      <Text style={styles.fill}>{children}</Text>
    </View>
  )
}

const dismissedVersionNotice = createStringPreference('droi.versionNoticeDismissed')

function Notices({ computer, reconnecting }: { computer: PairedComputer; reconnecting: boolean }) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const appVersion = Constants.expoConfig?.version ?? ''
  const meta = useQuery({
    queryKey: ['meta', computer.address],
    queryFn: async (): Promise<GatewayMeta> => {
      const response = await fetch(new URL(GATEWAY_META_PATH, computer.address).toString())
      return (await response.json()) as GatewayMeta
    },
    staleTime: Infinity,
  })
  const [dismissed, setDismissed] = usePreference(dismissedVersionNotice)
  const computerVersion = meta.data?.version ?? ''
  const noticeKey = `${computer.id} ${appVersion} ${computerVersion}`
  const showVersion = versionMismatch(appVersion, computerVersion) && dismissed !== noticeKey

  if (!reconnecting && !showVersion) return null
  return (
    <View pointerEvents="box-none" style={[styles.notices, { top: insets.top + HEADER_HEIGHT }]}>
      {reconnecting ? (
        <View
          role="status"
          aria-label="Reconnecting"
          style={[styles.banner, { backgroundColor: colors.popover, borderColor: colors.border }]}
        >
          <PulsingDot color={colors.attention} />
          <Text size="sm">Reconnecting…</Text>
        </View>
      ) : null}
      {showVersion ? (
        <View
          role="alert"
          style={[styles.banner, { backgroundColor: colors.popover, borderColor: colors.border }]}
        >
          <Text size="sm" style={styles.fill}>
            {computer.name} runs Droi {computerVersion}; this app is {appVersion}. Run pnpm
            install:phone to match.
          </Text>
          <IconButton label="Dismiss" icon={X} onPress={() => setDismissed(noticeKey)} />
        </View>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  panel: { padding: space.xl, gap: space.md },
  address: {
    alignSelf: 'flex-start',
    paddingHorizontal: space.sm,
    paddingVertical: space.xs,
    borderRadius: radius.sm,
  },
  checks: { gap: space.sm, marginTop: space.sm },
  check: { flexDirection: 'row', gap: space.sm },
  retrying: { flexDirection: 'row', alignItems: 'center', gap: space.sm, marginTop: space.md },
  notices: { position: 'absolute', left: space.md, right: space.md, gap: space.sm },
  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    borderRadius: radius.lg,
    borderWidth: StyleSheet.hairlineWidth,
  },
})

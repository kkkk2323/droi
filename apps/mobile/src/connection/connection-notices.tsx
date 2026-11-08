// What a page of the connected computer shows when the connection is not simply up: the
// computer is no longer paired, it cannot be reached, its Droi runs no Kernel, it is
// reconnecting, or it runs another Droi version than the one this app was built from.
import { useConnectionState, useKernelConnection } from '@droi/daemon-layer/connection-context'
import { GATEWAY_META_PATH, gatewayEndpoint, type GatewayMeta } from '@droi/daemon-layer/gateway'
import { createStringPreference, usePreference } from '@droi/daemon-layer/local-preference'
import { useQuery } from '@tanstack/react-query'
import Constants from 'expo-constants'
import { X } from 'lucide-react-native'
import type { ReactNode } from 'react'
import { ScrollView, StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native'
import { PulsingDot, Spinner } from '../ui/activity'
import { PairingForm } from '../computers/pairing-form'
import { followRelay, type PairedComputer } from '../computers/store'
import { versionMismatch } from '../lib/version'
import { Heading, IconButton, Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function ConnectionGate({
  computer,
  keepWhenUnreachable = false,
  children,
}: {
  computer: PairedComputer
  /** Shows what is known below the notice while the computer cannot be reached (the Session list). */
  keepWhenUnreachable?: boolean
  children: ReactNode
}) {
  const state = useConnectionState()
  if (state.status === 'unreachable' && keepWhenUnreachable) {
    return (
      <View style={styles.fill}>
        <Unreachable computer={computer} style={styles.top} />
        {children}
      </View>
    )
  }
  if (state.status === 'unpaired' || state.status === 'unreachable') {
    return (
      <View style={styles.fill}>
        {state.status === 'unpaired' ? (
          <NoLongerPaired computer={computer} />
        ) : (
          <Unreachable computer={computer} />
        )}
      </View>
    )
  }
  if (state.status === 'no-kernel') {
    return (
      <View style={styles.fill}>
        <NoKernel computer={computer} />
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

function NoKernel({ computer }: { computer: PairedComputer }) {
  const colors = useColors()
  return (
    <ScrollView contentContainerStyle={styles.panel}>
      <Heading>{computer.name} runs Droi without its Kernel</Heading>
      <Text tone="muted">
        This app works with the Sessions of Droi’s own Kernel. Update Droi on the computer, or turn
        its Kernel on, and the phone connects by itself.
      </Text>
      <View style={styles.retrying}>
        <Spinner size={14} color={colors.mutedForeground} />
        <Text tone="muted" size="sm">
          Checking again…
        </Text>
      </View>
    </ScrollView>
  )
}

function Unreachable({
  computer,
  style,
}: {
  computer: PairedComputer
  style?: StyleProp<ViewStyle>
}) {
  const colors = useColors()
  return (
    <ScrollView style={style} contentContainerStyle={styles.panel}>
      <Heading>Can’t reach {computer.name}</Heading>
      <Text tone="muted">Trying to connect to</Text>
      <Text mono size="sm" style={[styles.address, { backgroundColor: colors.muted }]}>
        {computer.address}
      </Text>
      {computer.relay ? (
        <>
          <Text tone="muted">and through its relay</Text>
          <Text mono size="sm" style={[styles.address, { backgroundColor: colors.muted }]}>
            {computer.relay}
          </Text>
        </>
      ) : null}
      <View role="list" aria-label="What to check" style={styles.checks}>
        <Check>Remote Access is turned on in Droi → Settings on the computer.</Check>
        {computer.relay ? (
          <Check>
            Droi → Settings → Remote Access on the computer shows the relay as connected.
          </Check>
        ) : (
          <>
            <Check>
              The iPhone is on the same network as the computer, or Tailscale is on for both.
            </Check>
            <Check>
              Away from the computer’s network? Set up a relay in Droi → Settings → Remote Access on
              the computer, then connect once on its network or scan its new code.
            </Check>
          </>
        )}
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
  const appVersion = Constants.expoConfig?.version ?? ''
  const connection = useKernelConnection()
  const via = connection.gatewayUrl()
  const meta = useQuery({
    queryKey: ['meta', via],
    queryFn: async (): Promise<GatewayMeta> => {
      const response = await fetch(gatewayEndpoint(via, GATEWAY_META_PATH).toString())
      const answer = (await response.json()) as GatewayMeta
      followRelay(computer.id, answer)
      return answer
    },
    staleTime: Infinity,
  })
  const [dismissed, setDismissed] = usePreference(dismissedVersionNotice)
  const computerVersion = meta.data?.version ?? ''
  const noticeKey = `${computer.id} ${appVersion} ${computerVersion}`
  const showVersion = versionMismatch(appVersion, computerVersion) && dismissed !== noticeKey

  if (!reconnecting && !showVersion) return null
  return (
    <View pointerEvents="box-none" style={styles.notices}>
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
  top: { flexGrow: 0 },
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
  notices: { position: 'absolute', top: space.sm, left: space.md, right: space.md, gap: space.sm },
  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    borderRadius: radius.md,
    borderWidth: 1,
  },
})

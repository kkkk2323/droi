// Settings: the Paired Computers, Session list and alert preferences, and the
// app's version and signature.
import { showArchivedSessions, usePreference } from '@droi/daemon-layer/local-preference'
import { useQuery } from '@tanstack/react-query'
import Constants from 'expo-constants'
import { useRouter } from 'expo-router'
import { ScrollView, StyleSheet } from 'react-native'
import { alertSwitches } from '../alerts/alert-preferences'
import { pairedComputers } from '../computers/store'
import { daysLeft } from '../lib/signature'
import { signatureExpiry } from '../platform/signature'
import { ListRow, ListSection, ListSwitch } from '../ui/list'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SettingsScreen() {
  const colors = useColors()
  const router = useRouter()
  const [computers] = usePreference(pairedComputers)
  const [showArchived, setShowArchived] = usePreference(showArchivedSessions)
  const [alerts, setAlerts] = usePreference(alertSwitches)
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
      <ListSection>
        <ListRow
          label="Paired computers"
          value={String(computers.length)}
          onPress={() => router.push('/computers')}
        />
      </ListSection>
      <ListSection title="Sessions">
        <ListSwitch
          label="Show archived sessions"
          value={showArchived}
          onChange={setShowArchived}
        />
      </ListSection>
      <ListSection
        title="Alerts"
        footer="While the app is open, for Sessions other than the one on screen. Sounds follow the silent switch."
      >
        <ListSwitch
          label="Sound when finished"
          value={alerts.finishedSound}
          onChange={(on) => setAlerts({ ...alerts, finishedSound: on })}
        />
        <ListSwitch
          label="Haptic when finished"
          value={alerts.finishedHaptic}
          onChange={(on) => setAlerts({ ...alerts, finishedHaptic: on })}
        />
        <ListSwitch
          label="Sound when it needs input"
          value={alerts.needsInputSound}
          onChange={(on) => setAlerts({ ...alerts, needsInputSound: on })}
        />
        <ListSwitch
          label="Haptic when it needs input"
          value={alerts.needsInputHaptic}
          onChange={(on) => setAlerts({ ...alerts, needsInputHaptic: on })}
        />
      </ListSection>
      <ListSection
        title="About"
        footer="The app is signed with a free Apple account for seven days. Run pnpm install:phone on the computer with the iPhone connected to sign and install it again."
      >
        <ListRow label="Version" value={version} />
        <ListRow label="Signature" value={signature} />
      </ListSection>
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  content: { padding: space.lg, gap: space.xl },
})

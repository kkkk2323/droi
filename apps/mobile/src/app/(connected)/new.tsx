import { useLocalSearchParams } from 'expo-router'
import { NewSessionScreen } from '../../screens/new-session-screen'

/** `?workspace=<path>` preselects a Workspace, `?scratch=1` None. */
export default function NewSession() {
  const { workspace, scratch } = useLocalSearchParams<{ workspace?: string; scratch?: string }>()
  return (
    <NewSessionScreen
      initialPick={
        scratch ? { kind: 'scratch' } : workspace ? { kind: 'recent', path: workspace } : null
      }
    />
  )
}

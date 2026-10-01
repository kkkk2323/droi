import { useLocalSearchParams } from 'expo-router'
import { OpenSessionScreen } from '../../../screens/session-screen'

export default function OpenSession() {
  const { id } = useLocalSearchParams<{ id: string }>()
  return <OpenSessionScreen sessionId={id} />
}

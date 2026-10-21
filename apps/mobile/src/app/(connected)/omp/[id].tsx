import { useLocalSearchParams } from 'expo-router'
import { OmpSessionScreen } from '../../../screens/omp-session-screen'

export default function OpenOmpSession() {
  const { id } = useLocalSearchParams<{ id: string }>()
  return <OmpSessionScreen sessionId={id} />
}

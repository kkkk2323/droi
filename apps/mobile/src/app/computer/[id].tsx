import { useLocalSearchParams } from 'expo-router'
import { ComputerScreen } from '../../screens/computer-screen'

export default function EditComputer() {
  const { id } = useLocalSearchParams<{ id: string }>()
  return <ComputerScreen id={id} />
}

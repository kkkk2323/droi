import { usePreference } from '@droi/daemon-layer/local-preference'
import { pairedComputers, selectedComputer, selectedComputerId } from '../computers/store'
import { ComputerConnection } from '../connection/computer-connection'
import { MainScreen } from '../screens/main-screen'
import { PairingScreen } from '../screens/pairing-screen'

export default function Main() {
  const [computers] = usePreference(pairedComputers)
  const [selectedId] = usePreference(selectedComputerId)
  const computer = selectedComputer(computers, selectedId)
  if (!computer) return <PairingScreen firstLaunch />
  return (
    <ComputerConnection key={computer.id} computer={computer}>
      <MainScreen computer={computer} />
    </ComputerConnection>
  )
}

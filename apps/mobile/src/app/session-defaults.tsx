import { usePreference } from '@droi/daemon-layer/local-preference'
import { pairedComputers, selectedComputer, selectedComputerId } from '../computers/store'
import { ComputerConnection } from '../connection/computer-connection'
import { SessionDefaultsScreen } from '../screens/session-defaults-screen'

/** The selected computer's Session defaults, over a connection of this page's own. */
export default function SessionDefaults() {
  const [computers] = usePreference(pairedComputers)
  const [selectedId] = usePreference(selectedComputerId)
  const computer = selectedComputer(computers, selectedId)
  if (!computer) return null
  return (
    <ComputerConnection key={computer.id} computer={computer}>
      <SessionDefaultsScreen computerName={computer.name} />
    </ComputerConnection>
  )
}

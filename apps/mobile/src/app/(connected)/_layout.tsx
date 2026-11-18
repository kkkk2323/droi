// The selected computer's own stack: its Session list, Sessions and New
// session over one connection. Choosing another computer remounts it, so it
// starts again from that computer's list.
import { Stack } from 'expo-router'
import { ComputerConnection } from '../../connection/computer-connection'
import { ConnectedComputerProvider, useSelectedComputer } from '../../sessions/connected-computer'
import { useStackScreenOptions } from '../../ui/stack-options'

export const unstable_settings = { initialRouteName: 'index' }

export default function ConnectedLayout() {
  const computer = useSelectedComputer()
  const screenOptions = useStackScreenOptions()
  const stack = (
    <Stack screenOptions={screenOptions}>
      <Stack.Screen name="index" options={{ title: 'Sessions' }} />
      <Stack.Screen name="session/[id]" options={{ title: '' }} />
      <Stack.Screen name="new" options={{ title: 'New session' }} />
    </Stack>
  )
  if (!computer) return stack
  return (
    <ComputerConnection key={computer.id} computer={computer}>
      <ConnectedComputerProvider computer={computer}>{stack}</ConnectedComputerProvider>
    </ComputerConnection>
  )
}

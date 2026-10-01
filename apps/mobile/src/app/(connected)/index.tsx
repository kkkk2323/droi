import { Stack } from 'expo-router'
import { PairingScreen } from '../../screens/pairing-screen'
import { SessionsScreen } from '../../screens/sessions-screen'
import { useConnectedComputer } from '../../sessions/connected-computer'

export default function Main() {
  const connected = useConnectedComputer()
  if (!connected) {
    return (
      <>
        <Stack.Screen options={{ headerShown: false }} />
        <PairingScreen firstLaunch />
      </>
    )
  }
  return <SessionsScreen />
}

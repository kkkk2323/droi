// Pairing from a pasted or scanned link: resolve it against the computer's
// Gateway, store the Paired Computer, and let a connection pick up the token.
import { useState } from 'react'
import { PAIRING_TOKEN_QUERY } from '../connection/computer-connection'
import { keychain } from '../platform/keychain'
import { appQueryClient } from '../query-client'
import { PairingError, resolvePairing } from './pairing'
import { savePairing } from './store'

export function usePairing(onPaired: (computerId: string) => void) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const pair = async (text: string): Promise<boolean> => {
    setBusy(true)
    setError(null)
    try {
      const result = await resolvePairing(text)
      await savePairing(result, keychain)
      await appQueryClient.invalidateQueries({ queryKey: [PAIRING_TOKEN_QUERY, result.id] })
      setBusy(false)
      onPaired(result.id)
      return true
    } catch (cause) {
      setError(cause instanceof PairingError ? cause.message : String(cause))
      setBusy(false)
      return false
    }
  }

  return { pair, busy, error }
}

// One Paired Computer: its name, its address (for example a Tailscale name
// that works away from home) and removing it.
import { usePreference } from '@droi/daemon-layer/local-preference'
import { useRouter } from 'expo-router'
import { useState } from 'react'
import { ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { normalizeAddress } from '../computers/address'
import { pairedComputers, removeComputer, updateComputer } from '../computers/store'
import { keychain } from '../platform/keychain'
import { Button, Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function ComputerScreen({ id }: { id: string }) {
  const [computers] = usePreference(pairedComputers)
  const computer = computers.find((c) => c.id === id)
  if (!computer) return null
  return <ComputerForm key={computer.id} {...computer} />
}

function ComputerForm({ id, name, address }: { id: string; name: string; address: string }) {
  const colors = useColors()
  const router = useRouter()
  const [draftName, setDraftName] = useState(name)
  const [draftAddress, setDraftAddress] = useState(address)
  const [error, setError] = useState<string | null>(null)
  const [confirmingRemove, setConfirmingRemove] = useState(false)

  const save = () => {
    const normalized = normalizeAddress(draftAddress)
    if (!normalized) {
      setError('Enter an address like 192.168.1.10:41417 or my-mac.tailnet.ts.net:41417.')
      return
    }
    updateComputer(id, { name: draftName.trim() || name, address: normalized })
    router.back()
  }

  const inputStyle = [
    styles.input,
    { borderColor: colors.input, color: colors.foreground, backgroundColor: colors.background },
  ]

  return (
    <ScrollView
      style={{ backgroundColor: colors.background }}
      contentContainerStyle={styles.content}
      keyboardShouldPersistTaps="handled"
    >
      <View style={styles.field}>
        <Text size="sm" weight="medium">
          Name
        </Text>
        <TextInput
          aria-label="Name"
          value={draftName}
          onChangeText={setDraftName}
          style={inputStyle}
        />
      </View>
      <View style={styles.field}>
        <Text size="sm" weight="medium">
          Address
        </Text>
        <TextInput
          aria-label="Address"
          value={draftAddress}
          onChangeText={(text) => {
            setDraftAddress(text)
            setError(null)
          }}
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="url"
          style={[inputStyle, { fontFamily: fonts.mono }]}
        />
        <Text tone="muted" size="sm">
          A name that reaches the computer from anywhere (Tailscale, or a Surge Ponte name such as
          laptop.myhome:41417) works at home and away.
        </Text>
        {error ? (
          <Text role="alert" size="sm" style={{ color: colors.destructiveForeground }}>
            {error}
          </Text>
        ) : null}
      </View>
      <Button label="Save" onPress={save} />
      {confirmingRemove ? (
        <View style={styles.confirm}>
          <Text size="sm">
            Remove {name}? The phone forgets its Pairing Token; pair again to come back.
          </Text>
          <View style={styles.confirmButtons}>
            <Button
              label="Cancel"
              variant="secondary"
              style={styles.grow}
              onPress={() => setConfirmingRemove(false)}
            />
            <Button
              label="Remove"
              style={[styles.grow, { backgroundColor: colors.destructive }]}
              onPress={() => void removeComputer(id, keychain).then(() => router.back())}
            />
          </View>
        </View>
      ) : (
        <Button label="Remove computer" variant="ghost" onPress={() => setConfirmingRemove(true)} />
      )}
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  content: { padding: space.lg, gap: space.lg },
  field: { gap: space.xs },
  input: {
    borderWidth: 1,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    height: 44,
    fontFamily: fonts.sans,
    fontSize: fontSize.base,
  },
  confirm: { gap: space.md },
  confirmButtons: { flexDirection: 'row', gap: space.sm },
  grow: { flex: 1 },
})

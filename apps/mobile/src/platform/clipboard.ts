import * as Clipboard from 'expo-clipboard'

export async function copyText(text: string): Promise<void> {
  await Clipboard.setStringAsync(text)
}

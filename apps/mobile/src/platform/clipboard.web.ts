// Web stand-in for the clipboard: keeps the last copied text for the tests.
import { standIns } from './stand-ins'

export async function copyText(text: string): Promise<void> {
  standIns().clipboard = text
}

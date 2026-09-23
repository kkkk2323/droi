import type { useRouter } from 'expo-router'

/** Close every pushed page and show the main screen. */
export function backToMain(router: ReturnType<typeof useRouter>): void {
  if (router.canDismiss()) router.dismissAll()
}

import type { useRouter } from 'expo-router'

/** Close every pushed page and show the Session list. */
export function backToMain(router: ReturnType<typeof useRouter>): void {
  if (router.canDismiss()) router.dismissAll()
}

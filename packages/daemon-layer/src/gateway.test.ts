import { expect, test } from 'vitest'
import {
  gatewayDaemonUrl,
  gatewayEndpoint,
  gatewayScratchUrl,
  GATEWAY_SCRATCH_PATH,
} from './gateway'

test("an endpoint goes after a Relay address's path, and on an origin as before", () => {
  const relay = 'https://relay.example.com/c/abcdefghijklmnop'
  expect(gatewayDaemonUrl(relay, 't')).toBe(
    'wss://relay.example.com/c/abcdefghijklmnop/daemon?token=t',
  )
  expect(gatewayDaemonUrl('http://192.168.1.2:41417', 't')).toBe(
    'ws://192.168.1.2:41417/daemon?token=t',
  )
  expect(gatewayScratchUrl(`${relay}/`, 't', GATEWAY_SCRATCH_PATH)).toBe(
    `${relay}/scratch-workspaces?token=t`,
  )
  expect(gatewayEndpoint('http://a:1/', '/meta').toString()).toBe('http://a:1/meta')
})

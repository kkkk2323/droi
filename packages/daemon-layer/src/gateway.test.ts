import { expect, test } from 'vitest'
import {
  gatewayEndpoint,
  gatewayKernelUrl,
  gatewayScratchUrl,
  GATEWAY_SCRATCH_PATH,
} from './gateway'

test("an endpoint goes after a Relay address's path, and on an origin as before", () => {
  const relay = 'https://relay.example.com/c/abcdefghijklmnop'
  expect(gatewayKernelUrl(relay, 't')).toBe(
    'wss://relay.example.com/c/abcdefghijklmnop/kernel?token=t',
  )
  expect(gatewayKernelUrl('http://192.168.1.2:41417', 't')).toBe(
    'ws://192.168.1.2:41417/kernel?token=t',
  )
  expect(gatewayScratchUrl(`${relay}/`, 't', GATEWAY_SCRATCH_PATH)).toBe(
    `${relay}/scratch-workspaces?token=t`,
  )
  expect(gatewayEndpoint('http://a:1/', '/meta').toString()).toBe('http://a:1/meta')
})

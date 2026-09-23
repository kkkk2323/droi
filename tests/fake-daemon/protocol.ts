import { z } from 'zod'

export const JsonRpcRequestSchema = z.object({
  jsonrpc: z.literal('2.0'),
  id: z.union([z.string(), z.number()]),
  method: z.string().regex(/^daemon\./, 'Daemon methods are prefixed daemon.'),
  params: z.record(z.string(), z.unknown()),
  type: z.literal('request').optional(),
})

export type JsonRpcRequest = z.infer<typeof JsonRpcRequestSchema>

import { Readable } from 'stream'
import { pipeline } from 'stream/promises'

export async function pipelineNode(
  ...streams: Array<NodeJS.ReadableStream | NodeJS.WritableStream>
): Promise<void> {
  if (streams.length < 2) throw new Error('pipelineNode requires at least 2 streams')
  await pipeline(streams[0] as any, ...(streams.slice(1) as any))
}

export function webToNodeReadable(stream: ReadableStream<Uint8Array>): Readable {
  return Readable.fromWeb(stream as any)
}

export function nodeToWebReadable(stream: Readable): ReadableStream<Uint8Array> {
  return Readable.toWeb(stream) as ReadableStream<Uint8Array>
}

import { createWriteStream } from 'fs'
import { mkdir } from 'fs/promises'
import type { IncomingHttpHeaders } from 'http'
import { join } from 'path'
import busboy from 'busboy'
import { sanitizeFilename } from './path.ts'
import { pipelineNode, webToNodeReadable } from './stream.ts'

export interface UploadedFile {
  name: string
  path: string
}

export class UploadError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

function toIncomingHeaders(req: Request): IncomingHttpHeaders {
  const headers: IncomingHttpHeaders = {}
  for (const [key, value] of req.headers.entries()) {
    headers[key.toLowerCase()] = value
  }
  return headers
}

export async function saveMultipartFiles(params: {
  request: Request
  attachDir: string
}): Promise<UploadedFile[]> {
  const contentType = params.request.headers.get('content-type') || ''
  if (!contentType.includes('multipart/form-data')) {
    throw new UploadError(400, 'Expected multipart/form-data')
  }
  if (!params.request.body) {
    throw new UploadError(400, 'Missing request body')
  }

  await mkdir(params.attachDir, { recursive: true })

  const parser = busboy({ headers: toIncomingHeaders(params.request) })
  const files: UploadedFile[] = []
  const writeTasks: Array<Promise<void>> = []
  let seq = 0

  parser.on('file', (_fieldName, file, info) => {
    const originalName = info.filename || `upload-${Date.now()}.bin`
    const safeName = sanitizeFilename(originalName)
    const destPath = join(params.attachDir, `${Date.now()}-${seq}-${safeName}`)
    seq += 1

    const task = pipelineNode(file, createWriteStream(destPath)).then(() => {
      files.push({ name: originalName, path: destPath })
    })
    writeTasks.push(task)
  })

  const parseDone = new Promise<void>((resolve, reject) => {
    parser.once('error', reject)
    parser.once('close', resolve)
  })

  const input = webToNodeReadable(params.request.body)
  await Promise.all([pipelineNode(input, parser), parseDone])
  await Promise.all(writeTasks)

  return files
}

import { deflateRawSync } from 'zlib'

function u16(n: number): Buffer {
  const b = Buffer.allocUnsafe(2)
  b.writeUInt16LE(n & 0xffff, 0)
  return b
}

function u32(n: number): Buffer {
  const b = Buffer.allocUnsafe(4)
  b.writeUInt32LE(n >>> 0, 0)
  return b
}

function dosTimeDate(date: Date): { time: number; date: number } {
  const d = new Date(date)
  const time = ((d.getHours() & 0x1f) << 11) | ((d.getMinutes() & 0x3f) << 5) | ((Math.floor(d.getSeconds() / 2) & 0x1f) << 0)
  const dosDate = (((d.getFullYear() - 1980) & 0x7f) << 9) | (((d.getMonth() + 1) & 0x0f) << 5) | (d.getDate() & 0x1f)
  return { time, date: dosDate }
}

// CRC32 implementation (standard IEEE 802.3 polynomial).
const CRC_TABLE = (() => {
  const table = new Uint32Array(256)
  for (let i = 0; i < 256; i++) {
    let c = i
    for (let k = 0; k < 8; k++) {
      c = (c & 1) ? (0xedb88320 ^ (c >>> 1)) : (c >>> 1)
    }
    table[i] = c >>> 0
  }
  return table
})()

function crc32(buf: Buffer): number {
  let c = 0xffffffff
  for (let i = 0; i < buf.length; i++) {
    c = CRC_TABLE[(c ^ buf[i]) & 0xff] ^ (c >>> 8)
  }
  return (c ^ 0xffffffff) >>> 0
}

type ZipFile = {
  name: string
  data: Buffer
  mtime: Date
  compress: boolean
}

export class ZipBuilder {
  private readonly files: ZipFile[] = []

  addFile(name: string, data: Buffer | string, opts?: { mtime?: Date; compress?: boolean }) {
    const normalized = String(name || '').replace(/^\/+/, '')
    const buf = Buffer.isBuffer(data) ? data : Buffer.from(String(data), 'utf8')
    this.files.push({
      name: normalized,
      data: buf,
      mtime: opts?.mtime || new Date(),
      compress: opts?.compress === true,
    })
  }

  toBuffer(): Buffer {
    const localParts: Buffer[] = []
    const centralParts: Buffer[] = []
    let offset = 0

    for (const f of this.files) {
      const nameBuf = Buffer.from(f.name, 'utf8')
      const uncompressed = f.data
      const method = f.compress ? 8 : 0
      const compressed = f.compress ? deflateRawSync(uncompressed) : uncompressed
      const crc = crc32(uncompressed)
      const { time, date } = dosTimeDate(f.mtime)

      const localHeader = Buffer.concat([
        u32(0x04034b50),
        u16(20),
        u16(0),
        u16(method),
        u16(time),
        u16(date),
        u32(crc),
        u32(compressed.length),
        u32(uncompressed.length),
        u16(nameBuf.length),
        u16(0),
        nameBuf,
      ])

      localParts.push(localHeader, compressed)

      const centralHeader = Buffer.concat([
        u32(0x02014b50),
        u16(20),
        u16(20),
        u16(0),
        u16(method),
        u16(time),
        u16(date),
        u32(crc),
        u32(compressed.length),
        u32(uncompressed.length),
        u16(nameBuf.length),
        u16(0),
        u16(0),
        u16(0),
        u16(0),
        u32(0),
        u32(offset),
        nameBuf,
      ])
      centralParts.push(centralHeader)

      offset += localHeader.length + compressed.length
    }

    const centralDir = Buffer.concat(centralParts)
    const centralOffset = offset
    const centralSize = centralDir.length

    const eocd = Buffer.concat([
      u32(0x06054b50),
      u16(0),
      u16(0),
      u16(this.files.length),
      u16(this.files.length),
      u32(centralSize),
      u32(centralOffset),
      u16(0),
    ])

    return Buffer.concat([...localParts, centralDir, eocd])
  }
}

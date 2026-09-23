import { standIns } from './stand-ins'

export async function signatureExpiry(): Promise<Date | null> {
  const expiry = standIns().signatureExpiry
  return expiry ? new Date(expiry) : null
}

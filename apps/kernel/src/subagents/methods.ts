import { invalidParams, optionalBoolean, optionalObject, optionalString } from '../protocol/rpc.ts'
import { deleteDroid, droidFields, loadDroids, writeDroid } from './droids.ts'

/** `droids.list`: every droid the `task` tool can start, the built-in and a person's own, and the files that are not one. */
export function droidsList(userDir: string): unknown {
  return loadDroids(userDir)
}

/**
 * `droids.set {name, droid?, remove?}`: write a person's droid as `droid` says, under its own name (a droid
 * renamed from `name` leaves `name`'s file), or with `remove` delete it. A built-in droid cannot be removed;
 * writing one of its name puts a person's in its place, and removing that brings the built-in back.
 */
export function droidsSet(userDir: string, params: Record<string, unknown>): unknown {
  const name = optionalString(params, 'name')
  if (name === undefined || name === '') {
    throw invalidParams([{ path: 'name', message: 'a droid name is required' }])
  }
  if (optionalBoolean(params, 'remove') === true) {
    if (!deleteDroid(userDir, name)) {
      throw invalidParams([{ path: 'name', message: `no droid ${name} of your own` }])
    }
    return loadDroids(userDir)
  }
  const raw = optionalObject(params, 'droid')
  if (raw === undefined)
    throw invalidParams([{ path: 'droid', message: 'droid or remove is required' }])
  const fields = droidFields(raw)
  if ('error' in fields) throw invalidParams([{ path: 'droid', message: fields.error }])
  writeDroid(userDir, fields)
  if (fields.name !== name) deleteDroid(userDir, name)
  return loadDroids(userDir)
}

import { stat } from 'fs/promises'

/**
 * Check if a path exists and is a directory
 */
export async function isDirectory(dirPath: string): Promise<boolean> {
  try {
    return (await stat(dirPath)).isDirectory()
  } catch {
    return false
  }
}

/**
 * Check if a path exists and is a file
 */
export async function isFile(filePath: string): Promise<boolean> {
  try {
    return (await stat(filePath)).isFile()
  } catch {
    return false
  }
}

// Aliases for backward compatibility
export const isDir = isDirectory

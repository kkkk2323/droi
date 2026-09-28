// Contract between the Desktop Shell and the Local Client for choosing a
// Workspace in the system's folder dialog. Only the Local Client has it; a
// Remote Client is on another device and types the path instead.

/** Asks for a folder; resolves to its path, or null when cancelled. */
export type PickFolder = () => Promise<string | null>

export const PICK_FOLDER_IPC = 'droi:pick-folder'

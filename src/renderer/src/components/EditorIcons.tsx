import React from 'react'

interface IconProps {
  className?: string
}

// VS Code icon (from Devicon)
export function VsCodeIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 128 128">
      <path fill="#007ACC" d="M121.7 13.8 95.3 1.1c-3.1-1.3-6.7-.6-9.1 1.6L1.7 81.4c-2.1 2-2.1 5.3 0 7.3l7.1 6.4c1.8 1.6 4.5 1.8 6.5.4l93.8-71.1c3.5-2.6 8.5-.1 8.5 4.2v-.3c0-3.2-1.9-6.1-4.9-7.3z" />
      <path fill="#1F9CF0" d="m121.7 114.2-26.4 12.7c-3.1 1.5-6.7.8-9.1-1.6L1.7 46.6c-2.1-2-2.1-5.3 0-7.3l7.1-6.4c1.8-1.6 4.5-1.8 6.5-.4l93.8 71.1c3.5 2.6 8.5.1 8.5-4.2v.3c0 3.2-1.9 6.1-4.9 7.3z" />
      <path fill="url(#vscode-gradient)" d="M95.3 126.9c-3.4 1.5-7.3.6-9.8-2.3 3.2 3.2 8.5.9 8.5-3.3V6c0-4.2-5.3-6.5-8.5-3.3 2.5-2.9 6.4-3.8 9.8-2.3l26.4 12.7c3 1.4 5 4.5 5 7.9v86c0 3.4-2 6.5-5 7.9l-26.4 12.7z" />
      <defs>
        <linearGradient id="vscode-gradient" x1="64" y1="0" x2="64" y2="128" gradientUnits="userSpaceOnUse">
          <stop stopColor="#fff" stopOpacity="0.25" />
          <stop offset="1" stopColor="#fff" stopOpacity="0" />
        </linearGradient>
      </defs>
    </svg>
  )
}

// Cursor icon
export function CursorIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <path
        fill="url(#cursor-gradient)"
        d="M12 2C6.477 2 2 6.477 2 12s4.477 10 10 10 10-4.477 10-10S17.523 2 12 2zm0 2a8 8 0 1 1 0 16 8 8 0 0 1 0-16z"
      />
      <path
        fill="url(#cursor-gradient)"
        d="M12 6a6 6 0 1 0 0 12 6 6 0 0 0 0-12zm0 2a4 4 0 1 1 0 8 4 4 0 0 1 0-8z"
      />
      <defs>
        <linearGradient id="cursor-gradient" x1="2" y1="2" x2="22" y2="22">
          <stop stopColor="#7C3AED" />
          <stop offset="1" stopColor="#2563EB" />
        </linearGradient>
      </defs>
    </svg>
  )
}

// Zed icon
export function ZedIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <path
        fill="#FACC15"
        d="M3.5 4.5h17L12.5 13l8 6.5h-17l8-6.5-8-8zm3 2l5 5-5 4h8l-5-4 5-5h-8z"
      />
    </svg>
  )
}

// Windsurf icon
export function WindsurfIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <path
        fill="#06B6D4"
        d="M4 4c3 0 5.5 1 7 3 1.5-2 4-3 7-3-3 1-5 3-6 5h4c-2 1-3.5 2.5-4 4 .5 1.5 2 3 4 4h-4c1 2 3 4 6 5-3 0-5.5-1-7-3-1.5 2-4 3-7 3 3-1 5-3 6-5H6c2-1 3.5-2.5 4-4-.5-1.5-2-3-4-4h4c-1-2-3-4-6-5z"
      />
    </svg>
  )
}

// IntelliJ IDEA icon (from Devicon)
export function IdeaIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 128 128">
      <path fill="url(#idea-a)" d="m23.5 88-17.2-13.6 10.1-18.8 15.2 5.1z" />
      <path fill="#087CFA" d="m122 36.7-2.1 67.8-45.1 18.1-24.5-15.9z" />
      <path fill="url(#idea-b)" d="M122 36.7 99.7 58.4 71 23.3l14.1-15.9z" />
      <path fill="url(#idea-c)" d="m50.2 106.7-35.8 13 7.5-26.3 9.7-32.6L5 51.8l16.9-46.3 38.3 4.5L99.7 58.4z" />
      <path d="M27.4 27.4h73.1v73.1H27.4z" />
      <path fill="#fff" d="M36.5 86.7h27.4v4.6H36.5zm13.7-45.1v-5h-13.6v5h3.8v17.3h-3.8v5h13.6v-5h-3.8V41.6zm13.1 22.6c-1.9 0-3.7-.4-5.4-1.2-1.4-.7-2.6-1.6-3.6-2.9l3.8-4.2c.7.8 1.5 1.5 2.4 2 .8.5 1.7.7 2.6.7 1.1 0 2-.3 2.7-1.1.6-.7.9-1.8.9-3.4V36.6h6.1v17.9c0 1.5-.2 2.9-.7 4.3-.4 1.2-1.1 2.3-2 3.2-.9.9-2 1.5-3.2 2-1.2.4-2.5.7-4 .7" />
      <defs>
        <linearGradient id="idea-a" x1="11.2" y1="59.2" x2="58.9" y2="56.8" gradientUnits="userSpaceOnUse">
          <stop stopColor="#FC801D" />
          <stop offset=".4" stopColor="#087CFA" />
        </linearGradient>
        <linearGradient id="idea-b" x1="89" y1="54.1" x2="73.1" y2="6.5" gradientUnits="userSpaceOnUse">
          <stop stopColor="#FE2857" />
          <stop offset=".8" stopColor="#087CFA" />
        </linearGradient>
        <linearGradient id="idea-c" x1="18.7" y1="26.6" x2="78.8" y2="126" gradientUnits="userSpaceOnUse">
          <stop stopColor="#FE2857" />
          <stop offset="1" stopColor="#087CFA" />
        </linearGradient>
      </defs>
    </svg>
  )
}

// WebStorm icon (from Devicon)
export function WebStormIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 128 128">
      <path fill="url(#ws-a)" d="m21.3 111.3-15.8-93.5 29.2-12.1 18.6 11.1 17.1-9.2 35.6 13.7-19.9 101.3z" />
      <path fill="url(#ws-b)" d="m122.5 45.1-15.1-37.3-27.4-2.3-42.3 40.7 11.4 52.4 21.3 14.9 52.2-31-12.8-24z" />
      <path fill="url(#ws-c)" d="m99.1 39.6 10.6 18.9 12.8-13.3-9.4-23.2z" />
      <path d="M27.4 27.4h73.1v73.1H27.4z" />
      <path fill="#fff" d="M36.5 86.7h27.4v4.6H36.5zm26-50.2-4.1 16-4.7-16h-4.6l-4.7 16-4.1-16h-6.1l7.8 27.4h5.1l4.5-15.9 4.4 15.9h5.2l7.8-27.4zm7.5 23.5 3.6-4.3c3.1 2.6 6.3 4.1 8.2 3.3 2.4 0 4-.9 4-2.6 0-1.5-.9-2.3-5.5-3.5-5.5-1.4-9.1-2.9-9.1-8.4 0-5 4-8.3 9.6-8.3 4.3 0 7.8 1.3 10.2 3.5l-3.1 4.6c-2.5-2-5-3.1-7.2-2.7-2.3 0-3.5 1-3.5 2.4 0 1.8 1.2 2.4 5.9 3.7 5.6 1.5 8.7 3.5 8.7 8.3 0 5.5-4.2 8.5-10.1 8.5-4.5.1-8.5-1.5-11.7-4.4" />
      <defs>
        <linearGradient id="ws-a" x1="38.9" y1="6.5" x2="63.7" y2="95.9" gradientUnits="userSpaceOnUse">
          <stop stopColor="#07C3F2" />
          <stop offset=".9" stopColor="#087CFA" />
        </linearGradient>
        <linearGradient id="ws-b" x1="46.6" y1="17.9" x2="88.7" y2="79.5" gradientUnits="userSpaceOnUse">
          <stop stopColor="#FCF84A" />
          <stop offset=".4" stopColor="#07C3F2" />
        </linearGradient>
        <linearGradient id="ws-c" x1="88.3" y1="25.5" x2="93.8" y2="45" gradientUnits="userSpaceOnUse">
          <stop stopColor="#07C3F2" />
          <stop offset=".9" stopColor="#087CFA" />
        </linearGradient>
      </defs>
    </svg>
  )
}

// Sublime Text icon
export function SublimeIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <rect width="24" height="24" rx="3" fill="#FF9800" />
      <path fill="#fff" d="M6 9l12-3v3L6 12zm0 3l12-3v3L6 15zm0 3l12-3v3L6 18z" opacity="0.9" />
    </svg>
  )
}

// Finder/Folder icon
export function FinderIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <path
        fill="#4AA9E9"
        d="M3 6a2 2 0 0 1 2-2h4.586a1 1 0 0 1 .707.293L12 6h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6z"
      />
      <path fill="#2D7EB3" d="M3 8h18v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8z" />
    </svg>
  )
}

// Terminal icon
export function TerminalIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <rect width="24" height="24" rx="4" fill="#333" />
      <path stroke="#0F0" strokeWidth="2" strokeLinecap="round" d="M6 8l4 4-4 4" />
      <path stroke="#0F0" strokeWidth="2" strokeLinecap="round" d="M12 16h6" />
    </svg>
  )
}

// iTerm icon
export function ItermIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <rect width="24" height="24" rx="4" fill="#000" />
      <rect x="2" y="2" width="20" height="20" rx="3" fill="url(#iterm-gradient)" />
      <path fill="#fff" d="M6 10l4 3-4 3v-6zm5 5h6v2h-6z" opacity="0.9" />
      <defs>
        <linearGradient id="iterm-gradient" x1="2" y1="2" x2="22" y2="22">
          <stop stopColor="#6EE7B7" />
          <stop offset="1" stopColor="#14B8A6" />
        </linearGradient>
      </defs>
    </svg>
  )
}

// Ghostty icon
export function GhosttyIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <circle cx="12" cy="12" r="10" fill="#6EE7B7" />
      <path fill="#fff" d="M8 9l4 3-4 3V9zm5 4h5v2h-5z" opacity="0.9" />
    </svg>
  )
}

// Warp icon
export function WarpIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <rect width="24" height="24" rx="4" fill="#01A4FF" />
      <path d="M5 12h4l2-4 2 8 2-4h4" stroke="#fff" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

// Antigravity icon (placeholder)
export function AntigravityIcon({ className = 'size-4' }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none">
      <circle cx="12" cy="12" r="10" fill="#8B5CF6" />
      <text x="12" y="16" textAnchor="middle" fill="#fff" fontSize="10" fontWeight="bold">A</text>
    </svg>
  )
}

// Map editor IDs to icon components
export const EDITOR_ICON_MAP: Record<string, React.FC<IconProps>> = {
  vscode: VsCodeIcon,
  cursor: CursorIcon,
  antigravity: AntigravityIcon,
  windsurf: WindsurfIcon,
  zed: ZedIcon,
  idea: IdeaIcon,
  webstorm: WebStormIcon,
  sublime: SublimeIcon,
  finder: FinderIcon,
  terminal: TerminalIcon,
  iterm: ItermIcon,
  ghostty: GhosttyIcon,
  warp: WarpIcon,
}

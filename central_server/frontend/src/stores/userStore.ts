import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface UserState {
  username: string | null
  sessionId: string | null
  setUsername: (name: string) => void
  clearUser: () => void
}

const generateSessionId = (username: string): string => {
  const safeUsername = encodeURIComponent(username)
  return `session_${safeUsername}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}`
}

// Check if a session ID contains only ASCII characters (safe for HTTP headers)
const isAsciiOnly = (str: string): boolean => /^[\x00-\x7F]*$/.test(str)

export const useUserStore = create<UserState>()(
  persist(
    (set, get) => ({
      username: null,
      sessionId: null,

      setUsername: (name: string) => {
        const current = get()
        // Regenerate if username changes or if existing sessionId contains non-ASCII
        if (current.username === name && current.sessionId && isAsciiOnly(current.sessionId)) {
          return
        }
        set({
          username: name,
          sessionId: generateSessionId(name),
        })
      },

      clearUser: () => set({ username: null, sessionId: null }),
    }),
    {
      name: 'fleet-user',
      version: 1,
      migrate: (persisted: any, version: number) => {
        if (version === 0 && persisted?.sessionId && !isAsciiOnly(persisted.sessionId)) {
          persisted.sessionId = persisted.username
            ? generateSessionId(persisted.username)
            : null
        }
        return persisted as UserState
      },
    }
  )
)

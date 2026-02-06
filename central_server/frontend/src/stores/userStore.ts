import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface UserState {
  username: string | null
  sessionId: string | null
  setUsername: (name: string) => void
  clearUser: () => void
}

const generateSessionId = (username: string): string => {
  return `session_${username}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}`
}

export const useUserStore = create<UserState>()(
  persist(
    (set, get) => ({
      username: null,
      sessionId: null,

      setUsername: (name: string) => {
        const current = get()
        // Only regenerate sessionId if username changes
        if (current.username === name && current.sessionId) {
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
    }
  )
)

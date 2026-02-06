import { useState } from 'react'
import { User } from 'lucide-react'
import { useUserStore } from '../stores/userStore'

export function UsernameDialog() {
  const { username, setUsername } = useUserStore()
  const [input, setInput] = useState('')

  if (username) return null

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = input.trim()
    if (trimmed) {
      setUsername(trimmed)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <form
        onSubmit={handleSubmit}
        className="bg-surface border border-primary rounded-xl p-6 w-80 shadow-2xl"
      >
        <div className="flex items-center gap-3 mb-4">
          <div className="w-10 h-10 rounded-full bg-blue-600/20 flex items-center justify-center">
            <User size={20} className="text-blue-400" />
          </div>
          <div>
            <h2 className="text-primary font-semibold">Fleet UI</h2>
            <p className="text-muted text-xs">Enter your name to continue</p>
          </div>
        </div>
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="Name"
          autoFocus
          className="w-full px-3 py-2 bg-base border border-primary rounded-lg text-primary placeholder-muted text-sm focus:outline-none focus:border-blue-500 mb-4"
        />
        <button
          type="submit"
          disabled={!input.trim()}
          className="w-full py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-600 disabled:cursor-not-allowed text-white rounded-lg text-sm font-medium transition-colors"
        >
          Confirm
        </button>
      </form>
    </div>
  )
}

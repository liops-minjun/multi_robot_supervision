import { createContext, useContext, useEffect, useState, useCallback, useRef, ReactNode } from 'react'

// WebSocket message types
export interface BehaviorTreeLockMessage {
  type: 'behavior_tree_lock'
  behavior_tree_id: string
  action: 'acquired' | 'released' | 'expired'
  locked_by?: string
  session_id?: string
  expires_at?: number
}

export interface GraphSyncMessage {
  type: 'graph_sync'
  behavior_tree_id: string
  agent_id?: string
  action: 'updated' | 'deployed' | 'unassigned'
}

export type WebSocketMessage = BehaviorTreeLockMessage | GraphSyncMessage | { type: string; [key: string]: unknown }

interface WebSocketContextValue {
  isConnected: boolean
  lastMessage: WebSocketMessage | null
  // Subscribe to specific behavior tree lock events
  subscribeLockEvents: (graphId: string, callback: (msg: BehaviorTreeLockMessage) => void) => () => void
  // Subscribe to graph sync events
  subscribeSyncEvents: (graphId: string, callback: (msg: GraphSyncMessage) => void) => () => void
}

const WebSocketContext = createContext<WebSocketContextValue | null>(null)

export function WebSocketProvider({ children }: { children: ReactNode }) {
  const [isConnected, setIsConnected] = useState(false)
  const [lastMessage, setLastMessage] = useState<WebSocketMessage | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Subscription maps
  const lockSubscribersRef = useRef<Map<string, Set<(msg: BehaviorTreeLockMessage) => void>>>(new Map())
  const syncSubscribersRef = useRef<Map<string, Set<(msg: GraphSyncMessage) => void>>>(new Map())

  const connect = useCallback(() => {
    // Don't reconnect if already connected
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      return
    }

    // Determine WebSocket URL
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = window.location.host
    // In development, the API is proxied through Vite on the same port
    const wsUrl = `${protocol}//${host}/ws/monitor`

    console.log('[WebSocket] Connecting to', wsUrl)
    const ws = new WebSocket(wsUrl)

    ws.onopen = () => {
      console.log('[WebSocket] Connected')
      setIsConnected(true)
    }

    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data) as WebSocketMessage
        setLastMessage(message)

        // Route messages to subscribers
        if (message.type === 'behavior_tree_lock') {
          const lockMsg = message as BehaviorTreeLockMessage
          const subscribers = lockSubscribersRef.current.get(lockMsg.behavior_tree_id)
          if (subscribers) {
            subscribers.forEach(callback => callback(lockMsg))
          }
        } else if (message.type === 'graph_sync') {
          const syncMsg = message as GraphSyncMessage
          const subscribers = syncSubscribersRef.current.get(syncMsg.behavior_tree_id)
          if (subscribers) {
            subscribers.forEach(callback => callback(syncMsg))
          }
        }
      } catch (e) {
        console.error('[WebSocket] Failed to parse message:', e)
      }
    }

    ws.onclose = () => {
      console.log('[WebSocket] Disconnected')
      setIsConnected(false)
      wsRef.current = null

      // Attempt reconnection after delay
      reconnectTimeoutRef.current = setTimeout(() => {
        console.log('[WebSocket] Attempting reconnection...')
        connect()
      }, 5000)
    }

    ws.onerror = (error) => {
      console.error('[WebSocket] Error:', error)
    }

    wsRef.current = ws
  }, [])

  // Connect on mount
  useEffect(() => {
    connect()

    return () => {
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [connect])

  // Subscribe to lock events for a specific graph
  const subscribeLockEvents = useCallback((graphId: string, callback: (msg: BehaviorTreeLockMessage) => void) => {
    if (!lockSubscribersRef.current.has(graphId)) {
      lockSubscribersRef.current.set(graphId, new Set())
    }
    lockSubscribersRef.current.get(graphId)!.add(callback)

    // Return unsubscribe function
    return () => {
      const subscribers = lockSubscribersRef.current.get(graphId)
      if (subscribers) {
        subscribers.delete(callback)
        if (subscribers.size === 0) {
          lockSubscribersRef.current.delete(graphId)
        }
      }
    }
  }, [])

  // Subscribe to sync events for a specific graph
  const subscribeSyncEvents = useCallback((graphId: string, callback: (msg: GraphSyncMessage) => void) => {
    if (!syncSubscribersRef.current.has(graphId)) {
      syncSubscribersRef.current.set(graphId, new Set())
    }
    syncSubscribersRef.current.get(graphId)!.add(callback)

    // Return unsubscribe function
    return () => {
      const subscribers = syncSubscribersRef.current.get(graphId)
      if (subscribers) {
        subscribers.delete(callback)
        if (subscribers.size === 0) {
          syncSubscribersRef.current.delete(graphId)
        }
      }
    }
  }, [])

  return (
    <WebSocketContext.Provider
      value={{
        isConnected,
        lastMessage,
        subscribeLockEvents,
        subscribeSyncEvents,
      }}
    >
      {children}
    </WebSocketContext.Provider>
  )
}

export function useWebSocket() {
  const context = useContext(WebSocketContext)
  if (!context) {
    throw new Error('useWebSocket must be used within a WebSocketProvider')
  }
  return context
}

// Optional hook for components that might be used outside the provider
export function useWebSocketOptional(): WebSocketContextValue | null {
  return useContext(WebSocketContext)
}

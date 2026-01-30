import { createContext, useContext, useState, useCallback, ReactNode } from 'react'
import type { RobotTelemetry } from '../types'

export interface CapturedTelemetry {
  type: string  // e.g., 'joint_state', 'odometry.pose', 'odometry.twist'
  value: unknown
  capturedAt: Date
  robotId?: string
}

interface TelemetryContextValue {
  capturedValue: CapturedTelemetry | null
  setCapturedValue: (value: CapturedTelemetry | null) => void
  clearCapturedValue: () => void
  isPanelOpen: boolean
  setIsPanelOpen: (open: boolean) => void
  togglePanel: () => void
  // Live telemetry support
  selectedRobotId: string | null
  setSelectedRobotId: (robotId: string | null) => void
  liveTelemetry: RobotTelemetry | null
  setLiveTelemetry: (telemetry: RobotTelemetry | null) => void
}

const TelemetryContext = createContext<TelemetryContextValue | null>(null)

export function TelemetryProvider({ children }: { children: ReactNode }) {
  const [capturedValue, setCapturedValue] = useState<CapturedTelemetry | null>(null)
  const [isPanelOpen, setIsPanelOpen] = useState(false)
  const [selectedRobotId, setSelectedRobotId] = useState<string | null>(null)
  const [liveTelemetry, setLiveTelemetry] = useState<RobotTelemetry | null>(null)

  const clearCapturedValue = useCallback(() => {
    setCapturedValue(null)
  }, [])

  const togglePanel = useCallback(() => {
    setIsPanelOpen((prev) => !prev)
  }, [])

  return (
    <TelemetryContext.Provider
      value={{
        capturedValue,
        setCapturedValue,
        clearCapturedValue,
        isPanelOpen,
        setIsPanelOpen,
        togglePanel,
        selectedRobotId,
        setSelectedRobotId,
        liveTelemetry,
        setLiveTelemetry,
      }}
    >
      {children}
    </TelemetryContext.Provider>
  )
}

export function useTelemetry() {
  const context = useContext(TelemetryContext)
  if (!context) {
    throw new Error('useTelemetry must be used within a TelemetryProvider')
  }
  return context
}

// Optional hook that returns null outside provider (for components that might be used both inside and outside)
export function useTelemetryOptional(): TelemetryContextValue | null {
  return useContext(TelemetryContext)
}

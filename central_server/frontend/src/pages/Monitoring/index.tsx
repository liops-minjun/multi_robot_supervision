import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Bot, Workflow, Wifi, WifiOff, ChevronRight, RefreshCw, Server, Play, Loader2, Circle, CheckCircle, XCircle, Clock, AlertTriangle } from 'lucide-react'
import { robotApi, actionGraphApi, agentApi, fleetApi } from '../../api/client'
import type { Robot, Agent, ExecutionPhase, RobotStateSnapshot } from '../../types'
import { useTranslation } from '../../i18n'

// Execution status component
function ExecutionStatusIndicator({ phase, size = 'sm' }: { phase: ExecutionPhase | string | undefined; size?: 'sm' | 'md' }) {
  const config: Record<string, { bg: string; text: string; icon: React.ReactNode; label: string }> = {
    idle: { bg: 'bg-gray-500/20', text: 'text-secondary', icon: <Circle className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />, label: 'Idle' },
    offline: { bg: 'bg-gray-600/20', text: 'text-muted', icon: <XCircle className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />, label: 'Offline' },
    starting: { bg: 'bg-yellow-500/20', text: 'text-yellow-400', icon: <Loader2 className={`${size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} animate-spin`} />, label: 'Starting' },
    executing: { bg: 'bg-blue-500/20', text: 'text-blue-400', icon: <Play className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />, label: 'Executing' },
    completing: { bg: 'bg-green-500/20', text: 'text-green-400', icon: <CheckCircle className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />, label: 'Completing' },
    waiting_for_precondition: { bg: 'bg-orange-500/20', text: 'text-orange-400', icon: <Clock className={`${size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} animate-pulse`} />, label: 'Waiting' },
  }
  const c = config[phase || 'idle'] || config.idle

  return (
    <span className={`flex items-center gap-1 px-1.5 py-0.5 rounded ${c.bg} ${c.text} text-[9px] font-medium`}>
      {c.icon}
      {c.label}
    </span>
  )
}

// Blocking conditions display component
function BlockingConditionsDisplay({ conditions, compact = false }: {
  conditions?: Array<{
    condition_id: string
    description: string
    target_agent_id?: string
    target_agent_name?: string
    required_state: string
    current_state?: string
    reason: string
  }>
  compact?: boolean
}) {
  if (!conditions || conditions.length === 0) return null

  if (compact) {
    return (
      <div className="flex items-center gap-1 text-orange-400 text-[10px]">
        <AlertTriangle className="w-3 h-3" />
        <span>Waiting: {conditions[0]?.target_agent_name || conditions[0]?.target_agent_id}</span>
      </div>
    )
  }

  return (
    <div className="mt-2 p-2 bg-orange-500/10 border border-orange-500/30 rounded-lg">
      <div className="flex items-center gap-1.5 text-orange-400 text-xs font-medium mb-2">
        <Clock className="w-3.5 h-3.5" />
        <span>Waiting for Preconditions</span>
      </div>
      <div className="space-y-1.5">
        {conditions.map((condition, idx) => (
          <div key={condition.condition_id || idx} className="flex items-start gap-2 text-[11px]">
            <AlertTriangle className="w-3 h-3 text-orange-400 mt-0.5 flex-shrink-0" />
            <div className="flex-1 min-w-0">
              <div className="text-secondary">{condition.description}</div>
              {condition.target_agent_name && (
                <div className="text-muted mt-0.5">
                  Target: <span className="text-orange-300">{condition.target_agent_name}</span>
                  {condition.current_state && (
                    <span className="ml-2">
                      (Current: <span className="text-secondary">{condition.current_state}</span> → Need: <span className="text-green-400">{condition.required_state}</span>)
                    </span>
                  )}
                </div>
              )}
              <div className="text-muted text-[10px] mt-0.5">{condition.reason}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

export default function Monitoring() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selectedRobot, setSelectedRobot] = useState<Robot | null>(null)
  const [selectedActionGraphId, setSelectedActionGraphId] = useState<string | null>(null)
  const [executingGraphId, setExecutingGraphId] = useState<string | null>(null)

  // Fetch all robots
  const { data: robots = [], isLoading: robotsLoading, refetch: refetchRobots } = useQuery({
    queryKey: ['robots'],
    queryFn: () => robotApi.list(),
    refetchInterval: 5000, // Auto refresh every 5 seconds
  })

  // Fetch all agents
  const { data: agents = [] } = useQuery({
    queryKey: ['agents'],
    queryFn: () => agentApi.list(),
    refetchInterval: 5000,
  })

  // Fetch fleet state for execution phase info
  const { data: fleetState } = useQuery({
    queryKey: ['fleet-state'],
    queryFn: () => fleetApi.getState(),
    refetchInterval: 1000, // 1s for real-time updates
    refetchIntervalInBackground: true,
  })

  // Get robot state snapshot with execution phase
  const getRobotStateSnapshot = (robotId: string): RobotStateSnapshot | undefined => {
    if (!fleetState?.robots) return undefined
    return fleetState.robots[robotId]
  }

  // Fetch all action graphs
  const { data: actionGraphs = [] } = useQuery({
    queryKey: ['actionGraphs'],
    queryFn: () => actionGraphApi.list(),
  })

  // Get agent by ID
  const getAgent = (agentId: string | null): Agent | undefined => {
    if (!agentId) return undefined
    return agents.find(a => a.id === agentId)
  }

  // Group robots by agent
  const robotsByAgent = robots.reduce((acc, robot) => {
    const agentId = robot.agent_id || 'unassigned'
    if (!acc[agentId]) acc[agentId] = []
    acc[agentId].push(robot)
    return acc
  }, {} as Record<string, Robot[]>)

  // Get action graphs for robot's agent
  const getActionGraphsForAgent = (agentId: string | null) => {
    if (!agentId) return []
    return actionGraphs.filter(f => f.agent_id === agentId)
  }

  // Execute action graph mutation
  const executeMutation = useMutation({
    mutationFn: ({ graphId, agentId }: { graphId: string; agentId: string }) =>
      actionGraphApi.execute(graphId, agentId),
    onMutate: ({ graphId }) => {
      setExecutingGraphId(graphId)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      setExecutingGraphId(null)
    },
    onError: () => {
      setExecutingGraphId(null)
    },
  })

  return (
    <div className="h-screen flex bg-base">
      {/* Left Panel - Robot Agents */}
      <div className="w-80 bg-surface border-r border-primary flex flex-col">
        <div className="p-4 border-b border-primary flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Bot className="w-5 h-5 text-blue-400" />
            <h2 className="font-semibold text-primary">{t('monitoring.title')}</h2>
          </div>
          <button
            onClick={() => refetchRobots()}
            className="p-1.5 text-muted hover:text-primary hover:bg-elevated rounded transition-colors"
          >
            <RefreshCw size={16} />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {robotsLoading ? (
            <div className="p-4 text-center text-muted">{t('monitoring.loading')}</div>
          ) : Object.keys(robotsByAgent).length === 0 ? (
            <div className="p-4 text-center">
              <Bot className="w-12 h-12 mx-auto mb-3 text-muted" />
              <p className="text-muted text-sm">{t('monitoring.noRobots')}</p>
            </div>
          ) : (
            Object.entries(robotsByAgent).map(([agentId, agentRobots]) => {
              const agent = getAgent(agentId)
              const isOnline = agent?.status === 'online'
              return (
              <div key={agentId} className="border-b border-primary">
                {/* Agent Header */}
                <div className="px-4 py-2 bg-elevated flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Server className={`w-3 h-3 ${isOnline ? 'text-green-500' : 'text-muted'}`} />
                    <span className="text-xs font-semibold text-secondary uppercase tracking-wider">
                      {agent?.name || agentId}
                    </span>
                    {agent?.ip_address && (
                      <span className="text-[10px] text-muted font-mono">
                        {agent.ip_address}
                      </span>
                    )}
                  </div>
                </div>

                {/* Robots */}
                {agentRobots.map(robot => {
                  const stateSnapshot = getRobotStateSnapshot(robot.id)
                  const executionPhase = stateSnapshot?.execution_phase || (robot.is_online ? 'idle' : 'offline')
                  const isWaitingForPrecondition = executionPhase === 'waiting_for_precondition' || stateSnapshot?.is_waiting_for_precondition

                  return (
                    <div
                      key={robot.id}
                      onClick={() => setSelectedRobot(robot)}
                      className={`px-4 py-3 flex items-center gap-3 cursor-pointer transition-colors ${
                        selectedRobot?.id === robot.id
                          ? 'bg-blue-600/20 border-l-2 border-blue-500'
                          : isWaitingForPrecondition
                            ? 'bg-orange-500/5 hover:bg-orange-500/10'
                            : 'hover:bg-elevated'
                      }`}
                    >
                      {/* Online Status */}
                      <div className="flex-shrink-0">
                        {robot.is_online ? (
                          <Wifi className="w-4 h-4 text-green-500" />
                        ) : (
                          <WifiOff className="w-4 h-4 text-muted" />
                        )}
                      </div>

                      {/* Robot Info */}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium text-primary truncate">
                            {robot.name}
                          </span>
                          {/* Execution phase indicator */}
                          <ExecutionStatusIndicator phase={executionPhase} />
                        </div>
                        <div className="flex items-center gap-2 mt-0.5">
                          <span className="text-[10px] text-muted font-mono">{robot.ip_address}</span>
                          {robot.is_online && (stateSnapshot?.current_state || stateSnapshot?.state_code) && (
                            <span className="text-[10px] text-muted">{stateSnapshot.current_state || stateSnapshot.state_code}</span>
                          )}
                        </div>
                        {/* Compact blocking conditions display */}
                        {isWaitingForPrecondition && stateSnapshot?.blocking_conditions && (
                          <BlockingConditionsDisplay conditions={stateSnapshot.blocking_conditions} compact />
                        )}
                      </div>

                      <ChevronRight className="w-4 h-4 text-muted" />
                    </div>
                  )
                })}
              </div>
            )})
          )}
        </div>

        {/* Summary */}
        <div className="p-4 border-t border-primary bg-elevated">
          <div className="grid grid-cols-2 gap-4 text-center">
            <div>
              <div className="text-2xl font-bold text-green-400">
                {robots.filter(r => r.is_online).length}
              </div>
              <div className="text-[10px] text-muted uppercase">{t('agent.online')}</div>
            </div>
            <div>
              <div className="text-2xl font-bold text-secondary">
                {robots.filter(r => !r.is_online).length}
              </div>
              <div className="text-[10px] text-muted uppercase">{t('agent.offline')}</div>
            </div>
          </div>
        </div>
      </div>

      {/* Right Panel - Robot Detail & Workflows */}
      <div className="flex-1 flex flex-col">
        {selectedRobot ? (
          <>
            {/* Robot Header */}
            <div className="bg-surface border-b border-primary p-4">
              {(() => {
                const stateSnapshot = getRobotStateSnapshot(selectedRobot.id)
                const executionPhase = stateSnapshot?.execution_phase || (selectedRobot.is_online ? 'idle' : 'offline')
                const isWaitingForPrecondition = executionPhase === 'waiting_for_precondition' || stateSnapshot?.is_waiting_for_precondition

                return (
                  <div>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <div className={`w-3 h-3 rounded-full ${
                          isWaitingForPrecondition ? 'bg-orange-500 animate-pulse' :
                          selectedRobot.is_online ? 'bg-green-500' : 'bg-muted'
                        }`} />
                        <div>
                          <h2 className="text-lg font-semibold text-primary">{selectedRobot.name}</h2>
                          <p className="text-sm text-muted">
                            {getAgent(selectedRobot.agent_id)?.name || 'Unassigned'} • {selectedRobot.ip_address}
                          </p>
                        </div>
                      </div>
                      <div className="flex items-center gap-4">
                        {/* Execution Status */}
                        <div className="text-right">
                          <div className="text-xs text-muted mb-1">{t('monitoring.currentState')}</div>
                          <ExecutionStatusIndicator phase={executionPhase} size="md" />
                        </div>

                        {/* State Code */}
                        {selectedRobot.is_online && (
                          <div className="text-right">
                            <div className="text-xs text-muted">State Code</div>
                            <div
                              className="text-sm font-medium"
                              style={{ color: getStateColor(stateSnapshot?.current_state || stateSnapshot?.state_code || selectedRobot.current_state) }}
                            >
                              {stateSnapshot?.current_state || stateSnapshot?.state_code || 'idle'}
                            </div>
                          </div>
                        )}

                        {/* Current Graph */}
                        {stateSnapshot?.current_graph_id && (
                          <div className="text-right">
                            <div className="text-xs text-muted">Graph</div>
                            <div className="text-sm font-medium text-blue-400">
                              {actionGraphs.find(g => g.id === stateSnapshot.current_graph_id)?.name
                                || stateSnapshot.current_graph_id.slice(0, 8)}
                            </div>
                          </div>
                        )}

                        {/* Waiting Time */}
                        {isWaitingForPrecondition && stateSnapshot?.waiting_for_precondition_since && (
                          <div className="text-right">
                            <div className="text-xs text-muted">Waiting Since</div>
                            <div className="text-sm font-medium text-orange-400">
                              {formatWaitingTime(stateSnapshot.waiting_for_precondition_since)}
                            </div>
                          </div>
                        )}
                      </div>
                    </div>

                    {/* Full Blocking Conditions Display */}
                    {isWaitingForPrecondition && stateSnapshot?.blocking_conditions && (
                      <BlockingConditionsDisplay conditions={stateSnapshot.blocking_conditions} />
                    )}
                  </div>
                )
              })()}
            </div>

            {/* Available Workflows */}
            <div className="flex-1 overflow-auto p-4">
              <div className="flex items-center gap-2 mb-4">
                <Workflow className="w-4 h-4 text-blue-400" />
                <h3 className="text-sm font-semibold text-primary">{t('monitoring.availableWorkflows')}</h3>
                <span className="text-xs text-muted">
                  ({getActionGraphsForAgent(selectedRobot.agent_id).length})
                </span>
              </div>

              {getActionGraphsForAgent(selectedRobot.agent_id).length === 0 ? (
                <div className="text-center py-12">
                  <Workflow className="w-12 h-12 mx-auto mb-3 text-muted" />
                  <p className="text-muted">{t('monitoring.noWorkflows')}</p>
                  <p className="text-xs text-muted mt-1">
                    {t('monitoring.createWorkflowsHint')}
                  </p>
                </div>
              ) : (
                <div className="grid grid-cols-2 gap-4">
                  {getActionGraphsForAgent(selectedRobot.agent_id).map(actionGraph => (
                    <div
                      key={actionGraph.id}
                      onClick={() => setSelectedActionGraphId(actionGraph.id)}
                      className={`bg-surface rounded-lg border cursor-pointer transition-all ${
                        selectedActionGraphId === actionGraph.id
                          ? 'border-blue-500 ring-1 ring-blue-500/50'
                          : 'border-primary hover:border-secondary'
                      }`}
                    >
                      <div className="p-4">
                        <div className="flex items-center justify-between mb-2">
                          <h4 className="font-medium text-primary">{actionGraph.name}</h4>
                          <span className="text-xs text-muted">v{actionGraph.version}</span>
                        </div>
                        {actionGraph.description && (
                          <p className="text-xs text-muted mb-3 line-clamp-2">
                            {actionGraph.description}
                          </p>
                        )}
                        <div className="flex items-center justify-between text-xs">
                          <span className="text-muted">
                            {t('monitoring.actionGraphId')}: {actionGraph.id.slice(0, 8)}...
                          </span>
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              if (selectedRobot?.agent_id) {
                                executeMutation.mutate({
                                  graphId: actionGraph.id,
                                  agentId: selectedRobot.agent_id,
                                })
                              }
                            }}
                            disabled={executingGraphId === actionGraph.id || !selectedRobot?.agent_id}
                            className="flex items-center gap-1 px-2 py-1 bg-green-500/20 text-green-400 rounded hover:bg-green-500/30 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                          >
                            {executingGraphId === actionGraph.id ? (
                              <Loader2 className="w-3 h-3 animate-spin" />
                            ) : (
                              <Play className="w-3 h-3" />
                            )}
                            {t('monitoring.execute')}
                          </button>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center">
              <Bot className="w-16 h-16 mx-auto mb-4 text-muted" />
              <h3 className="text-lg font-medium text-secondary mb-2">{t('monitoring.selectRobot')}</h3>
              <p className="text-sm text-muted">
                {t('monitoring.selectRobotHint')}
              </p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

// Helper function for state colors
function getStateColor(state: string): string {
  const colors: Record<string, string> = {
    idle: '#22c55e',
    error: '#ef4444',
    navigating: '#3b82f6',
    moving_arm: '#8b5cf6',
    gripping: '#f59e0b',
    waiting: '#6b7280',
    waiting_confirm: '#eab308',
  }
  return colors[state] || '#6b7280'
}

// Helper function to format waiting time
function formatWaitingTime(isoTimestamp: string): string {
  try {
    const waitingSince = new Date(isoTimestamp)
    const now = new Date()
    const diffMs = now.getTime() - waitingSince.getTime()
    const diffSec = Math.floor(diffMs / 1000)

    if (diffSec < 60) {
      return `${diffSec}s`
    }
    const diffMin = Math.floor(diffSec / 60)
    const remainingSec = diffSec % 60
    if (diffMin < 60) {
      return `${diffMin}m ${remainingSec}s`
    }
    const diffHour = Math.floor(diffMin / 60)
    const remainingMin = diffMin % 60
    return `${diffHour}h ${remainingMin}m`
  } catch {
    return isoTimestamp
  }
}

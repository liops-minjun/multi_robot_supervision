import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Bot, Workflow, Wifi, WifiOff, ChevronRight, RefreshCw, Server, Play, Loader2, Circle, CheckCircle, XCircle } from 'lucide-react'
import { robotApi, actionGraphApi, agentApi, fleetApi } from '../../api/client'
import type { Robot, Agent, ExecutionPhase, RobotStateSnapshot } from '../../types'
import { useTranslation } from '../../i18n'

// Execution status component
function ExecutionStatusIndicator({ phase, size = 'sm' }: { phase: ExecutionPhase | string | undefined; size?: 'sm' | 'md' }) {
  const config: Record<string, { bg: string; text: string; icon: React.ReactNode; label: string }> = {
    idle: { bg: 'bg-gray-500/20', text: 'text-gray-400', icon: <Circle className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />, label: 'Idle' },
    offline: { bg: 'bg-gray-600/20', text: 'text-gray-500', icon: <XCircle className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />, label: 'Offline' },
    starting: { bg: 'bg-yellow-500/20', text: 'text-yellow-400', icon: <Loader2 className={`${size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} animate-spin`} />, label: 'Starting' },
    executing: { bg: 'bg-blue-500/20', text: 'text-blue-400', icon: <Play className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />, label: 'Executing' },
    completing: { bg: 'bg-green-500/20', text: 'text-green-400', icon: <CheckCircle className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />, label: 'Completing' },
  }
  const c = config[phase || 'idle'] || config.idle

  return (
    <span className={`flex items-center gap-1 px-1.5 py-0.5 rounded ${c.bg} ${c.text} text-[9px] font-medium`}>
      {c.icon}
      {c.label}
    </span>
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
    <div className="h-screen flex bg-[#0f0f1a]">
      {/* Left Panel - Robot Agents */}
      <div className="w-80 bg-[#16162a] border-r border-[#2a2a4a] flex flex-col">
        <div className="p-4 border-b border-[#2a2a4a] flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Bot className="w-5 h-5 text-blue-400" />
            <h2 className="font-semibold text-white">{t('monitoring.title')}</h2>
          </div>
          <button
            onClick={() => refetchRobots()}
            className="p-1.5 text-gray-500 hover:text-white hover:bg-[#2a2a4a] rounded transition-colors"
          >
            <RefreshCw size={16} />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {robotsLoading ? (
            <div className="p-4 text-center text-gray-500">{t('monitoring.loading')}</div>
          ) : Object.keys(robotsByAgent).length === 0 ? (
            <div className="p-4 text-center">
              <Bot className="w-12 h-12 mx-auto mb-3 text-gray-600" />
              <p className="text-gray-500 text-sm">{t('monitoring.noRobots')}</p>
            </div>
          ) : (
            Object.entries(robotsByAgent).map(([agentId, agentRobots]) => {
              const agent = getAgent(agentId)
              const isOnline = agent?.status === 'online'
              return (
              <div key={agentId} className="border-b border-[#2a2a4a]">
                {/* Agent Header */}
                <div className="px-4 py-2 bg-[#1a1a2e] flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Server className={`w-3 h-3 ${isOnline ? 'text-green-500' : 'text-gray-600'}`} />
                    <span className="text-xs font-semibold text-gray-400 uppercase tracking-wider">
                      {agent?.name || agentId}
                    </span>
                    {agent?.ip_address && (
                      <span className="text-[10px] text-gray-600 font-mono">
                        {agent.ip_address}
                      </span>
                    )}
                  </div>
                </div>

                {/* Robots */}
                {agentRobots.map(robot => {
                  const stateSnapshot = getRobotStateSnapshot(robot.id)
                  const executionPhase = stateSnapshot?.execution_phase || (robot.is_online ? 'idle' : 'offline')

                  return (
                    <div
                      key={robot.id}
                      onClick={() => setSelectedRobot(robot)}
                      className={`px-4 py-3 flex items-center gap-3 cursor-pointer transition-colors ${
                        selectedRobot?.id === robot.id
                          ? 'bg-blue-600/20 border-l-2 border-blue-500'
                          : 'hover:bg-[#1a1a2e]'
                      }`}
                    >
                      {/* Online Status */}
                      <div className="flex-shrink-0">
                        {robot.is_online ? (
                          <Wifi className="w-4 h-4 text-green-500" />
                        ) : (
                          <WifiOff className="w-4 h-4 text-gray-600" />
                        )}
                      </div>

                      {/* Robot Info */}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium text-white truncate">
                            {robot.name}
                          </span>
                          {/* Execution phase indicator */}
                          <ExecutionStatusIndicator phase={executionPhase} />
                        </div>
                        <div className="flex items-center gap-2 mt-0.5">
                          <span className="text-[10px] text-gray-600 font-mono">{robot.ip_address}</span>
                          {robot.is_online && stateSnapshot?.state_code && (
                            <span className="text-[10px] text-gray-500">{stateSnapshot.state_code}</span>
                          )}
                        </div>
                      </div>

                      <ChevronRight className="w-4 h-4 text-gray-600" />
                    </div>
                  )
                })}
              </div>
            )})
          )}
        </div>

        {/* Summary */}
        <div className="p-4 border-t border-[#2a2a4a] bg-[#1a1a2e]">
          <div className="grid grid-cols-2 gap-4 text-center">
            <div>
              <div className="text-2xl font-bold text-green-400">
                {robots.filter(r => r.is_online).length}
              </div>
              <div className="text-[10px] text-gray-500 uppercase">{t('agent.online')}</div>
            </div>
            <div>
              <div className="text-2xl font-bold text-gray-400">
                {robots.filter(r => !r.is_online).length}
              </div>
              <div className="text-[10px] text-gray-500 uppercase">{t('agent.offline')}</div>
            </div>
          </div>
        </div>
      </div>

      {/* Right Panel - Robot Detail & Workflows */}
      <div className="flex-1 flex flex-col">
        {selectedRobot ? (
          <>
            {/* Robot Header */}
            <div className="bg-[#16162a] border-b border-[#2a2a4a] p-4">
              {(() => {
                const stateSnapshot = getRobotStateSnapshot(selectedRobot.id)
                const executionPhase = stateSnapshot?.execution_phase || (selectedRobot.is_online ? 'idle' : 'offline')

                return (
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                      <div className={`w-3 h-3 rounded-full ${selectedRobot.is_online ? 'bg-green-500' : 'bg-gray-600'}`} />
                      <div>
                        <h2 className="text-lg font-semibold text-white">{selectedRobot.name}</h2>
                        <p className="text-sm text-gray-500">
                          {getAgent(selectedRobot.agent_id)?.name || 'Unassigned'} • {selectedRobot.ip_address}
                        </p>
                      </div>
                    </div>
                    <div className="flex items-center gap-4">
                      {/* Execution Status */}
                      <div className="text-right">
                        <div className="text-xs text-gray-500 mb-1">{t('monitoring.currentState')}</div>
                        <ExecutionStatusIndicator phase={executionPhase} size="md" />
                      </div>

                      {/* State Code */}
                      {selectedRobot.is_online && (
                        <div className="text-right">
                          <div className="text-xs text-gray-500">State Code</div>
                          <div
                            className="text-sm font-medium"
                            style={{ color: getStateColor(stateSnapshot?.state_code || selectedRobot.current_state) }}
                          >
                            {stateSnapshot?.state_code || selectedRobot.current_state || 'idle'}
                          </div>
                        </div>
                      )}

                      {/* Current Graph */}
                      {stateSnapshot?.current_graph_id && (
                        <div className="text-right">
                          <div className="text-xs text-gray-500">Graph</div>
                          <div className="text-sm font-medium text-blue-400">
                            {actionGraphs.find(g => g.id === stateSnapshot.current_graph_id)?.name
                              || stateSnapshot.current_graph_id.slice(0, 8)}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                )
              })()}
            </div>

            {/* Available Workflows */}
            <div className="flex-1 overflow-auto p-4">
              <div className="flex items-center gap-2 mb-4">
                <Workflow className="w-4 h-4 text-blue-400" />
                <h3 className="text-sm font-semibold text-white">{t('monitoring.availableWorkflows')}</h3>
                <span className="text-xs text-gray-600">
                  ({getActionGraphsForAgent(selectedRobot.agent_id).length})
                </span>
              </div>

              {getActionGraphsForAgent(selectedRobot.agent_id).length === 0 ? (
                <div className="text-center py-12">
                  <Workflow className="w-12 h-12 mx-auto mb-3 text-gray-600" />
                  <p className="text-gray-500">{t('monitoring.noWorkflows')}</p>
                  <p className="text-xs text-gray-600 mt-1">
                    {t('monitoring.createWorkflowsHint')}
                  </p>
                </div>
              ) : (
                <div className="grid grid-cols-2 gap-4">
                  {getActionGraphsForAgent(selectedRobot.agent_id).map(actionGraph => (
                    <div
                      key={actionGraph.id}
                      onClick={() => setSelectedActionGraphId(actionGraph.id)}
                      className={`bg-[#16162a] rounded-lg border cursor-pointer transition-all ${
                        selectedActionGraphId === actionGraph.id
                          ? 'border-blue-500 ring-1 ring-blue-500/50'
                          : 'border-[#2a2a4a] hover:border-[#3a3a5a]'
                      }`}
                    >
                      <div className="p-4">
                        <div className="flex items-center justify-between mb-2">
                          <h4 className="font-medium text-white">{actionGraph.name}</h4>
                          <span className="text-xs text-gray-500">v{actionGraph.version}</span>
                        </div>
                        {actionGraph.description && (
                          <p className="text-xs text-gray-500 mb-3 line-clamp-2">
                            {actionGraph.description}
                          </p>
                        )}
                        <div className="flex items-center justify-between text-xs">
                          <span className="text-gray-600">
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
              <Bot className="w-16 h-16 mx-auto mb-4 text-gray-600" />
              <h3 className="text-lg font-medium text-gray-400 mb-2">{t('monitoring.selectRobot')}</h3>
              <p className="text-sm text-gray-600">
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

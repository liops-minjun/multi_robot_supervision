import { useState, useEffect, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Server,
  Wifi,
  WifiOff,
  Activity,
  RefreshCw,
  ChevronRight,
  Zap,
  Clock,
  Layout,
  Heart,
  HeartPulse,
  Play,
  Pause,
  Square,
  Loader2,
  CheckCircle,
  XCircle,
  Circle,
  Terminal,
  ChevronDown,
  ChevronUp,
  RotateCcw,
  AlertTriangle,
} from 'lucide-react'
import { agentApi, actionGraphApi, fleetApi, stateDefinitionApi, taskApi, logsApi } from '../../api/client'
import type { AgentCapabilityInfo, AgentConnectionStatus, ActionGraph, RobotStateSnapshot, StateDefinition, ExecutionPhase, TaskLogEntry, TaskLogLevel } from '../../types'
import ActionGraphViewer from '../../components/ActionGraphViewer'
import { useTranslation } from '../../i18n'

// Status badge component
function StatusBadge({ status, t }: { status: string; t: (key: 'agent.online' | 'agent.offline' | 'agent.warning') => string }) {
  const config: Record<string, { bg: string; text: string; dot: string; labelKey: 'agent.online' | 'agent.offline' | 'agent.warning' }> = {
    online: { bg: 'bg-green-500/20', text: 'text-green-400', dot: 'bg-green-500', labelKey: 'agent.online' },
    offline: { bg: 'bg-gray-500/20', text: 'text-gray-400', dot: 'bg-gray-500', labelKey: 'agent.offline' },
    warning: { bg: 'bg-yellow-500/20', text: 'text-yellow-400', dot: 'bg-yellow-500', labelKey: 'agent.warning' },
  }
  const c = config[status] || config.offline

  return (
    <span className={`flex items-center gap-1.5 px-2 py-1 rounded-full ${c.bg} ${c.text} text-xs`}>
      <div className={`w-2 h-2 rounded-full ${c.dot}`} />
      {t(c.labelKey)}
    </span>
  )
}

// Heartbeat badge component
function HeartbeatBadge({
  health,
  ageMs,
  t
}: {
  health: 'healthy' | 'warning' | 'critical' | string
  ageMs: number
  t: (key: 'agent.heartbeatHealthy' | 'agent.heartbeatWarning' | 'agent.heartbeatCritical' | 'agent.heartbeatAge') => string
}) {
  const config: Record<string, { bg: string; text: string; icon: string; labelKey: 'agent.heartbeatHealthy' | 'agent.heartbeatWarning' | 'agent.heartbeatCritical' }> = {
    healthy: { bg: 'bg-green-500/20', text: 'text-green-400', icon: 'pulse', labelKey: 'agent.heartbeatHealthy' },
    warning: { bg: 'bg-yellow-500/20', text: 'text-yellow-400', icon: 'slow', labelKey: 'agent.heartbeatWarning' },
    critical: { bg: 'bg-red-500/20', text: 'text-red-400', icon: 'dead', labelKey: 'agent.heartbeatCritical' },
  }
  const c = config[health] || config.critical

  // Format age
  const formatAge = (ms: number): string => {
    if (ms < 1000) return `${ms}ms`
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
    return `${Math.floor(ms / 60000)}m ${Math.floor((ms % 60000) / 1000)}s`
  }

  return (
    <span className={`flex items-center gap-1.5 px-2 py-1 rounded-full ${c.bg} ${c.text} text-xs`}>
      {health === 'healthy' ? (
        <HeartPulse className="w-3 h-3" />
      ) : (
        <Heart className="w-3 h-3" />
      )}
      <span>{t(c.labelKey)}</span>
      <span className="opacity-75">({formatAge(ageMs)})</span>
    </span>
  )
}

// Execution Phase badge component - shows explicit task execution state
function ExecutionPhaseBadge({
  phase,
  currentStepName,
  graphName,
  blockingConditions,
}: {
  phase: ExecutionPhase | string | undefined
  currentStepName?: string | null
  graphName?: string | null
  blockingConditions?: Array<{
    condition_id: string
    description: string
    target_agent_id?: string
    target_agent_name?: string
    required_state: string
    current_state?: string
    reason: string
  }> | null
}) {
  const config: Record<string, { bg: string; text: string; icon: React.ReactNode; label: string }> = {
    idle: {
      bg: 'bg-gray-500/20',
      text: 'text-gray-400',
      icon: <Circle className="w-3 h-3" />,
      label: 'Idle'
    },
    offline: {
      bg: 'bg-gray-600/20',
      text: 'text-gray-500',
      icon: <XCircle className="w-3 h-3" />,
      label: 'Offline'
    },
    starting: {
      bg: 'bg-yellow-500/20',
      text: 'text-yellow-400',
      icon: <Loader2 className="w-3 h-3 animate-spin" />,
      label: 'Starting...'
    },
    executing: {
      bg: 'bg-blue-500/20',
      text: 'text-blue-400',
      icon: <Play className="w-3 h-3" />,
      label: 'Executing'
    },
    completing: {
      bg: 'bg-green-500/20',
      text: 'text-green-400',
      icon: <CheckCircle className="w-3 h-3" />,
      label: 'Completing'
    },
    waiting_for_precondition: {
      bg: 'bg-orange-500/20',
      text: 'text-orange-400',
      icon: <Clock className="w-3 h-3 animate-pulse" />,
      label: 'Waiting'
    },
  }

  const c = config[phase || 'idle'] || config.idle

  return (
    <div className="flex flex-col gap-2">
      <div className={`flex items-center gap-2 px-3 py-2 rounded-lg ${c.bg}`}>
        <div className={`flex items-center gap-1.5 ${c.text}`}>
          {c.icon}
          <span className="font-medium">{c.label}</span>
        </div>
        {(phase === 'executing' || phase === 'starting') && graphName && (
          <span className="text-xs text-gray-500">
            {graphName}
            {currentStepName && <span className="text-gray-400"> / {currentStepName}</span>}
          </span>
        )}
        {phase === 'waiting_for_precondition' && blockingConditions && blockingConditions.length > 0 && (
          <span className="text-xs text-orange-300">
            {blockingConditions[0].target_agent_name || blockingConditions[0].target_agent_id}
          </span>
        )}
      </div>
      {/* Detailed blocking conditions */}
      {phase === 'waiting_for_precondition' && blockingConditions && blockingConditions.length > 0 && (
        <div className="px-3 py-2 bg-orange-500/10 border border-orange-500/20 rounded-lg">
          <div className="flex items-center gap-1.5 text-orange-400 text-[10px] font-medium mb-1.5">
            <AlertTriangle className="w-3 h-3" />
            <span>Waiting for Preconditions</span>
          </div>
          <div className="space-y-1">
            {blockingConditions.map((condition, idx) => (
              <div key={condition.condition_id || idx} className="text-[11px] text-gray-300">
                <span className="text-gray-500">{condition.description}</span>
                {condition.target_agent_name && (
                  <span className="ml-1 text-orange-300">({condition.target_agent_name})</span>
                )}
                {condition.current_state && (
                  <span className="ml-1 text-gray-500">
                    {condition.current_state} → <span className="text-green-400">{condition.required_state}</span>
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// Capability card component
function CapabilityCard({
  capability,
  expanded,
  onToggle,
  inUseByStep,
  t,
}: {
  capability: AgentCapabilityInfo
  expanded: boolean
  onToggle: () => void
  inUseByStep?: { id: string; name: string } | null
  t: (key: 'agent.available' | 'agent.unavailable' | 'agent.actionServer' | 'common.status' | 'common.type') => string
}) {
  // action_server를 주요 이름으로 사용 (앞의 / 제거)
  const serverName = capability.action_server.replace(/^\//, '')
  // action_type에서 짧은 이름 추출 (예: test_msgs/TestAction -> TestAction)
  const typeName = capability.action_type.split('/').pop() || capability.action_type

  return (
    <div className="bg-[#1a1a2e] rounded-lg border border-[#2a2a4a] overflow-hidden">
      <button
        onClick={onToggle}
        className="w-full p-4 flex items-center justify-between hover:bg-[#22223a] transition-colors"
      >
        <div className="flex items-center gap-3">
          <Zap className="w-4 h-4 text-purple-400" />
          <div className="flex flex-col items-start">
            <span className="text-white font-medium font-mono">{serverName}</span>
            <span className="text-[10px] text-gray-500">{typeName}</span>
          </div>
          {inUseByStep ? (
            <span className="text-xs px-2 py-0.5 bg-cyan-500/20 text-cyan-400 rounded flex items-center gap-1">
              <Loader2 className="w-3 h-3 animate-spin" />
              사용 중 - {inUseByStep.name}
            </span>
          ) : capability.is_available ? (
            <span className="text-xs px-2 py-0.5 bg-green-500/20 text-green-400 rounded">
              {t('agent.available')}
            </span>
          ) : (
            <span className="text-xs px-2 py-0.5 bg-gray-500/20 text-gray-400 rounded">
              {t('agent.unavailable')}
            </span>
          )}
        </div>
        <ChevronRight
          className={`w-4 h-4 text-gray-500 transition-transform duration-200 ${
            expanded ? 'rotate-90' : ''
          }`}
        />
      </button>

      {expanded && (
        <div className="px-4 pb-4 border-t border-[#2a2a4a]">
          <div className="space-y-3 pt-3">
            {/* Action Server (전체 경로) */}
            <div>
              <div className="text-xs text-gray-400 uppercase tracking-wider mb-1">{t('agent.actionServer')}</div>
              <div className="flex items-center gap-2 px-3 py-2 bg-[#16162a] rounded-lg">
                <Activity className="w-3 h-3 text-blue-400" />
                <span className="text-sm text-gray-300 font-mono">{capability.action_server}</span>
              </div>
            </div>
            {/* Action Type (전체 경로) */}
            <div>
              <div className="text-xs text-gray-400 uppercase tracking-wider mb-1">{t('common.type')}</div>
              <div className="text-xs text-gray-500 font-mono px-3 py-2 bg-[#16162a] rounded-lg">
                {capability.action_type}
              </div>
            </div>
            {capability.status && (
              <div className="text-xs text-gray-500">
                {t('common.status')}: <span className="text-gray-400">{capability.status}</span>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// Execution Logs Panel component
function ExecutionLogsPanel({
  logs,
  isLoading,
  isExpanded,
  onToggleExpand,
}: {
  logs: TaskLogEntry[]
  isLoading: boolean
  isExpanded: boolean
  onToggleExpand: () => void
}) {
  const logLevelColors: Record<TaskLogLevel, { bg: string; text: string; border: string }> = {
    DEBUG: { bg: 'bg-gray-500/10', text: 'text-gray-400', border: 'border-gray-600' },
    INFO: { bg: 'bg-blue-500/10', text: 'text-blue-400', border: 'border-blue-600' },
    WARN: { bg: 'bg-yellow-500/10', text: 'text-yellow-400', border: 'border-yellow-600' },
    ERROR: { bg: 'bg-red-500/10', text: 'text-red-400', border: 'border-red-600' },
    UNKNOWN: { bg: 'bg-gray-500/10', text: 'text-gray-400', border: 'border-gray-600' },
  }

  const formatTime = (timestamp: string | number) => {
    const date = typeof timestamp === 'number'
      ? new Date(timestamp)
      : new Date(timestamp)
    const time = date.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
    const ms = date.getMilliseconds().toString().padStart(3, '0')
    return `${time}.${ms}`
  }

  return (
    <div className="bg-[#0d0d1a] rounded-lg border border-[#2a2a4a] overflow-hidden">
      {/* Header */}
      <button
        onClick={onToggleExpand}
        className="w-full flex items-center justify-between px-4 py-3 bg-[#16162a] hover:bg-[#1a1a2e] transition-colors"
      >
        <div className="flex items-center gap-2">
          <Terminal className="w-4 h-4 text-green-400" />
          <span className="text-sm font-medium text-white">Execution Logs</span>
          <span className="text-xs text-gray-500">({logs.length})</span>
        </div>
        {isExpanded ? (
          <ChevronUp className="w-4 h-4 text-gray-500" />
        ) : (
          <ChevronDown className="w-4 h-4 text-gray-500" />
        )}
      </button>

      {/* Log content */}
      {isExpanded && (
        <div className="max-h-[300px] overflow-y-auto font-mono text-xs">
          {isLoading ? (
            <div className="flex items-center justify-center py-8 text-gray-500">
              <Loader2 className="w-4 h-4 animate-spin mr-2" />
              Loading logs...
            </div>
          ) : logs.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-8 text-gray-500">
              <Terminal className="w-8 h-8 mb-2 opacity-50" />
              <p>No execution logs yet</p>
              <p className="text-[10px] mt-1">Logs will appear here during task execution</p>
            </div>
          ) : (
            <div className="divide-y divide-[#1a1a2e]">
              {logs.map((log, index) => {
                const colors = logLevelColors[log.level_str] || logLevelColors.UNKNOWN
                return (
                  <div
                    key={`${log.timestamp_ms}-${index}`}
                    className={`px-3 py-2 ${colors.bg} hover:bg-[#1a1a2e]/50 transition-colors`}
                  >
                    <div className="flex items-start gap-2">
                      {/* Timestamp */}
                      <span className="text-gray-500 flex-shrink-0">
                        {formatTime(log.timestamp_ms || log.timestamp)}
                      </span>
                      {/* Level badge */}
                      <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${colors.text} ${colors.bg} border ${colors.border} flex-shrink-0`}>
                        {log.level_str}
                      </span>
                      {/* Component */}
                      {log.component && (
                        <span className="text-purple-400 flex-shrink-0">
                          [{log.component}]
                        </span>
                      )}
                      {/* Message */}
                      <span className={`${colors.text} break-all`}>
                        {log.message}
                      </span>
                    </div>
                    {/* Step/Task info */}
                    {(log.step_id || log.task_id) && (
                      <div className="mt-1 ml-[72px] text-[10px] text-gray-600">
                        {log.task_id && <span>task:{log.task_id.slice(0, 8)}</span>}
                        {log.step_id && <span className="ml-2">step:{log.step_id}</span>}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

const resolveAgentId = (robot: RobotStateSnapshot) => robot.id || ''
const resolveAgentName = (robot: RobotStateSnapshot) => robot.name || resolveAgentId(robot)
const resolveRobotState = (robot: RobotStateSnapshot) => robot.current_state || robot.state || ''
const resolveRobotExecuting = (robot: RobotStateSnapshot) => robot.is_executing ?? false

const resolveActiveStepId = (graph: ActionGraph | null, robot?: RobotStateSnapshot | null) => {
  if (!graph || !robot) return null
  if (robot.current_step_id) return robot.current_step_id
  if (!resolveRobotExecuting(robot)) return null

  const currentState = resolveRobotState(robot)
  if (!currentState) return null

  const agentId = robot.agent_id || null

  for (const step of graph.steps) {
    const rawTargets: Array<{
      state?: string
      target_type?: string
      agent_id?: string
      targetType?: string
      agentId?: string
    }> = (step.during_state_targets || step.duringStateTargets || []) as any
    for (const target of rawTargets) {
      const targetType = (target.target_type || target.targetType || '').toLowerCase()
      if (!target.state || target.state !== currentState) {
        continue
      }
      if (targetType === '' || targetType === 'self' || targetType === 'all') {
        return step.id
      }
      if (targetType === 'agent') {
        const targetAgent = target.agent_id || target.agentId || ''
        if (!targetAgent || (agentId && targetAgent === agentId)) {
          return step.id
        }
      }
    }

    const duringStates = step.during_states || step.duringStates || []
    if (duringStates.includes(currentState)) {
      return step.id
    }
  }

  return null
}

export default function AgentDashboard() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)
  const [expandedCapabilities, setExpandedCapabilities] = useState<string[]>([])
  const [selectedRobotId, setSelectedRobotId] = useState<string | null>(null)
  const [logsExpanded, setLogsExpanded] = useState(true)

  // Fetch all agents
  const {
    data: agents = [],
    isLoading: agentsLoading,
    refetch: refetchAgents,
  } = useQuery({
    queryKey: ['agents'],
    queryFn: () => agentApi.list(),
    refetchInterval: 1000,
    refetchIntervalInBackground: true,
  })

  // Fetch connection status for all agents (heartbeat monitoring)
  const { data: connectionStatus = [] } = useQuery({
    queryKey: ['agent-connection-status'],
    queryFn: () => agentApi.getConnectionStatus(),
    refetchInterval: 1000, // 1s heartbeat refresh
    refetchIntervalInBackground: true,
  })

  // Create a map of agent ID to connection status
  const connectionStatusMap = connectionStatus.reduce((acc, status) => {
    acc[status.id] = status
    return acc
  }, {} as Record<string, AgentConnectionStatus>)

  // Fetch capabilities for selected agent
  const { data: agentCapabilities, isLoading: capabilitiesLoading } = useQuery({
    queryKey: ['agent-capabilities', selectedAgentId],
    queryFn: () => agentApi.getCapabilities(selectedAgentId!),
    enabled: !!selectedAgentId,
    refetchInterval: 5000,
  })

  const { data: actionGraphs = [], isLoading: graphsLoading } = useQuery({
    queryKey: ['action-graphs', 'fleet'],
    queryFn: () => actionGraphApi.list({ includeTemplates: false }),
    refetchInterval: 10000,
  })

  const fleetGraphMeta = useMemo(() => {
    if (actionGraphs.length === 0) return null
    const sorted = [...actionGraphs].sort((a, b) => {
      const aTime = new Date(a.updated_at || a.created_at).getTime()
      const bTime = new Date(b.updated_at || b.created_at).getTime()
      return bTime - aTime
    })
    return sorted[0]
  }, [actionGraphs])

  const { data: fleetGraph } = useQuery({
    queryKey: ['action-graph', fleetGraphMeta?.id],
    queryFn: () => actionGraphApi.get(fleetGraphMeta!.id),
    enabled: !!fleetGraphMeta,
  })

  const { data: stateDefinitions = [] } = useQuery({
    queryKey: ['state-definitions'],
    queryFn: () => stateDefinitionApi.list(),
  })

  const selectedStateDef: StateDefinition | null = stateDefinitions[0] || null

  const { data: agentState } = useQuery({
    queryKey: ['agent-state', selectedAgentId],
    queryFn: () => fleetApi.getAgentState(selectedAgentId!),
    enabled: !!selectedAgentId,
    refetchInterval: 1000,
    refetchIntervalInBackground: true,
  })

  // Fetch execution logs for selected agent
  const { data: agentLogs = [], isLoading: logsLoading } = useQuery({
    queryKey: ['agent-logs', selectedAgentId],
    queryFn: () => logsApi.getAgentLogs(selectedAgentId!, 50),
    enabled: !!selectedAgentId,
    refetchInterval: 2000, // Refresh every 2s for real-time feel
    refetchIntervalInBackground: true,
  })

  const selectedAgent = agents.find((a) => a.id === selectedAgentId)
  const selectedAgentConnection = selectedAgentId ? connectionStatusMap[selectedAgentId] : undefined
  const pingLatencyText = (() => {
    const latencyUs = selectedAgentConnection?.ping_latency_us
    if (latencyUs != null) {
      const latencyMs = latencyUs / 1000
      const msText = latencyMs.toFixed(4)
      return msText === '0.0000' ? `${latencyUs} us` : `${msText} ms`
    }

    const latencyMs = selectedAgentConnection?.ping_latency_ms
    if (latencyMs != null) {
      return `${latencyMs.toFixed(4)} ms`
    }

    return null
  })()

  const toggleCapability = (actionServer: string) => {
    setExpandedCapabilities((prev) =>
      prev.includes(actionServer) ? prev.filter((c) => c !== actionServer) : [...prev, actionServer]
    )
  }

  const agentRobots = agentState?.robots || []

  useEffect(() => {
    if (!agentRobots.length) {
      setSelectedRobotId(null)
      return
    }
    const existing = selectedRobotId &&
      agentRobots.some(robot => resolveAgentId(robot) === selectedRobotId)
    if (existing) return
    const executing = agentRobots.find(robot => resolveRobotExecuting(robot))
    setSelectedRobotId(resolveAgentId(executing || agentRobots[0]))
  }, [agentRobots, selectedRobotId])

  const selectedRobotState = useMemo(() => {
    if (!selectedRobotId) return null
    return agentRobots.find(robot => resolveAgentId(robot) === selectedRobotId) || null
  }, [agentRobots, selectedRobotId])

  const currentStepId = useMemo(() => {
    return resolveActiveStepId(fleetGraph || null, selectedRobotState)
  }, [fleetGraph, selectedRobotState])

  const selectedRobotCurrentState = selectedRobotState ? resolveRobotState(selectedRobotState) : ''
  const selectedRobotExecuting = selectedRobotState ? resolveRobotExecuting(selectedRobotState) : false
  const currentTaskId = selectedRobotState?.current_task_id

  // Task execution control mutations
  const executeGraphMutation = useMutation({
    mutationFn: ({ graphId, agentId }: { graphId: string; agentId: string }) => {
      console.log('[executeGraphMutation] Starting execution:', { graphId, agentId })
      return actionGraphApi.execute(graphId, agentId)
    },
    onSuccess: (data) => {
      console.log('[executeGraphMutation] Success:', data)
      queryClient.invalidateQueries({ queryKey: ['agent-state'] })
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
    onError: (error: Error) => {
      console.error('[executeGraphMutation] Error:', error)
      alert(`Failed to start execution: ${error.message}`)
    },
  })

  const pauseTaskMutation = useMutation({
    mutationFn: (taskId: string) => taskApi.pause(taskId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent-state'] })
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
  })

  const resumeTaskMutation = useMutation({
    mutationFn: (taskId: string) => taskApi.resume(taskId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent-state'] })
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
  })

  const cancelTaskMutation = useMutation({
    mutationFn: ({ taskId, reason }: { taskId: string; reason?: string }) =>
      taskApi.cancel(taskId, reason),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent-state'] })
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
  })

  const resetStateMutation = useMutation({
    mutationFn: (agentId: string) => agentApi.resetState(agentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent-state'] })
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['agents'] })
    },
  })

  const isExecutionLoading =
    executeGraphMutation.isPending ||
    pauseTaskMutation.isPending ||
    resumeTaskMutation.isPending ||
    cancelTaskMutation.isPending ||
    resetStateMutation.isPending

  // Summary stats
  const onlineCount = agents.filter((a) => a.status === 'online').length
  const offlineCount = agents.filter((a) => a.status !== 'online').length

  return (
    <div className="h-screen flex bg-[#0f0f1a]">
      {/* Left Panel - Agent List */}
      <div className="w-80 bg-[#16162a] border-r border-[#2a2a4a] flex flex-col">
        {/* Header */}
        <div className="p-4 border-b border-[#2a2a4a] flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Server className="w-5 h-5 text-blue-400" />
            <h2 className="font-semibold text-white">{t('agent.title')}</h2>
          </div>
          <button
            onClick={() => refetchAgents()}
            className="p-1.5 text-gray-500 hover:text-white hover:bg-[#2a2a4a] rounded transition-colors"
            title={t('common.refresh')}
          >
            <RefreshCw size={16} />
          </button>
        </div>

        {/* Agent List */}
        <div className="flex-1 overflow-y-auto">
          {agentsLoading ? (
            <div className="p-4 text-center text-gray-500">{t('agent.loading')}</div>
          ) : agents.length === 0 ? (
            <div className="p-8 text-center">
              <Server className="w-12 h-12 mx-auto mb-3 text-gray-600" />
              <p className="text-gray-500 text-sm">{t('agent.noAgents')}</p>
              <p className="text-xs text-gray-600 mt-1">
                {t('agent.noAgentsHint')}
              </p>
            </div>
          ) : (
            <div className="py-2">
              {agents.map((agent) => (
                <button
                  key={agent.id}
                  onClick={() => setSelectedAgentId(agent.id)}
                  className={`w-full px-4 py-3 flex items-center gap-3 transition-colors ${
                    selectedAgentId === agent.id
                      ? 'bg-blue-600/20 border-l-2 border-blue-500'
                      : 'hover:bg-[#1a1a2e] border-l-2 border-transparent'
                  }`}
                >
                  {/* Status indicator */}
                  <div className="flex-shrink-0 flex items-center gap-1">
                    {agent.status === 'online' ? (
                      <Wifi className="w-4 h-4 text-green-500" />
                    ) : (
                      <WifiOff className="w-4 h-4 text-gray-600" />
                    )}
                    {/* Heartbeat indicator */}
                    {connectionStatusMap[agent.id] && (
                      <div className={`w-2 h-2 rounded-full ${
                        connectionStatusMap[agent.id].heartbeat_health === 'healthy' ? 'bg-green-500 animate-pulse' :
                        connectionStatusMap[agent.id].heartbeat_health === 'warning' ? 'bg-yellow-500' :
                        'bg-red-500'
                      }`} title={t('agent.heartbeat')} />
                    )}
                  </div>

                  {/* Agent info */}
                  <div className="flex-1 min-w-0 text-left">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-white truncate">
                        {agent.name}
                      </span>
                    </div>
                    <div className="flex items-center gap-2 mt-0.5">
                      {agent.ip_address && (
                        <span className="text-[10px] text-gray-600 font-mono">
                          {agent.ip_address}
                        </span>
                      )}
                    </div>
                  </div>

                  <ChevronRight className="w-4 h-4 text-gray-600" />
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Summary Stats */}
        <div className="p-4 border-t border-[#2a2a4a] bg-[#1a1a2e]">
          <div className="grid grid-cols-2 gap-3 text-center">
            <div>
              <div className="text-xl font-bold text-green-400">{onlineCount}</div>
              <div className="text-[10px] text-gray-500 uppercase">{t('agent.online')}</div>
            </div>
            <div>
              <div className="text-xl font-bold text-gray-400">{offlineCount}</div>
              <div className="text-[10px] text-gray-500 uppercase">{t('agent.offline')}</div>
            </div>
          </div>
        </div>
      </div>

      {/* Right Panel - Agent Details */}
      <div className="flex-1 flex flex-col">
        {selectedAgent ? (
          <>
            {/* Agent Header */}
            <div className="bg-[#16162a] border-b border-[#2a2a4a] p-6">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-4">
                  <div
                    className={`w-12 h-12 rounded-xl flex items-center justify-center ${
                      selectedAgent.status === 'online'
                        ? 'bg-green-500/20'
                        : 'bg-gray-500/20'
                    }`}
                  >
                    <Server
                      className={`w-6 h-6 ${
                        selectedAgent.status === 'online'
                          ? 'text-green-400'
                          : 'text-gray-400'
                      }`}
                    />
                  </div>
                  <div>
                    <h2 className="text-xl font-semibold text-white">{selectedAgent.name}</h2>
                    <div className="flex items-center gap-3 mt-1">
                      <span className="text-sm text-gray-500 font-mono">{selectedAgent.id}</span>
                      {selectedAgent.ip_address && (
                        <>
                          <span className="text-gray-600">|</span>
                          <span className="text-sm text-gray-500">{selectedAgent.ip_address}</span>
                        </>
                      )}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <StatusBadge status={selectedAgent.status} t={t} />
                  {connectionStatusMap[selectedAgent.id] && (
                    <HeartbeatBadge
                      health={connectionStatusMap[selectedAgent.id].heartbeat_health}
                      ageMs={connectionStatusMap[selectedAgent.id].heartbeat_age_ms}
                      t={t}
                    />
                  )}
                </div>
              </div>

              {/* Stats */}
              <div className="grid grid-cols-4 gap-4 mt-6">
                <div className="bg-[#1a1a2e] rounded-lg p-4">
                  <div className="flex items-center gap-2 text-gray-400 text-sm mb-1">
                    <Zap className="w-4 h-4" />
                    {t('agent.actionServers')}
                  </div>
                  <div className="text-2xl font-bold text-white">
                    {agentCapabilities?.total || 0}
                  </div>
                </div>
                <div className="bg-[#1a1a2e] rounded-lg p-4">
                  <div className="flex items-center gap-2 text-gray-400 text-sm mb-1">
                    <HeartPulse className={`w-4 h-4 ${
                      connectionStatusMap[selectedAgent.id]?.heartbeat_health === 'healthy' ? 'text-green-400' :
                      connectionStatusMap[selectedAgent.id]?.heartbeat_health === 'warning' ? 'text-yellow-400' :
                      'text-red-400'
                    }`} />
                    {t('agent.heartbeat')}
                  </div>
                  <div className={`text-sm font-medium ${
                    connectionStatusMap[selectedAgent.id]?.heartbeat_health === 'healthy' ? 'text-green-400' :
                    connectionStatusMap[selectedAgent.id]?.heartbeat_health === 'warning' ? 'text-yellow-400' :
                    'text-red-400'
                  }`}>
                    {connectionStatusMap[selectedAgent.id]?.last_heartbeat
                      ? new Date(connectionStatusMap[selectedAgent.id].last_heartbeat!).toLocaleTimeString()
                      : t('agent.never')}
                  </div>
                </div>
                <div className="bg-[#1a1a2e] rounded-lg p-4">
                  <div className="flex items-center gap-2 text-gray-400 text-sm mb-1">
                    <Activity className="w-4 h-4 text-blue-400" />
                    {t('agent.ping')}
                  </div>
                  <div className="text-sm font-medium text-white">
                    {pingLatencyText
                      ? pingLatencyText
                      : t('agent.never')}
                  </div>
                </div>
                <div className="bg-[#1a1a2e] rounded-lg p-4">
                  <div className="flex items-center gap-2 text-gray-400 text-sm mb-1">
                    <Clock className="w-4 h-4" />
                    {t('agent.lastSeen')}
                  </div>
                  <div className="text-sm font-medium text-white">
                    {selectedAgent.last_seen
                      ? new Date(selectedAgent.last_seen).toLocaleTimeString()
                      : t('agent.never')}
                  </div>
                </div>
              </div>
            </div>

            {/* Scrollable content area */}
            <div className="flex-1 overflow-auto p-6 space-y-8">
              {/* ROS2 Action Servers */}
              <div>
                <div className="flex items-center gap-2 mb-4">
                  <Activity className="w-5 h-5 text-purple-400" />
                  <h3 className="text-lg font-semibold text-white">{t('agent.detectedActionServers')}</h3>
                  <span className="text-sm text-gray-500">
                    ({agentCapabilities?.capabilities?.length || 0} {t('agent.actionTypes')})
                  </span>
                </div>

                {capabilitiesLoading ? (
                  <div className="text-center py-8 text-gray-500">{t('agent.loadingActionServers')}</div>
                ) : !agentCapabilities?.capabilities?.length ? (
                  <div className="text-center py-8">
                    <Activity className="w-10 h-10 mx-auto mb-3 text-gray-600" />
                    <p className="text-gray-500 text-sm">{t('agent.noActionServers')}</p>
                    <p className="text-xs text-gray-600 mt-1">
                      {t('agent.noActionServersHint')}
                    </p>
                  </div>
                ) : (
                  <div className="space-y-3">
                    {agentCapabilities.capabilities.map((capability) => {
                      // Find if this capability is currently in use by a step
                      // Match by action_server (unique identifier) only
                      let inUseByStep: { id: string; name: string } | null = null
                      if (selectedRobotExecuting && currentStepId && fleetGraph) {
                        const currentStep = fleetGraph.steps.find(s => s.id === currentStepId)
                        if (currentStep?.action?.server === capability.action_server) {
                          inUseByStep = {
                            id: currentStep.id,
                            // Show job_name first, then step name, then step id
                            name: currentStep.job_name || currentStep.name || currentStep.id,
                          }
                        }
                      }
                      return (
                        <CapabilityCard
                          key={capability.action_server}
                          capability={capability}
                          expanded={expandedCapabilities.includes(capability.action_server)}
                          onToggle={() => toggleCapability(capability.action_server)}
                          inUseByStep={inUseByStep}
                          t={t}
                        />
                      )
                    })}
                  </div>
                )}
              </div>

              {/* Action Graph */}
              <div>
                <div className="flex items-center gap-2 mb-4">
                  <Layout className="w-5 h-5 text-cyan-400" />
                  <h3 className="text-lg font-semibold text-white">Action Graph</h3>
                  {fleetGraph && (
                    <span className="text-sm text-gray-500">v{fleetGraph.version}</span>
                  )}
                </div>

                {graphsLoading ? (
                  <div className="text-center py-8 text-gray-500">Loading action graph...</div>
                ) : !fleetGraph ? (
                  <div className="text-center py-8">
                    <Layout className="w-10 h-10 mx-auto mb-3 text-gray-600" />
                    <p className="text-gray-500 text-sm">No action graph configured</p>
                    <p className="text-xs text-gray-600 mt-1">
                      Create an Action Graph to visualize execution for this agent.
                    </p>
                  </div>
                ) : (
                  <div className="space-y-3">
                    <div className="flex flex-wrap items-center gap-2 text-xs text-gray-400">
                      <span className="uppercase tracking-wider text-[10px] text-gray-500">Graph</span>
                      <span className="text-gray-200 font-medium">{fleetGraph.name}</span>
                      <span className="text-gray-600">{fleetGraph.id}</span>
                    </div>

                    <div className="flex flex-wrap items-center gap-3">
                      <label className="text-xs text-gray-400">Robot</label>
                      <select
                        value={selectedRobotId || ''}
                        onChange={(e) => setSelectedRobotId(e.target.value)}
                        className="px-2 py-1 bg-[#1a1a2e] border border-[#2a2a4a] rounded text-xs text-white"
                        disabled={agentRobots.length === 0}
                      >
                        {agentRobots.length === 0 && (
                          <option value="">No robots</option>
                        )}
                        {agentRobots.map((robot) => {
                          const agentId = resolveAgentId(robot)
                          return (
                            <option key={agentId} value={agentId}>
                              {resolveAgentName(robot)}
                            </option>
                          )
                        })}
                      </select>
                    </div>

                    {/* Execution Status Panel */}
                    {selectedRobotState && (
                      <div className="bg-[#16162a] rounded-lg border border-[#2a2a4a] p-4 space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-xs text-gray-500 uppercase tracking-wider">Execution Status</span>
                          <ExecutionPhaseBadge
                            phase={selectedRobotState.execution_phase}
                            currentStepName={currentStepId ? (fleetGraph?.steps.find(s => s.id === currentStepId)?.job_name || fleetGraph?.steps.find(s => s.id === currentStepId)?.name) : null}
                            graphName={fleetGraph?.name}
                            blockingConditions={selectedRobotState.blocking_conditions}
                          />
                        </div>

                        {/* Execution Control Buttons */}
                        <div className="flex items-center gap-2 pt-2 border-t border-[#2a2a4a]">
                          {!selectedRobotExecuting ? (
                            // Start button - when not executing
                            <button
                              onClick={() => {
                                if (fleetGraph && selectedRobotId) {
                                  executeGraphMutation.mutate({
                                    graphId: fleetGraph.id,
                                    agentId: selectedRobotId,
                                  })
                                }
                              }}
                              disabled={!fleetGraph || isExecutionLoading || !selectedRobotState.is_online}
                              className="flex items-center gap-1.5 px-3 py-1.5 bg-green-600 hover:bg-green-500 disabled:bg-gray-600 disabled:cursor-not-allowed text-white text-xs font-medium rounded transition-colors"
                              title={!selectedRobotState.is_online ? 'Agent is offline' : !fleetGraph ? 'No graph available' : 'Start execution'}
                            >
                              {isExecutionLoading ? (
                                <Loader2 className="w-3.5 h-3.5 animate-spin" />
                              ) : (
                                <Play className="w-3.5 h-3.5" />
                              )}
                              Start
                            </button>
                          ) : (
                            // Pause/Resume and Stop buttons - when executing
                            <>
                              <button
                                onClick={() => {
                                  if (currentTaskId) {
                                    pauseTaskMutation.mutate(currentTaskId)
                                  }
                                }}
                                disabled={!currentTaskId || isExecutionLoading}
                                className="flex items-center gap-1.5 px-3 py-1.5 bg-yellow-600 hover:bg-yellow-500 disabled:bg-gray-600 disabled:cursor-not-allowed text-white text-xs font-medium rounded transition-colors"
                                title="Pause execution"
                              >
                                {pauseTaskMutation.isPending ? (
                                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                ) : (
                                  <Pause className="w-3.5 h-3.5" />
                                )}
                                Pause
                              </button>
                              <button
                                onClick={() => {
                                  if (currentTaskId) {
                                    cancelTaskMutation.mutate({
                                      taskId: currentTaskId,
                                      reason: 'User cancelled from Agent Dashboard',
                                    })
                                  }
                                }}
                                disabled={!currentTaskId || isExecutionLoading}
                                className="flex items-center gap-1.5 px-3 py-1.5 bg-red-600 hover:bg-red-500 disabled:bg-gray-600 disabled:cursor-not-allowed text-white text-xs font-medium rounded transition-colors"
                                title="Stop execution"
                              >
                                {cancelTaskMutation.isPending ? (
                                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                ) : (
                                  <Square className="w-3.5 h-3.5" />
                                )}
                                Stop
                              </button>
                            </>
                          )}
                          {/* Reset State button - only available when NOT executing */}
                          <button
                            onClick={() => {
                              if (selectedRobotId && window.confirm('Reset agent state to idle? This will clear all execution state.')) {
                                resetStateMutation.mutate(selectedRobotId)
                              }
                            }}
                            disabled={isExecutionLoading || selectedRobotExecuting}
                            className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-600 hover:bg-gray-500 disabled:bg-gray-700 disabled:cursor-not-allowed text-white text-xs font-medium rounded transition-colors ml-auto"
                            title={selectedRobotExecuting ? 'Cannot reset while executing' : 'Reset agent state to idle'}
                          >
                            {resetStateMutation.isPending ? (
                              <Loader2 className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                              <RotateCcw className="w-3.5 h-3.5" />
                            )}
                            Reset State
                          </button>
                          {/* Show task ID if executing */}
                          {currentTaskId && (
                            <span className="text-[10px] text-gray-500 font-mono">
                              Task: {currentTaskId.slice(0, 8)}...
                            </span>
                          )}
                        </div>

                        <div className="grid grid-cols-2 gap-4 text-xs">
                          {/* Current State */}
                          <div>
                            <span className="text-gray-500">State:</span>
                            <div className="text-gray-200 font-medium mt-0.5">
                              {selectedRobotState.state_code || selectedRobotCurrentState || 'idle'}
                            </div>
                          </div>

                          {/* Graph Info */}
                          <div>
                            <span className="text-gray-500">Graph:</span>
                            <div className="text-gray-200 font-medium mt-0.5">
                              {selectedRobotState.current_graph_id
                                ? fleetGraph?.name || selectedRobotState.current_graph_id.slice(0, 8)
                                : 'None assigned'}
                            </div>
                          </div>

                          {/* Current Step */}
                          {selectedRobotExecuting && currentStepId && (
                            <div>
                              <span className="text-gray-500">Current Step:</span>
                              <div className="text-blue-400 font-medium mt-0.5">
                                {fleetGraph?.steps.find(s => s.id === currentStepId)?.job_name || fleetGraph?.steps.find(s => s.id === currentStepId)?.name || currentStepId}
                              </div>
                            </div>
                          )}

                          {/* Semantic Tags */}
                          {selectedRobotState.semantic_tags && selectedRobotState.semantic_tags.length > 0 && (
                            <div className="col-span-2">
                              <span className="text-gray-500">Tags:</span>
                              <div className="flex flex-wrap gap-1 mt-1">
                                {selectedRobotState.semantic_tags.map((tag) => (
                                  <span key={tag} className="px-1.5 py-0.5 bg-purple-500/20 text-purple-400 rounded text-[10px]">
                                    {tag}
                                  </span>
                                ))}
                              </div>
                            </div>
                          )}
                        </div>
                      </div>
                    )}

                    <div className="h-[380px] rounded-lg border border-[#2a2a4a] overflow-hidden bg-[#0f0f1a]">
                      <ActionGraphViewer
                        actionGraph={fleetGraph}
                        stateDef={selectedStateDef}
                        currentStepId={currentStepId}
                        className="h-full"
                        showControls={true}
                        showMiniMap={false}
                      />
                    </div>
                  </div>
                )}
              </div>

              {/* Execution Logs */}
              <div>
                <ExecutionLogsPanel
                  logs={agentLogs}
                  isLoading={logsLoading}
                  isExpanded={logsExpanded}
                  onToggleExpand={() => setLogsExpanded(!logsExpanded)}
                />
              </div>
            </div>
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center">
              <Server className="w-16 h-16 mx-auto mb-4 text-gray-700" />
              <h3 className="text-lg font-medium text-gray-400 mb-2">{t('agent.selectAgent')}</h3>
              <p className="text-sm text-gray-600 max-w-md">
                {t('agent.selectAgentHint')}
              </p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

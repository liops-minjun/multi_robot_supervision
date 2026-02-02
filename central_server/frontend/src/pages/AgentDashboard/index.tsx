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
  Gauge,
  Navigation,
  GitBranch,
  Pencil,
  Check,
  X,
  Trash2,
} from 'lucide-react'
import { agentApi, actionGraphApi, fleetApi, stateDefinitionApi, taskApi, logsApi, telemetryApi } from '../../api/client'
import type { AgentCapabilityInfo, AgentConnectionStatus, ActionGraph, RobotStateSnapshot, StateDefinition, ExecutionPhase, TaskLogEntry, TaskLogLevel, RobotTelemetry, LifecycleState } from '../../types'
import { getLifecycleStateInfo } from '../../types'
import { ActionGraphViewer } from '../../components/BehaviorTreeViewer'
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
    <div className="bg-elevated rounded-lg border border-primary overflow-hidden">
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
          ) : (
            <>
              {/* Lifecycle state badge - only shown for lifecycle nodes (not 'unknown') */}
              {capability.lifecycle_state && capability.lifecycle_state !== 'unknown' && (() => {
                const info = getLifecycleStateInfo(capability.lifecycle_state as LifecycleState)
                const colorClasses: Record<string, string> = {
                  green: 'bg-green-500/20 text-green-400',
                  yellow: 'bg-yellow-500/20 text-yellow-400',
                  gray: 'bg-gray-500/20 text-gray-400',
                  red: 'bg-red-500/20 text-red-400',
                }
                return (
                  <span
                    className={`text-xs px-2 py-0.5 rounded ${colorClasses[info.color] || colorClasses.gray}`}
                    title={info.description}
                  >
                    {info.label}
                  </span>
                )
              })()}
              {/* Availability badge */}
              {capability.is_available ? (
                <span className="text-xs px-2 py-0.5 bg-green-500/20 text-green-400 rounded">
                  {t('agent.available')}
                </span>
              ) : (
                <span className="text-xs px-2 py-0.5 bg-gray-500/20 text-gray-400 rounded">
                  {t('agent.unavailable')}
                </span>
              )}
            </>
          )}
        </div>
        <ChevronRight
          className={`w-4 h-4 text-gray-500 transition-transform duration-200 ${
            expanded ? 'rotate-90' : ''
          }`}
        />
      </button>

      {expanded && (
        <div className="px-4 pb-4 border-t border-primary">
          <div className="space-y-3 pt-3">
            {/* Action Server (전체 경로) */}
            <div>
              <div className="text-xs text-gray-400 uppercase tracking-wider mb-1">{t('agent.actionServer')}</div>
              <div className="flex items-center gap-2 px-3 py-2 bg-surface rounded-lg">
                <Activity className="w-3 h-3 text-blue-400" />
                <span className="text-sm text-gray-300 font-mono">{capability.action_server}</span>
              </div>
            </div>
            {/* Action Type (전체 경로) */}
            <div>
              <div className="text-xs text-gray-400 uppercase tracking-wider mb-1">{t('common.type')}</div>
              <div className="text-xs text-gray-500 font-mono px-3 py-2 bg-surface rounded-lg">
                {capability.action_type}
              </div>
            </div>
            {/* Lifecycle State (only for lifecycle nodes) */}
            {capability.lifecycle_state && capability.lifecycle_state !== 'unknown' && (() => {
              const info = getLifecycleStateInfo(capability.lifecycle_state as LifecycleState)
              const dotColors: Record<string, string> = {
                green: 'bg-green-500',
                yellow: 'bg-yellow-500',
                gray: 'bg-gray-500',
                red: 'bg-red-500',
              }
              return (
                <div>
                  <div className="text-xs text-gray-400 uppercase tracking-wider mb-1">Lifecycle State</div>
                  <div className="flex items-center gap-2 px-3 py-2 bg-surface rounded-lg">
                    <div className={`w-2 h-2 rounded-full ${dotColors[info.color] || dotColors.gray}`} />
                    <span className="text-sm text-gray-300">{info.label}</span>
                    <span className="text-xs text-gray-500">- {info.description}</span>
                  </div>
                </div>
              )
            })()}
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
    <div className="bg-sunken rounded-lg border border-primary overflow-hidden">
      {/* Header */}
      <button
        onClick={onToggleExpand}
        className="w-full flex items-center justify-between px-4 py-3 bg-surface hover:bg-elevated transition-colors"
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
                    className={`px-3 py-2 ${colors.bg} hover:bg-elevated/50 transition-colors`}
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

// Telemetry Panel component - shows JointState, Odometry, TF data
function TelemetryPanel({
  telemetry,
  isLoading,
  isExpanded,
  onToggleExpand,
  lastUpdated,
}: {
  telemetry: RobotTelemetry | null
  isLoading: boolean
  isExpanded: boolean
  onToggleExpand: () => void
  lastUpdated?: string
}) {
  const formatNumber = (n: number, decimals = 3) => n.toFixed(decimals)
  const formatQuaternion = (q: { x: number; y: number; z: number; w: number }) =>
    `(${formatNumber(q.x)}, ${formatNumber(q.y)}, ${formatNumber(q.z)}, ${formatNumber(q.w)})`
  const formatVector3 = (v: { x: number; y: number; z: number }) =>
    `(${formatNumber(v.x)}, ${formatNumber(v.y)}, ${formatNumber(v.z)})`

  const hasData = telemetry && (telemetry.joint_state || telemetry.odometry || (telemetry.transforms && telemetry.transforms.length > 0))

  return (
    <div className="bg-sunken rounded-lg border border-primary overflow-hidden">
      {/* Header */}
      <button
        onClick={onToggleExpand}
        className="w-full flex items-center justify-between px-4 py-3 bg-surface hover:bg-elevated transition-colors"
      >
        <div className="flex items-center gap-2">
          <Gauge className="w-4 h-4 text-cyan-400" />
          <span className="text-sm font-medium text-white">Telemetry</span>
          {hasData && (
            <span className="text-xs text-gray-500">
              ({[
                telemetry?.joint_state && 'JointState',
                telemetry?.odometry && 'Odometry',
                telemetry?.transforms?.length && `${telemetry.transforms.length} TF`,
              ].filter(Boolean).join(', ')})
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {lastUpdated && (
            <span className="text-[10px] text-gray-500">
              {new Date(lastUpdated).toLocaleTimeString()}
            </span>
          )}
          {isExpanded ? (
            <ChevronUp className="w-4 h-4 text-gray-500" />
          ) : (
            <ChevronDown className="w-4 h-4 text-gray-500" />
          )}
        </div>
      </button>

      {/* Content */}
      {isExpanded && (
        <div className="p-4 space-y-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-6 text-gray-500">
              <Loader2 className="w-4 h-4 animate-spin mr-2" />
              Loading telemetry...
            </div>
          ) : !hasData ? (
            <div className="flex flex-col items-center justify-center py-6 text-gray-500">
              <Gauge className="w-8 h-8 mb-2 opacity-50" />
              <p className="text-sm">No telemetry data available</p>
              <p className="text-[10px] mt-1">Telemetry will appear when the agent sends data</p>
            </div>
          ) : (
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              {/* JointState Section */}
              {telemetry?.joint_state && (
                <div className="bg-surface rounded-lg p-3 border border-primary">
                  <div className="flex flex-col gap-1.5 mb-3">
                    <div className="flex items-center gap-2">
                      <Gauge className="w-4 h-4 text-orange-400" />
                      <span className="text-xs font-medium text-white uppercase tracking-wider">JointState</span>
                      <span className="text-[10px] text-gray-500">({telemetry.joint_state.name.length} joints)</span>
                    </div>
                    {telemetry.joint_state.topic_name && (
                      <div className="flex items-center gap-2 px-2 py-1 bg-sunken rounded">
                        <span className="text-[10px] text-gray-500">TOPIC</span>
                        <span className="text-xs text-cyan-400 font-mono">{telemetry.joint_state.topic_name}</span>
                      </div>
                    )}
                  </div>
                  <div className="space-y-1.5 max-h-[200px] overflow-y-auto">
                    <div className="grid grid-cols-4 gap-2 text-[10px] text-gray-500 uppercase tracking-wider pb-1 border-b border-primary">
                      <span>Joint</span>
                      <span>Position</span>
                      <span>Velocity</span>
                      <span>Effort</span>
                    </div>
                    {telemetry.joint_state.name.map((name, idx) => (
                      <div key={name} className="grid grid-cols-4 gap-2 text-xs font-mono">
                        <span className="text-gray-300 truncate" title={name}>{name}</span>
                        <span className="text-cyan-400">{formatNumber(telemetry.joint_state?.position?.[idx] ?? 0)}</span>
                        <span className="text-yellow-400">{formatNumber(telemetry.joint_state?.velocity?.[idx] ?? 0)}</span>
                        <span className="text-purple-400">{formatNumber(telemetry.joint_state?.effort?.[idx] ?? 0)}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Odometry Section */}
              {telemetry?.odometry && (
                <div className="bg-surface rounded-lg p-3 border border-primary">
                  <div className="flex flex-col gap-1.5 mb-3">
                    <div className="flex items-center gap-2">
                      <Navigation className="w-4 h-4 text-green-400" />
                      <span className="text-xs font-medium text-white uppercase tracking-wider">Odometry</span>
                      <span className="text-[10px] text-gray-500">{telemetry.odometry.frame_id} → {telemetry.odometry.child_frame_id}</span>
                    </div>
                    {telemetry.odometry.topic_name && (
                      <div className="flex items-center gap-2 px-2 py-1 bg-sunken rounded">
                        <span className="text-[10px] text-gray-500">TOPIC</span>
                        <span className="text-xs text-cyan-400 font-mono">{telemetry.odometry.topic_name}</span>
                      </div>
                    )}
                  </div>
                  <div className="space-y-3">
                    {/* Position */}
                    <div>
                      <div className="text-[10px] text-gray-500 uppercase tracking-wider mb-1">Position</div>
                      <div className="grid grid-cols-3 gap-2 text-xs font-mono">
                        <div>
                          <span className="text-gray-500">X: </span>
                          <span className="text-cyan-400">{formatNumber(telemetry.odometry.pose.position.x)}</span>
                        </div>
                        <div>
                          <span className="text-gray-500">Y: </span>
                          <span className="text-cyan-400">{formatNumber(telemetry.odometry.pose.position.y)}</span>
                        </div>
                        <div>
                          <span className="text-gray-500">Z: </span>
                          <span className="text-cyan-400">{formatNumber(telemetry.odometry.pose.position.z)}</span>
                        </div>
                      </div>
                    </div>
                    {/* Orientation */}
                    <div>
                      <div className="text-[10px] text-gray-500 uppercase tracking-wider mb-1">Orientation (Quaternion)</div>
                      <div className="text-xs font-mono text-yellow-400">
                        {formatQuaternion(telemetry.odometry.pose.orientation)}
                      </div>
                    </div>
                    {/* Linear Velocity */}
                    <div>
                      <div className="text-[10px] text-gray-500 uppercase tracking-wider mb-1">Linear Velocity</div>
                      <div className="text-xs font-mono text-green-400">
                        {formatVector3(telemetry.odometry.twist.linear)} m/s
                      </div>
                    </div>
                    {/* Angular Velocity */}
                    <div>
                      <div className="text-[10px] text-gray-500 uppercase tracking-wider mb-1">Angular Velocity</div>
                      <div className="text-xs font-mono text-purple-400">
                        {formatVector3(telemetry.odometry.twist.angular)} rad/s
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* Transforms Section */}
              {telemetry?.transforms && telemetry.transforms.length > 0 && (
                <div className="bg-surface rounded-lg p-3 border border-primary lg:col-span-2">
                  <div className="flex flex-col gap-1.5 mb-3">
                    <div className="flex items-center gap-2">
                      <GitBranch className="w-4 h-4 text-blue-400" />
                      <span className="text-xs font-medium text-white uppercase tracking-wider">TF Transforms</span>
                      <span className="text-[10px] text-gray-500">({telemetry.transforms.length} transforms)</span>
                    </div>
                    <div className="flex items-center gap-2 px-2 py-1 bg-sunken rounded">
                      <span className="text-[10px] text-gray-500">TOPIC</span>
                      <span className="text-xs text-cyan-400 font-mono">/tf, /tf_static</span>
                    </div>
                  </div>
                  <div className="space-y-2 max-h-[200px] overflow-y-auto">
                    {telemetry.transforms.map((tf, idx) => (
                      <div key={`${tf.frame_id}-${tf.child_frame_id}-${idx}`} className="bg-sunken rounded p-2 text-xs">
                        <div className="flex items-center gap-2 mb-1">
                          <span className="text-blue-400 font-mono">{tf.frame_id}</span>
                          <span className="text-gray-600">→</span>
                          <span className="text-cyan-400 font-mono">{tf.child_frame_id}</span>
                        </div>
                        <div className="grid grid-cols-2 gap-2 font-mono text-[10px]">
                          <div>
                            <span className="text-gray-500">T: </span>
                            <span className="text-green-400">{formatVector3(tf.translation)}</span>
                          </div>
                          <div>
                            <span className="text-gray-500">R: </span>
                            <span className="text-yellow-400">{formatQuaternion(tf.rotation)}</span>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
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

  // Only return current_step_id if the robot is executing THIS specific graph
  // This prevents showing execution state from a different graph
  if (robot.current_step_id) {
    const currentGraphId = robot.current_graph_id || (robot as any).currentGraphId
    if (currentGraphId && currentGraphId === graph.id) {
      return robot.current_step_id
    }
    // Robot is executing a different graph - don't show its step here
    return null
  }
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
  const [selectedGraphId, setSelectedGraphId] = useState<string | null>(null)
  const [logsExpanded, setLogsExpanded] = useState(true)
  const [telemetryExpanded, setTelemetryExpanded] = useState(false)
  const [isEditingName, setIsEditingName] = useState(false)
  const [editedName, setEditedName] = useState('')
  // For inline editing in agent list
  const [editingAgentInList, setEditingAgentInList] = useState<string | null>(null)
  const [listEditedName, setListEditedName] = useState('')
  // Filter to show only online agents (default: false - show all agents)
  const [showOnlineOnly, setShowOnlineOnly] = useState(false)

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

  // Fetch action graphs ASSIGNED to the selected agent (only deployed ones can be executed)
  const { data: assignedGraphs = [], isLoading: graphsLoading } = useQuery({
    queryKey: ['agent-assigned-graphs', selectedAgentId],
    queryFn: () => agentApi.getAssignedActionGraphs(selectedAgentId!),
    enabled: !!selectedAgentId,
    refetchInterval: 10000,
  })

  // Deploy action graph mutation
  const deployGraphMutation = useMutation({
    mutationFn: ({ graphId, agentId }: { graphId: string; agentId: string }) =>
      agentApi.deployActionGraph(graphId, agentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent-assigned-graphs', selectedAgentId] })
    },
  })

  // Show ALL assigned graphs (including pending/failed) and sort by status then update time
  const sortedActionGraphs = useMemo(() => {
    const statusOrder: Record<string, number> = {
      'deployed': 0,
      'deploying': 1,
      'pending': 2,
      'failed': 3,
      'outdated': 4,
    }
    return assignedGraphs
      .sort((a, b) => {
        // Sort by status first (deployed first), then by update time
        const statusA = statusOrder[a.deployment_status] ?? 5
        const statusB = statusOrder[b.deployment_status] ?? 5
        if (statusA !== statusB) return statusA - statusB
        const aTime = new Date(a.updated_at || a.created_at).getTime()
        const bTime = new Date(b.updated_at || b.created_at).getTime()
        return bTime - aTime
      })
      .map(g => ({
        id: g.behavior_tree_id,
        name: g.behavior_tree_name || g.behavior_tree_id,
        version: g.deployed_version,
        server_version: g.server_version,
        deployment_status: g.deployment_status,
        deployment_error: g.deployment_error,
        updated_at: g.updated_at,
        created_at: g.created_at,
      }))
  }, [assignedGraphs])

  // Auto-select first graph when graphs load or selection becomes invalid
  useEffect(() => {
    if (sortedActionGraphs.length > 0) {
      const currentValid = sortedActionGraphs.some(g => g.id === selectedGraphId)
      if (!selectedGraphId || !currentValid) {
        setSelectedGraphId(sortedActionGraphs[0].id)
      }
    } else {
      setSelectedGraphId(null)
    }
  }, [sortedActionGraphs, selectedGraphId])

  const fleetGraphMeta = useMemo(() => {
    if (!selectedGraphId) return null
    return sortedActionGraphs.find(g => g.id === selectedGraphId) || null
  }, [sortedActionGraphs, selectedGraphId])

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

  // Fetch telemetry for selected robot
  const { data: robotTelemetry, isLoading: telemetryLoading } = useQuery({
    queryKey: ['robot-telemetry', selectedRobotId],
    queryFn: () => telemetryApi.getRobotTelemetry(selectedRobotId!),
    enabled: !!selectedRobotId && telemetryExpanded, // Only fetch when panel is expanded
    refetchInterval: telemetryExpanded ? 500 : false, // 500ms refresh when expanded
    refetchIntervalInBackground: true,
  })

  // Check if action graph can be executed (capability validation)
  const { data: executabilityCheck } = useQuery({
    queryKey: ['executability-check', fleetGraphMeta?.id, selectedRobotId],
    queryFn: () => actionGraphApi.checkExecutability(fleetGraphMeta!.id, selectedRobotId!),
    enabled: !!fleetGraphMeta?.id && !!selectedRobotId,
    refetchInterval: 5000, // Check every 5 seconds
    refetchIntervalInBackground: true,
  })

  // Derived state for execution safety
  const canExecute = executabilityCheck?.can_execute ?? false
  const executabilityMessage = executabilityCheck?.message || ''
  const missingCapabilities = executabilityCheck?.missing_capabilities || []
  const unavailableServers = executabilityCheck?.unavailable_servers || []

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

  // Check if robot is executing the CURRENTLY VIEWED graph (not just any graph)
  const isExecutingCurrentGraph = useMemo(() => {
    if (!selectedRobotState || !selectedRobotExecuting || !selectedGraphId) return false
    const currentGraphId = selectedRobotState.current_graph_id || (selectedRobotState as any).currentGraphId
    return currentGraphId === selectedGraphId
  }, [selectedRobotState, selectedRobotExecuting, selectedGraphId])

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

  const renameAgentMutation = useMutation({
    mutationFn: ({ agentId, name }: { agentId: string; name: string }) =>
      agentApi.update(agentId, { name }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      setIsEditingName(false)
    },
    onError: (error: Error) => {
      alert(`Failed to rename agent: ${error.message}`)
    },
  })

  const deleteAgentMutation = useMutation({
    mutationFn: (agentId: string) => agentApi.delete(agentId),
    onSuccess: (_, deletedAgentId) => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      // Clear selection if deleted agent was selected
      if (selectedAgentId === deletedAgentId) {
        setSelectedAgentId(null)
      }
    },
    onError: (error: Error) => {
      alert(`Failed to delete agent: ${error.message}`)
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

  // Filtered agents list
  const filteredAgents = showOnlineOnly
    ? agents.filter((a) => a.status === 'online')
    : agents

  return (
    <div className="h-screen flex bg-base">
      {/* Left Panel - Agent List */}
      <div className="w-80 bg-surface border-r border-primary flex flex-col">
        {/* Header */}
        <div className="p-4 border-b border-primary">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Server className="w-5 h-5 text-blue-400" />
              <h2 className="font-semibold text-white">{t('agent.title')}</h2>
            </div>
            <button
              onClick={() => refetchAgents()}
              className="p-1.5 text-gray-500 hover:text-white hover:bg-elevated rounded transition-colors"
              title={t('common.refresh')}
            >
              <RefreshCw size={16} />
            </button>
          </div>
          {/* Filter toggle */}
          <div className="mt-2 flex items-center gap-2">
            <button
              onClick={() => setShowOnlineOnly(!showOnlineOnly)}
              className={`text-xs px-2 py-1 rounded transition-colors ${
                showOnlineOnly
                  ? 'bg-green-500/20 text-green-400 border border-green-500/30'
                  : 'bg-gray-500/20 text-gray-400 border border-gray-500/30'
              }`}
            >
              {showOnlineOnly ? `Online only (${onlineCount})` : `All (${agents.length})`}
            </button>
            {!showOnlineOnly && offlineCount > 0 && (
              <span className="text-[10px] text-gray-500">
                {offlineCount} offline
              </span>
            )}
          </div>
        </div>

        {/* Agent List */}
        <div className="flex-1 overflow-y-auto">
          {agentsLoading ? (
            <div className="p-4 text-center text-gray-500">{t('agent.loading')}</div>
          ) : filteredAgents.length === 0 ? (
            <div className="p-8 text-center">
              <Server className="w-12 h-12 mx-auto mb-3 text-gray-600" />
              <p className="text-gray-500 text-sm">
                {showOnlineOnly ? 'No online agents' : t('agent.noAgents')}
              </p>
              <p className="text-xs text-gray-600 mt-1">
                {showOnlineOnly && agents.length > 0
                  ? `${offlineCount} offline agent(s) hidden`
                  : t('agent.noAgentsHint')}
              </p>
            </div>
          ) : (
            <div className="py-2">
              {filteredAgents.map((agent) => (
                <button
                  key={agent.id}
                  onClick={() => setSelectedAgentId(agent.id)}
                  className={`w-full px-4 py-3 flex items-center gap-3 transition-colors ${
                    selectedAgentId === agent.id
                      ? 'bg-blue-600/20 border-l-2 border-blue-500'
                      : 'hover:bg-elevated border-l-2 border-transparent'
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
                      {editingAgentInList === agent.id ? (
                        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                          <input
                            type="text"
                            value={listEditedName}
                            onChange={(e) => setListEditedName(e.target.value)}
                            className="text-sm font-medium text-white bg-elevated border border-blue-500 rounded px-1.5 py-0.5 w-28 focus:outline-none focus:ring-1 focus:ring-blue-500"
                            autoFocus
                            onKeyDown={(e) => {
                              if (e.key === 'Enter' && listEditedName.trim()) {
                                renameAgentMutation.mutate({ agentId: agent.id, name: listEditedName.trim() })
                                setEditingAgentInList(null)
                              } else if (e.key === 'Escape') {
                                setEditingAgentInList(null)
                              }
                            }}
                            onBlur={() => {
                              if (listEditedName.trim() && listEditedName !== agent.name) {
                                renameAgentMutation.mutate({ agentId: agent.id, name: listEditedName.trim() })
                              }
                              setEditingAgentInList(null)
                            }}
                          />
                        </div>
                      ) : (
                        <>
                          <span
                            className="text-sm font-medium text-white truncate cursor-pointer hover:text-blue-300"
                            onDoubleClick={(e) => {
                              e.stopPropagation()
                              setListEditedName(agent.name)
                              setEditingAgentInList(agent.id)
                            }}
                            title="Double-click to rename"
                          >
                            {agent.name}
                          </span>
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              setListEditedName(agent.name)
                              setEditingAgentInList(agent.id)
                            }}
                            className="p-0.5 text-gray-600 hover:text-blue-400 transition-colors"
                            title="Rename"
                          >
                            <Pencil className="w-3 h-3" />
                          </button>
                          {/* Delete button - only for offline agents */}
                          {agent.status !== 'online' && (
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                if (confirm(`Delete agent "${agent.name}"?`)) {
                                  deleteAgentMutation.mutate(agent.id)
                                }
                              }}
                              className="p-0.5 text-gray-600 hover:text-red-400 transition-colors"
                              title="Delete agent"
                            >
                              <Trash2 className="w-3 h-3" />
                            </button>
                          )}
                        </>
                      )}
                    </div>
                    <div className="flex items-center gap-2 mt-0.5">
                      {agent.ip_address && (
                        <span className="text-[10px] text-gray-600 font-mono">
                          {agent.ip_address}
                        </span>
                      )}
                    </div>
                  </div>

                  {agent.status === 'online' ? (
                    <ChevronRight className="w-4 h-4 text-gray-600" />
                  ) : (
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        if (confirm(`Delete agent "${agent.name}"?`)) {
                          deleteAgentMutation.mutate(agent.id)
                        }
                      }}
                      className="p-1.5 text-gray-600 hover:text-red-400 hover:bg-red-500/10 rounded transition-colors"
                      title="Delete agent"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  )}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Summary Stats */}
        <div className="p-4 border-t border-primary bg-elevated">
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
            <div className="bg-surface border-b border-primary p-6">
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
                    {/* Editable Agent Name */}
                    {isEditingName ? (
                      <div className="flex items-center gap-2">
                        <input
                          type="text"
                          value={editedName}
                          onChange={(e) => setEditedName(e.target.value)}
                          className="text-xl font-semibold text-white bg-elevated border border-blue-500 rounded px-2 py-1 focus:outline-none focus:ring-2 focus:ring-blue-500"
                          autoFocus
                          onKeyDown={(e) => {
                            if (e.key === 'Enter' && editedName.trim()) {
                              renameAgentMutation.mutate({ agentId: selectedAgent.id, name: editedName.trim() })
                            } else if (e.key === 'Escape') {
                              setIsEditingName(false)
                              setEditedName(selectedAgent.name)
                            }
                          }}
                        />
                        <button
                          onClick={() => {
                            if (editedName.trim()) {
                              renameAgentMutation.mutate({ agentId: selectedAgent.id, name: editedName.trim() })
                            }
                          }}
                          disabled={!editedName.trim() || renameAgentMutation.isPending}
                          className="p-1 text-green-400 hover:text-green-300 disabled:text-gray-600"
                          title="Save"
                        >
                          {renameAgentMutation.isPending ? (
                            <Loader2 className="w-5 h-5 animate-spin" />
                          ) : (
                            <Check className="w-5 h-5" />
                          )}
                        </button>
                        <button
                          onClick={() => {
                            setIsEditingName(false)
                            setEditedName(selectedAgent.name)
                          }}
                          className="p-1 text-gray-400 hover:text-gray-300"
                          title="Cancel"
                        >
                          <X className="w-5 h-5" />
                        </button>
                      </div>
                    ) : (
                      <div className="flex items-center gap-2">
                        <h2
                          className="text-xl font-semibold text-white cursor-pointer hover:text-blue-300 transition-colors"
                          onDoubleClick={() => {
                            setEditedName(selectedAgent.name)
                            setIsEditingName(true)
                          }}
                          title="Double-click to rename"
                        >
                          {selectedAgent.name}
                        </h2>
                        <button
                          onClick={() => {
                            setEditedName(selectedAgent.name)
                            setIsEditingName(true)
                          }}
                          className="p-1 text-gray-500 hover:text-blue-400 transition-colors"
                          title="Rename agent"
                        >
                          <Pencil className="w-4 h-4" />
                        </button>
                      </div>
                    )}
                    <div className="flex items-center gap-3 mt-1">
                      <span className="text-sm text-gray-500 font-mono" title="Server-assigned ID">{selectedAgent.id}</span>
                      <span className="text-[10px] px-1.5 py-0.5 bg-blue-500/20 text-blue-400 rounded">Server ID</span>
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
                <div className="bg-elevated rounded-lg p-4">
                  <div className="flex items-center gap-2 text-gray-400 text-sm mb-1">
                    <Zap className="w-4 h-4" />
                    {t('agent.actionServers')}
                  </div>
                  <div className="text-2xl font-bold text-white">
                    {agentCapabilities?.total || 0}
                  </div>
                </div>
                <div className="bg-elevated rounded-lg p-4">
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
                <div className="bg-elevated rounded-lg p-4">
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
                <div className="bg-elevated rounded-lg p-4">
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

              {/* Behavior Tree */}
              <div>
                <div className="flex items-center gap-2 mb-4">
                  <Layout className="w-5 h-5 text-cyan-400" />
                  <h3 className="text-lg font-semibold text-white">Behavior Tree</h3>
                  {fleetGraph && (
                    <span className="text-sm text-gray-500">v{fleetGraph.version}</span>
                  )}
                </div>

                {graphsLoading ? (
                  <div className="text-center py-8 text-gray-500">Loading behavior tree...</div>
                ) : !fleetGraph ? (
                  <div className="text-center py-8">
                    <Layout className="w-10 h-10 mx-auto mb-3 text-gray-600" />
                    <p className="text-gray-500 text-sm">No behavior tree configured</p>
                    <p className="text-xs text-gray-600 mt-1">
                      Create a Behavior Tree to visualize execution for this agent.
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
                        className="px-2 py-1 bg-elevated border border-primary rounded text-xs text-white"
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
                      <div className="bg-surface rounded-lg border border-primary p-4 space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-xs text-gray-500 uppercase tracking-wider">Execution Status</span>
                          {/* Show different status when robot is executing a different graph */}
                          {selectedRobotExecuting && !isExecutingCurrentGraph ? (
                            <span className="px-2 py-1 bg-yellow-500/20 text-yellow-400 text-xs rounded">
                              Executing different graph
                            </span>
                          ) : (
                            <ExecutionPhaseBadge
                              phase={selectedRobotState.execution_phase}
                              currentStepName={currentStepId ? (fleetGraph?.steps.find(s => s.id === currentStepId)?.job_name || fleetGraph?.steps.find(s => s.id === currentStepId)?.name) : null}
                              graphName={fleetGraph?.name}
                              blockingConditions={selectedRobotState.blocking_conditions}
                            />
                          )}
                        </div>

                        {/* Execution Control Buttons */}
                        <div className="flex items-center gap-2 pt-2 border-t border-primary">
                          {!selectedRobotExecuting ? (
                            // Start button - when not executing
                            <>
                              <button
                                onClick={() => {
                                  if (fleetGraph && selectedRobotId && canExecute) {
                                    executeGraphMutation.mutate({
                                      graphId: fleetGraph.id,
                                      agentId: selectedRobotId,
                                    })
                                  }
                                }}
                                disabled={!fleetGraph || isExecutionLoading || !selectedRobotState.is_online || !canExecute}
                                className="flex items-center gap-1.5 px-3 py-1.5 bg-green-600 hover:bg-green-500 disabled:bg-gray-600 disabled:cursor-not-allowed text-white text-xs font-medium rounded transition-colors"
                                title={
                                  !selectedRobotState.is_online
                                    ? 'Agent is offline'
                                    : !fleetGraph
                                      ? 'No graph available'
                                      : !canExecute
                                        ? `Cannot execute: ${executabilityMessage}${unavailableServers.length > 0 ? ` (Unavailable: ${unavailableServers.join(', ')})` : ''}`
                                        : 'Start execution'
                                }
                              >
                                {isExecutionLoading ? (
                                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                ) : (
                                  <Play className="w-3.5 h-3.5" />
                                )}
                                Start
                              </button>
                              {/* Capability Warning - when Start is disabled due to missing capabilities */}
                              {!canExecute && selectedRobotState.is_online && fleetGraph && (
                                <div className="flex items-center gap-1.5 px-2 py-1 bg-red-500/20 text-red-400 text-xs rounded">
                                  <AlertTriangle className="w-3.5 h-3.5" />
                                  <span>
                                    {unavailableServers.length > 0
                                      ? `${unavailableServers.length} action server${unavailableServers.length > 1 ? 's' : ''} offline`
                                      : missingCapabilities.length > 0
                                        ? `Missing ${missingCapabilities.length} capability${missingCapabilities.length > 1 ? 'ies' : ''}`
                                        : 'Capability check failed'}
                                  </span>
                                </div>
                              )}
                            </>
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
                              {selectedRobotCurrentState || selectedRobotState.state_code || 'idle'}
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

                    {/* Behavior Tree Selection */}
                    {sortedActionGraphs.length === 0 ? (
                      <div className="flex items-center gap-2 mb-2 px-2 py-1.5 bg-yellow-500/10 border border-yellow-500/30 rounded">
                        <AlertTriangle className="w-3.5 h-3.5 text-yellow-400" />
                        <span className="text-xs text-yellow-400">
                          No graphs assigned to this agent. Assign and deploy a graph from the Behavior Tree Editor.
                        </span>
                      </div>
                    ) : (
                      <div className="mb-2 space-y-2">
                        <div className="flex items-center gap-2">
                          <label className="text-xs text-gray-400">Behavior Tree:</label>
                          <select
                            value={selectedGraphId || ''}
                            onChange={(e) => setSelectedGraphId(e.target.value)}
                            className="flex-1 px-2 py-1 bg-elevated border border-primary rounded text-xs text-white focus:outline-none focus:border-blue-500"
                          >
                            {sortedActionGraphs.map((graph) => {
                              const statusIcon = graph.deployment_status === 'deployed' ? '\u2713' :
                                                 graph.deployment_status === 'pending' ? '\u25cb' :
                                                 graph.deployment_status === 'deploying' ? '\u21bb' :
                                                 graph.deployment_status === 'failed' ? '\u2717' : '\u25cb'
                              return (
                                <option key={graph.id} value={graph.id}>
                                  {statusIcon} {graph.name} {graph.version ? `(v${graph.version})` : graph.deployment_status === 'pending' ? '(not deployed)' : ''}
                                </option>
                              )
                            })}
                          </select>
                          <span className="text-[10px] text-gray-500">
                            {sortedActionGraphs.length} graphs
                          </span>
                        </div>
                        {/* Deployment Status Banner */}
                        {fleetGraphMeta && fleetGraphMeta.deployment_status !== 'deployed' && (
                          <div className={`flex items-center justify-between gap-2 px-2 py-1.5 rounded border ${
                            fleetGraphMeta.deployment_status === 'pending' ? 'bg-yellow-500/10 border-yellow-500/30' :
                            fleetGraphMeta.deployment_status === 'deploying' ? 'bg-blue-500/10 border-blue-500/30' :
                            fleetGraphMeta.deployment_status === 'failed' ? 'bg-red-500/10 border-red-500/30' :
                            'bg-orange-500/10 border-orange-500/30'
                          }`}>
                            <div className="flex items-center gap-2">
                              {fleetGraphMeta.deployment_status === 'deploying' ? (
                                <Loader2 className="w-3.5 h-3.5 text-blue-400 animate-spin" />
                              ) : fleetGraphMeta.deployment_status === 'failed' ? (
                                <XCircle className="w-3.5 h-3.5 text-red-400" />
                              ) : (
                                <AlertTriangle className="w-3.5 h-3.5 text-yellow-400" />
                              )}
                              <span className={`text-xs ${
                                fleetGraphMeta.deployment_status === 'pending' ? 'text-yellow-400' :
                                fleetGraphMeta.deployment_status === 'deploying' ? 'text-blue-400' :
                                fleetGraphMeta.deployment_status === 'failed' ? 'text-red-400' :
                                'text-orange-400'
                              }`}>
                                {fleetGraphMeta.deployment_status === 'pending' && 'Graph assigned but not deployed. Deploy to enable execution.'}
                                {fleetGraphMeta.deployment_status === 'deploying' && 'Deploying graph to agent...'}
                                {fleetGraphMeta.deployment_status === 'failed' && `Deployment failed: ${fleetGraphMeta.deployment_error || 'Unknown error'}`}
                                {fleetGraphMeta.deployment_status === 'outdated' && `Graph outdated (server: v${fleetGraphMeta.server_version}, deployed: v${fleetGraphMeta.version})`}
                              </span>
                            </div>
                            {(fleetGraphMeta.deployment_status === 'pending' || fleetGraphMeta.deployment_status === 'failed' || fleetGraphMeta.deployment_status === 'outdated') && selectedAgentId && (
                              <button
                                onClick={() => deployGraphMutation.mutate({ graphId: fleetGraphMeta.id, agentId: selectedAgentId })}
                                disabled={deployGraphMutation.isPending}
                                className="flex items-center gap-1 px-2 py-1 bg-blue-600 hover:bg-blue-700 disabled:bg-blue-600/50 text-white rounded text-xs transition-colors"
                              >
                                {deployGraphMutation.isPending ? (
                                  <Loader2 className="w-3 h-3 animate-spin" />
                                ) : (
                                  <Play className="w-3 h-3" />
                                )}
                                {fleetGraphMeta.deployment_status === 'outdated' ? 'Update' : 'Deploy'}
                              </button>
                            )}
                          </div>
                        )}
                      </div>
                    )}

                    <div className="h-[380px] rounded-lg border border-primary overflow-hidden bg-base">
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

              {/* Telemetry */}
              {selectedRobotId && (
                <div>
                  <TelemetryPanel
                    telemetry={robotTelemetry || null}
                    isLoading={telemetryLoading}
                    isExpanded={telemetryExpanded}
                    onToggleExpand={() => setTelemetryExpanded(!telemetryExpanded)}
                    lastUpdated={robotTelemetry?.updated_at}
                  />
                </div>
              )}

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

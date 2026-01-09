import { useState } from 'react'
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
  FileCode,
  Check,
  AlertTriangle,
  Link2,
  Heart,
  HeartPulse,
} from 'lucide-react'
import { agentApi, templateApi } from '../../api/client'
import type { AgentCapabilityInfo, TemplateCompatibilityInfo, AgentConnectionStatus } from '../../types'
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

// Capability card component
function CapabilityCard({
  capability,
  expanded,
  onToggle,
  t,
}: {
  capability: AgentCapabilityInfo
  expanded: boolean
  onToggle: () => void
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
          {capability.is_available ? (
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

// Template card component
function TemplateCard({
  template,
  onAssign,
  assigning,
  t,
}: {
  template: TemplateCompatibilityInfo
  onAssign: () => void
  assigning: boolean
  t: (key: 'agent.compatible' | 'agent.partial' | 'agent.assigned' | 'agent.missing' | 'agent.assign' | 'agent.assigning') => string
}) {
  return (
    <div
      className={`bg-[#1a1a2e] rounded-lg border p-4 ${
        template.is_fully_compatible ? 'border-[#2a2a4a]' : 'border-yellow-500/30'
      }`}
    >
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3">
          <FileCode className={`w-5 h-5 mt-0.5 ${
            template.is_fully_compatible ? 'text-blue-400' : 'text-yellow-400'
          }`} />
          <div>
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium text-white">{template.template_name}</span>
              {template.is_fully_compatible ? (
                <span className="text-[9px] px-1.5 py-0.5 bg-green-500/20 text-green-400 rounded flex items-center gap-1">
                  <Check size={10} />
                  {t('agent.compatible')}
                </span>
              ) : (
                <span className="text-[9px] px-1.5 py-0.5 bg-yellow-500/20 text-yellow-400 rounded flex items-center gap-1">
                  <AlertTriangle size={10} />
                  {t('agent.partial')}
                </span>
              )}
              {template.already_assigned && (
                <span className="text-[9px] px-1.5 py-0.5 bg-blue-500/20 text-blue-400 rounded">
                  {t('agent.assigned')}
                </span>
              )}
            </div>

            {/* Required action types */}
            {template.required_action_types && template.required_action_types.length > 0 && (
              <div className="flex flex-wrap gap-1 mt-2">
                {template.required_action_types.map((at) => {
                  const isMissing = template.missing_capabilities?.includes(at) ?? false
                  return (
                    <span
                      key={at}
                      className={`text-[9px] px-1.5 py-0.5 rounded ${
                        isMissing
                          ? 'bg-red-500/20 text-red-400'
                          : 'bg-purple-500/20 text-purple-400'
                      }`}
                    >
                      {isMissing && '✗ '}{at.split('/').pop()}
                    </span>
                  )
                })}
              </div>
            )}

            {/* Missing capabilities warning */}
            {!template.is_fully_compatible && template.missing_capabilities && template.missing_capabilities.length > 0 && (
              <div className="text-[10px] text-yellow-400 mt-2">
                {t('agent.missing')}: {template.missing_capabilities.map(c => c.split('/').pop()).join(', ')}
              </div>
            )}
          </div>
        </div>

        {/* Assign button */}
        {!template.already_assigned && (
          <button
            onClick={onAssign}
            disabled={!template.is_fully_compatible || assigning}
            className={`px-3 py-1.5 text-xs rounded-lg flex items-center gap-1.5 transition-colors ${
              template.is_fully_compatible
                ? 'bg-blue-600 hover:bg-blue-500 text-white'
                : 'bg-gray-700 text-gray-500 cursor-not-allowed'
            }`}
          >
            <Link2 size={12} />
            {assigning ? t('agent.assigning') : t('agent.assign')}
          </button>
        )}
      </div>
    </div>
  )
}

export default function AgentDashboard() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)
  const [expandedCapabilities, setExpandedCapabilities] = useState<string[]>([])

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

  // Fetch compatible templates for selected agent
  const { data: compatibleTemplates, isLoading: templatesLoading } = useQuery({
    queryKey: ['agent-compatible-templates', selectedAgentId],
    queryFn: () => agentApi.getCompatibleTemplates(selectedAgentId!),
    enabled: !!selectedAgentId,
    refetchInterval: 10000,
  })

  // Assign template mutation
  const assignTemplate = useMutation({
    mutationFn: ({ templateId, agentId }: { templateId: string; agentId: string }) =>
      templateApi.assign(templateId, agentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent-compatible-templates', selectedAgentId] })
    },
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

  // Sort templates: fully compatible first, then by name
  const sortedTemplates = compatibleTemplates?.templates
    ?.slice()
    .sort((a, b) => {
      if (a.is_fully_compatible !== b.is_fully_compatible) {
        return a.is_fully_compatible ? -1 : 1
      }
      if (a.already_assigned !== b.already_assigned) {
        return a.already_assigned ? 1 : -1
      }
      return a.template_name.localeCompare(b.template_name)
    }) || []

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
                    {agentCapabilities.capabilities.map((capability) => (
                      <CapabilityCard
                        key={capability.action_server}
                        capability={capability}
                        expanded={expandedCapabilities.includes(capability.action_server)}
                        onToggle={() => toggleCapability(capability.action_server)}
                        t={t}
                      />
                    ))}
                  </div>
                )}
              </div>

              {/* Compatible Templates */}
              <div>
                <div className="flex items-center gap-2 mb-4">
                  <FileCode className="w-5 h-5 text-blue-400" />
                  <h3 className="text-lg font-semibold text-white">{t('agent.compatibleTemplates')}</h3>
                  <span className="text-sm text-gray-500">
                    ({sortedTemplates.filter(tmpl => tmpl.is_fully_compatible).length} {t('agent.compatible')} / {sortedTemplates.length} {t('agent.total')})
                  </span>
                </div>

                {templatesLoading ? (
                  <div className="text-center py-8 text-gray-500">{t('agent.loadingTemplates')}</div>
                ) : sortedTemplates.length === 0 ? (
                  <div className="text-center py-8">
                    <FileCode className="w-10 h-10 mx-auto mb-3 text-gray-600" />
                    <p className="text-gray-500 text-sm">{t('agent.noTemplates')}</p>
                    <p className="text-xs text-gray-600 mt-1">
                      {t('agent.noTemplatesHint')}
                    </p>
                  </div>
                ) : (
                  <div className="space-y-3">
                    {sortedTemplates.map((template) => (
                      <TemplateCard
                        key={template.template_id}
                        template={template}
                        assigning={assignTemplate.isPending}
                        onAssign={() =>
                          assignTemplate.mutate({
                            templateId: template.template_id,
                            agentId: selectedAgentId!,
                          })
                        }
                        t={t}
                      />
                    ))}
                  </div>
                )}
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

import { CheckCircle2, XCircle, AlertCircle, Loader2, Clock3, Lock, Bot, Layers3, Activity, type LucideIcon } from 'lucide-react'
import type { PlanResult, PlanExecution, PlanningTaskSpec, TaskDistributorResource } from '../../../types'
import { useTranslation } from '../../../i18n'

interface Props {
  plan: PlanResult | null
  isLoading: boolean
  taskPlanning?: PlanningTaskSpec | null
  taskName?: string
  requiredActionTypes?: string[]
  execution?: PlanExecution | null
  resources?: TaskDistributorResource[]
}

const STEP_STATUS_STYLES: Record<string, { bg: string; text: string; border: string }> = {
  completed: { bg: 'bg-green-500/10', text: 'text-green-400', border: 'border-green-500/20' },
  running: { bg: 'bg-blue-500/10', text: 'text-blue-400', border: 'border-blue-500/20' },
  failed: { bg: 'bg-red-500/10', text: 'text-red-400', border: 'border-red-500/20' },
  pending: { bg: 'bg-surface', text: 'text-muted', border: 'border-border' },
  cancelled: { bg: 'bg-gray-500/10', text: 'text-gray-400', border: 'border-gray-500/20' },
}

export default function PlanVisualization({
  plan,
  isLoading,
  taskPlanning,
  taskName,
  requiredActionTypes = [],
  execution,
  resources = [],
}: Props) {
  const { t } = useTranslation()
  const formatResourceToken = (token: string) => {
    if (token.startsWith('type:')) {
      const resource = resources.find(item => item.id === token.slice(5))
      return resource ? `TYPE ${resource.name}` : token
    }
    if (token.startsWith('instance:')) {
      const resource = resources.find(item => item.id === token.slice(9))
      return resource ? resource.name : token
    }
    return token
  }
  const translateStatus = (status?: string) => {
    switch (status) {
      case 'pending':
        return t('status.pending')
      case 'running':
        return t('status.running')
      case 'completed':
        return t('status.completed')
      case 'failed':
        return t('status.failed')
      case 'cancelled':
        return t('status.cancelled')
      default:
        return t('status.pending')
    }
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center rounded-2xl border border-border bg-base/50 py-12">
        <div className="flex items-center gap-3 text-sm text-secondary">
          <Loader2 size={18} className="animate-spin text-accent" />
          {t('common.loading')}
        </div>
      </div>
    )
  }

  if (!plan) {
    return (
      <div className="rounded-2xl border border-dashed border-border bg-base/40 px-4 py-10 text-center">
        <AlertCircle size={28} className="mx-auto mb-3 text-muted" />
        <p className="text-sm font-medium text-secondary">{t('pddl.noResult')}</p>
        <p className="mt-2 text-sm text-muted">{t('pddl.noResultHint')}</p>
      </div>
    )
  }

  if (!plan.is_valid) {
    return (
      <div className="rounded-2xl border border-red-500/20 bg-red-500/10 p-4">
        <div className="mb-2 flex items-center gap-2 text-sm font-medium text-red-400">
          <XCircle size={16} />
          {t('pddl.planFailedTitle')}
        </div>
        <p className="text-sm text-red-300">{plan.error_message}</p>
      </div>
    )
  }

  const execStepStatus = new Map<string, string>()
  if (execution?.steps) {
    for (const step of execution.steps) {
      execStepStatus.set(`${step.task_id}:${step.agent_id}`, step.status)
    }
  }

  const groups: Record<number, typeof plan.assignments> = {}
  for (const assignment of plan.assignments) {
    if (!groups[assignment.order]) groups[assignment.order] = []
    groups[assignment.order].push(assignment)
  }

  const sortedOrders = Object.keys(groups).map(Number).sort((a, b) => a - b)
  const isExecuting = execution && (execution.status === 'running' || execution.status === 'pending')
  const executionStatus = execution?.status ? translateStatus(execution.status) : t('pddl.planReady')
  const executionSteps = execution?.steps || []
  const completedStepCount = executionSteps.filter(step => step.status === 'completed').length
  const assignedAgentCount = new Set(plan.assignments.map(assignment => assignment.agent_id)).size
  const progressPercent = execution && execution.total_orders > 0
    ? Math.min(100, Math.max(0, ((execution.current_order + 1) / execution.total_orders) * 100))
    : 0
  const executionTone = execution?.status === 'completed'
    ? 'bg-emerald-500/10 text-emerald-400'
    : execution?.status === 'failed'
      ? 'bg-red-500/10 text-red-400'
      : isExecuting
        ? 'bg-blue-500/10 text-blue-400'
        : 'bg-slate-500/10 text-muted'

  return (
    <div className="space-y-4">
      <div className="rounded-2xl border border-border bg-base/70 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted">
              {t('pddl.executionState')}
            </div>
            <div className="mt-2 flex items-center gap-2 text-base font-semibold text-primary">
              {execution?.status === 'completed' ? (
                <CheckCircle2 size={18} className="text-emerald-400" />
              ) : execution?.status === 'failed' ? (
                <XCircle size={18} className="text-red-400" />
              ) : isExecuting ? (
                <Loader2 size={18} className="animate-spin text-blue-400" />
              ) : (
                <CheckCircle2 size={18} className="text-emerald-400" />
              )}
              {t('pddl.planSummary', { steps: String(plan.total_tasks ?? plan.total_steps), waves: String(plan.parallel_groups) })}
            </div>
            <p className="mt-2 text-sm text-secondary">
              {execution
                ? `${executionStatus} · ${completedStepCount}/${plan.total_tasks ?? plan.total_steps} Task`
                : t('pddl.readyToExecuteHint')}
            </p>
          </div>
          <span className={`rounded-full px-3 py-1 text-[11px] font-medium ${executionTone}`}>
            {executionStatus}
          </span>
        </div>

        <div className="mt-4 rounded-2xl border border-border bg-surface px-3 py-2 text-[11px] text-secondary">
          {t('pddl.dispatchModeHint')}
        </div>

        <div className="mt-4 grid grid-cols-3 gap-2">
          <MetricCard icon={Layers3} label={t('pddl.wave')} value={String(plan.parallel_groups)} />
          <MetricCard icon={Activity} label="Task" value={String(plan.total_tasks ?? plan.total_steps)} />
          <MetricCard icon={Bot} label={t('pddl.selectAgents')} value={String(assignedAgentCount)} />
        </div>

        {execution && execution.total_orders > 0 && (
          <div className="mt-4">
            <div className="mb-2 flex items-center justify-between text-[11px] text-muted">
              <span>{t('pddl.executionProgress')}</span>
              <span>{execution.current_order + 1} / {execution.total_orders}</span>
            </div>
            <div className="h-2 overflow-hidden rounded-full bg-surface">
              <div
                className={`h-full rounded-full transition-all ${
                  execution.status === 'failed' ? 'bg-red-500' :
                  execution.status === 'completed' ? 'bg-emerald-500' : 'bg-blue-500'
                }`}
                style={{ width: `${progressPercent}%` }}
              />
            </div>
          </div>
        )}
      </div>

      <div className="space-y-3">
        {sortedOrders.map(order => {
          const isCurrentOrder = isExecuting && execution.current_order === order
          const isCompletedOrder = execution && execution.current_order > order
          const waveAssignments = groups[order]

          return (
            <div
              key={order}
              className={`overflow-hidden rounded-2xl border ${
                isCurrentOrder ? 'border-blue-500/30 bg-blue-500/5' :
                isCompletedOrder ? 'border-emerald-500/20 bg-emerald-500/5' :
                'border-border bg-base/60'
              }`}
            >
              <div className={`flex items-center justify-between gap-3 border-b px-4 py-3 ${
                isCurrentOrder ? 'border-blue-500/20 bg-blue-500/10' :
                isCompletedOrder ? 'border-emerald-500/20 bg-emerald-500/10' :
                'border-border bg-surface'
              }`}>
                <div>
                  <div className="flex items-center gap-2 text-sm font-semibold text-primary">
                    {isCurrentOrder && <Loader2 size={14} className="animate-spin text-blue-400" />}
                    {isCompletedOrder && <CheckCircle2 size={14} className="text-emerald-400" />}
                    {!isCurrentOrder && !isCompletedOrder && <Clock3 size={14} className="text-muted" />}
                    {t('pddl.waveN', { n: String(order + 1) })}
                  </div>
                  <div className="mt-1 text-[11px] text-secondary">
                    {t('pddl.waveSummary', { count: String(waveAssignments.length) })}
                  </div>
                </div>
                <div className="flex flex-wrap gap-2">
                  {waveAssignments.length > 1 && (
                    <span className="rounded-full bg-accent/10 px-2.5 py-1 text-[11px] font-medium text-accent">
                      {t('pddl.parallel')}
                    </span>
                  )}
                  <span className={`rounded-full px-2.5 py-1 text-[11px] font-medium ${
                    isCurrentOrder ? 'bg-blue-500/10 text-blue-400' :
                    isCompletedOrder ? 'bg-emerald-500/10 text-emerald-400' :
                    'bg-surface text-muted'
                  }`}>
                    {isCurrentOrder ? t('status.running') : isCompletedOrder ? t('status.completed') : t('status.pending')}
                  </span>
                </div>
              </div>

              <div className="grid gap-2 p-3">
                {waveAssignments.map(assignment => {
                  const requiredResources = taskPlanning?.required_resources ?? []
                  const stepStatus = execStepStatus.get(`${assignment.task_id}:${assignment.agent_id}`) || 'pending'
                  const statusStyle = STEP_STATUS_STYLES[stepStatus] || STEP_STATUS_STYLES.pending
                  const executionStep = executionSteps.find(step => step.task_id === assignment.task_id && step.agent_id === assignment.agent_id)

                  return (
                    <div
                      key={`${assignment.task_id}:${assignment.agent_id}:${assignment.order}`}
                      className={`rounded-2xl border p-4 ${statusStyle.border} ${statusStyle.bg}`}
                    >
                      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                        <div className="min-w-0">
                          <div className="text-sm font-semibold text-primary">
                            {assignment.task_name || taskName || assignment.task_id}
                          </div>
                          <div className="mt-1 text-[11px] text-secondary">
                            Capability: {requiredActionTypes.length > 0 ? requiredActionTypes.map(item => item.split('/').pop()).join(', ') : 'Any'}
                          </div>
                        </div>
                        <div className="flex flex-wrap gap-2">
                          <span className="rounded-full bg-surface px-2.5 py-1 text-[11px] font-medium text-accent">
                            {assignment.agent_name || assignment.agent_id}
                          </span>
                          <span className={`rounded-full px-2.5 py-1 text-[11px] font-medium ${statusStyle.text} ${statusStyle.bg}`}>
                            {translateStatus(stepStatus)}
                          </span>
                        </div>
                      </div>

                      <p className="mt-3 text-sm leading-6 text-secondary">{assignment.reason}</p>

                      {requiredResources.length > 0 && (
                        <div className="mt-3 flex flex-wrap gap-2">
                          {requiredResources.map(resource => (
                            <span key={`acq-${resource}`} className="inline-flex items-center gap-1 rounded-full bg-amber-500/10 px-2.5 py-1 text-[11px] text-amber-400">
                              <Lock size={12} />
                              {formatResourceToken(resource)}
                            </span>
                          ))}
                        </div>
                      )}

                      {executionStep?.error && (
                        <div className="mt-3 rounded-2xl bg-red-500/10 px-3 py-2 text-[11px] text-red-300">
                          {executionStep.error}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )
        })}
      </div>

      {execution?.error && (
        <div className="rounded-2xl border border-red-500/20 bg-red-500/10 p-4">
          <p className="text-sm font-medium text-red-300">{t('pddl.executionError')}</p>
          <p className="mt-1 text-sm text-red-300">{execution.error}</p>
        </div>
      )}
    </div>
  )
}

function MetricCard({
  icon: Icon,
  label,
  value,
}: {
  icon: LucideIcon
  label: string
  value: string
}) {
  return (
    <div className="rounded-2xl border border-border bg-surface px-3 py-3">
      <div className="flex items-center gap-2 text-[11px] text-muted">
        <Icon size={14} />
        {label}
      </div>
      <div className="mt-2 text-lg font-semibold text-primary">{value}</div>
    </div>
  )
}

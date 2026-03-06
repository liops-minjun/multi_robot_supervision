import { CheckCircle, XCircle, AlertCircle } from 'lucide-react'
import type { PlanResult } from '../../../types'
import { useTranslation } from '../../../i18n'

interface Props {
  plan: PlanResult | null
  isLoading: boolean
}

export default function PlanVisualization({ plan, isLoading }: Props) {
  const { t } = useTranslation()

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <div className="animate-spin w-6 h-6 border-2 border-accent border-t-transparent rounded-full" />
      </div>
    )
  }

  if (!plan) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-muted">
        <AlertCircle size={24} className="mb-2" />
        <p className="text-xs">{t('pddl.noResult')}</p>
      </div>
    )
  }

  if (!plan.is_valid) {
    return (
      <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg">
        <div className="flex items-center gap-2 text-red-400 text-sm font-medium mb-1">
          <XCircle size={16} />
          Plan Failed
        </div>
        <p className="text-xs text-red-300">{plan.error_message}</p>
      </div>
    )
  }

  // Group assignments by order
  const groups: Record<number, typeof plan.assignments> = {}
  for (const a of plan.assignments) {
    if (!groups[a.order]) groups[a.order] = []
    groups[a.order].push(a)
  }
  const sortedOrders = Object.keys(groups).map(Number).sort((a, b) => a - b)

  return (
    <div className="space-y-3">
      {/* Summary */}
      <div className="flex items-center gap-2 text-sm">
        <CheckCircle size={16} className="text-green-400" />
        <span className="text-primary font-medium">
          {plan.total_steps} steps / {plan.parallel_groups} {t('pddl.parallelGroups')}
        </span>
      </div>

      {/* Parallel groups */}
      <div className="space-y-2">
        {sortedOrders.map(order => (
          <div key={order} className="rounded-lg border border-border overflow-hidden">
            <div className="px-3 py-1.5 bg-hover text-xs font-medium text-secondary flex items-center justify-between">
              <span>{t('pddl.order')} {order}</span>
              {groups[order].length > 1 && (
                <span className="text-accent text-[10px]">parallel</span>
              )}
            </div>
            <div className="divide-y divide-border">
              {groups[order].map(a => (
                <div key={a.step_id} className="px-3 py-2 text-xs">
                  <div className="flex items-center justify-between">
                    <span className="font-medium text-primary">{a.step_name || a.step_id}</span>
                    <span className="text-accent">{a.agent_name || a.agent_id}</span>
                  </div>
                  <div className="text-muted text-[10px] mt-0.5">{a.reason}</div>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

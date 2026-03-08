import { AlertTriangle, ArrowRight } from 'lucide-react'
import type { GraphStep } from '../../../types'
import { useTranslation } from '../../../i18n'

interface Props {
  steps: GraphStep[]
}

interface ConflictEntry {
  left: GraphStep
  right: GraphStep
  resources: string[]
}

export default function ResourceConflictMatrix({ steps }: Props) {
  const { t } = useTranslation()

  const resourceSteps = steps.filter(step => (step.resource_acquire?.length ?? 0) > 0)
  if (resourceSteps.length < 2) return null

  const conflicts: ConflictEntry[] = []
  for (let i = 0; i < resourceSteps.length; i++) {
    for (let j = i + 1; j < resourceSteps.length; j++) {
      const left = resourceSteps[i]
      const right = resourceSteps[j]
      const shared = (left.resource_acquire ?? []).filter(resource =>
        (right.resource_acquire ?? []).includes(resource)
      )

      if (shared.length > 0) {
        conflicts.push({ left, right, resources: shared })
      }
    }
  }

  if (conflicts.length === 0) {
    return (
      <div className="rounded-2xl border border-dashed border-border bg-base/40 px-4 py-8 text-center">
        <p className="text-sm font-medium text-secondary">{t('pddl.noConflicts')}</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.18em] text-muted">
            <AlertTriangle size={14} className="text-yellow-400" />
            {t('pddl.conflictMatrix')}
          </div>
          <h3 className="mt-2 text-lg font-semibold text-primary">{t('pddl.conflictOverviewTitle')}</h3>
        </div>
        <span className="rounded-full bg-yellow-500/10 px-3 py-1 text-[11px] font-medium text-yellow-400">
          {conflicts.length}
        </span>
      </div>

      <div className="space-y-3">
        {conflicts.map(conflict => (
          <div key={`${conflict.left.id}:${conflict.right.id}`} className="rounded-2xl border border-yellow-500/20 bg-yellow-500/8 p-4">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div className="flex min-w-0 items-center gap-2 text-sm font-semibold text-primary">
                <span className="truncate">{conflict.left.name || conflict.left.id}</span>
                <ArrowRight size={14} className="shrink-0 text-muted" />
                <span className="truncate">{conflict.right.name || conflict.right.id}</span>
              </div>
              <span className="rounded-full bg-yellow-500/10 px-2.5 py-1 text-[11px] font-medium text-yellow-400">
                {conflict.resources.length} {t('pddl.resources')}
              </span>
            </div>

            <div className="mt-3 flex flex-wrap gap-2">
              {conflict.resources.map(resource => (
                <span key={resource} className="rounded-full bg-surface px-2.5 py-1 text-[11px] text-secondary">
                  {resource}
                </span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

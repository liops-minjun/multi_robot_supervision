import { Lock, Unlock, Circle } from 'lucide-react'
import type { TaskDistributorResource, GraphStep, ResourceAllocation } from '../../../types'
import { useTranslation } from '../../../i18n'

interface Props {
  resources: TaskDistributorResource[]
  steps: GraphStep[]
  allocations?: ResourceAllocation[]
}

export default function ResourceCardView({ resources, steps, allocations = [] }: Props) {
  const { t } = useTranslation()

  if (resources.length === 0) return null

  // Build step-resource mapping
  const resourceStepMap = new Map<string, { acquire: string[]; release: string[] }>()
  for (const res of resources) {
    resourceStepMap.set(res.name, { acquire: [], release: [] })
  }
  for (const step of steps) {
    for (const r of step.resource_acquire ?? []) {
      const entry = resourceStepMap.get(r)
      if (entry) entry.acquire.push(step.name || step.id)
    }
    for (const r of step.resource_release ?? []) {
      const entry = resourceStepMap.get(r)
      if (entry) entry.release.push(step.name || step.id)
    }
  }

  // Build allocation lookup
  const allocationMap = new Map<string, ResourceAllocation>()
  for (const a of allocations) {
    allocationMap.set(a.resource, a)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted">{t('pddl.resourceStatus')}</div>
          <h3 className="mt-2 text-lg font-semibold text-primary">{t('pddl.resourceOverviewTitle')}</h3>
        </div>
        <span className="rounded-full bg-surface px-3 py-1 text-[11px] font-medium text-secondary">
          {resources.length}
        </span>
      </div>

      <div className="grid grid-cols-1 gap-3">
        {resources.map(res => {
          const mapping = resourceStepMap.get(res.name)
          const alloc = allocationMap.get(res.name)
          const isHeld = !!alloc

          return (
            <div
              key={res.id}
              className={`rounded-2xl border p-4 ${
                isHeld
                  ? 'border-yellow-500/30 bg-yellow-500/8'
                  : 'border-border bg-base/60'
              }`}
            >
              <div className="flex items-start justify-between gap-3">
                <div>
                  <div className="font-mono text-sm font-semibold text-yellow-400">{res.name}</div>
                  {res.description && (
                    <p className="mt-1 text-xs leading-5 text-secondary">{res.description}</p>
                  )}
                </div>
                {isHeld ? (
                  <span className="inline-flex items-center gap-1 rounded-full bg-yellow-500/10 px-2.5 py-1 text-[11px] font-medium text-yellow-400">
                    <Circle size={6} className="fill-yellow-400" />
                    {alloc.holder_agent}
                  </span>
                ) : (
                  <span className="inline-flex items-center gap-1 rounded-full bg-green-500/10 px-2.5 py-1 text-[11px] font-medium text-green-400">
                    <Circle size={6} className="fill-green-400" />
                    {t('pddl.resourceAvailable')}
                  </span>
                )}
              </div>

              {mapping && (mapping.acquire.length > 0 || mapping.release.length > 0) && (
                <div className="mt-4 flex flex-wrap gap-2">
                  {mapping.acquire.map(stepName => (
                    <span key={`acq-${stepName}`} className="inline-flex items-center gap-1 rounded-full bg-yellow-500/10 px-2.5 py-1 text-[11px] text-yellow-400">
                      <Lock size={12} />
                      {stepName}
                    </span>
                  ))}
                  {mapping.release.map(stepName => (
                    <span key={`rel-${stepName}`} className="inline-flex items-center gap-1 rounded-full bg-green-500/10 px-2.5 py-1 text-[11px] text-green-400">
                      <Unlock size={12} />
                      {stepName}
                    </span>
                  ))}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

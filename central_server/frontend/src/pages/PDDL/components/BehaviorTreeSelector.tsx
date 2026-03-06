import { useMemo } from 'react'
import { Package, Variable, ArrowRight } from 'lucide-react'
import type { BehaviorTree, PlanningStateVar, GraphStep, GraphListItem } from '../../../types'
import { useTranslation } from '../../../i18n'

interface Props {
  treeList: GraphListItem[]
  selectedBT: BehaviorTree | null
  onSelect: (id: string) => void
}

export default function BehaviorTreeSelector({ treeList, selectedBT, onSelect }: Props) {
  const { t } = useTranslation()

  const planningStates: PlanningStateVar[] = selectedBT?.planning_states ?? []
  const steps: GraphStep[] = useMemo(() => {
    if (!selectedBT) return []
    return selectedBT.steps.filter(s => s.type !== 'terminal')
  }, [selectedBT])

  return (
    <div className="flex flex-col gap-3">
      {/* BT Selector */}
      <div>
        <label className="text-xs font-medium text-secondary mb-1 block">{t('pddl.selectBT')}</label>
        <select
          className="w-full px-3 py-2 rounded-lg bg-hover border border-border text-primary text-sm"
          value={selectedBT?.id ?? ''}
          onChange={e => onSelect(e.target.value)}
        >
          <option value="">{t('pddl.selectBTPlaceholder')}</option>
          {treeList.map(bt => (
            <option key={bt.id} value={bt.id}>
              {bt.name} (v{bt.version})
            </option>
          ))}
        </select>
      </div>

      {/* Planning State Variables */}
      {selectedBT && (
        <div>
          <div className="text-xs font-medium text-secondary mb-1 flex items-center gap-1">
            <Variable size={12} />
            {t('pddl.planningStates')}
          </div>
          {planningStates.length === 0 ? (
            <p className="text-xs text-muted italic">{t('pddl.noPlanningStates')}</p>
          ) : (
            <div className="space-y-1">
              {planningStates.map(sv => (
                <div key={sv.name} className="flex items-center gap-2 px-2 py-1 bg-hover rounded text-xs">
                  <span className="font-mono text-accent">{sv.name}</span>
                  <span className="text-muted">({sv.type})</span>
                  {sv.initial_value && (
                    <span className="text-secondary ml-auto">= {sv.initial_value}</span>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Step List */}
      {selectedBT && steps.length > 0 && (
        <div>
          <div className="text-xs font-medium text-secondary mb-1 flex items-center gap-1">
            <Package size={12} />
            {t('pddl.steps')} ({steps.length})
          </div>
          <div className="space-y-1 max-h-[300px] overflow-y-auto">
            {steps.map(step => (
              <div key={step.id} className="px-2 py-1.5 bg-hover rounded text-xs">
                <div className="font-medium text-primary">{step.name || step.id}</div>
                {step.action?.type && (
                  <div className="text-muted mt-0.5">{step.action.type}</div>
                )}
                {(step.resource_acquire?.length || step.resource_release?.length) && (
                  <div className="flex gap-2 mt-0.5">
                    {step.resource_acquire?.map(r => (
                      <span key={r} className="text-yellow-400 text-[10px]">+{r}</span>
                    ))}
                    {step.resource_release?.map(r => (
                      <span key={r} className="text-green-400 text-[10px]">-{r}</span>
                    ))}
                  </div>
                )}
                {step.planning_preconditions?.length ? (
                  <div className="text-blue-400 text-[10px] mt-0.5">
                    pre: {step.planning_preconditions.map(c => `${c.variable}${c.operator || '=='}${c.value}`).join(', ')}
                  </div>
                ) : null}
                {step.planning_effects?.length ? (
                  <div className="text-purple-400 text-[10px] mt-0.5 flex items-center gap-0.5">
                    <ArrowRight size={8} />
                    {step.planning_effects.map(e => `${e.variable}=${e.value}`).join(', ')}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

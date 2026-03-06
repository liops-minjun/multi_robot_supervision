import { Plus, X } from 'lucide-react'
import type { PlanningStateVar } from '../../../types'
import { useTranslation } from '../../../i18n'

interface Props {
  label: string
  stateVars: PlanningStateVar[]
  values: Record<string, string>
  onChange: (values: Record<string, string>) => void
}

export default function GoalEditor({ label, stateVars, values, onChange }: Props) {
  const { t } = useTranslation()

  const unusedVars = stateVars.filter(sv => !(sv.name in values))

  const updateValue = (variable: string, value: string) => {
    onChange({ ...values, [variable]: value })
  }

  const removeEntry = (variable: string) => {
    const next = { ...values }
    delete next[variable]
    onChange(next)
  }

  const addEntry = (variable: string) => {
    const sv = stateVars.find(s => s.name === variable)
    onChange({ ...values, [variable]: sv?.initial_value ?? '' })
  }

  return (
    <div>
      <label className="text-xs font-medium text-secondary mb-1 block">{label}</label>
      <div className="space-y-1">
        {Object.entries(values).map(([variable, value]) => {
          const sv = stateVars.find(s => s.name === variable)
          return (
            <div key={variable} className="flex items-center gap-1.5">
              <span className="font-mono text-xs text-accent w-28 truncate" title={variable}>
                {variable}
              </span>
              <span className="text-muted text-xs">=</span>
              <input
                type="text"
                className="flex-1 px-2 py-1 rounded bg-hover border border-border text-primary text-xs"
                value={value}
                onChange={e => updateValue(variable, e.target.value)}
                placeholder={sv?.initial_value || t('pddl.value')}
              />
              <button
                onClick={() => removeEntry(variable)}
                className="text-muted hover:text-red-400 transition-colors"
              >
                <X size={12} />
              </button>
            </div>
          )
        })}
      </div>

      {unusedVars.length > 0 && (
        <div className="flex flex-wrap gap-1 mt-1.5">
          {unusedVars.map(sv => (
            <button
              key={sv.name}
              onClick={() => addEntry(sv.name)}
              className="flex items-center gap-0.5 px-1.5 py-0.5 rounded bg-hover text-[10px] text-muted hover:text-primary hover:bg-accent/10 transition-colors"
            >
              <Plus size={10} />
              {sv.name}
            </button>
          ))}
        </div>
      )}

      {stateVars.length === 0 && (
        <p className="text-xs text-muted italic">{t('pddl.noPlanningStates')}</p>
      )}
    </div>
  )
}

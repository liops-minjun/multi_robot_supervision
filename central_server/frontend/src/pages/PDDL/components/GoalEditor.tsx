import { Plus, X, Sparkles } from 'lucide-react'
import { useTranslation } from '../../../i18n'

const TYPE_BADGE: Record<string, { bg: string; text: string }> = {
  bool: { bg: 'bg-green-500/15', text: 'text-green-400' },
  int: { bg: 'bg-blue-500/15', text: 'text-blue-400' },
  string: { bg: 'bg-orange-500/15', text: 'text-orange-400' },
}

interface StateVar {
  name: string
  type?: string
  initial_value?: string
}

interface Props {
  label: string
  stateVars: StateVar[]
  values: Record<string, string>
  onChange: (values: Record<string, string>) => void
}

export default function GoalEditor({ label, stateVars, values, onChange }: Props) {
  const { t } = useTranslation()

  const unusedVars = stateVars.filter(sv => !(sv.name in values))
  const activeEntries = Object.entries(values)

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
    <div className="space-y-4">
      {label && <label className="block text-xs font-medium text-secondary">{label}</label>}

      {stateVars.length === 0 && (
        <div className="rounded-2xl border border-dashed border-border bg-base/40 px-4 py-8 text-center">
          <p className="text-sm font-medium text-secondary">{t('pddl.noPlanningStates')}</p>
        </div>
      )}

      {stateVars.length > 0 && activeEntries.length === 0 && (
        <div className="rounded-2xl border border-dashed border-border bg-base/50 px-4 py-6">
          <div className="flex items-center gap-2 text-sm font-semibold text-primary">
            <Sparkles size={16} className="text-accent" />
            {t('pddl.goalEditorEmptyTitle')}
          </div>
          <p className="mt-2 text-sm leading-6 text-secondary">
            {t('pddl.goalEditorEmptyHint')}
          </p>
        </div>
      )}

      <div className="space-y-3">
        {activeEntries.map(([variable, value]) => {
          const sv = stateVars.find(s => s.name === variable)
          const badge = TYPE_BADGE[sv?.type || 'string'] || TYPE_BADGE.string
          return (
            <div key={variable} className="rounded-2xl border border-border bg-base/70 p-4">
              <div className="flex flex-col gap-3 xl:flex-row xl:items-center">
                <div className="min-w-0 xl:w-60">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="truncate font-mono text-sm font-semibold text-primary" title={variable}>
                      {variable}
                    </span>
                    {sv?.type && (
                      <span className={`rounded-full px-2 py-1 text-[11px] font-medium ${badge.bg} ${badge.text}`}>
                        {sv.type}
                      </span>
                    )}
                  </div>
                  <div className="mt-2 text-[11px] text-muted">
                    {t('pddl.defaultValue')}: {sv?.initial_value || '-'}
                  </div>
                </div>

                <div className="flex flex-1 items-center gap-3">
                  {sv?.type === 'bool' ? (
                    <select
                      className="w-full rounded-2xl border border-border bg-surface px-3 py-3 text-sm text-primary outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      value={value}
                      onChange={e => updateValue(variable, e.target.value)}
                    >
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  ) : (
                    <input
                      type={sv?.type === 'int' ? 'number' : 'text'}
                      className="w-full rounded-2xl border border-border bg-surface px-3 py-3 text-sm text-primary outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      value={value}
                      onChange={e => updateValue(variable, e.target.value)}
                      placeholder={sv?.initial_value || t('pddl.value')}
                    />
                  )}
                  <button
                    onClick={() => removeEntry(variable)}
                    className="rounded-2xl border border-border bg-surface p-3 text-muted transition hover:text-red-400"
                    title={t('common.delete')}
                  >
                    <X size={16} />
                  </button>
                </div>
              </div>
            </div>
          )
        })}
      </div>

      {unusedVars.length > 0 && (
        <div className="rounded-2xl border border-border bg-base/50 p-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div>
              <div className="text-sm font-semibold text-primary">{t('pddl.quickAdd')}</div>
              <p className="mt-1 text-xs text-secondary">{t('pddl.quickAddHint')}</p>
            </div>
            <span className="rounded-full bg-surface px-3 py-1 text-[11px] font-medium text-secondary">
              {unusedVars.length}
            </span>
          </div>
          <div className="flex flex-wrap gap-2">
          {unusedVars.map(sv => {
            const badge = TYPE_BADGE[sv.type || 'string'] || TYPE_BADGE.string
            return (
              <button
                key={sv.name}
                onClick={() => addEntry(sv.name)}
                className="flex items-center gap-1 rounded-full border border-border bg-surface px-3 py-1.5 text-[11px] text-secondary transition hover:border-accent/20 hover:bg-accent/10 hover:text-primary"
              >
                <Plus size={12} />
                {sv.name}
                {sv.type && (
                  <span className={`rounded-full px-2 py-0.5 ${badge.bg} ${badge.text}`}>{sv.type}</span>
                )}
              </button>
            )
          })}
        </div>
        </div>
      )}
    </div>
  )
}

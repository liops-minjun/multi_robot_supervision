import { Plus, Trash2, X } from 'lucide-react'
import { useMemo } from 'react'
import type { PlanningCondition, RealtimeGoalRule, TaskDistributorState } from '../../../types'
import GoalEditor from './GoalEditor'

interface Props {
  stateVars: TaskDistributorState[]
  goals: RealtimeGoalRule[]
  onChange: (goals: RealtimeGoalRule[]) => void
}

function cloneCondition(condition: PlanningCondition): PlanningCondition {
  return {
    variable: condition.variable,
    operator: condition.operator,
    value: condition.value,
  }
}

function createEmptyGoal(index: number): RealtimeGoalRule {
  return {
    id: `realtime_goal_${index + 1}`,
    name: `Realtime goal ${index + 1}`,
    priority: index + 1,
    enabled: true,
    activation_conditions: [],
    goal_state: {},
  }
}

function createDefaultCondition(stateVars: TaskDistributorState[]): PlanningCondition {
  const selected = stateVars[0]
  return {
    variable: selected?.name || '',
    operator: '==',
    value: selected?.type === 'bool' ? 'true' : (selected?.initial_value || ''),
  }
}

export default function RealtimeGoalEditor({ stateVars, goals, onChange }: Props) {
  const stateVarMap = useMemo(
    () => new Map(stateVars.map(stateVar => [stateVar.name, stateVar])),
    [stateVars],
  )

  const updateGoal = (goalID: string, updater: (goal: RealtimeGoalRule) => RealtimeGoalRule) => {
    onChange(goals.map(goal => (goal.id === goalID ? updater(goal) : goal)))
  }

  const addGoal = () => {
    onChange([...goals, createEmptyGoal(goals.length)])
  }

  const removeGoal = (goalID: string) => {
    onChange(goals.filter(goal => goal.id !== goalID))
  }

  const addCondition = (goalID: string) => {
    updateGoal(goalID, (goal) => ({
      ...goal,
      activation_conditions: [...(goal.activation_conditions || []), createDefaultCondition(stateVars)],
    }))
  }

  const updateCondition = (goalID: string, index: number, updater: (condition: PlanningCondition) => PlanningCondition) => {
    updateGoal(goalID, (goal) => ({
      ...goal,
      activation_conditions: (goal.activation_conditions || []).map((condition, conditionIndex) => (
        conditionIndex === index ? updater(condition) : cloneCondition(condition)
      )),
    }))
  }

  const removeCondition = (goalID: string, index: number) => {
    updateGoal(goalID, (goal) => ({
      ...goal,
      activation_conditions: (goal.activation_conditions || []).filter((_, conditionIndex) => conditionIndex !== index),
    }))
  }

  return (
    <div className="space-y-3">
      {goals.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border bg-base/40 px-4 py-5 text-sm text-secondary">
          실시간 goal 후보가 아직 없습니다.
        </div>
      ) : goals.map((goal, goalIndex) => (
        <div key={goal.id} className="rounded-2xl border border-border bg-base/60 p-4 space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <input
              className="min-w-[180px] flex-1 rounded-xl border border-border bg-surface px-3 py-2 text-sm text-primary outline-none"
              value={goal.name}
              onChange={(e) => updateGoal(goal.id, current => ({ ...current, name: e.target.value }))}
              placeholder="Goal 이름"
            />
            <input
              className="w-20 rounded-xl border border-border bg-surface px-3 py-2 text-sm text-primary outline-none"
              type="number"
              value={goal.priority}
              onChange={(e) => updateGoal(goal.id, current => ({ ...current, priority: Number(e.target.value) || goalIndex + 1 }))}
              title="작을수록 높은 우선순위"
            />
            <label className="inline-flex items-center gap-2 rounded-xl border border-border bg-surface px-3 py-2 text-xs text-secondary">
              <input
                type="checkbox"
                checked={goal.enabled}
                onChange={(e) => updateGoal(goal.id, current => ({ ...current, enabled: e.target.checked }))}
              />
              enabled
            </label>
            <button
              onClick={() => removeGoal(goal.id)}
              className="inline-flex items-center gap-1 rounded-xl border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-300"
            >
              <Trash2 size={12} />
              삭제
            </button>
          </div>

          <div className="rounded-xl border border-border bg-surface/60 p-3">
            <div className="mb-2 flex items-center justify-between gap-2">
              <div>
                <div className="text-xs font-semibold text-primary">활성 조건</div>
                <div className="text-[11px] text-secondary">현재 상태가 이 조건을 만족할 때만 이 goal 후보를 사용합니다.</div>
              </div>
              <button
                onClick={() => addCondition(goal.id)}
                className="inline-flex items-center gap-1 rounded-lg border border-border bg-base px-2 py-1 text-[11px] text-secondary"
              >
                <Plus size={12} />
                조건 추가
              </button>
            </div>

            <div className="space-y-2">
              {(goal.activation_conditions || []).length === 0 ? (
                <div className="text-[11px] text-muted">활성 조건이 없으면 goal 미충족 시 항상 후보가 됩니다.</div>
              ) : (goal.activation_conditions || []).map((condition, conditionIndex) => {
                const stateVar = stateVarMap.get(condition.variable)
                return (
                  <div key={`${goal.id}:condition:${conditionIndex}`} className="flex flex-wrap items-center gap-2">
                    <select
                      className="min-w-[180px] flex-1 rounded-xl border border-border bg-base px-3 py-2 text-sm text-primary outline-none"
                      value={condition.variable}
                      onChange={(e) => updateCondition(goal.id, conditionIndex, current => ({
                        ...current,
                        variable: e.target.value,
                        value: stateVarMap.get(e.target.value)?.type === 'bool' ? 'true' : (stateVarMap.get(e.target.value)?.initial_value || ''),
                      }))}
                    >
                      {stateVars.map((state) => (
                        <option key={state.id} value={state.name}>{state.name}</option>
                      ))}
                    </select>

                    <select
                      className="w-24 rounded-xl border border-border bg-base px-3 py-2 text-sm text-primary outline-none"
                      value={condition.operator || '=='}
                      onChange={(e) => updateCondition(goal.id, conditionIndex, current => ({
                        ...current,
                        operator: e.target.value as PlanningCondition['operator'],
                      }))}
                    >
                      <option value="==">==</option>
                      <option value="!=">!=</option>
                    </select>

                    {stateVar?.type === 'bool' ? (
                      <select
                        className="w-28 rounded-xl border border-border bg-base px-3 py-2 text-sm text-primary outline-none"
                        value={condition.value}
                        onChange={(e) => updateCondition(goal.id, conditionIndex, current => ({
                          ...current,
                          value: e.target.value,
                        }))}
                      >
                        <option value="true">true</option>
                        <option value="false">false</option>
                      </select>
                    ) : (
                      <input
                        className="min-w-[120px] flex-1 rounded-xl border border-border bg-base px-3 py-2 text-sm text-primary outline-none"
                        type={stateVar?.type === 'int' ? 'number' : 'text'}
                        value={condition.value}
                        onChange={(e) => updateCondition(goal.id, conditionIndex, current => ({
                          ...current,
                          value: e.target.value,
                        }))}
                      />
                    )}

                    <button
                      onClick={() => removeCondition(goal.id, conditionIndex)}
                      className="rounded-xl border border-border bg-base p-2 text-muted hover:text-red-400"
                    >
                      <X size={14} />
                    </button>
                  </div>
                )
              })}
            </div>
          </div>

          <div className="rounded-xl border border-border bg-surface/60 p-3">
            <div className="mb-2">
              <div className="text-xs font-semibold text-primary">목표 상태</div>
              <div className="text-[11px] text-secondary">이 goal 후보가 선택되면 planner가 만족시킬 목표 상태입니다.</div>
            </div>
            <GoalEditor
              label=""
              stateVars={stateVars}
              values={goal.goal_state}
              onChange={(nextGoalState) => updateGoal(goal.id, current => ({ ...current, goal_state: nextGoalState }))}
            />
          </div>
        </div>
      ))}

      <button
        onClick={addGoal}
        className="inline-flex items-center gap-2 rounded-xl border border-accent/20 bg-accent/10 px-4 py-2 text-sm font-medium text-accent"
      >
        <Plus size={14} />
        실시간 goal 추가
      </button>
    </div>
  )
}

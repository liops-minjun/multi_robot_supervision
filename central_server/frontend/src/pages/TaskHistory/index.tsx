import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Play, Pause, Square, CheckCircle, XCircle, Clock, RefreshCw,
  AlertCircle, ChevronRight, Timer, Bot, Workflow, Calendar
} from 'lucide-react'
import { taskApi } from '../../api/client'
import { useTranslation } from '../../i18n'
import type { Task } from '../../types'

export default function TaskHistory() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)

  const { data: tasks = [], isLoading, refetch } = useQuery({
    queryKey: ['tasks', filter],
    queryFn: () => taskApi.list(filter || undefined),
    refetchInterval: 5000,
  })

  const selected = tasks.find(t => t.id === selectedId)

  const cancelMutation = useMutation({
    mutationFn: (id: string) => taskApi.cancel(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tasks'] }),
  })

  const pauseMutation = useMutation({
    mutationFn: (id: string) => taskApi.pause(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tasks'] }),
  })

  const resumeMutation = useMutation({
    mutationFn: (id: string) => taskApi.resume(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tasks'] }),
  })

  const confirmMutation = useMutation({
    mutationFn: ({ id, confirmed }: { id: string; confirmed: boolean }) =>
      taskApi.confirm(id, confirmed),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tasks'] }),
  })

  const statusFilters = [
    { value: '', label: t('task.allStatus'), color: 'bg-slate-500/20 text-slate-300' },
    { value: 'running', label: t('status.running'), color: 'bg-blue-500/20 text-blue-300' },
    { value: 'pending', label: t('status.pending'), color: 'bg-slate-500/20 text-slate-300' },
    { value: 'completed', label: t('status.completed'), color: 'bg-emerald-500/20 text-emerald-300' },
    { value: 'failed', label: t('status.failed'), color: 'bg-red-500/20 text-red-300' },
    { value: 'paused', label: t('status.paused'), color: 'bg-amber-500/20 text-amber-300' },
    { value: 'waiting_confirm', label: t('status.waiting_confirm'), color: 'bg-purple-500/20 text-purple-300' },
  ]

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-900 via-slate-800 to-slate-900 p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-3xl font-bold text-white">{t('task.title')}</h1>
          <p className="text-slate-400 mt-1">Task Execution History & Control</p>
        </div>
        <button
          onClick={() => refetch()}
          className="flex items-center gap-2 px-4 py-2.5 bg-slate-700 hover:bg-slate-600 text-white rounded-xl transition-colors"
        >
          <RefreshCw size={18} />
          {t('common.refresh')}
        </button>
      </div>

      {/* Filters */}
      <div className="flex gap-2 mb-6 overflow-x-auto pb-2">
        {statusFilters.map(f => (
          <button
            key={f.value}
            onClick={() => setFilter(f.value)}
            className={`px-4 py-2 rounded-xl text-sm font-medium transition-all whitespace-nowrap ${
              filter === f.value
                ? `${f.color} ring-2 ring-offset-2 ring-offset-slate-900 ${f.color.replace('text-', 'ring-').replace('/20', '/50')}`
                : 'bg-slate-800 text-slate-400 hover:bg-slate-700'
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* List */}
        <div className="lg:col-span-1 bg-slate-800/50 backdrop-blur-sm rounded-2xl border border-slate-700/50 overflow-hidden">
          <div className="px-5 py-4 border-b border-slate-700/50 flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="p-2 bg-purple-500/10 rounded-lg">
                <Timer className="w-5 h-5 text-purple-400" />
              </div>
              <h2 className="text-lg font-semibold text-white">{t('task.list')}</h2>
            </div>
            <span className="text-sm bg-slate-700 px-2.5 py-1 rounded-lg text-slate-300">
              {tasks.length}
            </span>
          </div>
          <div className="divide-y divide-slate-700/30 max-h-[calc(100vh-320px)] overflow-y-auto">
            {isLoading ? (
              <div className="p-8 text-center">
                <RefreshCw className="w-8 h-8 text-slate-600 mx-auto mb-3 animate-spin" />
                <p className="text-slate-500">{t('common.loading')}</p>
              </div>
            ) : tasks.length === 0 ? (
              <div className="p-8 text-center">
                <Clock className="w-12 h-12 text-slate-600 mx-auto mb-3" />
                <p className="text-slate-500">{t('task.noTasks')}</p>
              </div>
            ) : (
              tasks.map(task => (
                <TaskListItem
                  key={task.id}
                  task={task}
                  selected={selectedId === task.id}
                  onClick={() => setSelectedId(task.id)}
                />
              ))
            )}
          </div>
        </div>

        {/* Detail */}
        <div className="lg:col-span-2 bg-slate-800/50 backdrop-blur-sm rounded-2xl border border-slate-700/50 overflow-hidden">
          {selected ? (
            <TaskDetail
              task={selected}
              onCancel={() => cancelMutation.mutate(selected.id)}
              onPause={() => pauseMutation.mutate(selected.id)}
              onResume={() => resumeMutation.mutate(selected.id)}
              onConfirm={(confirmed) => confirmMutation.mutate({ id: selected.id, confirmed })}
            />
          ) : (
            <div className="h-full min-h-[400px] flex items-center justify-center">
              <div className="text-center">
                <div className="w-20 h-20 mx-auto mb-4 rounded-2xl bg-slate-700/50 flex items-center justify-center">
                  <Workflow className="w-10 h-10 text-slate-500" />
                </div>
                <p className="text-slate-400">{t('task.selectToView')}</p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function TaskListItem({
  task,
  selected,
  onClick,
}: {
  task: Task
  selected: boolean
  onClick: () => void
}) {
  const statusConfig: Record<string, { icon: React.ReactNode; bg: string; text: string }> = {
    pending: { icon: <Clock size={16} />, bg: 'bg-slate-500/20', text: 'text-slate-400' },
    running: { icon: <Play size={16} />, bg: 'bg-blue-500/20', text: 'text-blue-400' },
    completed: { icon: <CheckCircle size={16} />, bg: 'bg-emerald-500/20', text: 'text-emerald-400' },
    failed: { icon: <XCircle size={16} />, bg: 'bg-red-500/20', text: 'text-red-400' },
    cancelled: { icon: <Square size={16} />, bg: 'bg-slate-500/20', text: 'text-slate-400' },
    paused: { icon: <Pause size={16} />, bg: 'bg-amber-500/20', text: 'text-amber-400' },
    waiting_confirm: { icon: <AlertCircle size={16} />, bg: 'bg-purple-500/20', text: 'text-purple-400' },
  }

  const config = statusConfig[task.status] || statusConfig.pending

  return (
    <div
      onClick={onClick}
      className={`p-4 cursor-pointer transition-all ${
        selected
          ? 'bg-blue-500/10 border-l-4 border-l-blue-500'
          : 'hover:bg-slate-700/30 border-l-4 border-l-transparent'
      }`}
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className={`w-10 h-10 rounded-xl flex items-center justify-center ${config.bg}`}>
            <span className={config.text}>{config.icon}</span>
          </div>
          <div>
            <p className="font-medium text-white">{task.flow_name || task.flow_id}</p>
            <p className="text-xs text-slate-500">{task.agent_name || task.agent_id}</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs text-slate-500 font-mono">
            {task.started_at ? new Date(task.started_at).toLocaleTimeString() : '-'}
          </span>
          <ChevronRight className={`w-4 h-4 ${selected ? 'text-blue-400' : 'text-slate-600'}`} />
        </div>
      </div>
      {task.progress && (
        <div className="mt-3 ml-13">
          <div className="w-full h-1.5 bg-slate-700 rounded-full overflow-hidden">
            <div
              className={`h-full rounded-full transition-all ${
                task.status === 'completed' ? 'bg-emerald-500' :
                task.status === 'failed' ? 'bg-red-500' :
                task.status === 'running' ? 'bg-blue-500' : 'bg-slate-500'
              }`}
              style={{ width: `${(task.progress.current / task.progress.total) * 100}%` }}
            />
          </div>
        </div>
      )}
    </div>
  )
}

function TaskDetail({
  task,
  onCancel,
  onPause,
  onResume,
  onConfirm,
}: {
  task: Task
  onCancel: () => void
  onPause: () => void
  onResume: () => void
  onConfirm: (confirmed: boolean) => void
}) {
  const { t } = useTranslation()

  const statusConfig: Record<string, { bg: string; text: string; bar: string }> = {
    pending: { bg: 'bg-slate-500/20', text: 'text-slate-300', bar: 'bg-slate-500' },
    running: { bg: 'bg-blue-500/20', text: 'text-blue-300', bar: 'bg-blue-500' },
    completed: { bg: 'bg-emerald-500/20', text: 'text-emerald-300', bar: 'bg-emerald-500' },
    failed: { bg: 'bg-red-500/20', text: 'text-red-300', bar: 'bg-red-500' },
    cancelled: { bg: 'bg-slate-500/20', text: 'text-slate-300', bar: 'bg-slate-500' },
    paused: { bg: 'bg-amber-500/20', text: 'text-amber-300', bar: 'bg-amber-500' },
    waiting_confirm: { bg: 'bg-purple-500/20', text: 'text-purple-300', bar: 'bg-purple-500' },
  }

  const config = statusConfig[task.status] || statusConfig.pending

  const canCancel = ['running', 'paused', 'waiting_confirm'].includes(task.status)
  const canPause = task.status === 'running'
  const canResume = task.status === 'paused'
  const needsConfirm = task.status === 'waiting_confirm'

  const progressPercent = task.progress
    ? (task.progress.current / task.progress.total) * 100
    : 0

  return (
    <div>
      {/* Header */}
      <div className="px-6 py-4 border-b border-slate-700/50 bg-slate-800/30">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold text-white">{task.flow_name || task.flow_id}</h2>
            <p className="text-sm text-slate-500 font-mono mt-1">{task.id}</p>
          </div>
          <span className={`px-4 py-2 rounded-xl text-sm font-medium ${config.bg} ${config.text}`}>
            {t(`status.${task.status}` as any) || task.status}
          </span>
        </div>
      </div>

      <div className="p-6 space-y-6">
        {/* Controls */}
        <div className="flex flex-wrap gap-2">
          {canPause && (
            <button
              onClick={onPause}
              className="flex items-center gap-2 px-4 py-2.5 bg-amber-500 hover:bg-amber-600 text-white rounded-xl transition-colors shadow-lg shadow-amber-500/20"
            >
              <Pause size={16} />
              {t('task.pause')}
            </button>
          )}
          {canResume && (
            <button
              onClick={onResume}
              className="flex items-center gap-2 px-4 py-2.5 bg-emerald-500 hover:bg-emerald-600 text-white rounded-xl transition-colors shadow-lg shadow-emerald-500/20"
            >
              <Play size={16} />
              {t('task.resume')}
            </button>
          )}
          {canCancel && (
            <button
              onClick={onCancel}
              className="flex items-center gap-2 px-4 py-2.5 bg-red-500 hover:bg-red-600 text-white rounded-xl transition-colors shadow-lg shadow-red-500/20"
            >
              <Square size={16} />
              {t('task.cancel')}
            </button>
          )}
        </div>

        {/* Manual Confirmation */}
        {needsConfirm && (
          <div className="bg-purple-500/10 border border-purple-500/20 rounded-xl p-5">
            <div className="flex items-center gap-3 mb-4">
              <div className="w-10 h-10 rounded-xl bg-purple-500/20 flex items-center justify-center">
                <AlertCircle className="w-5 h-5 text-purple-400" />
              </div>
              <div>
                <h3 className="font-medium text-white">{t('task.manualConfirm')}</h3>
                <p className="text-sm text-slate-400 mt-0.5">Waiting for operator approval to continue</p>
              </div>
            </div>
            <div className="flex gap-3">
              <button
                onClick={() => onConfirm(true)}
                className="flex-1 flex items-center justify-center gap-2 px-4 py-3 bg-emerald-500 hover:bg-emerald-600 text-white rounded-xl transition-colors"
              >
                <CheckCircle size={18} />
                {t('common.confirm')}
              </button>
              <button
                onClick={() => onConfirm(false)}
                className="flex-1 flex items-center justify-center gap-2 px-4 py-3 bg-red-500 hover:bg-red-600 text-white rounded-xl transition-colors"
              >
                <XCircle size={18} />
                {t('common.reject')}
              </button>
            </div>
          </div>
        )}

        {/* Info Cards */}
        <div className="grid grid-cols-2 gap-4">
          <div className="bg-slate-700/30 rounded-xl p-4">
            <div className="flex items-center gap-2 mb-2">
              <Bot className="w-4 h-4 text-cyan-400" />
              <p className="text-sm text-slate-400">{t('robot.title')}</p>
            </div>
            <p className="text-white font-medium">{task.agent_name || task.agent_id}</p>
          </div>
          <div className="bg-slate-700/30 rounded-xl p-4">
            <div className="flex items-center gap-2 mb-2">
              <Workflow className="w-4 h-4 text-purple-400" />
              <p className="text-sm text-slate-400">ActionGraph</p>
            </div>
            <p className="text-white font-medium">{task.flow_name || task.flow_id}</p>
          </div>
        </div>

        {/* Progress */}
        {task.progress && (
          <div className="bg-slate-700/30 rounded-xl p-5">
            <div className="flex items-center justify-between mb-3">
              <h3 className="font-medium text-white">{t('task.progress')}</h3>
              <span className="text-sm text-slate-400">
                {task.progress.current}/{task.progress.total} steps
              </span>
            </div>
            <div className="relative mb-4">
              <div className="w-full h-3 bg-slate-800/50 rounded-full overflow-hidden">
                <div
                  className={`h-full ${config.bar} rounded-full transition-all duration-500`}
                  style={{ width: `${progressPercent}%` }}
                />
              </div>
              <span className="absolute right-0 -top-1 text-xs font-medium text-slate-400">
                {progressPercent.toFixed(0)}%
              </span>
            </div>
            {task.current_step_id && (
              <div className="flex items-center gap-2 text-sm">
                <span className="text-slate-500">{t('task.currentStep')}:</span>
                <span className="text-white font-mono bg-slate-800/50 px-2 py-1 rounded">
                  {task.current_step_id}
                </span>
              </div>
            )}
          </div>
        )}

        {/* Error */}
        {task.error_message && (
          <div className="bg-red-500/10 border border-red-500/20 rounded-xl p-4">
            <div className="flex items-center gap-2 mb-2">
              <XCircle className="w-5 h-5 text-red-400" />
              <h3 className="font-medium text-red-400">{t('task.error')}</h3>
            </div>
            <p className="text-sm text-red-300/80">{task.error_message}</p>
          </div>
        )}

        {/* Timestamps */}
        <div className="bg-slate-700/30 rounded-xl p-4">
          <div className="flex items-center gap-2 mb-3">
            <Calendar className="w-4 h-4 text-slate-400" />
            <h3 className="text-sm font-medium text-slate-400">Timeline</h3>
          </div>
          <div className="space-y-2 text-sm">
            <div className="flex items-center justify-between">
              <span className="text-slate-500">{t('time.created')}</span>
              <span className="text-slate-300 font-mono">
                {new Date(task.created_at).toLocaleString()}
              </span>
            </div>
            {task.started_at && (
              <div className="flex items-center justify-between">
                <span className="text-slate-500">{t('time.started')}</span>
                <span className="text-slate-300 font-mono">
                  {new Date(task.started_at).toLocaleString()}
                </span>
              </div>
            )}
            {task.completed_at && (
              <div className="flex items-center justify-between">
                <span className="text-slate-500">{t('time.completed')}</span>
                <span className="text-slate-300 font-mono">
                  {new Date(task.completed_at).toLocaleString()}
                </span>
              </div>
            )}
          </div>
        </div>

        {/* Step Results */}
        {task.step_results && Object.keys(task.step_results).length > 0 && (
          <div className="bg-slate-700/30 rounded-xl p-4">
            <h3 className="font-medium text-white mb-3">{t('task.stepResults')}</h3>
            <div className="bg-slate-800/50 rounded-lg p-4 font-mono text-xs text-slate-300 overflow-x-auto">
              <pre>{JSON.stringify(task.step_results, null, 2)}</pre>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

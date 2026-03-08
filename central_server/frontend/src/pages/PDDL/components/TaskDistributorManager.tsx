import { useState, useCallback, useMemo } from 'react'
import { Plus, Trash2, Edit, Check, X, Boxes, Sparkles, SlidersHorizontal } from 'lucide-react'
import type { TaskDistributor, TaskDistributorState, TaskDistributorResource } from '../../../types'
import { taskDistributorApi } from '../../../api/client'
import { useTranslation } from '../../../i18n'

const TYPE_BADGE: Record<string, { bg: string; text: string }> = {
  bool: { bg: 'bg-green-500/15', text: 'text-green-400' },
  int: { bg: 'bg-blue-500/15', text: 'text-blue-400' },
  string: { bg: 'bg-orange-500/15', text: 'text-orange-400' },
}

type ManagerTab = 'builder' | 'manual'

interface Props {
  distributors: TaskDistributor[]
  selectedId: string | null
  onSelect: (id: string | null) => void
  onRefresh: () => void
}

function buildInstanceNames(typeName: string, count: number) {
  const normalized = typeName.trim().replace(/\s+/g, ' ')
  if (!normalized) return []

  return Array.from({ length: Math.max(1, count) }, (_, index) => {
    const suffix = String(index + 1)
    return /\s/.test(normalized) ? `${normalized} ${suffix}` : `${normalized}${suffix}`
  })
}

function inferResourceType(name: string) {
  const match = name.match(/^(.*?)(?:\s?)(\d+)$/)
  if (!match) return name
  return match[1].trim() || name
}

function isResourceType(resource: TaskDistributorResource) {
  return resource.kind === 'type'
}

function isResourceInstance(resource: TaskDistributorResource) {
  return !resource.kind || resource.kind === 'instance'
}

export default function TaskDistributorManager({ distributors, selectedId, onSelect, onRefresh }: Props) {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<ManagerTab>('builder')
  const [isCreating, setIsCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [editingName, setEditingName] = useState<string | null>(null)
  const [editNameValue, setEditNameValue] = useState('')

  const [newStateName, setNewStateName] = useState('')
  const [newStateType, setNewStateType] = useState<'bool' | 'int' | 'string'>('string')
  const [newStateInitialValue, setNewStateInitialValue] = useState('')
  const [editingStateId, setEditingStateId] = useState<string | null>(null)
  const [editStateInitialValue, setEditStateInitialValue] = useState('')

  const [newResourceName, setNewResourceName] = useState('')
  const [editingResourceId, setEditingResourceId] = useState<string | null>(null)
  const [editResourceDescription, setEditResourceDescription] = useState('')

  const [resourceTypeName, setResourceTypeName] = useState('')
  const [instanceCount, setInstanceCount] = useState(2)
  const [resourceDescription, setResourceDescription] = useState('')
  const [autoCreateOccupancyStates, setAutoCreateOccupancyStates] = useState(true)
  const [isGenerating, setIsGenerating] = useState(false)
  const [builderMessage, setBuilderMessage] = useState<string | null>(null)

  const selected = distributors.find(d => d.id === selectedId) || null

  const generatedResourceNames = useMemo(
    () => buildInstanceNames(resourceTypeName, instanceCount),
    [resourceTypeName, instanceCount]
  )

  const generatedStateNames = useMemo(
    () => generatedResourceNames.map(resourceName => `${resourceName} ${t('pddl.occupiedSuffix')}`),
    [generatedResourceNames, t]
  )

  const groupedResources = useMemo(() => {
    if (!selected) return []
    const groups = new Map<string, { typeResource?: TaskDistributorResource; items: TaskDistributorResource[] }>()
    const resourceById = new Map((selected.resources || []).map(resource => [resource.id, resource]))

    for (const resource of selected.resources || []) {
      if (isResourceType(resource)) {
        const existing = groups.get(resource.name) || { items: [] }
        existing.typeResource = resource
        groups.set(resource.name, existing)
        continue
      }

      const parentType = resource.parent_resource_id ? resourceById.get(resource.parent_resource_id) : null
      const typeName = parentType?.name || inferResourceType(resource.name)
      const existing = groups.get(typeName) || { items: [] }
      existing.items.push(resource)
      groups.set(typeName, existing)
    }

    return Array.from(groups.entries())
      .map(([typeName, value]) => ({
        typeName,
        typeResource: value.typeResource || null,
        items: [...value.items].sort((a, b) => a.name.localeCompare(b.name)),
      }))
      .sort((a, b) => a.typeName.localeCompare(b.typeName))
  }, [selected])

  const handleCreate = useCallback(async () => {
    if (!newName.trim()) return
    try {
      const created = await taskDistributorApi.create({ name: newName.trim() })
      setNewName('')
      setIsCreating(false)
      onRefresh()
      onSelect(created.id)
    } catch (err) {
      console.error('Failed to create distributor:', err)
    }
  }, [newName, onRefresh, onSelect])

  const handleRename = useCallback(async (id: string) => {
    if (!editNameValue.trim()) return
    try {
      await taskDistributorApi.update(id, { name: editNameValue.trim() })
      setEditingName(null)
      onRefresh()
    } catch (err) {
      console.error('Failed to rename distributor:', err)
    }
  }, [editNameValue, onRefresh])

  const handleDelete = useCallback(async (id: string) => {
    if (!confirm(t('pddl.deleteDistributor') + '?')) return
    try {
      await taskDistributorApi.delete(id)
      if (selectedId === id) onSelect(null)
      onRefresh()
    } catch (err) {
      console.error('Failed to delete distributor:', err)
    }
  }, [selectedId, onSelect, onRefresh, t])

  const handleAddState = useCallback(async () => {
    if (!selected || !newStateName.trim()) return
    try {
      await taskDistributorApi.createState(selected.id, {
        name: newStateName.trim(),
        type: newStateType,
        initial_value: newStateInitialValue || undefined,
      })
      setNewStateName('')
      setNewStateInitialValue('')
      onRefresh()
    } catch (err) {
      console.error('Failed to add state:', err)
    }
  }, [selected, newStateName, newStateType, newStateInitialValue, onRefresh])

  const handleDeleteState = useCallback(async (stateId: string) => {
    if (!selected) return
    try {
      await taskDistributorApi.deleteState(selected.id, stateId)
      onRefresh()
    } catch (err) {
      console.error('Failed to delete state:', err)
    }
  }, [selected, onRefresh])

  const handleUpdateStateInitialValue = useCallback(async (sv: TaskDistributorState) => {
    if (!selected) return
    try {
      await taskDistributorApi.updateState(selected.id, sv.id, {
        name: sv.name,
        type: sv.type,
        initial_value: editStateInitialValue,
        description: sv.description,
      })
      setEditingStateId(null)
      onRefresh()
    } catch (err) {
      console.error('Failed to update state:', err)
    }
  }, [selected, editStateInitialValue, onRefresh])

  const handleAddResource = useCallback(async () => {
    if (!selected || !newResourceName.trim()) return
    try {
      await taskDistributorApi.createResource(selected.id, {
        name: newResourceName.trim(),
        kind: 'instance',
      })
      setNewResourceName('')
      onRefresh()
    } catch (err) {
      console.error('Failed to add resource:', err)
    }
  }, [selected, newResourceName, onRefresh])

  const handleDeleteResource = useCallback(async (resourceId: string) => {
    if (!selected) return
    try {
      await taskDistributorApi.deleteResource(selected.id, resourceId)
      onRefresh()
    } catch (err) {
      console.error('Failed to delete resource:', err)
    }
  }, [selected, onRefresh])

  const handleUpdateResourceDescription = useCallback(async (res: TaskDistributorResource) => {
    if (!selected) return
    try {
      await taskDistributorApi.updateResource(selected.id, res.id, {
        name: res.name,
        kind: res.kind || 'instance',
        parent_resource_id: res.parent_resource_id,
        description: editResourceDescription || undefined,
      })
      setEditingResourceId(null)
      onRefresh()
    } catch (err) {
      console.error('Failed to update resource:', err)
    }
  }, [selected, editResourceDescription, onRefresh])

  const handleGenerateResourceType = useCallback(async () => {
    if (!selected || generatedResourceNames.length === 0) return

    const resourceList = selected.resources || []
    const existingResourceNames = new Set(resourceList.filter(isResourceInstance).map(resource => resource.name))
    const existingStateNames = new Set((selected.states || []).map(state => state.name))
    const existingType = resourceList.find(resource => isResourceType(resource) && resource.name === resourceTypeName.trim())

    const resourcesToCreate = generatedResourceNames.filter(name => !existingResourceNames.has(name))
    const statesToCreate = autoCreateOccupancyStates
      ? generatedStateNames.filter(name => !existingStateNames.has(name))
      : []

    setIsGenerating(true)
    setBuilderMessage(null)

    try {
      const typeResource = existingType || await taskDistributorApi.createResource(selected.id, {
        name: resourceTypeName.trim(),
        kind: 'type',
        description: resourceDescription.trim() || undefined,
      })

      await Promise.all(resourcesToCreate.map(resourceName =>
        taskDistributorApi.createResource(selected.id, {
          name: resourceName,
          kind: 'instance',
          parent_resource_id: typeResource.id,
          description: resourceDescription.trim() || undefined,
        })
      ))

      await Promise.all(statesToCreate.map(stateName =>
        taskDistributorApi.createState(selected.id, {
          name: stateName,
          type: 'bool',
          initial_value: 'false',
          description: `${stateName} ${t('pddl.generatorStateDescriptionSuffix')}`,
        })
      ))

      const skippedCount = (generatedResourceNames.length - resourcesToCreate.length) + (generatedStateNames.length - statesToCreate.length)
      setBuilderMessage(
        t('pddl.generatorSummary', {
          resources: String(resourcesToCreate.length),
          states: String(statesToCreate.length),
          skipped: String(skippedCount),
        })
      )
      setResourceTypeName('')
      setResourceDescription('')
      setInstanceCount(2)
      onRefresh()
    } catch (err) {
      console.error('Failed to generate resources:', err)
      setBuilderMessage(t('pddl.generatorError'))
    } finally {
      setIsGenerating(false)
    }
  }, [
    selected,
    generatedResourceNames,
    generatedStateNames,
    autoCreateOccupancyStates,
    resourceTypeName,
    resourceDescription,
    onRefresh,
    t,
  ])

  return (
    <div className="grid gap-5 xl:grid-cols-[240px_minmax(0,1fr)]">
      <aside className="rounded-3xl border border-border bg-base/70 p-4">
        <div className="mb-3 flex items-center justify-between">
          <div>
            <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted">{t('pddl.taskDistributor')}</div>
            <div className="mt-2 text-sm font-semibold text-primary">{t('pddl.distributorListTitle')}</div>
          </div>
          <button
            onClick={() => setIsCreating(true)}
            className="rounded-2xl border border-border bg-surface p-2 text-muted transition hover:text-primary"
            title={t('pddl.createDistributor')}
          >
            <Plus size={16} />
          </button>
        </div>

        {isCreating && (
          <div className="mb-3 rounded-2xl border border-border bg-surface p-3">
            <input
              className="w-full rounded-2xl border border-border bg-base px-3 py-2 text-sm text-primary outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              placeholder={t('pddl.distributorName')}
              value={newName}
              onChange={e => setNewName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleCreate()}
              autoFocus
            />
            <div className="mt-3 flex gap-2">
              <button onClick={handleCreate} className="inline-flex items-center gap-1 rounded-2xl bg-accent px-3 py-2 text-sm font-medium text-white">
                <Check size={14} />
                {t('common.create')}
              </button>
              <button
                onClick={() => { setIsCreating(false); setNewName('') }}
                className="inline-flex items-center gap-1 rounded-2xl border border-border bg-base px-3 py-2 text-sm text-secondary"
              >
                <X size={14} />
                {t('common.cancel')}
              </button>
            </div>
          </div>
        )}

        {distributors.length === 0 ? (
          <p className="rounded-2xl border border-dashed border-border bg-base/40 px-3 py-8 text-center text-sm italic text-muted">
            {t('pddl.noDistributors')}
          </p>
        ) : (
          <div className="space-y-2">
            {distributors.map(distributor => (
              <div
                key={distributor.id}
                className={`group rounded-2xl border p-3 transition ${
                  selectedId === distributor.id
                    ? 'border-accent/30 bg-accent/10'
                    : 'border-border bg-surface hover:border-border-secondary'
                }`}
                onClick={() => onSelect(distributor.id)}
              >
                {editingName === distributor.id ? (
                  <div className="flex items-center gap-2" onClick={e => e.stopPropagation()}>
                    <input
                      className="flex-1 rounded-xl border border-border bg-base px-2 py-1.5 text-sm text-primary"
                      value={editNameValue}
                      onChange={e => setEditNameValue(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && handleRename(distributor.id)}
                      autoFocus
                    />
                    <button onClick={() => handleRename(distributor.id)} className="p-1 text-green-400"><Check size={14} /></button>
                    <button onClick={() => setEditingName(null)} className="p-1 text-muted"><X size={14} /></button>
                  </div>
                ) : (
                  <div className="flex items-start gap-3">
                    <div className="min-w-0 flex-1 cursor-pointer">
                      <div className="truncate text-sm font-semibold text-primary">{distributor.name}</div>
                      <div className="mt-1 text-[11px] text-secondary">
                        {(distributor.resources?.length || 0)} {t('pddl.resources')} · {(distributor.states?.length || 0)} {t('pddl.states')}
                      </div>
                    </div>
                    <div className="flex gap-1 opacity-0 transition group-hover:opacity-100">
                      <button
                        onClick={e => { e.stopPropagation(); setEditingName(distributor.id); setEditNameValue(distributor.name) }}
                        className="rounded-xl p-1 text-muted hover:text-primary"
                      >
                        <Edit size={14} />
                      </button>
                      <button
                        onClick={e => { e.stopPropagation(); handleDelete(distributor.id) }}
                        className="rounded-xl p-1 text-muted hover:text-red-400"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </aside>

      {!selected ? (
        <div className="flex min-h-[260px] items-center justify-center rounded-3xl border border-dashed border-border bg-base/30 px-6 text-center text-sm text-muted">
          {distributors.length > 0 ? t('pddl.selectDistributorToConfigure') : t('pddl.noDistributors')}
        </div>
      ) : (
        <div className="space-y-5">
          <section className="rounded-3xl border border-border bg-base/70 p-4">
            <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted">{t('pddl.taskDistributor')}</div>
                <h3 className="mt-2 text-lg font-semibold text-primary">{selected.name}</h3>
                <p className="mt-1 text-sm text-secondary">{t('pddl.distributorBuilderHint')}</p>
              </div>
              <div className="flex flex-wrap gap-2">
                <TabButton
                  active={activeTab === 'builder'}
                  onClick={() => setActiveTab('builder')}
                  icon={Boxes}
                  label={t('pddl.builderTab')}
                />
                <TabButton
                  active={activeTab === 'manual'}
                  onClick={() => setActiveTab('manual')}
                  icon={SlidersHorizontal}
                  label={t('pddl.manualTab')}
                />
              </div>
            </div>
          </section>

          {activeTab === 'builder' ? (
            <div className="grid gap-5 xl:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.9fr)]">
              <section className="rounded-3xl border border-border bg-base/70 p-5">
                <div className="flex items-center gap-2 text-sm font-semibold text-primary">
                  <Sparkles size={18} className="text-accent" />
                  {t('pddl.resourceTypeGeneratorTitle')}
                </div>
                <p className="mt-2 text-sm leading-6 text-secondary">{t('pddl.resourceTypeGeneratorHint')}</p>

                <div className="mt-5 grid gap-4 md:grid-cols-2">
                  <label className="block">
                    <span className="mb-2 block text-[11px] font-semibold uppercase tracking-[0.18em] text-muted">
                      {t('pddl.resourceTypeName')}
                    </span>
                    <input
                      className="w-full rounded-2xl border border-border bg-surface px-3 py-3 text-sm text-primary outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      placeholder={t('pddl.resourceTypePlaceholder')}
                      value={resourceTypeName}
                      onChange={e => setResourceTypeName(e.target.value)}
                    />
                  </label>

                  <label className="block">
                    <span className="mb-2 block text-[11px] font-semibold uppercase tracking-[0.18em] text-muted">
                      {t('pddl.instanceCount')}
                    </span>
                    <input
                      type="number"
                      min={1}
                      max={32}
                      className="w-full rounded-2xl border border-border bg-surface px-3 py-3 text-sm text-primary outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      value={instanceCount}
                      onChange={e => setInstanceCount(Math.max(1, Number(e.target.value) || 1))}
                    />
                  </label>

                  <label className="block md:col-span-2">
                    <span className="mb-2 block text-[11px] font-semibold uppercase tracking-[0.18em] text-muted">
                      {t('common.description')}
                    </span>
                    <input
                      className="w-full rounded-2xl border border-border bg-surface px-3 py-3 text-sm text-primary outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      placeholder={t('pddl.resourceDescriptionPlaceholder')}
                      value={resourceDescription}
                      onChange={e => setResourceDescription(e.target.value)}
                    />
                  </label>
                </div>

                <label className="mt-4 flex items-center gap-3 rounded-2xl border border-border bg-surface px-4 py-3 text-sm text-primary">
                  <input
                    type="checkbox"
                    checked={autoCreateOccupancyStates}
                    onChange={e => setAutoCreateOccupancyStates(e.target.checked)}
                    className="h-4 w-4 rounded border-border"
                  />
                  <div>
                    <div className="font-medium">{t('pddl.autoCreateOccupancyStates')}</div>
                    <div className="mt-1 text-xs text-secondary">{t('pddl.autoCreateOccupancyStatesHint')}</div>
                  </div>
                </label>

                <div className="mt-5 rounded-2xl border border-border bg-surface p-4">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-sm font-semibold text-primary">{t('pddl.generatorPreviewTitle')}</div>
                      <div className="mt-1 text-xs text-secondary">{t('pddl.generatorPreviewHint')}</div>
                    </div>
                    <span className="rounded-full bg-base px-3 py-1 text-[11px] font-medium text-secondary">
                      {generatedResourceNames.length}
                    </span>
                  </div>

                  <div className="mt-4 grid gap-3 sm:grid-cols-2">
                    {generatedResourceNames.length > 0 ? (
                      generatedResourceNames.map((resourceName, index) => (
                        <div key={resourceName} className="rounded-2xl border border-border bg-base/60 p-3">
                          <div className="text-[11px] uppercase tracking-[0.18em] text-muted">
                            {t('pddl.instanceLabel')} {index + 1}
                          </div>
                          <div className="mt-2 text-sm font-semibold text-primary">{resourceName}</div>
                          {autoCreateOccupancyStates && (
                            <div className="mt-3 rounded-2xl bg-accent/10 px-3 py-2 text-[11px] text-accent">
                              {generatedStateNames[index]}
                            </div>
                          )}
                        </div>
                      ))
                    ) : (
                      <div className="rounded-2xl border border-dashed border-border bg-base/40 px-4 py-8 text-center text-sm text-muted sm:col-span-2">
                        {t('pddl.generatorPreviewEmpty')}
                      </div>
                    )}
                  </div>
                </div>

                {builderMessage && (
                  <div className="mt-4 rounded-2xl border border-accent/20 bg-accent/10 px-4 py-3 text-sm text-accent">
                    {builderMessage}
                  </div>
                )}

                <div className="mt-5">
                  <button
                    onClick={handleGenerateResourceType}
                    disabled={!selected || generatedResourceNames.length === 0 || isGenerating}
                    className="inline-flex items-center gap-2 rounded-2xl bg-accent px-4 py-3 text-sm font-medium text-white transition hover:bg-accent/80 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    {isGenerating ? <Check size={16} className="animate-pulse" /> : <Plus size={16} />}
                    {t('pddl.generateResourceType')}
                  </button>
                </div>
              </section>

              <section className="rounded-3xl border border-border bg-base/70 p-5">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <div className="text-sm font-semibold text-primary">{t('pddl.currentResourceTypesTitle')}</div>
                    <div className="mt-1 text-xs text-secondary">{t('pddl.currentResourceTypesHint')}</div>
                  </div>
                  <span className="rounded-full bg-surface px-3 py-1 text-[11px] font-medium text-secondary">
                    {groupedResources.length}
                  </span>
                </div>

                <div className="mt-4 space-y-3">
                  {groupedResources.length > 0 ? (
                    groupedResources.map(group => (
                      <div key={group.typeName} className="rounded-2xl border border-border bg-surface p-4">
                        <div className="flex items-center justify-between gap-3">
                          <div>
                            <div className="text-sm font-semibold text-primary">{group.typeName}</div>
                            {group.typeResource && (
                              <div className="mt-1 text-[11px] text-secondary">
                                {group.typeResource.description || 'resource type'}
                              </div>
                            )}
                          </div>
                          <span className="rounded-full bg-base px-2.5 py-1 text-[11px] font-medium text-secondary">
                            {group.items.length}
                          </span>
                        </div>
                        <div className="mt-3 flex flex-wrap gap-2">
                          {group.items.map(item => (
                            <span key={item.id} className="rounded-full bg-base px-2.5 py-1 text-[11px] text-secondary">
                              {item.name}
                            </span>
                          ))}
                        </div>
                      </div>
                    ))
                  ) : (
                    <div className="rounded-2xl border border-dashed border-border bg-base/40 px-4 py-8 text-center text-sm text-muted">
                      {t('pddl.currentResourceTypesEmpty')}
                    </div>
                  )}
                </div>
              </section>
            </div>
          ) : (
            <div className="grid gap-5 xl:grid-cols-[minmax(0,1.1fr)_minmax(320px,0.9fr)]">
              <section className="rounded-3xl border border-border bg-base/70 p-5">
                <div className="mb-4 flex items-center justify-between gap-3">
                  <div className="text-sm font-semibold text-primary">
                    {t('pddl.states')} ({selected.states?.length || 0})
                  </div>
                </div>
                <div className="overflow-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="text-left text-[11px] uppercase tracking-[0.18em] text-muted">
                        <th className="pb-3 pr-3 font-medium">{t('common.name')}</th>
                        <th className="pb-3 pr-3 font-medium">{t('common.type')}</th>
                        <th className="pb-3 pr-3 font-medium">{t('pddl.initialValue')}</th>
                        <th className="pb-3 w-10 font-medium" />
                      </tr>
                    </thead>
                    <tbody>
                      {(selected.states || []).map(state => {
                        const badge = TYPE_BADGE[state.type] || TYPE_BADGE.string
                        const isEditing = editingStateId === state.id

                        return (
                          <tr key={state.id} className="border-t border-border">
                            <td className="py-3 pr-3 font-mono text-accent">{state.name}</td>
                            <td className="py-3 pr-3">
                              <span className={`rounded-full px-2 py-1 text-[11px] font-medium ${badge.bg} ${badge.text}`}>{state.type}</span>
                            </td>
                            <td className="py-3 pr-3">
                              {isEditing ? (
                                <div className="flex items-center gap-2">
                                  <input
                                    className="w-28 rounded-xl border border-border bg-surface px-2 py-1.5 text-sm text-primary"
                                    value={editStateInitialValue}
                                    onChange={e => setEditStateInitialValue(e.target.value)}
                                    onKeyDown={e => e.key === 'Enter' && handleUpdateStateInitialValue(state)}
                                    autoFocus
                                  />
                                  <button onClick={() => handleUpdateStateInitialValue(state)} className="p-1 text-green-400"><Check size={14} /></button>
                                  <button onClick={() => setEditingStateId(null)} className="p-1 text-muted"><X size={14} /></button>
                                </div>
                              ) : (
                                <button
                                  onClick={() => { setEditingStateId(state.id); setEditStateInitialValue(state.initial_value || '') }}
                                  className="text-left text-secondary transition hover:text-primary"
                                  title={t('pddl.editInitialValue')}
                                >
                                  {state.initial_value || '-'}
                                </button>
                              )}
                            </td>
                            <td className="py-3">
                              <button
                                onClick={() => handleDeleteState(state.id)}
                                className="rounded-xl p-1 text-muted transition hover:text-red-400"
                              >
                                <Trash2 size={14} />
                              </button>
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>
                <div className="mt-4 grid gap-3 md:grid-cols-[minmax(0,1fr)_120px_120px_auto]">
                  <input
                    className="rounded-2xl border border-border bg-surface px-3 py-3 text-sm text-primary"
                    placeholder={t('pddl.stateName')}
                    value={newStateName}
                    onChange={e => setNewStateName(e.target.value)}
                    onKeyDown={e => e.key === 'Enter' && handleAddState()}
                  />
                  <select
                    className="rounded-2xl border border-border bg-surface px-3 py-3 text-sm text-primary"
                    value={newStateType}
                    onChange={e => setNewStateType(e.target.value as 'bool' | 'int' | 'string')}
                  >
                    <option value="bool">bool</option>
                    <option value="int">int</option>
                    <option value="string">string</option>
                  </select>
                  <input
                    className="rounded-2xl border border-border bg-surface px-3 py-3 text-sm text-primary"
                    placeholder={t('pddl.initialValue')}
                    value={newStateInitialValue}
                    onChange={e => setNewStateInitialValue(e.target.value)}
                    onKeyDown={e => e.key === 'Enter' && handleAddState()}
                  />
                  <button
                    onClick={handleAddState}
                    disabled={!newStateName.trim()}
                    className="inline-flex items-center justify-center rounded-2xl bg-accent px-4 py-3 text-sm font-medium text-white disabled:opacity-40"
                  >
                    <Plus size={16} />
                  </button>
                </div>
              </section>

              <section className="rounded-3xl border border-border bg-base/70 p-5">
                <div className="mb-4 flex items-center justify-between gap-3">
                  <div className="text-sm font-semibold text-primary">
                    {t('pddl.resources')} ({selected.resources?.length || 0})
                  </div>
                </div>
                <div className="space-y-3">
                  {(selected.resources || []).map(resource => {
                    const isEditing = editingResourceId === resource.id
                    const parentTypeName = resource.parent_resource_id
                      ? (selected.resources || []).find(item => item.id === resource.parent_resource_id)?.name
                      : ''
                    const kindLabel = resource.kind === 'type' ? 'TYPE' : 'INSTANCE'

                    return (
                      <div key={resource.id} className="rounded-2xl border border-border bg-surface p-3">
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <div className="truncate font-mono text-sm font-semibold text-yellow-400">{resource.name}</div>
                              <span className="rounded-full bg-base px-2 py-0.5 text-[10px] font-medium text-secondary">{kindLabel}</span>
                            </div>
                            {parentTypeName && (
                              <div className="mt-1 text-[11px] text-secondary">{parentTypeName}</div>
                            )}
                            {isEditing ? (
                              <div className="mt-2 flex items-center gap-2">
                                <input
                                  className="w-full rounded-xl border border-border bg-base px-2 py-1.5 text-sm text-primary"
                                  value={editResourceDescription}
                                  onChange={e => setEditResourceDescription(e.target.value)}
                                  onKeyDown={e => e.key === 'Enter' && handleUpdateResourceDescription(resource)}
                                  autoFocus
                                  placeholder={t('common.description')}
                                />
                                <button onClick={() => handleUpdateResourceDescription(resource)} className="p-1 text-green-400"><Check size={14} /></button>
                                <button onClick={() => setEditingResourceId(null)} className="p-1 text-muted"><X size={14} /></button>
                              </div>
                            ) : (
                              <button
                                onClick={() => { setEditingResourceId(resource.id); setEditResourceDescription(resource.description || '') }}
                                className="mt-2 text-left text-sm text-secondary transition hover:text-primary"
                                title={resource.description || t('pddl.editDescription')}
                              >
                                {resource.description || '-'}
                              </button>
                            )}
                          </div>
                          <button
                            onClick={() => handleDeleteResource(resource.id)}
                            className="rounded-xl p-1 text-muted transition hover:text-red-400"
                          >
                            <Trash2 size={14} />
                          </button>
                        </div>
                      </div>
                    )
                  })}
                </div>
                <div className="mt-4 flex gap-3">
                  <input
                    className="flex-1 rounded-2xl border border-border bg-surface px-3 py-3 text-sm text-primary"
                    placeholder={t('pddl.resourceName')}
                    value={newResourceName}
                    onChange={e => setNewResourceName(e.target.value)}
                    onKeyDown={e => e.key === 'Enter' && handleAddResource()}
                  />
                  <button
                    onClick={handleAddResource}
                    disabled={!newResourceName.trim()}
                    className="inline-flex items-center justify-center rounded-2xl bg-accent px-4 py-3 text-sm font-medium text-white disabled:opacity-40"
                  >
                    <Plus size={16} />
                  </button>
                </div>
              </section>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function TabButton({
  active,
  onClick,
  icon: Icon,
  label,
}: {
  active: boolean
  onClick: () => void
  icon: typeof Boxes
  label: string
}) {
  return (
    <button
      onClick={onClick}
      className={`inline-flex items-center gap-2 rounded-2xl border px-4 py-3 text-sm font-medium transition ${
        active
          ? 'border-accent/30 bg-accent/10 text-accent'
          : 'border-border bg-surface text-secondary hover:text-primary'
      }`}
    >
      <Icon size={16} />
      {label}
    </button>
  )
}

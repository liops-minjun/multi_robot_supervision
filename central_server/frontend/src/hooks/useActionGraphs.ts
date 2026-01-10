import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { actionGraphApi, agentApi } from '../api/client'
import type { GraphCreateRequest } from '../types'

// Agent hooks
export function useAgents() {
  return useQuery({
    queryKey: ['agents'],
    queryFn: () => agentApi.list(),
  })
}

export function useAgent(agentId: string | null) {
  return useQuery({
    queryKey: ['agent', agentId],
    queryFn: () => agentApi.get(agentId!),
    enabled: !!agentId,
  })
}

// Action Graph hooks
export function useActionGraphs(params?: {
  agentId?: string
  includeTemplates?: boolean
}) {
  return useQuery({
    queryKey: ['actionGraphs', params?.agentId, params?.includeTemplates],
    queryFn: () => actionGraphApi.list(params),
  })
}

export function useActionGraph(id: string | null) {
  return useQuery({
    queryKey: ['actionGraph', id],
    queryFn: () => actionGraphApi.get(id!),
    enabled: !!id,
  })
}

export function useCreateActionGraph() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (actionGraph: GraphCreateRequest) => actionGraphApi.create(actionGraph),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ['actionGraphs'] })
      // Also invalidate agent-specific queries if agent_id was provided
      if (variables.agent_id) {
        queryClient.invalidateQueries({ queryKey: ['actionGraphs', variables.agent_id] })
      }
    },
  })
}

export function useUpdateActionGraph() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<GraphCreateRequest> }) =>
      actionGraphApi.update(id, data),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ['actionGraphs'] })
      queryClient.invalidateQueries({ queryKey: ['actionGraph', variables.id] })
    },
  })
}

export function useDeleteActionGraph() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (id: string) => actionGraphApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['actionGraphs'] })
    },
  })
}

export function useExecuteActionGraph() {
  return useMutation({
    mutationFn: ({ flowId, agentId, params }: {
      flowId: string
      agentId: string
      params?: Record<string, unknown>
    }) => actionGraphApi.execute(flowId, agentId, params),
  })
}

export function useValidateActionGraph() {
  return useMutation({
    mutationFn: (id: string) => actionGraphApi.validate(id),
  })
}

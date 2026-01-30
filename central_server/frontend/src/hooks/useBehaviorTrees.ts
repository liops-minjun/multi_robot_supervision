import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { behaviorTreeApi, agentApi } from '../api/client'
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

// Behavior Tree hooks
export function useBehaviorTrees(params?: {
  agentId?: string
  includeTemplates?: boolean
}) {
  return useQuery({
    queryKey: ['behaviorTrees', params?.agentId, params?.includeTemplates],
    queryFn: () => behaviorTreeApi.list(params),
  })
}

export function useBehaviorTree(id: string | null) {
  return useQuery({
    queryKey: ['behaviorTree', id],
    queryFn: () => behaviorTreeApi.get(id!),
    enabled: !!id,
  })
}

export function useCreateBehaviorTree() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (behaviorTree: GraphCreateRequest) => behaviorTreeApi.create(behaviorTree),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ['behaviorTrees'] })
      // Also invalidate agent-specific queries if agent_id was provided
      if (variables.agent_id) {
        queryClient.invalidateQueries({ queryKey: ['behaviorTrees', variables.agent_id] })
      }
    },
  })
}

export function useUpdateBehaviorTree() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<GraphCreateRequest> }) =>
      behaviorTreeApi.update(id, data),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ['behaviorTrees'] })
      queryClient.invalidateQueries({ queryKey: ['behaviorTree', variables.id] })
    },
  })
}

export function useDeleteBehaviorTree() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (id: string) => behaviorTreeApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['behaviorTrees'] })
    },
  })
}

export function useExecuteBehaviorTree() {
  return useMutation({
    mutationFn: ({ flowId, agentId, params }: {
      flowId: string
      agentId: string
      params?: Record<string, unknown>
    }) => behaviorTreeApi.execute(flowId, agentId, params),
  })
}

export function useValidateBehaviorTree() {
  return useMutation({
    mutationFn: (id: string) => behaviorTreeApi.validate(id),
  })
}

// Backward compatibility aliases
export const useActionGraphs = useBehaviorTrees
export const useActionGraph = useBehaviorTree
export const useCreateActionGraph = useCreateBehaviorTree
export const useUpdateActionGraph = useUpdateBehaviorTree
export const useDeleteActionGraph = useDeleteBehaviorTree
export const useExecuteActionGraph = useExecuteBehaviorTree
export const useValidateActionGraph = useValidateBehaviorTree

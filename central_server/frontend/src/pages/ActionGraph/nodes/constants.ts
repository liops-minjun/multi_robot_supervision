import type { ActionOutcome, DuringStateTarget } from '../../../types'

// Auto-generated state suffixes
export const AUTO_STATE_SUFFIXES = {
  during: '_during',
  succeed: '_succeed',
  failed: '_failed',
  aborted: '_aborted',
  cancelled: '_cancelled',
  timeout: '_timeout',
} as const

// Color palette for end states
export const OUTCOME_COLORS: Record<ActionOutcome, string> = {
  success: '#22c55e',
  failed: '#ef4444',
  aborted: '#ef4444',
  cancelled: '#6b7280',
  timeout: '#f59e0b',
  rejected: '#ef4444',
}

export const OUTCOME_HANDLE_IDS: Record<ActionOutcome, string> = {
  success: 'success',
  failed: 'failed',
  aborted: 'aborted',
  cancelled: 'cancelled',
  timeout: 'timeout',
  rejected: 'failed',
}

export const END_STATE_COLORS: Record<string, string> = {
  success: '#22c55e',
  completed: '#22c55e',
  idle: '#22c55e',
  error: '#ef4444',
  failed: '#ef4444',
  timeout: '#f59e0b',
  partial: '#eab308',
  cancelled: '#6b7280',
}

export const OUTCOME_OPTIONS: Array<{ value: ActionOutcome; label: string }> = [
  { value: 'success', label: '성공' },
  { value: 'failed', label: '실패' },
  { value: 'aborted', label: '중단' },
  { value: 'cancelled', label: '취소' },
  { value: 'timeout', label: '타임아웃' },
  { value: 'rejected', label: '거부됨' },
]

export const DURING_TARGET_OPTIONS: Array<{ value: NonNullable<DuringStateTarget['target_type']>; label: string }> = [
  { value: 'self', label: '자신' },
  { value: 'all', label: '전체' },
  { value: 'agent', label: '에이전트' },
]

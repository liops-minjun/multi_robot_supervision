package state

import (
	"sync"
	"time"
)

// TaskLogLevel represents the log severity level
type TaskLogLevel int32

const (
	TaskLogDebug TaskLogLevel = 0
	TaskLogInfo  TaskLogLevel = 1
	TaskLogWarn  TaskLogLevel = 2
	TaskLogError TaskLogLevel = 3
)

// TaskLogEntry represents a single task execution log entry
type TaskLogEntry struct {
	AgentID     string            `json:"agent_id"`
	TaskID      string            `json:"task_id"`
	StepID      string            `json:"step_id"`
	CommandID   string            `json:"command_id"`
	Level       TaskLogLevel      `json:"level"`
	LevelStr    string            `json:"level_str"`
	Message     string            `json:"message"`
	Component   string            `json:"component"`
	TimestampMs int64             `json:"timestamp_ms"`
	Timestamp   time.Time         `json:"timestamp"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// TaskLogManager manages task execution logs with a bounded ring buffer
type TaskLogManager struct {
	mu sync.RWMutex

	// Ring buffer for all logs (bounded to prevent memory growth)
	logs     []TaskLogEntry
	logIndex int // Current write position
	logCount int // Total logs written (for overflow detection)

	// Index by task ID for efficient lookups
	taskLogs map[string][]int // task_id -> indices in logs buffer

	// Subscribers for real-time log streaming
	subscribers map[string]chan TaskLogEntry // subscriber_id -> channel
	subMu       sync.RWMutex

	// Configuration
	maxLogs    int // Maximum logs to retain
	maxPerTask int // Maximum logs per task
}

// NewTaskLogManager creates a new task log manager
func NewTaskLogManager() *TaskLogManager {
	return &TaskLogManager{
		logs:        make([]TaskLogEntry, 1000), // Pre-allocate 1000 slots
		taskLogs:    make(map[string][]int),
		subscribers: make(map[string]chan TaskLogEntry),
		maxLogs:     10000,
		maxPerTask:  500,
	}
}

// AddLog adds a new task log entry
func (m *TaskLogManager) AddLog(entry TaskLogEntry) {
	m.mu.Lock()

	// Set timestamp if not provided
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.UnixMilli(entry.TimestampMs)
	}
	if entry.TimestampMs == 0 {
		entry.TimestampMs = time.Now().UnixMilli()
		entry.Timestamp = time.Now()
	}

	// Set level string
	entry.LevelStr = levelToString(entry.Level)

	// Add to ring buffer
	idx := m.logIndex % m.maxLogs
	if m.logIndex >= m.maxLogs {
		// Buffer is full, overwrite oldest entry
		oldEntry := m.logs[idx]
		// Remove from task index
		if oldEntry.TaskID != "" {
			m.removeFromTaskIndex(oldEntry.TaskID, idx)
		}
	}

	// Grow buffer if needed
	if idx >= len(m.logs) {
		m.logs = append(m.logs, entry)
	} else {
		m.logs[idx] = entry
	}

	// Add to task index
	if entry.TaskID != "" {
		m.taskLogs[entry.TaskID] = append(m.taskLogs[entry.TaskID], idx)
		// Trim task logs if too many
		if len(m.taskLogs[entry.TaskID]) > m.maxPerTask {
			m.taskLogs[entry.TaskID] = m.taskLogs[entry.TaskID][1:]
		}
	}

	m.logIndex++
	m.logCount++

	m.mu.Unlock()

	// Notify subscribers (outside lock)
	m.notifySubscribers(entry)
}

// GetTaskLogs returns logs for a specific task
func (m *TaskLogManager) GetTaskLogs(taskID string, limit int) []TaskLogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	indices, exists := m.taskLogs[taskID]
	if !exists {
		return nil
	}

	// Return most recent logs first
	result := make([]TaskLogEntry, 0, len(indices))
	for i := len(indices) - 1; i >= 0; i-- {
		idx := indices[i]
		if idx < len(m.logs) {
			result = append(result, m.logs[idx])
		}
		if limit > 0 && len(result) >= limit {
			break
		}
	}

	return result
}

// GetAgentLogs returns recent logs for a specific agent
func (m *TaskLogManager) GetAgentLogs(agentID string, limit int) []TaskLogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	result := make([]TaskLogEntry, 0, limit)

	// Scan from most recent
	end := m.logIndex
	start := end - m.maxLogs
	if start < 0 {
		start = 0
	}

	for i := end - 1; i >= start && len(result) < limit; i-- {
		idx := i % m.maxLogs
		if idx < len(m.logs) && m.logs[idx].AgentID == agentID {
			result = append(result, m.logs[idx])
		}
	}

	return result
}

// GetRecentLogs returns the most recent logs across all agents
func (m *TaskLogManager) GetRecentLogs(limit int) []TaskLogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	result := make([]TaskLogEntry, 0, limit)

	end := m.logIndex
	start := end - m.maxLogs
	if start < 0 {
		start = 0
	}

	for i := end - 1; i >= start && len(result) < limit; i-- {
		idx := i % m.maxLogs
		if idx < len(m.logs) && m.logs[idx].Message != "" {
			result = append(result, m.logs[idx])
		}
	}

	return result
}

// Subscribe creates a channel for real-time log streaming
func (m *TaskLogManager) Subscribe(subscriberID string) <-chan TaskLogEntry {
	m.subMu.Lock()
	defer m.subMu.Unlock()

	// Create buffered channel
	ch := make(chan TaskLogEntry, 100)
	m.subscribers[subscriberID] = ch

	return ch
}

// Unsubscribe removes a log subscriber
func (m *TaskLogManager) Unsubscribe(subscriberID string) {
	m.subMu.Lock()
	defer m.subMu.Unlock()

	if ch, exists := m.subscribers[subscriberID]; exists {
		close(ch)
		delete(m.subscribers, subscriberID)
	}
}

// GetStats returns log manager statistics
func (m *TaskLogManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"total_logs":      m.logCount,
		"buffer_size":     len(m.logs),
		"max_logs":        m.maxLogs,
		"tasks_tracked":   len(m.taskLogs),
		"subscriber_count": len(m.subscribers),
	}
}

// ClearTaskLogs removes logs for a specific task (e.g., after task completion)
func (m *TaskLogManager) ClearTaskLogs(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.taskLogs, taskID)
}

// Helper functions

func (m *TaskLogManager) removeFromTaskIndex(taskID string, idx int) {
	indices := m.taskLogs[taskID]
	for i, v := range indices {
		if v == idx {
			m.taskLogs[taskID] = append(indices[:i], indices[i+1:]...)
			break
		}
	}
	if len(m.taskLogs[taskID]) == 0 {
		delete(m.taskLogs, taskID)
	}
}

func (m *TaskLogManager) notifySubscribers(entry TaskLogEntry) {
	m.subMu.RLock()
	defer m.subMu.RUnlock()

	for _, ch := range m.subscribers {
		select {
		case ch <- entry:
		default:
			// Channel full, skip
		}
	}
}

func levelToString(level TaskLogLevel) string {
	switch level {
	case TaskLogDebug:
		return "DEBUG"
	case TaskLogInfo:
		return "INFO"
	case TaskLogWarn:
		return "WARN"
	case TaskLogError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

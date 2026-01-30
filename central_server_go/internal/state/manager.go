package state

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// RobotState represents the in-memory state of a robot
type RobotState struct {
	ID            string
	Name          string
	AgentID       string
	CurrentState  string
	ReportedState string
	IsOnline      bool
	IsExecuting   bool
	CurrentTaskID string
	CurrentStepID string
	LastSeen      time.Time

	// Enhanced state tracking
	CurrentStateCode string   // State code (e.g., "pick:executing")
	SemanticTags     []string // Current semantic tags
	CurrentGraphID   string   // Currently executing graph ID

	// Precondition waiting status (for UI display)
	IsWaitingForPrecondition    bool
	WaitingForPreconditionSince time.Time
	BlockingConditions          []BlockingConditionInfo

	// Telemetry data for parameter loading
	Telemetry *RobotTelemetry
}

// AgentConnection represents a connected agent (1:1 model: agent = robot)
type AgentConnection struct {
	ID            string
	Name          string
	IPAddress     string
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	LastPing      time.Time
	PingLatency   time.Duration
}

// ZoneReservation represents a zone lock
type ZoneReservation struct {
	ZoneID     string
	AgentID    string // In 1:1 model, agent_id = robot_id
	ReservedAt time.Time
	ExpiresAt  time.Time
}

// FleetSnapshot is a point-in-time snapshot of the fleet state
type FleetSnapshot struct {
	Timestamp time.Time
	Robots    map[string]*RobotState
	Zones     map[string]*ZoneReservation
	Agents    map[string]*AgentConnection
}

// HeartbeatConfig defines heartbeat timeout settings
type HeartbeatConfig struct {
	Timeout       time.Duration // Time before agent is considered offline
	CheckInterval time.Duration // How often to check for stale connections
}

// DefaultHeartbeatConfig returns default heartbeat settings
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		Timeout:       15 * time.Second, // Mark offline after 15 seconds without heartbeat
		CheckInterval: 5 * time.Second,  // Check every 5 seconds
	}
}

// AgentDisconnectCallback is called when an agent is detected as disconnected
// In 1:1 model, agent_id = robot_id, so only agentID is needed
type AgentDisconnectCallback func(agentID string)

// GlobalStateManager manages all fleet state with thread-safe operations
// This is the SINGLE SOURCE OF TRUTH for runtime state
// All state changes go through this manager to prevent race conditions
type GlobalStateManager struct {
	mu sync.RWMutex

	// Robot states indexed by robot ID
	robots map[string]*RobotState

	// Active state overrides per robot (agentID -> sourceID -> override)
	stateOverrides map[string]map[string]StateOverride

	// Zone reservations indexed by zone ID
	zones map[string]*ZoneReservation

	// Connected agents indexed by agent ID
	agents map[string]*AgentConnection

	// Zone expiry time
	zoneExpiryDuration time.Duration

	// Behavior Tree cache for fast lookup (avoids DB I/O during task execution)
	graphCache *GraphCache

	// Metadata cache for agent/capability/graph metadata (avoids N+1 queries)
	metadataCache *MetadataCache

	// State registry for enhanced state tracking and cross-agent queries
	stateRegistry *StateRegistry

	// Task log manager for execution log streaming
	taskLogManager *TaskLogManager

	// Heartbeat configuration
	heartbeatConfig HeartbeatConfig

	// Callback when agent disconnects
	onAgentDisconnect AgentDisconnectCallback

	// Background worker management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// StateOverride represents a temporary coordination state.
type StateOverride struct {
	State string
	SetAt time.Time
}

// NewGlobalStateManager creates a new state manager
func NewGlobalStateManager() *GlobalStateManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &GlobalStateManager{
		robots:             make(map[string]*RobotState),
		stateOverrides:     make(map[string]map[string]StateOverride),
		zones:              make(map[string]*ZoneReservation),
		agents:             make(map[string]*AgentConnection),
		zoneExpiryDuration: 30 * time.Second,
		graphCache:         NewGraphCache(),
		metadataCache:      NewMetadataCache(30 * time.Second), // 30s TTL
		stateRegistry:      NewStateRegistry(),
		taskLogManager:     NewTaskLogManager(),
		heartbeatConfig:    DefaultHeartbeatConfig(),
		ctx:                ctx,
		cancel:             cancel,
	}
}

// TaskLogManager returns the task log manager for execution log streaming
func (m *GlobalStateManager) TaskLogManager() *TaskLogManager {
	return m.taskLogManager
}

// StateRegistry returns the state registry for enhanced state tracking
func (m *GlobalStateManager) StateRegistry() *StateRegistry {
	return m.stateRegistry
}

// MetadataCache returns the metadata cache for agent/capability lookups
func (m *GlobalStateManager) MetadataCache() *MetadataCache {
	return m.metadataCache
}

// SetHeartbeatConfig sets custom heartbeat configuration
func (m *GlobalStateManager) SetHeartbeatConfig(config HeartbeatConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeatConfig = config
}

// SetOnAgentDisconnect sets the callback for agent disconnection
func (m *GlobalStateManager) SetOnAgentDisconnect(callback AgentDisconnectCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onAgentDisconnect = callback
}

// Start starts background workers for the state manager
func (m *GlobalStateManager) Start() {
	m.wg.Add(3)
	go m.runCacheCleanup()
	go m.runHeartbeatChecker()
	go m.runStaleAgentCleanup()
	log.Println("GlobalStateManager background workers started (cache cleanup + heartbeat checker + stale agent cleanup)")
}

// Stop stops all background workers gracefully
func (m *GlobalStateManager) Stop() {
	m.cancel()
	m.wg.Wait()
	log.Println("GlobalStateManager background workers stopped")
}

// runCacheCleanup periodically cleans up stale cache entries
func (m *GlobalStateManager) runCacheCleanup() {
	defer m.wg.Done()

	// Clean up cache entries older than 1 hour every 10 minutes
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			evicted := m.graphCache.EvictStale(1 * time.Hour)
			if evicted > 0 {
				log.Printf("Evicted %d stale graph cache entries", evicted)
			}
		}
	}
}

// runHeartbeatChecker periodically checks for stale agent connections
func (m *GlobalStateManager) runHeartbeatChecker() {
	defer m.wg.Done()

	m.mu.RLock()
	checkInterval := m.heartbeatConfig.CheckInterval
	m.mu.RUnlock()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkStaleAgents()
		}
	}
}

// checkStaleAgents checks for agents that haven't sent heartbeat within timeout
func (m *GlobalStateManager) checkStaleAgents() {
	m.mu.Lock()

	now := time.Now()
	timeout := m.heartbeatConfig.Timeout
	staleAgentIDs := make([]string, 0)

	// Find stale agents
	for agentID, agent := range m.agents {
		timeSinceHeartbeat := now.Sub(agent.LastHeartbeat)
		if timeSinceHeartbeat > timeout {
			staleAgentIDs = append(staleAgentIDs, agentID)
		}
	}

	// Mark stale agents as offline (1:1 model: agent_id = robot_id)
	for _, agentID := range staleAgentIDs {
		log.Printf("[Heartbeat] Agent %s timed out (last heartbeat > %v ago)", agentID, timeout)

		// In 1:1 model, agent_id = robot_id, so mark the robot with same ID as offline
		if robot, exists := m.robots[agentID]; exists {
			robot.IsOnline = false
		}

		// Remove agent from connected list
		delete(m.agents, agentID)
	}

	// Get callback reference while holding lock
	callback := m.onAgentDisconnect
	m.mu.Unlock()

	// Call disconnect callbacks outside of lock to prevent deadlock
	if callback != nil {
		for _, agentID := range staleAgentIDs {
			callback(agentID)
		}
	}

	// Invalidate graph caches for disconnected agents
	for _, agentID := range staleAgentIDs {
		m.graphCache.InvalidateAgentCache(agentID)
	}
}

// runStaleAgentCleanup periodically removes agents that have been offline for too long
// This prevents accumulation of orphan agents when fleet agents reconnect with new IDs
func (m *GlobalStateManager) runStaleAgentCleanup() {
	defer m.wg.Done()

	// Clean up agents offline for more than 1 hour, check every 10 minutes
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			removed := m.CleanupStaleAgents(1 * time.Hour)
			if removed > 0 {
				log.Printf("[StaleCleanup] Removed %d stale agents from in-memory state", removed)
			}
		}
	}
}

// CleanupStaleAgents removes agents that have been offline longer than maxOfflineAge
// Returns the number of agents removed
func (m *GlobalStateManager) CleanupStaleAgents(maxOfflineAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	count := 0
	toRemove := make([]string, 0)

	for agentID, robot := range m.robots {
		if !robot.IsOnline {
			offlineAge := now.Sub(robot.LastSeen)
			if offlineAge > maxOfflineAge {
				toRemove = append(toRemove, agentID)
			}
		}
	}

	for _, agentID := range toRemove {
		log.Printf("[StaleCleanup] Removing stale agent: %s (offline for %v)",
			agentID, now.Sub(m.robots[agentID].LastSeen).Round(time.Second))
		delete(m.robots, agentID)
		delete(m.stateOverrides, agentID)
		delete(m.agents, agentID)
		m.graphCache.InvalidateAgentCache(agentID)
		count++
	}

	return count
}

// GraphCache returns the behavior tree cache
func (m *GlobalStateManager) GraphCache() *GraphCache {
	return m.graphCache
}

// ============================================================
// Robot State Operations (Atomic)
// ============================================================

// RegisterRobot adds or updates a robot in the state manager
func (m *GlobalStateManager) RegisterRobot(id, name, agentID, initialState string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stateCode := initialState
	if stateCode == "" {
		stateCode = "idle"
	}

	robot := &RobotState{
		ID:               id,
		Name:             name,
		AgentID:          agentID,
		CurrentState:     initialState,
		ReportedState:    initialState,
		CurrentStateCode: stateCode,
		SemanticTags:     []string{},
		IsOnline:         true,
		LastSeen:         time.Now(),
	}
	m.robots[id] = robot

	// Also register in state registry for cross-agent queries
	m.stateRegistry.UpdateAgentState(id, stateCode, nil, "", true, false)
}

// UnregisterRobot removes a robot from the state manager
// In 1:1 model, id = agent_id = robot_id
func (m *GlobalStateManager) UnregisterRobot(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.robots, id)
	delete(m.stateOverrides, id)
	// Also release any zone reservations
	for zoneID, res := range m.zones {
		if res.AgentID == id {
			delete(m.zones, zoneID)
		}
	}
	// Remove from state registry
	m.stateRegistry.RemoveAgent(id)
}

// GetRobotState returns a copy of the robot state (thread-safe read)
func (m *GlobalStateManager) GetRobotState(agentID string) (*RobotState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.robots[agentID]
	if !exists {
		return nil, false
	}
	// Return a copy to prevent external mutation
	stateCopy := *state
	return &stateCopy, true
}

// UpdateRobotState atomically updates a robot's state
func (m *GlobalStateManager) UpdateRobotState(agentID, newState string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return fmt.Errorf("robot %s not found", agentID)
	}

	robot.ReportedState = newState
	if !m.hasStateOverrideLocked(agentID) {
		robot.CurrentState = newState
	} else {
		if override := m.effectiveOverrideLocked(agentID); override != "" {
			robot.CurrentState = override
		}
	}
	robot.LastSeen = time.Now()
	return nil
}

// UpdateRobotEnhancedState atomically updates a robot's enhanced state (code + tags)
// Also updates the StateRegistry for cross-agent queries
func (m *GlobalStateManager) UpdateRobotEnhancedState(agentID, stateCode string, semanticTags []string, graphID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return fmt.Errorf("robot %s not found", agentID)
	}

	robot.CurrentStateCode = stateCode
	robot.SemanticTags = semanticTags
	robot.CurrentGraphID = graphID
	robot.LastSeen = time.Now()

	// Update state registry for cross-agent queries
	m.stateRegistry.UpdateAgentState(agentID, stateCode, semanticTags, graphID, robot.IsOnline, robot.IsExecuting)

	return nil
}

// GetRobotEnhancedState returns the enhanced state info for a robot
func (m *GlobalStateManager) GetRobotEnhancedState(agentID string) (stateCode string, semanticTags []string, graphID string, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return "", nil, "", false
	}

	return robot.CurrentStateCode, robot.SemanticTags, robot.CurrentGraphID, true
}

// SetRobotStateOverride applies a temporary coordination state for a robot.
func (m *GlobalStateManager) SetRobotStateOverride(agentID, sourceID, state string) error {
	if state == "" {
		return fmt.Errorf("state override is empty")
	}
	if sourceID == "" {
		return fmt.Errorf("state override source is empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return fmt.Errorf("robot %s not found", agentID)
	}

	if m.stateOverrides[agentID] == nil {
		m.stateOverrides[agentID] = make(map[string]StateOverride)
	}
	m.stateOverrides[agentID][sourceID] = StateOverride{
		State: state,
		SetAt: time.Now(),
	}
	robot.CurrentState = m.effectiveOverrideLocked(agentID)
	robot.LastSeen = time.Now()
	return nil
}

// ClearRobotStateOverride removes a temporary coordination state for a robot.
func (m *GlobalStateManager) ClearRobotStateOverride(agentID, sourceID string) error {
	if sourceID == "" {
		return fmt.Errorf("state override source is empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return fmt.Errorf("robot %s not found", agentID)
	}

	if overrides, ok := m.stateOverrides[agentID]; ok {
		delete(overrides, sourceID)
		if len(overrides) == 0 {
			delete(m.stateOverrides, agentID)
		}
	}

	if m.hasStateOverrideLocked(agentID) {
		if override := m.effectiveOverrideLocked(agentID); override != "" {
			robot.CurrentState = override
		}
	} else if robot.ReportedState != "" {
		robot.CurrentState = robot.ReportedState
	}
	robot.LastSeen = time.Now()
	return nil
}

func (m *GlobalStateManager) hasStateOverrideLocked(agentID string) bool {
	overrides, ok := m.stateOverrides[agentID]
	return ok && len(overrides) > 0
}

func (m *GlobalStateManager) effectiveOverrideLocked(agentID string) string {
	overrides, ok := m.stateOverrides[agentID]
	if !ok || len(overrides) == 0 {
		return ""
	}
	var latest StateOverride
	found := false
	for _, override := range overrides {
		if !found || override.SetAt.After(latest.SetAt) {
			latest = override
			found = true
		}
	}
	if found {
		return latest.State
	}
	return ""
}

// ResolveTargetAgents returns agent IDs for a state target selection.
// In 1:1 model, agent_id = robot_id, so this returns the same IDs.
func (m *GlobalStateManager) ResolveTargetAgents(executingAgentID, targetType, targetAgentID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	targetType = strings.ToLower(targetType)
	var ids []string

	switch targetType {
	case "", "self":
		if _, exists := m.robots[executingAgentID]; exists {
			ids = append(ids, executingAgentID)
		}
	case "all":
		ids = make([]string, 0, len(m.robots))
		for id := range m.robots {
			ids = append(ids, id)
		}
	case "agent", "specific":
		if targetAgentID == "" {
			return nil
		}
		// In 1:1 model, robot.ID = agent_id
		if _, exists := m.robots[targetAgentID]; exists {
			ids = append(ids, targetAgentID)
		}
	default:
		if _, exists := m.robots[executingAgentID]; exists {
			ids = append(ids, executingAgentID)
		}
	}

	return ids
}

// UpdateRobotExecution atomically updates a robot's execution state
func (m *GlobalStateManager) UpdateRobotExecution(agentID string, isExecuting bool, taskID, stepID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return fmt.Errorf("robot %s not found", agentID)
	}

	robot.IsExecuting = isExecuting
	robot.CurrentTaskID = taskID
	robot.CurrentStepID = stepID
	robot.LastSeen = time.Now()

	// Update state registry
	m.stateRegistry.SetAgentExecuting(agentID, isExecuting)
	return nil
}

// TryUpdateRobotExecutionFromHeartbeat atomically checks and updates robot execution state from heartbeat.
// Returns (updated bool, error) where updated=false means server is managing execution and heartbeat was ignored.
// This prevents the TOCTOU race condition between checking state and updating it.
func (m *GlobalStateManager) TryUpdateRobotExecutionFromHeartbeat(agentID string, hbIsExecuting bool, hbTaskID, hbStepID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return false, fmt.Errorf("robot %s not found", agentID)
	}

	// Check if server is actively managing a task (inside the lock!)
	if robot.IsExecuting && robot.CurrentTaskID != "" {
		// Server is managing this task - check if we should skip heartbeat update
		if hbTaskID != robot.CurrentTaskID || !hbIsExecuting {
			// Different task, or agent reports not executing - skip to preserve server state
			// Agent may report is_executing=false between steps, but server knows task is still running
			return false, nil // Skipped, not an error
		}
		// Same task and agent reports executing - update CurrentStepID from heartbeat
		// Agent knows which step it's currently executing, so we should reflect that
		if hbStepID != "" && hbStepID != robot.CurrentStepID {
			robot.CurrentStepID = hbStepID
			robot.LastSeen = time.Now()
			return true, nil // Updated step from heartbeat
		}
		return false, nil // No change needed
	}

	// No server-managed task running - accept heartbeat update
	// But if agent reports not executing with a stale task_id, clear it
	if !hbIsExecuting && hbTaskID != "" {
		// Agent is not executing but reports a task_id - this is stale data
		// Clear execution state completely
		robot.IsExecuting = false
		robot.CurrentTaskID = ""
		robot.CurrentStepID = ""
	} else {
		// Use heartbeat values as-is
		robot.IsExecuting = hbIsExecuting
		robot.CurrentTaskID = hbTaskID
		robot.CurrentStepID = hbStepID
	}
	robot.LastSeen = time.Now()

	// Update state registry
	m.stateRegistry.SetAgentExecuting(agentID, robot.IsExecuting)
	return true, nil
}

// SetRobotOnline sets a robot's online status
func (m *GlobalStateManager) SetRobotOnline(agentID string, isOnline bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return fmt.Errorf("robot %s not found", agentID)
	}

	robot.IsOnline = isOnline
	robot.LastSeen = time.Now()

	// Update state registry
	m.stateRegistry.SetAgentOnline(agentID, isOnline)
	return nil
}

// SetRobotWaitingForPrecondition updates the precondition waiting status of a robot
func (m *GlobalStateManager) SetRobotWaitingForPrecondition(agentID string, waiting bool, blockingInfos []BlockingConditionInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return fmt.Errorf("robot %s not found", agentID)
	}

	robot.IsWaitingForPrecondition = waiting
	if waiting {
		robot.WaitingForPreconditionSince = time.Now()
		robot.BlockingConditions = blockingInfos
	} else {
		robot.WaitingForPreconditionSince = time.Time{}
		robot.BlockingConditions = nil
	}

	return nil
}

// ============================================================
// Telemetry Operations (for Parameter Loading)
// ============================================================

// UpdateRobotTelemetry updates the telemetry data for a robot
func (m *GlobalStateManager) UpdateRobotTelemetry(robotID string, telemetry *RobotTelemetry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[robotID]
	if !exists {
		// Auto-register the robot if it doesn't exist yet
		// This handles the case where telemetry arrives before formal registration
		log.Printf("[StateManager] Robot %s not found for telemetry, auto-registering", robotID)
		robot = &RobotState{
			ID:               robotID,
			Name:             robotID,
			AgentID:          robotID,
			CurrentState:     "idle",
			ReportedState:    "idle",
			CurrentStateCode: "idle",
			SemanticTags:     []string{},
			IsOnline:         true,
			LastSeen:         time.Now(),
		}
		m.robots[robotID] = robot
	}

	if telemetry != nil {
		telemetry.UpdatedAt = time.Now()
	}
	robot.Telemetry = telemetry

	return nil
}

// GetRobotTelemetry returns the telemetry data for a robot
func (m *GlobalStateManager) GetRobotTelemetry(robotID string) *RobotTelemetry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	robot, exists := m.robots[robotID]
	if !exists || robot.Telemetry == nil {
		return nil
	}

	// Return a copy to avoid race conditions
	telemetryCopy := &RobotTelemetry{
		UpdatedAt: robot.Telemetry.UpdatedAt,
		IsStale:   time.Since(robot.Telemetry.UpdatedAt) > DefaultTelemetryStaleThreshold,
	}

	if robot.Telemetry.JointState != nil {
		js := *robot.Telemetry.JointState
		telemetryCopy.JointState = &js
	}

	if robot.Telemetry.Odometry != nil {
		odom := *robot.Telemetry.Odometry
		telemetryCopy.Odometry = &odom
	}

	if robot.Telemetry.Transforms != nil {
		telemetryCopy.Transforms = make([]TransformData, len(robot.Telemetry.Transforms))
		copy(telemetryCopy.Transforms, robot.Telemetry.Transforms)
	}

	return telemetryCopy
}

// GetRobotJointState returns only the joint state data for a robot
func (m *GlobalStateManager) GetRobotJointState(robotID string) *JointStateData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	robot, exists := m.robots[robotID]
	if !exists || robot.Telemetry == nil || robot.Telemetry.JointState == nil {
		return nil
	}

	js := *robot.Telemetry.JointState
	return &js
}

// GetRobotOdometry returns only the odometry data for a robot
func (m *GlobalStateManager) GetRobotOdometry(robotID string) *OdometryData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	robot, exists := m.robots[robotID]
	if !exists || robot.Telemetry == nil || robot.Telemetry.Odometry == nil {
		return nil
	}

	odom := *robot.Telemetry.Odometry
	return &odom
}

// GetRobotTransforms returns only the transforms for a robot
func (m *GlobalStateManager) GetRobotTransforms(robotID string) []TransformData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	robot, exists := m.robots[robotID]
	if !exists || robot.Telemetry == nil || len(robot.Telemetry.Transforms) == 0 {
		return nil
	}

	transforms := make([]TransformData, len(robot.Telemetry.Transforms))
	copy(transforms, robot.Telemetry.Transforms)
	return transforms
}

// ============================================================
// Zone Reservation Operations (Atomic - Critical for Race Condition Prevention)
// ============================================================

// TryReserveZone attempts to reserve a zone for a robot atomically
// Returns (success, current_holder) - if already reserved, returns false and the holder
// This is the key operation that prevents race conditions!
// In 1:1 model, agentID = agentID
func (m *GlobalStateManager) TryReserveZone(zoneID, agentID string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// Check if zone is already reserved
	if existing, exists := m.zones[zoneID]; exists {
		// Check if reservation has expired
		if now.Before(existing.ExpiresAt) {
			// Zone is reserved by someone else
			if existing.AgentID != agentID {
				return false, existing.AgentID
			}
			// Same agent already has it - extend reservation
			existing.ExpiresAt = now.Add(m.zoneExpiryDuration)
			return true, ""
		}
		// Reservation expired, allow new reservation
	}

	// Reserve the zone
	m.zones[zoneID] = &ZoneReservation{
		ZoneID:     zoneID,
		AgentID:    agentID,
		ReservedAt: now,
		ExpiresAt:  now.Add(m.zoneExpiryDuration),
	}

	return true, ""
}

// ReleaseZone releases a zone reservation
// In 1:1 model, agentID = agentID
func (m *GlobalStateManager) ReleaseZone(zoneID, agentID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.zones[zoneID]
	if !exists {
		return false
	}

	// Only the holder can release
	if existing.AgentID != agentID {
		return false
	}

	delete(m.zones, zoneID)
	return true
}

// GetZoneHolder returns the current holder of a zone (returns agent_id)
func (m *GlobalStateManager) GetZoneHolder(zoneID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	existing, exists := m.zones[zoneID]
	if !exists {
		return "", false
	}

	// Check if expired
	if time.Now().After(existing.ExpiresAt) {
		return "", false
	}

	return existing.AgentID, true
}

// CleanupExpiredZones removes expired zone reservations
func (m *GlobalStateManager) CleanupExpiredZones() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	count := 0

	for zoneID, res := range m.zones {
		if now.After(res.ExpiresAt) {
			delete(m.zones, zoneID)
			count++
		}
	}

	return count
}

// ============================================================
// Agent Operations
// ============================================================

// RegisterAgent adds or updates an agent connection
// In 1:1 model, agent_id = robot_id, so no separate agentIDs needed
func (m *GlobalStateManager) RegisterAgent(id, name, ipAddress string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	if existing, exists := m.agents[id]; exists {
		// Update existing
		existing.Name = name
		existing.IPAddress = ipAddress
		existing.LastHeartbeat = now
	} else {
		// New agent
		m.agents[id] = &AgentConnection{
			ID:            id,
			Name:          name,
			IPAddress:     ipAddress,
			ConnectedAt:   now,
			LastHeartbeat: now,
			LastPing:      time.Time{},
			PingLatency:   0,
		}
	}

	// In 1:1 model, agent_id = robot_id, so mark the robot with same ID as online
	if robot, exists := m.robots[id]; exists {
		robot.IsOnline = true
		robot.LastSeen = now
	}
}

// UnregisterAgent removes an agent and marks it as offline
// In 1:1 model, agent_id = robot_id
func (m *GlobalStateManager) UnregisterAgent(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.agents[id]
	if !exists {
		return
	}

	// In 1:1 model, agent_id = robot_id, so mark the robot with same ID as offline
	if robot, exists := m.robots[id]; exists {
		robot.IsOnline = false
	}

	delete(m.agents, id)

	// Invalidate graph cache for this agent (without holding main lock)
	go m.graphCache.InvalidateAgentCache(id)
}

// UpdateAgentHeartbeat updates the last heartbeat time
func (m *GlobalStateManager) UpdateAgentHeartbeat(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	agent, exists := m.agents[id]
	if !exists {
		return fmt.Errorf("agent %s not found", id)
	}

	agent.LastHeartbeat = time.Now()
	return nil
}

// UpdateAgentPing updates the latest ping latency measurement.
func (m *GlobalStateManager) UpdateAgentPing(id string, latency time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	agent, exists := m.agents[id]
	if !exists {
		return fmt.Errorf("agent %s not found", id)
	}

	agent.LastPing = time.Now()
	agent.PingLatency = latency
	return nil
}

// GetAgent returns a copy of agent connection info
func (m *GlobalStateManager) GetAgent(id string) (*AgentConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, exists := m.agents[id]
	if !exists {
		return nil, false
	}

	agentCopy := *agent
	return &agentCopy, true
}

// IsAgentOnline checks if an agent is connected
func (m *GlobalStateManager) IsAgentOnline(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.agents[id]
	return exists
}

// AgentStatus represents the status of an agent with heartbeat info
// In 1:1 model, agent_id = robot_id, so no separate RobotIDs needed
type AgentStatus struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	IPAddress       string        `json:"ip_address"`
	IsOnline        bool          `json:"is_online"`
	ConnectedAt     time.Time     `json:"connected_at"`
	LastHeartbeat   time.Time     `json:"last_heartbeat"`
	HeartbeatAge    time.Duration `json:"heartbeat_age"` // Time since last heartbeat
	LastPing        time.Time     `json:"last_ping"`
	PingLatency     time.Duration `json:"ping_latency"`
	HeartbeatHealth string        `json:"heartbeat_health"` // "healthy", "warning", "critical"
}

// GetAllAgentStatus returns status information for all connected agents
func (m *GlobalStateManager) GetAllAgentStatus() []AgentStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	timeout := m.heartbeatConfig.Timeout
	warningThreshold := timeout / 2 // Warn at 50% of timeout

	result := make([]AgentStatus, 0, len(m.agents))
	for _, agent := range m.agents {
		heartbeatAge := now.Sub(agent.LastHeartbeat)

		// Determine health status
		health := "healthy"
		if heartbeatAge > timeout {
			health = "critical"
		} else if heartbeatAge > warningThreshold {
			health = "warning"
		}

		status := AgentStatus{
			ID:              agent.ID,
			Name:            agent.Name,
			IPAddress:       agent.IPAddress,
			IsOnline:        true, // If in agents map, it's online
			ConnectedAt:     agent.ConnectedAt,
			LastHeartbeat:   agent.LastHeartbeat,
			HeartbeatAge:    heartbeatAge,
			LastPing:        agent.LastPing,
			PingLatency:     agent.PingLatency,
			HeartbeatHealth: health,
		}
		result = append(result, status)
	}

	return result
}

// GetAgentStatus returns status information for a specific agent
func (m *GlobalStateManager) GetAgentStatus(agentID string) (*AgentStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return nil, false
	}

	now := time.Now()
	timeout := m.heartbeatConfig.Timeout
	warningThreshold := timeout / 2
	heartbeatAge := now.Sub(agent.LastHeartbeat)

	health := "healthy"
	if heartbeatAge > timeout {
		health = "critical"
	} else if heartbeatAge > warningThreshold {
		health = "warning"
	}

	status := &AgentStatus{
		ID:              agent.ID,
		Name:            agent.Name,
		IPAddress:       agent.IPAddress,
		IsOnline:        true,
		ConnectedAt:     agent.ConnectedAt,
		LastHeartbeat:   agent.LastHeartbeat,
		HeartbeatAge:    heartbeatAge,
		LastPing:        agent.LastPing,
		PingLatency:     agent.PingLatency,
		HeartbeatHealth: health,
	}

	return status, true
}

// ============================================================
// Precondition Evaluation (Atomic)
// ============================================================

// EvaluatePreconditions checks preconditions atomically
// Returns (success, error_message)
// This is critical for preventing TOCTOU race conditions!
func (m *GlobalStateManager) EvaluatePreconditions(agentID string, conditions []Precondition) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return false, fmt.Sprintf("robot %s not found", agentID)
	}

	for _, cond := range conditions {
		switch cond.Type {
		case "robot_state":
			if !m.evaluateStateCondition(robot, cond.Condition) {
				return false, cond.Message
			}
		case "zone_free":
			if !m.evaluateZoneCondition(agentID, cond.Condition) {
				return false, cond.Message
			}
		case "robot_idle":
			if robot.IsExecuting {
				return false, cond.Message
			}
		case "robot_online":
			if !robot.IsOnline {
				return false, cond.Message
			}
		default:
			// Unknown condition type - skip or fail?
			// For safety, fail
			return false, fmt.Sprintf("unknown precondition type: %s", cond.Type)
		}
	}

	return true, ""
}

// StartConditionQuantifier defines who the condition applies to
type StartConditionQuantifier string

const (
	QuantifierSelf     StartConditionQuantifier = "self"     // Only the executing robot
	QuantifierAll      StartConditionQuantifier = "all"      // All matching robots (AND)
	QuantifierAny      StartConditionQuantifier = "any"      // At least one matching robot (OR)
	QuantifierNone     StartConditionQuantifier = "none"     // No matching robots
	QuantifierSpecific StartConditionQuantifier = "specific" // Specific robot by ID
)

// StartCondition represents a rich start condition from the frontend
// Matches the Python backend's StartStateCondition
type StartCondition struct {
	ID string `json:"id"`

	// Target selection
	Quantifier StartConditionQuantifier `json:"quantifier"`  // self, all, any, none, specific
	TargetType string                   `json:"target_type"` // self, agent, all
	AgentID    string                   `json:"agent_id"`    // For 'specific' quantifier or agent target

	// State condition
	State         string   `json:"state"`          // Required state value
	StateOperator string   `json:"state_operator"` // ==, !=, in, not_in
	AllowedStates []string `json:"allowed_states"` // For 'in' operator

	// Freshness requirement
	MaxStalenessSec float64 `json:"max_staleness_sec"` // Max allowed staleness (default: 30s)
	RequireOnline   bool    `json:"require_online"`    // Require robot to be online (default: true)

	// Logical operator to combine with next condition
	Operator string `json:"operator"` // and, or

	// Error message
	Message string `json:"error_message"`
}

// StartConditionGroup represents a group of conditions with logical operator
type StartConditionGroup struct {
	ID         string           `json:"id"`
	Operator   string           `json:"operator"` // and, or
	Conditions []StartCondition `json:"conditions"`
	Negated    bool             `json:"negated"` // NOT operator
}

// ConditionResult contains detailed result of condition evaluation
type ConditionResult struct {
	ConditionID    string   `json:"condition_id"`
	Passed         bool     `json:"passed"`
	TargetRobots   []string `json:"target_robots"`
	MatchingRobots []string `json:"matching_robots"`
	FailedRobots   []string `json:"failed_robots"`
	StaleRobots    []string `json:"stale_robots"`
	OfflineRobots  []string `json:"offline_robots"`
	Error          string   `json:"error,omitempty"`
}

// StartConditionValidationResult is the full validation result
type StartConditionValidationResult struct {
	Passed           bool              `json:"passed"`
	Timestamp        time.Time         `json:"timestamp"`
	ConditionResults []ConditionResult `json:"condition_results"`
	TotalConditions  int               `json:"total_conditions"`
	PassedConditions int               `json:"passed_conditions"`
	FailedConditions int               `json:"failed_conditions"`
	ErrorMessage     string            `json:"error_message,omitempty"`
}

// IsSelfOnly returns true if this condition only checks the executing robot's own state
// These conditions can be evaluated locally by the agent without server coordination
func (c *StartCondition) IsSelfOnly() bool {
	return c.Quantifier == QuantifierSelf ||
		(c.Quantifier == "" && c.TargetType == "self") ||
		(c.Quantifier == "" && c.TargetType == "" && c.AgentID == "")
}

// Precondition represents a simple condition to check (legacy/simple format)
type Precondition struct {
	Type      string // robot_state, zone_free, robot_idle, robot_online
	Condition string // Expression or value to check
	Message   string // Error message if failed
}

// evaluateStateCondition checks if robot is in expected state
func (m *GlobalStateManager) evaluateStateCondition(robot *RobotState, condition string) bool {
	// Simple equality check for now
	// Could be extended to support more complex expressions
	return robot.CurrentState == condition
}

// evaluateZoneCondition checks if a zone is free or owned by this agent
// In 1:1 model, agentID = agentID
func (m *GlobalStateManager) evaluateZoneCondition(agentID, zoneID string) bool {
	existing, exists := m.zones[zoneID]
	if !exists {
		return true // Zone is free
	}

	// Check if expired
	if time.Now().After(existing.ExpiresAt) {
		return true
	}

	// Zone is reserved - is it by this agent?
	return existing.AgentID == agentID
}

// ============================================================
// Rich Start Condition Evaluation (with quantifier support)
// ============================================================

// Default values for condition evaluation
const (
	DefaultMaxStalenessSec = 30.0
)

// EvaluateStartConditions evaluates rich start conditions with quantifier support
// Returns: (allPassed, failedConditionID, errorMessage)
func (m *GlobalStateManager) EvaluateStartConditions(executingAgentID string, conditions []StartCondition) (bool, string, string) {
	result := m.ValidateStartConditions(executingAgentID, conditions)
	if !result.Passed {
		// Find first failed condition
		for _, cr := range result.ConditionResults {
			if !cr.Passed {
				return false, cr.ConditionID, cr.Error
			}
		}
	}
	return result.Passed, "", ""
}

// EvaluateStartConditionList evaluates an ordered list with AND/OR operators.
// Returns (passed, error_message).
func (m *GlobalStateManager) EvaluateStartConditionList(executingAgentID string, conditions []StartCondition) (bool, string) {
	if len(conditions) == 0 {
		return true, ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	result := true
	var errorMessage string

	for i, cond := range conditions {
		condResult := m.evaluateSingleConditionDetailed(executingAgentID, cond, now)
		passed := condResult.Passed

		if i == 0 {
			result = passed
		} else {
			switch cond.Operator {
			case "or":
				result = result || passed
			default:
				result = result && passed
			}
		}

		if !passed && errorMessage == "" {
			if cond.Message != "" {
				errorMessage = cond.Message
			} else if condResult.Error != "" {
				errorMessage = condResult.Error
			} else {
				errorMessage = "start condition not satisfied"
			}
		}

		// Short-circuit if AND chain already failed
		if !result && cond.Operator != "or" {
			break
		}
	}

	return result, errorMessage
}

// ValidateStartConditions performs full validation with detailed results
func (m *GlobalStateManager) ValidateStartConditions(executingAgentID string, conditions []StartCondition) StartConditionValidationResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	results := make([]ConditionResult, 0, len(conditions))
	allPassed := true
	var errors []string

	for _, cond := range conditions {
		result := m.evaluateSingleConditionDetailed(executingAgentID, cond, now)
		results = append(results, result)
		if !result.Passed {
			allPassed = false
			if result.Error != "" {
				errors = append(errors, result.Error)
			}
		}
	}

	// Build error message
	errorMessage := ""
	if !allPassed && len(errors) > 0 {
		errorMessage = errors[0]
		for i := 1; i < len(errors); i++ {
			errorMessage += "; " + errors[i]
		}
	}

	passedCount := 0
	for _, r := range results {
		if r.Passed {
			passedCount++
		}
	}

	return StartConditionValidationResult{
		Passed:           allPassed,
		Timestamp:        now,
		ConditionResults: results,
		TotalConditions:  len(results),
		PassedConditions: passedCount,
		FailedConditions: len(results) - passedCount,
		ErrorMessage:     errorMessage,
	}
}

// BlockingConditionInfo describes why a precondition is blocking (for UI display)
type BlockingConditionInfo struct {
	ConditionID     string `json:"condition_id"`
	Description     string `json:"description"`
	TargetAgentID   string `json:"target_agent_id,omitempty"`
	TargetAgentName string `json:"target_agent_name,omitempty"`
	RequiredState   string `json:"required_state"`
	CurrentState    string `json:"current_state,omitempty"`
	Reason          string `json:"reason"` // state_mismatch, agent_offline, state_too_old, no_targets
}

// ============================================================
// Telemetry Types for Parameter Loading
// ============================================================

// Vector3 represents a 3D vector
type Vector3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Quaternion represents orientation
type Quaternion struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
	W float64 `json:"w"`
}

// Pose represents position and orientation
type Pose struct {
	Position    Vector3    `json:"position"`
	Orientation Quaternion `json:"orientation"`
}

// Twist represents linear and angular velocity
type Twist struct {
	Linear  Vector3 `json:"linear"`
	Angular Vector3 `json:"angular"`
}

// JointStateData represents joint state telemetry
type JointStateData struct {
	Name        []string  `json:"name"`
	Position    []float64 `json:"position"`
	Velocity    []float64 `json:"velocity,omitempty"`
	Effort      []float64 `json:"effort,omitempty"`
	TopicName   string    `json:"topic_name,omitempty"`   // ROS2 topic name for visualization
	TimestampNs int64     `json:"timestamp_ns,omitempty"` // ROS2 message timestamp in nanoseconds
}

// OdometryData represents odometry telemetry
type OdometryData struct {
	FrameID      string `json:"frame_id"`
	ChildFrameID string `json:"child_frame_id"`
	Pose         Pose   `json:"pose"`
	Twist        Twist  `json:"twist"`
	TopicName    string `json:"topic_name,omitempty"`   // ROS2 topic name for visualization
	TimestampNs  int64  `json:"timestamp_ns,omitempty"` // ROS2 message timestamp in nanoseconds
}

// TransformData represents TF transform
type TransformData struct {
	FrameID      string     `json:"frame_id"`
	ChildFrameID string     `json:"child_frame_id"`
	Translation  Vector3    `json:"translation"`
	Rotation     Quaternion `json:"rotation"`
	TimestampNs  int64      `json:"timestamp_ns,omitempty"` // ROS2 message timestamp in nanoseconds
}

// RobotTelemetry holds telemetry data for a robot
type RobotTelemetry struct {
	JointState *JointStateData  `json:"joint_state,omitempty"`
	Odometry   *OdometryData    `json:"odometry,omitempty"`
	Transforms []TransformData  `json:"transforms,omitempty"`
	UpdatedAt  time.Time        `json:"updated_at"`
	IsStale    bool             `json:"is_stale,omitempty"` // True if data is older than staleness threshold
}

// DefaultTelemetryStaleThreshold is the default duration after which telemetry is considered stale
const DefaultTelemetryStaleThreshold = 5 * time.Second

// IsTelemetryStale checks if telemetry data is older than the given threshold
func (t *RobotTelemetry) IsTelemetryStale(threshold time.Duration) bool {
	if t == nil {
		return true
	}
	return time.Since(t.UpdatedAt) > threshold
}

// EvaluateStartConditionsWithBlockingInfo evaluates conditions and returns detailed blocking info for UI
func (m *GlobalStateManager) EvaluateStartConditionsWithBlockingInfo(executingAgentID string, conditions []StartCondition) (bool, []BlockingConditionInfo) {
	if len(conditions) == 0 {
		return true, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	var blockingInfos []BlockingConditionInfo
	allPassed := true

	for _, cond := range conditions {
		result := m.evaluateSingleConditionDetailed(executingAgentID, cond, now)
		if !result.Passed {
			allPassed = false

			// Build blocking condition info for each failed robot
			if len(result.StaleRobots) > 0 {
				for _, robotID := range result.StaleRobots {
					robotState, _ := m.robots[robotID]
					info := BlockingConditionInfo{
						ConditionID:   cond.ID,
						Description:   cond.Message,
						TargetAgentID: robotID,
						RequiredState: cond.State,
						Reason:        "state_too_old",
					}
					if robotState != nil {
						info.TargetAgentName = robotState.Name
						info.CurrentState = robotState.CurrentState
					}
					if info.Description == "" {
						info.Description = fmt.Sprintf("Agent %s state is stale", robotID)
					}
					blockingInfos = append(blockingInfos, info)
				}
			}

			if len(result.OfflineRobots) > 0 {
				for _, robotID := range result.OfflineRobots {
					robotState, _ := m.robots[robotID]
					info := BlockingConditionInfo{
						ConditionID:   cond.ID,
						Description:   cond.Message,
						TargetAgentID: robotID,
						RequiredState: cond.State,
						Reason:        "agent_offline",
					}
					if robotState != nil {
						info.TargetAgentName = robotState.Name
						info.CurrentState = robotState.CurrentState
					}
					if info.Description == "" {
						info.Description = fmt.Sprintf("Agent %s is offline", robotID)
					}
					blockingInfos = append(blockingInfos, info)
				}
			}

			if len(result.FailedRobots) > 0 {
				for _, robotID := range result.FailedRobots {
					robotState, _ := m.robots[robotID]
					info := BlockingConditionInfo{
						ConditionID:   cond.ID,
						Description:   cond.Message,
						TargetAgentID: robotID,
						RequiredState: cond.State,
						Reason:        "state_mismatch",
					}
					if robotState != nil {
						info.TargetAgentName = robotState.Name
						info.CurrentState = robotState.CurrentState
					}
					if info.Description == "" {
						info.Description = fmt.Sprintf("Agent %s state mismatch: expected %s, got %s",
							robotID, cond.State, info.CurrentState)
					}
					blockingInfos = append(blockingInfos, info)
				}
			}

			// No target robots found
			if len(result.TargetRobots) == 0 && cond.Quantifier != QuantifierNone {
				info := BlockingConditionInfo{
					ConditionID:   cond.ID,
					Description:   cond.Message,
					RequiredState: cond.State,
					Reason:        "no_targets",
				}
				if info.Description == "" {
					info.Description = fmt.Sprintf("No target agents found for condition %s", cond.ID)
				}
				blockingInfos = append(blockingInfos, info)
			}
		}
	}

	return allPassed, blockingInfos
}

// evaluateSingleConditionDetailed evaluates one condition with full detail tracking
func (m *GlobalStateManager) evaluateSingleConditionDetailed(executingAgentID string, cond StartCondition, now time.Time) ConditionResult {
	result := ConditionResult{
		ConditionID:    cond.ID,
		TargetRobots:   []string{},
		MatchingRobots: []string{},
		FailedRobots:   []string{},
		StaleRobots:    []string{},
		OfflineRobots:  []string{},
	}

	// Get target robots
	targetRobots := m.getTargetRobots(executingAgentID, cond)

	if len(targetRobots) == 0 && cond.Quantifier != QuantifierNone {
		result.Passed = false
		result.Error = fmt.Sprintf("No target robots found for condition %s", cond.ID)
		return result
	}

	// Set defaults
	maxStaleness := cond.MaxStalenessSec
	if maxStaleness <= 0 {
		maxStaleness = DefaultMaxStalenessSec
	}
	requireOnline := cond.RequireOnline // default is false, but Python defaults to true

	// Evaluate each target robot
	for _, robot := range targetRobots {
		result.TargetRobots = append(result.TargetRobots, robot.ID)

		// Check staleness
		staleness := now.Sub(robot.LastSeen).Seconds()
		if staleness > maxStaleness {
			result.StaleRobots = append(result.StaleRobots, robot.ID)
			continue
		}

		// Check online status
		if requireOnline && !robot.IsOnline {
			result.OfflineRobots = append(result.OfflineRobots, robot.ID)
			continue
		}

		// Check state condition
		if m.robotMatchesStateCondition(robot, cond) {
			result.MatchingRobots = append(result.MatchingRobots, robot.ID)
		} else {
			result.FailedRobots = append(result.FailedRobots, robot.ID)
		}
	}

	// Apply quantifier logic
	switch cond.Quantifier {
	case QuantifierSelf:
		result.Passed = len(result.MatchingRobots) > 0

	case QuantifierAll, "":
		// All must match, none stale/offline
		result.Passed = len(result.MatchingRobots) == len(targetRobots) &&
			len(result.StaleRobots) == 0 &&
			len(result.OfflineRobots) == 0

	case QuantifierAny:
		result.Passed = len(result.MatchingRobots) > 0

	case QuantifierNone:
		result.Passed = len(result.MatchingRobots) == 0

	case QuantifierSpecific:
		result.Passed = len(result.MatchingRobots) > 0

	default:
		result.Passed = false
		result.Error = fmt.Sprintf("Unknown quantifier: %s", cond.Quantifier)
		return result
	}

	// Build error message if failed
	if !result.Passed {
		if cond.Message != "" {
			result.Error = cond.Message
		} else if len(result.StaleRobots) > 0 {
			result.Error = fmt.Sprintf("State too old for: %v (max %.0fs)", result.StaleRobots, maxStaleness)
		} else if len(result.OfflineRobots) > 0 {
			result.Error = fmt.Sprintf("Robots offline: %v", result.OfflineRobots)
		} else if len(result.FailedRobots) > 0 {
			result.Error = fmt.Sprintf("State mismatch for: %v (expected %s)", result.FailedRobots, cond.State)
		} else if len(result.MatchingRobots) == 0 && cond.Quantifier != QuantifierNone {
			result.Error = fmt.Sprintf("No robots matched condition (quantifier: %s)", cond.Quantifier)
		}
	}

	return result
}

// EvaluateConditionGroup evaluates a group of conditions with logical operator
func (m *GlobalStateManager) EvaluateConditionGroup(executingAgentID string, group StartConditionGroup) ConditionResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	results := make([]ConditionResult, 0, len(group.Conditions))

	for _, cond := range group.Conditions {
		result := m.evaluateSingleConditionDetailed(executingAgentID, cond, now)
		results = append(results, result)
	}

	// Apply logical operator
	var passed bool
	if group.Operator == "or" {
		passed = false
		for _, r := range results {
			if r.Passed {
				passed = true
				break
			}
		}
	} else { // "and" is default
		passed = true
		for _, r := range results {
			if !r.Passed {
				passed = false
				break
			}
		}
	}

	// Apply negation
	if group.Negated {
		passed = !passed
	}

	// Aggregate results
	aggregated := ConditionResult{
		ConditionID:    group.ID,
		Passed:         passed,
		TargetRobots:   []string{},
		MatchingRobots: []string{},
		FailedRobots:   []string{},
		StaleRobots:    []string{},
		OfflineRobots:  []string{},
	}

	for _, r := range results {
		aggregated.TargetRobots = append(aggregated.TargetRobots, r.TargetRobots...)
		aggregated.MatchingRobots = append(aggregated.MatchingRobots, r.MatchingRobots...)
		aggregated.FailedRobots = append(aggregated.FailedRobots, r.FailedRobots...)
		aggregated.StaleRobots = append(aggregated.StaleRobots, r.StaleRobots...)
		aggregated.OfflineRobots = append(aggregated.OfflineRobots, r.OfflineRobots...)
		if !r.Passed && aggregated.Error == "" {
			aggregated.Error = r.Error
		}
	}

	return aggregated
}

// getTargetRobots returns the list of robots to check based on condition's target
func (m *GlobalStateManager) getTargetRobots(executingAgentID string, cond StartCondition) []*RobotState {
	var targets []*RobotState

	switch cond.Quantifier {
	case QuantifierSelf, "":
		// Self or default: only the executing robot
		if robot, exists := m.robots[executingAgentID]; exists {
			targets = append(targets, robot)
		}

	case QuantifierSpecific:
		// Specific agent (1 Agent = 1 Robot in this architecture)
		if cond.AgentID != "" {
			// In 1:1 model, agent ID is also the robot ID
			if robot, exists := m.robots[cond.AgentID]; exists {
				targets = append(targets, robot)
			}
		}

	case QuantifierAll, QuantifierAny, QuantifierNone:
		// Multiple robots based on filter
		for _, robot := range m.robots {
			// Filter by agent if specified
			if cond.AgentID != "" && robot.AgentID != cond.AgentID {
				continue
			}
			targets = append(targets, robot)
		}
	}

	return targets
}

// robotMatchesStateCondition checks if a robot matches the state condition
func (m *GlobalStateManager) robotMatchesStateCondition(robot *RobotState, cond StartCondition) bool {
	// Check online status (default: must be online)
	if !robot.IsOnline {
		return false
	}

	// Apply state operator
	switch cond.StateOperator {
	case "!=", "not_equals":
		return robot.CurrentState != cond.State

	case "in":
		for _, s := range cond.AllowedStates {
			if robot.CurrentState == s {
				return true
			}
		}
		return false

	case "not_in":
		for _, s := range cond.AllowedStates {
			if robot.CurrentState == s {
				return false
			}
		}
		return true

	default: // "==" or empty (equals)
		return robot.CurrentState == cond.State
	}
}

// AreSelfOnlyConditions checks if all conditions can be evaluated locally by the agent
// Returns true if no cross-robot coordination is needed
func AreSelfOnlyConditions(conditions []StartCondition) bool {
	for _, cond := range conditions {
		if !cond.IsSelfOnly() {
			return false
		}
	}
	return true
}

// ============================================================
// Atomic Combined Operations (for race condition prevention)
// ============================================================

// TryStartExecution attempts to start execution atomically
// This combines: precondition check + zone reservation + state update
// In 1:1 model, agentID = agentID
// Returns (success, error_message)
func (m *GlobalStateManager) TryStartExecution(agentID, taskID, stepID, graphID string, requiredZones []string, preconditions []Precondition) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return false, fmt.Sprintf("agent %s not found", agentID)
	}

	// Check if already executing
	if robot.IsExecuting {
		return false, fmt.Sprintf("agent %s is already executing task %s", agentID, robot.CurrentTaskID)
	}

	// Check preconditions (while holding lock)
	for _, cond := range preconditions {
		switch cond.Type {
		case "agent_state", "robot_state":
			if !m.evaluateStateCondition(robot, cond.Condition) {
				return false, cond.Message
			}
		case "zone_free":
			if !m.evaluateZoneCondition(agentID, cond.Condition) {
				return false, cond.Message
			}
		case "agent_idle", "robot_idle":
			if robot.IsExecuting {
				return false, cond.Message
			}
		case "agent_online", "robot_online":
			if !robot.IsOnline {
				return false, cond.Message
			}
		}
	}

	// Reserve required zones
	now := time.Now()
	reservedZones := make([]string, 0, len(requiredZones))
	for _, zoneID := range requiredZones {
		existing, exists := m.zones[zoneID]
		if exists && now.Before(existing.ExpiresAt) && existing.AgentID != agentID {
			// Zone is taken by someone else - rollback reserved zones
			for _, reserved := range reservedZones {
				delete(m.zones, reserved)
			}
			return false, fmt.Sprintf("zone %s is reserved by agent %s", zoneID, existing.AgentID)
		}

		// Reserve the zone
		m.zones[zoneID] = &ZoneReservation{
			ZoneID:     zoneID,
			AgentID:    agentID,
			ReservedAt: now,
			ExpiresAt:  now.Add(m.zoneExpiryDuration),
		}
		reservedZones = append(reservedZones, zoneID)
	}

	// All checks passed - update execution state
	robot.IsExecuting = true
	robot.CurrentTaskID = taskID
	robot.CurrentStepID = stepID
	robot.CurrentGraphID = graphID
	robot.LastSeen = now

	// Update state registry for cross-agent queries
	m.stateRegistry.UpdateAgentState(agentID, robot.CurrentStateCode, robot.SemanticTags, graphID, robot.IsOnline, true)

	return true, ""
}

// CompleteExecution marks execution as complete and releases zones
// In 1:1 model, agentID = agentID
func (m *GlobalStateManager) CompleteExecution(agentID string, releasedZones []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return
	}

	robot.IsExecuting = false
	robot.CurrentTaskID = ""
	robot.CurrentStepID = ""
	robot.CurrentGraphID = ""
	robot.LastSeen = time.Now()

	// Clear any state overrides (failsafe cleanup)
	delete(m.stateOverrides, agentID)

	// Restore CurrentState to ReportedState now that overrides are cleared
	if robot.ReportedState != "" {
		robot.CurrentState = robot.ReportedState
	}

	// Update state registry
	m.stateRegistry.UpdateAgentState(agentID, robot.CurrentStateCode, robot.SemanticTags, "", robot.IsOnline, false)

	// Release zones
	for _, zoneID := range releasedZones {
		if existing, exists := m.zones[zoneID]; exists {
			if existing.AgentID == agentID {
				delete(m.zones, zoneID)
			}
		}
	}
}

// ============================================================
// Snapshot Operations
// ============================================================

// GetSnapshot returns a point-in-time snapshot of the entire fleet state
func (m *GlobalStateManager) GetSnapshot() *FleetSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := &FleetSnapshot{
		Timestamp: time.Now(),
		Robots:    make(map[string]*RobotState),
		Zones:     make(map[string]*ZoneReservation),
		Agents:    make(map[string]*AgentConnection),
	}

	// Copy robots
	for id, robot := range m.robots {
		robotCopy := *robot
		snapshot.Robots[id] = &robotCopy
	}

	// Copy zones (only non-expired)
	now := time.Now()
	for id, zone := range m.zones {
		if now.Before(zone.ExpiresAt) {
			zoneCopy := *zone
			snapshot.Zones[id] = &zoneCopy
		}
	}

	// Copy agents
	for id, agent := range m.agents {
		agentCopy := *agent
		snapshot.Agents[id] = &agentCopy
	}

	return snapshot
}

// GetRobotStates returns a copy of all robot states
func (m *GlobalStateManager) GetRobotStates() map[string]*RobotState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*RobotState)
	for id, robot := range m.robots {
		robotCopy := *robot
		result[id] = &robotCopy
	}
	return result
}

// GetOnlineRobots returns IDs of all online robots
func (m *GlobalStateManager) GetOnlineRobots() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []string
	for id, robot := range m.robots {
		if robot.IsOnline {
			result = append(result, id)
		}
	}
	return result
}

// ResetAgentState resets an agent's state to the initial "idle" state
// This clears execution state, cancels any running tasks, and resets all state fields
// In 1:1 model, agentID = robotID
func (m *GlobalStateManager) ResetAgentState(agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	// Reset to initial "idle" state
	robot.CurrentState = "idle"
	robot.ReportedState = "idle"
	robot.CurrentStateCode = "idle"
	robot.SemanticTags = []string{}
	robot.CurrentGraphID = ""
	robot.IsExecuting = false
	robot.CurrentTaskID = ""
	robot.CurrentStepID = ""
	robot.LastSeen = time.Now()

	// Clear any state overrides
	delete(m.stateOverrides, agentID)

	// Release any zone reservations held by this agent
	for zoneID, res := range m.zones {
		if res.AgentID == agentID {
			delete(m.zones, zoneID)
		}
	}

	// Update state registry
	m.stateRegistry.UpdateAgentState(agentID, "idle", []string{}, "", robot.IsOnline, false)
	m.stateRegistry.SetAgentExecuting(agentID, false)

	return nil
}

// ============================================================
// Multi-Agent Simultaneous Start Operations
// ============================================================

// MultiExecutionRequest represents a single agent's execution request in a multi-agent batch
type MultiExecutionRequest struct {
	AgentID       string
	TaskID        string
	StepID        string
	GraphID       string
	RequiredZones []string
}

// MultiExecutionResult contains the result of a multi-agent execution attempt
type MultiExecutionResult struct {
	Success       bool
	FailedAgentID string
	ErrorMessage  string
}

// TryStartMultiExecution attempts to start execution for multiple agents atomically
// All agents must pass validation for any to start - atomic all-or-nothing semantics
// Returns MultiExecutionResult with success/failure info
func (m *GlobalStateManager) TryStartMultiExecution(
	executions []MultiExecutionRequest,
	preconditions []Precondition,
) MultiExecutionResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Phase 1: Validate all agents exist and are not executing
	for _, exec := range executions {
		robot, exists := m.robots[exec.AgentID]
		if !exists {
			return MultiExecutionResult{
				Success:       false,
				FailedAgentID: exec.AgentID,
				ErrorMessage:  fmt.Sprintf("agent %s not found", exec.AgentID),
			}
		}
		if robot.IsExecuting {
			return MultiExecutionResult{
				Success:       false,
				FailedAgentID: exec.AgentID,
				ErrorMessage:  fmt.Sprintf("agent %s is already executing task %s", exec.AgentID, robot.CurrentTaskID),
			}
		}
		if !robot.IsOnline {
			return MultiExecutionResult{
				Success:       false,
				FailedAgentID: exec.AgentID,
				ErrorMessage:  fmt.Sprintf("agent %s is offline", exec.AgentID),
			}
		}
	}

	// Phase 2: Check preconditions for all agents
	for _, exec := range executions {
		robot := m.robots[exec.AgentID]
		for _, cond := range preconditions {
			passed := true
			switch cond.Type {
			case "agent_state", "robot_state":
				passed = m.evaluateStateCondition(robot, cond.Condition)
			case "zone_free":
				passed = m.evaluateZoneConditionForMulti(exec.AgentID, cond.Condition, executions)
			case "agent_idle", "robot_idle":
				passed = !robot.IsExecuting
			case "agent_online", "robot_online":
				passed = robot.IsOnline
			}
			if !passed {
				return MultiExecutionResult{
					Success:       false,
					FailedAgentID: exec.AgentID,
					ErrorMessage:  cond.Message,
				}
			}
		}
	}

	// Phase 3: Check zone conflicts within the batch and with external agents
	allRequestedZones := make(map[string]string) // zoneID -> agentID requesting it
	for _, exec := range executions {
		for _, zoneID := range exec.RequiredZones {
			// Check if another agent in this batch wants the same zone
			if existingAgent, conflict := allRequestedZones[zoneID]; conflict {
				return MultiExecutionResult{
					Success:       false,
					FailedAgentID: exec.AgentID,
					ErrorMessage:  fmt.Sprintf("zone %s is requested by both %s and %s", zoneID, existingAgent, exec.AgentID),
				}
			}
			// Check if zone is already reserved by an external agent
			if holder, exists := m.zones[zoneID]; exists {
				if time.Now().Before(holder.ExpiresAt) && !m.isAgentInBatch(holder.AgentID, executions) {
					return MultiExecutionResult{
						Success:       false,
						FailedAgentID: exec.AgentID,
						ErrorMessage:  fmt.Sprintf("zone %s is reserved by agent %s", zoneID, holder.AgentID),
					}
				}
			}
			allRequestedZones[zoneID] = exec.AgentID
		}
	}

	// Phase 4: All validations passed - commit changes atomically
	now := time.Now()
	for _, exec := range executions {
		robot := m.robots[exec.AgentID]
		robot.IsExecuting = true
		robot.CurrentTaskID = exec.TaskID
		robot.CurrentStepID = exec.StepID
		robot.CurrentGraphID = exec.GraphID
		robot.LastSeen = now

		// Reserve zones
		for _, zoneID := range exec.RequiredZones {
			m.zones[zoneID] = &ZoneReservation{
				ZoneID:     zoneID,
				AgentID:    exec.AgentID,
				ReservedAt: now,
				ExpiresAt:  now.Add(m.zoneExpiryDuration),
			}
		}

		// Update state registry
		m.stateRegistry.UpdateAgentState(exec.AgentID, robot.CurrentStateCode, robot.SemanticTags, exec.GraphID, robot.IsOnline, true)
	}

	return MultiExecutionResult{
		Success:       true,
		FailedAgentID: "",
		ErrorMessage:  "",
	}
}

// isAgentInBatch checks if an agent is part of the multi-execution batch
func (m *GlobalStateManager) isAgentInBatch(agentID string, executions []MultiExecutionRequest) bool {
	for _, exec := range executions {
		if exec.AgentID == agentID {
			return true
		}
	}
	return false
}

// evaluateZoneConditionForMulti checks zone conditions considering the multi-agent batch
func (m *GlobalStateManager) evaluateZoneConditionForMulti(agentID, zoneID string, executions []MultiExecutionRequest) bool {
	existing, exists := m.zones[zoneID]
	if !exists {
		return true // Zone is free
	}

	// Check if expired
	if time.Now().After(existing.ExpiresAt) {
		return true
	}

	// Zone is reserved - is it by this agent or another agent in the batch?
	if existing.AgentID == agentID {
		return true
	}

	// Check if holder is in the batch (will be releasing their old reservation)
	return m.isAgentInBatch(existing.AgentID, executions)
}

// CompleteMultiExecution marks execution as complete for multiple agents
func (m *GlobalStateManager) CompleteMultiExecution(executions []MultiExecutionRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for _, exec := range executions {
		robot, exists := m.robots[exec.AgentID]
		if !exists {
			continue
		}

		robot.IsExecuting = false
		robot.CurrentTaskID = ""
		robot.CurrentStepID = ""
		robot.CurrentGraphID = ""
		robot.LastSeen = now

		// Clear any state overrides (failsafe cleanup)
		delete(m.stateOverrides, exec.AgentID)

		// Restore CurrentState to ReportedState now that overrides are cleared
		if robot.ReportedState != "" {
			robot.CurrentState = robot.ReportedState
		}

		// Release zones
		for _, zoneID := range exec.RequiredZones {
			if existing, exists := m.zones[zoneID]; exists {
				if existing.AgentID == exec.AgentID {
					delete(m.zones, zoneID)
				}
			}
		}

		// Update state registry
		m.stateRegistry.UpdateAgentState(exec.AgentID, robot.CurrentStateCode, robot.SemanticTags, "", robot.IsOnline, false)
	}
}

package state

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// RobotState represents the in-memory state of a robot
type RobotState struct {
	ID            string
	Name          string
	AgentID       string
	CurrentState  string
	IsOnline      bool
	IsExecuting   bool
	CurrentTaskID string
	CurrentStepID string
	LastSeen      time.Time
}

// AgentConnection represents a connected agent
type AgentConnection struct {
	ID            string
	Name          string
	IPAddress     string
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	LastPing      time.Time
	PingLatency   time.Duration
	RobotIDs      []string
}

// ZoneReservation represents a zone lock
type ZoneReservation struct {
	ZoneID     string
	RobotID    string
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
type AgentDisconnectCallback func(agentID string, robotIDs []string)

// GlobalStateManager manages all fleet state with thread-safe operations
// This is the SINGLE SOURCE OF TRUTH for runtime state
// All state changes go through this manager to prevent race conditions
type GlobalStateManager struct {
	mu sync.RWMutex

	// Robot states indexed by robot ID
	robots map[string]*RobotState

	// Zone reservations indexed by zone ID
	zones map[string]*ZoneReservation

	// Connected agents indexed by agent ID
	agents map[string]*AgentConnection

	// Zone expiry time
	zoneExpiryDuration time.Duration

	// Action Graph cache for fast lookup (avoids DB I/O during task execution)
	graphCache *GraphCache

	// Heartbeat configuration
	heartbeatConfig HeartbeatConfig

	// Callback when agent disconnects
	onAgentDisconnect AgentDisconnectCallback

	// Background worker management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewGlobalStateManager creates a new state manager
func NewGlobalStateManager() *GlobalStateManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &GlobalStateManager{
		robots:             make(map[string]*RobotState),
		zones:              make(map[string]*ZoneReservation),
		agents:             make(map[string]*AgentConnection),
		zoneExpiryDuration: 30 * time.Second,
		graphCache:         NewGraphCache(),
		heartbeatConfig:    DefaultHeartbeatConfig(),
		ctx:                ctx,
		cancel:             cancel,
	}
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
	m.wg.Add(2)
	go m.runCacheCleanup()
	go m.runHeartbeatChecker()
	log.Println("GlobalStateManager background workers started (cache cleanup + heartbeat checker)")
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
	staleAgents := make([]struct {
		ID       string
		RobotIDs []string
	}, 0)

	// Find stale agents
	for agentID, agent := range m.agents {
		timeSinceHeartbeat := now.Sub(agent.LastHeartbeat)
		if timeSinceHeartbeat > timeout {
			staleAgents = append(staleAgents, struct {
				ID       string
				RobotIDs []string
			}{
				ID:       agentID,
				RobotIDs: append([]string{}, agent.RobotIDs...), // Copy slice
			})
		}
	}

	// Mark stale agents and their robots as offline
	for _, stale := range staleAgents {
		log.Printf("[Heartbeat] Agent %s timed out (last heartbeat > %v ago)", stale.ID, timeout)

		// Mark all robots as offline
		for _, robotID := range stale.RobotIDs {
			if robot, exists := m.robots[robotID]; exists {
				robot.IsOnline = false
			}
		}

		// Remove agent from connected list
		delete(m.agents, stale.ID)
	}

	// Get callback reference while holding lock
	callback := m.onAgentDisconnect
	m.mu.Unlock()

	// Call disconnect callbacks outside of lock to prevent deadlock
	if callback != nil {
		for _, stale := range staleAgents {
			callback(stale.ID, stale.RobotIDs)
		}
	}

	// Invalidate graph caches for disconnected agents
	for _, stale := range staleAgents {
		m.graphCache.InvalidateAgentCache(stale.ID)
	}
}

// GraphCache returns the action graph cache
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

	m.robots[id] = &RobotState{
		ID:           id,
		Name:         name,
		AgentID:      agentID,
		CurrentState: initialState,
		IsOnline:     true,
		LastSeen:     time.Now(),
	}
}

// UnregisterRobot removes a robot from the state manager
func (m *GlobalStateManager) UnregisterRobot(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.robots, id)
	// Also release any zone reservations
	for zoneID, res := range m.zones {
		if res.RobotID == id {
			delete(m.zones, zoneID)
		}
	}
}

// GetRobotState returns a copy of the robot state (thread-safe read)
func (m *GlobalStateManager) GetRobotState(robotID string) (*RobotState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.robots[robotID]
	if !exists {
		return nil, false
	}
	// Return a copy to prevent external mutation
	stateCopy := *state
	return &stateCopy, true
}

// UpdateRobotState atomically updates a robot's state
func (m *GlobalStateManager) UpdateRobotState(robotID, newState string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[robotID]
	if !exists {
		return fmt.Errorf("robot %s not found", robotID)
	}

	robot.CurrentState = newState
	robot.LastSeen = time.Now()
	return nil
}

// UpdateRobotExecution atomically updates a robot's execution state
func (m *GlobalStateManager) UpdateRobotExecution(robotID string, isExecuting bool, taskID, stepID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[robotID]
	if !exists {
		return fmt.Errorf("robot %s not found", robotID)
	}

	robot.IsExecuting = isExecuting
	robot.CurrentTaskID = taskID
	robot.CurrentStepID = stepID
	robot.LastSeen = time.Now()
	return nil
}

// SetRobotOnline sets a robot's online status
func (m *GlobalStateManager) SetRobotOnline(robotID string, isOnline bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[robotID]
	if !exists {
		return fmt.Errorf("robot %s not found", robotID)
	}

	robot.IsOnline = isOnline
	robot.LastSeen = time.Now()
	return nil
}

// ============================================================
// Zone Reservation Operations (Atomic - Critical for Race Condition Prevention)
// ============================================================

// TryReserveZone attempts to reserve a zone for a robot atomically
// Returns (success, current_holder) - if already reserved, returns false and the holder
// This is the key operation that prevents race conditions!
func (m *GlobalStateManager) TryReserveZone(zoneID, robotID string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// Check if zone is already reserved
	if existing, exists := m.zones[zoneID]; exists {
		// Check if reservation has expired
		if now.Before(existing.ExpiresAt) {
			// Zone is reserved by someone else
			if existing.RobotID != robotID {
				return false, existing.RobotID
			}
			// Same robot already has it - extend reservation
			existing.ExpiresAt = now.Add(m.zoneExpiryDuration)
			return true, ""
		}
		// Reservation expired, allow new reservation
	}

	// Reserve the zone
	m.zones[zoneID] = &ZoneReservation{
		ZoneID:     zoneID,
		RobotID:    robotID,
		ReservedAt: now,
		ExpiresAt:  now.Add(m.zoneExpiryDuration),
	}

	return true, ""
}

// ReleaseZone releases a zone reservation
func (m *GlobalStateManager) ReleaseZone(zoneID, robotID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.zones[zoneID]
	if !exists {
		return false
	}

	// Only the holder can release
	if existing.RobotID != robotID {
		return false
	}

	delete(m.zones, zoneID)
	return true
}

// GetZoneHolder returns the current holder of a zone
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

	return existing.RobotID, true
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
func (m *GlobalStateManager) RegisterAgent(id, name, ipAddress string, robotIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	if existing, exists := m.agents[id]; exists {
		// Update existing
		existing.Name = name
		existing.IPAddress = ipAddress
		existing.RobotIDs = robotIDs
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
			RobotIDs:      robotIDs,
		}
	}

	// Mark all robots as online
	for _, robotID := range robotIDs {
		if robot, exists := m.robots[robotID]; exists {
			robot.IsOnline = true
			robot.LastSeen = now
		}
	}
}

// UnregisterAgent removes an agent and marks its robots as offline
func (m *GlobalStateManager) UnregisterAgent(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	agent, exists := m.agents[id]
	if !exists {
		return
	}

	// Mark all robots as offline
	for _, robotID := range agent.RobotIDs {
		if robot, exists := m.robots[robotID]; exists {
			robot.IsOnline = false
		}
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
	agentCopy.RobotIDs = make([]string, len(agent.RobotIDs))
	copy(agentCopy.RobotIDs, agent.RobotIDs)
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
	RobotIDs        []string      `json:"robot_ids"`
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
			RobotIDs:        make([]string, len(agent.RobotIDs)),
			HeartbeatHealth: health,
		}
		copy(status.RobotIDs, agent.RobotIDs)
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
		RobotIDs:        make([]string, len(agent.RobotIDs)),
		HeartbeatHealth: health,
	}
	copy(status.RobotIDs, agent.RobotIDs)

	return status, true
}

// ============================================================
// Precondition Evaluation (Atomic)
// ============================================================

// EvaluatePreconditions checks preconditions atomically
// Returns (success, error_message)
// This is critical for preventing TOCTOU race conditions!
func (m *GlobalStateManager) EvaluatePreconditions(robotID string, conditions []Precondition) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	robot, exists := m.robots[robotID]
	if !exists {
		return false, fmt.Sprintf("robot %s not found", robotID)
	}

	for _, cond := range conditions {
		switch cond.Type {
		case "robot_state":
			if !m.evaluateStateCondition(robot, cond.Condition) {
				return false, cond.Message
			}
		case "zone_free":
			if !m.evaluateZoneCondition(robotID, cond.Condition) {
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
	TargetType string                   `json:"target_type"` // self, robot, agent, all
	RobotID    string                   `json:"robot_id"`    // For 'specific' quantifier
	AgentID    string                   `json:"agent_id"`    // For 'agent' target

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
		(c.Quantifier == "" && c.TargetType == "" && c.RobotID == "")
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

// evaluateZoneCondition checks if a zone is free or owned by this robot
func (m *GlobalStateManager) evaluateZoneCondition(robotID, zoneID string) bool {
	existing, exists := m.zones[zoneID]
	if !exists {
		return true // Zone is free
	}

	// Check if expired
	if time.Now().After(existing.ExpiresAt) {
		return true
	}

	// Zone is reserved - is it by this robot?
	return existing.RobotID == robotID
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
func (m *GlobalStateManager) EvaluateStartConditions(executingRobotID string, conditions []StartCondition) (bool, string, string) {
	result := m.ValidateStartConditions(executingRobotID, conditions)
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
func (m *GlobalStateManager) EvaluateStartConditionList(executingRobotID string, conditions []StartCondition) (bool, string) {
	if len(conditions) == 0 {
		return true, ""
	}

	now := time.Now()
	result := true
	var errorMessage string

	for i, cond := range conditions {
		condResult := m.evaluateSingleConditionDetailed(executingRobotID, cond, now)
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
func (m *GlobalStateManager) ValidateStartConditions(executingRobotID string, conditions []StartCondition) StartConditionValidationResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	results := make([]ConditionResult, 0, len(conditions))
	allPassed := true
	var errors []string

	for _, cond := range conditions {
		result := m.evaluateSingleConditionDetailed(executingRobotID, cond, now)
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

// evaluateSingleConditionDetailed evaluates one condition with full detail tracking
func (m *GlobalStateManager) evaluateSingleConditionDetailed(executingRobotID string, cond StartCondition, now time.Time) ConditionResult {
	result := ConditionResult{
		ConditionID:    cond.ID,
		TargetRobots:   []string{},
		MatchingRobots: []string{},
		FailedRobots:   []string{},
		StaleRobots:    []string{},
		OfflineRobots:  []string{},
	}

	// Get target robots
	targetRobots := m.getTargetRobots(executingRobotID, cond)

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
func (m *GlobalStateManager) EvaluateConditionGroup(executingRobotID string, group StartConditionGroup) ConditionResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	results := make([]ConditionResult, 0, len(group.Conditions))

	for _, cond := range group.Conditions {
		result := m.evaluateSingleConditionDetailed(executingRobotID, cond, now)
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
func (m *GlobalStateManager) getTargetRobots(executingRobotID string, cond StartCondition) []*RobotState {
	var targets []*RobotState

	switch cond.Quantifier {
	case QuantifierSelf, "":
		// Self or default: only the executing robot
		if robot, exists := m.robots[executingRobotID]; exists {
			targets = append(targets, robot)
		}

	case QuantifierSpecific:
		// Specific agent or robot
		if cond.AgentID != "" {
			for _, robot := range m.robots {
				if robot.AgentID == cond.AgentID {
					targets = append(targets, robot)
				}
			}
		} else if cond.RobotID != "" {
			if robot, exists := m.robots[cond.RobotID]; exists {
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
// Returns (success, error_message)
func (m *GlobalStateManager) TryStartExecution(robotID, taskID, stepID string, requiredZones []string, preconditions []Precondition) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[robotID]
	if !exists {
		return false, fmt.Sprintf("robot %s not found", robotID)
	}

	// Check if already executing
	if robot.IsExecuting {
		return false, fmt.Sprintf("robot %s is already executing task %s", robotID, robot.CurrentTaskID)
	}

	// Check preconditions (while holding lock)
	for _, cond := range preconditions {
		switch cond.Type {
		case "robot_state":
			if !m.evaluateStateCondition(robot, cond.Condition) {
				return false, cond.Message
			}
		case "zone_free":
			if !m.evaluateZoneCondition(robotID, cond.Condition) {
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
		}
	}

	// Reserve required zones
	now := time.Now()
	reservedZones := make([]string, 0, len(requiredZones))
	for _, zoneID := range requiredZones {
		existing, exists := m.zones[zoneID]
		if exists && now.Before(existing.ExpiresAt) && existing.RobotID != robotID {
			// Zone is taken by someone else - rollback reserved zones
			for _, reserved := range reservedZones {
				delete(m.zones, reserved)
			}
			return false, fmt.Sprintf("zone %s is reserved by robot %s", zoneID, existing.RobotID)
		}

		// Reserve the zone
		m.zones[zoneID] = &ZoneReservation{
			ZoneID:     zoneID,
			RobotID:    robotID,
			ReservedAt: now,
			ExpiresAt:  now.Add(m.zoneExpiryDuration),
		}
		reservedZones = append(reservedZones, zoneID)
	}

	// All checks passed - update execution state
	robot.IsExecuting = true
	robot.CurrentTaskID = taskID
	robot.CurrentStepID = stepID
	robot.LastSeen = now

	return true, ""
}

// CompleteExecution marks execution as complete and releases zones
func (m *GlobalStateManager) CompleteExecution(robotID string, releasedZones []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	robot, exists := m.robots[robotID]
	if !exists {
		return
	}

	robot.IsExecuting = false
	robot.CurrentTaskID = ""
	robot.CurrentStepID = ""
	robot.LastSeen = time.Now()

	// Release zones
	for _, zoneID := range releasedZones {
		if existing, exists := m.zones[zoneID]; exists {
			if existing.RobotID == robotID {
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
		agentCopy.RobotIDs = make([]string, len(agent.RobotIDs))
		copy(agentCopy.RobotIDs, agent.RobotIDs)
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

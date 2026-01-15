package state

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"central_server_go/internal/db"
)

// AgentStateEntry represents an agent's current state with semantic information
type AgentStateEntry struct {
	AgentID        string    `json:"agent_id"`
	StateCode      string    `json:"state_code"`       // e.g., "pick:executing", "idle"
	SemanticTags   []string  `json:"semantic_tags"`    // e.g., ["picking", "busy"]
	CurrentGraphID string    `json:"current_graph_id"` // Currently executing graph
	IsOnline       bool      `json:"is_online"`
	IsExecuting    bool      `json:"is_executing"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// StateRegistry tracks all agents' states with semantic tag indexing
// Provides O(1) lookups for semantic tags and efficient cross-agent queries
type StateRegistry struct {
	mu sync.RWMutex

	// Agent states indexed by agent ID
	agentStates map[string]*AgentStateEntry

	// Semantic tag index: tag -> set of agent IDs
	// Enables O(1) lookup for "find all agents with tag X"
	tagIndex map[string]map[string]struct{}

	// State code index: stateCode -> set of agent IDs
	// Enables O(1) lookup for "find all agents in state X"
	stateIndex map[string]map[string]struct{}
}

// NewStateRegistry creates a new state registry
func NewStateRegistry() *StateRegistry {
	return &StateRegistry{
		agentStates: make(map[string]*AgentStateEntry),
		tagIndex:    make(map[string]map[string]struct{}),
		stateIndex:  make(map[string]map[string]struct{}),
	}
}

// UpdateAgentState updates an agent's state and semantic tags
func (r *StateRegistry) UpdateAgentState(agentID, stateCode string, semanticTags []string, graphID string, isOnline, isExecuting bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// Get or create entry
	entry, exists := r.agentStates[agentID]
	if !exists {
		entry = &AgentStateEntry{AgentID: agentID}
		r.agentStates[agentID] = entry
	}

	// Remove old index entries
	r.removeFromIndexesLocked(agentID, entry.StateCode, entry.SemanticTags)

	// Update entry
	entry.StateCode = stateCode
	entry.SemanticTags = semanticTags
	entry.CurrentGraphID = graphID
	entry.IsOnline = isOnline
	entry.IsExecuting = isExecuting
	entry.UpdatedAt = now

	// Add new index entries
	r.addToIndexesLocked(agentID, stateCode, semanticTags)
}

// SetAgentOnline updates just the online status
func (r *StateRegistry) SetAgentOnline(agentID string, isOnline bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, exists := r.agentStates[agentID]; exists {
		entry.IsOnline = isOnline
		entry.UpdatedAt = time.Now()
	}
}

// SetAgentExecuting updates just the executing status
func (r *StateRegistry) SetAgentExecuting(agentID string, isExecuting bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, exists := r.agentStates[agentID]; exists {
		entry.IsExecuting = isExecuting
		entry.UpdatedAt = time.Now()
	}
}

// GetAgentState returns a copy of an agent's state entry
func (r *StateRegistry) GetAgentState(agentID string) (*AgentStateEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.agentStates[agentID]
	if !exists {
		return nil, false
	}

	// Return a copy
	copy := *entry
	copy.SemanticTags = append([]string{}, entry.SemanticTags...)
	return &copy, true
}

// GetAllAgentStates returns a copy of all agent states
func (r *StateRegistry) GetAllAgentStates() map[string]*AgentStateEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*AgentStateEntry, len(r.agentStates))
	for id, entry := range r.agentStates {
		copy := *entry
		copy.SemanticTags = append([]string{}, entry.SemanticTags...)
		result[id] = &copy
	}
	return result
}

// GetAgentsByTag returns all agent IDs that have a specific semantic tag
func (r *StateRegistry) GetAgentsByTag(tag string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agentSet, exists := r.tagIndex[tag]
	if !exists {
		return nil
	}

	result := make([]string, 0, len(agentSet))
	for agentID := range agentSet {
		result = append(result, agentID)
	}
	return result
}

// GetAgentsByState returns all agent IDs that are in a specific state code
func (r *StateRegistry) GetAgentsByState(stateCode string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agentSet, exists := r.stateIndex[stateCode]
	if !exists {
		return nil
	}

	result := make([]string, 0, len(agentSet))
	for agentID := range agentSet {
		result = append(result, agentID)
	}
	return result
}

// HasTag checks if an agent has a specific semantic tag
func (r *StateRegistry) HasTag(agentID, tag string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.agentStates[agentID]
	if !exists {
		return false
	}

	for _, t := range entry.SemanticTags {
		if t == tag {
			return true
		}
	}
	return false
}

// IsInState checks if an agent is in a specific state
func (r *StateRegistry) IsInState(agentID, stateCode string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.agentStates[agentID]
	if !exists {
		return false
	}

	return entry.StateCode == stateCode
}

// CountAgentsWithTag returns the number of agents with a specific tag
func (r *StateRegistry) CountAgentsWithTag(tag string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if agentSet, exists := r.tagIndex[tag]; exists {
		return len(agentSet)
	}
	return 0
}

// CountAgentsInState returns the number of agents in a specific state
func (r *StateRegistry) CountAgentsInState(stateCode string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if agentSet, exists := r.stateIndex[stateCode]; exists {
		return len(agentSet)
	}
	return 0
}

// RemoveAgent removes an agent from the registry
func (r *StateRegistry) RemoveAgent(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.agentStates[agentID]
	if !exists {
		return
	}

	// Remove from indexes
	r.removeFromIndexesLocked(agentID, entry.StateCode, entry.SemanticTags)

	// Remove entry
	delete(r.agentStates, agentID)
}

// Private helper methods

func (r *StateRegistry) addToIndexesLocked(agentID, stateCode string, tags []string) {
	// Add to state index
	if stateCode != "" {
		if r.stateIndex[stateCode] == nil {
			r.stateIndex[stateCode] = make(map[string]struct{})
		}
		r.stateIndex[stateCode][agentID] = struct{}{}
	}

	// Add to tag index
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if r.tagIndex[tag] == nil {
			r.tagIndex[tag] = make(map[string]struct{})
		}
		r.tagIndex[tag][agentID] = struct{}{}
	}
}

func (r *StateRegistry) removeFromIndexesLocked(agentID, stateCode string, tags []string) {
	// Remove from state index
	if stateCode != "" {
		if agentSet, exists := r.stateIndex[stateCode]; exists {
			delete(agentSet, agentID)
			if len(agentSet) == 0 {
				delete(r.stateIndex, stateCode)
			}
		}
	}

	// Remove from tag index
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if agentSet, exists := r.tagIndex[tag]; exists {
			delete(agentSet, agentID)
			if len(agentSet) == 0 {
				delete(r.tagIndex, tag)
			}
		}
	}
}

// ============================================================
// Enhanced Precondition Evaluation
// ============================================================

// PreconditionResult contains the result of evaluating a precondition
type PreconditionResult struct {
	Passed       bool     `json:"passed"`
	Error        string   `json:"error,omitempty"`
	MatchedCount int      `json:"matched_count,omitempty"`
	MatchedIDs   []string `json:"matched_ids,omitempty"`
}

// EvaluateEnhancedPrecondition evaluates an enhanced precondition
// Supports cross-agent state checking and semantic tag queries
func (r *StateRegistry) EvaluateEnhancedPrecondition(executingAgentID string, cond db.EnhancedPrecondition) PreconditionResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	switch cond.Type {
	case "self_state":
		return r.evaluateSelfStateLocked(executingAgentID, cond)
	case "agent_state":
		return r.evaluateAgentStateLocked(cond)
	case "semantic_tag":
		return r.evaluateSemanticTagLocked(cond)
	case "any_agent_state":
		return r.evaluateAnyAgentStateLocked(executingAgentID, cond)
	default:
		return PreconditionResult{
			Passed: false,
			Error:  fmt.Sprintf("unknown precondition type: %s", cond.Type),
		}
	}
}

// EvaluateEnhancedPreconditions evaluates multiple preconditions
// All must pass for overall success
func (r *StateRegistry) EvaluateEnhancedPreconditions(executingAgentID string, conditions []db.EnhancedPrecondition) (bool, string) {
	for _, cond := range conditions {
		result := r.EvaluateEnhancedPrecondition(executingAgentID, cond)
		if !result.Passed {
			msg := cond.Message
			if msg == "" {
				msg = result.Error
			}
			return false, msg
		}
	}
	return true, ""
}

// Private evaluation helpers

func (r *StateRegistry) evaluateSelfStateLocked(agentID string, cond db.EnhancedPrecondition) PreconditionResult {
	entry, exists := r.agentStates[agentID]
	if !exists {
		return PreconditionResult{
			Passed: false,
			Error:  fmt.Sprintf("agent %s not found in registry", agentID),
		}
	}

	passed := r.matchStateCodeLocked(entry.StateCode, cond.StateCode, cond.Operator)
	if !passed {
		return PreconditionResult{
			Passed: false,
			Error:  fmt.Sprintf("self state is '%s', expected '%s' (op: %s)", entry.StateCode, cond.StateCode, cond.Operator),
		}
	}

	return PreconditionResult{
		Passed:       true,
		MatchedCount: 1,
		MatchedIDs:   []string{agentID},
	}
}

func (r *StateRegistry) evaluateAgentStateLocked(cond db.EnhancedPrecondition) PreconditionResult {
	if cond.TargetAgentID == "" {
		return PreconditionResult{
			Passed: false,
			Error:  "target_agent_id is required for agent_state type",
		}
	}

	entry, exists := r.agentStates[cond.TargetAgentID]
	if !exists {
		return PreconditionResult{
			Passed: false,
			Error:  fmt.Sprintf("target agent %s not found in registry", cond.TargetAgentID),
		}
	}

	passed := r.matchStateCodeLocked(entry.StateCode, cond.StateCode, cond.Operator)
	if !passed {
		return PreconditionResult{
			Passed: false,
			Error:  fmt.Sprintf("agent %s state is '%s', expected '%s' (op: %s)", cond.TargetAgentID, entry.StateCode, cond.StateCode, cond.Operator),
		}
	}

	return PreconditionResult{
		Passed:       true,
		MatchedCount: 1,
		MatchedIDs:   []string{cond.TargetAgentID},
	}
}

func (r *StateRegistry) evaluateSemanticTagLocked(cond db.EnhancedPrecondition) PreconditionResult {
	if cond.SemanticTag == "" {
		return PreconditionResult{
			Passed: false,
			Error:  "semantic_tag is required for semantic_tag type",
		}
	}

	// Find all agents with this tag
	matchedIDs := make([]string, 0)
	if agentSet, exists := r.tagIndex[cond.SemanticTag]; exists {
		for agentID := range agentSet {
			// Apply filter if present
			if cond.Filter != nil && !r.matchesFilterLocked(agentID, cond.Filter) {
				continue
			}
			matchedIDs = append(matchedIDs, agentID)
		}
	}

	// Evaluate count condition
	passed := r.evaluateCountCondition(len(matchedIDs), cond.CountCondition)

	if !passed {
		return PreconditionResult{
			Passed:       false,
			Error:        fmt.Sprintf("semantic tag '%s' count %d does not satisfy '%s'", cond.SemanticTag, len(matchedIDs), cond.CountCondition),
			MatchedCount: len(matchedIDs),
			MatchedIDs:   matchedIDs,
		}
	}

	return PreconditionResult{
		Passed:       true,
		MatchedCount: len(matchedIDs),
		MatchedIDs:   matchedIDs,
	}
}

func (r *StateRegistry) evaluateAnyAgentStateLocked(executingAgentID string, cond db.EnhancedPrecondition) PreconditionResult {
	matchedIDs := make([]string, 0)

	for agentID, entry := range r.agentStates {
		// Skip self if not included in filter
		if cond.Filter != nil && !cond.Filter.IncludeSelf && agentID == executingAgentID {
			continue
		}

		// Apply filter
		if cond.Filter != nil && !r.matchesFilterLocked(agentID, cond.Filter) {
			continue
		}

		// Check state match
		if r.matchStateCodeLocked(entry.StateCode, cond.StateCode, cond.Operator) {
			matchedIDs = append(matchedIDs, agentID)
		}
	}

	// Evaluate count condition (default: >= 1)
	countCond := cond.CountCondition
	if countCond == "" {
		countCond = ">= 1"
	}
	passed := r.evaluateCountCondition(len(matchedIDs), countCond)

	if !passed {
		return PreconditionResult{
			Passed:       false,
			Error:        fmt.Sprintf("agents with state '%s' count %d does not satisfy '%s'", cond.StateCode, len(matchedIDs), countCond),
			MatchedCount: len(matchedIDs),
			MatchedIDs:   matchedIDs,
		}
	}

	return PreconditionResult{
		Passed:       true,
		MatchedCount: len(matchedIDs),
		MatchedIDs:   matchedIDs,
	}
}

func (r *StateRegistry) matchStateCodeLocked(actual, expected, operator string) bool {
	if operator == "" {
		operator = "=="
	}

	switch operator {
	case "==", "eq":
		return actual == expected
	case "!=", "ne":
		return actual != expected
	case "contains":
		return strings.Contains(actual, expected)
	case "prefix":
		return strings.HasPrefix(actual, expected)
	case "suffix":
		return strings.HasSuffix(actual, expected)
	default:
		return actual == expected
	}
}

func (r *StateRegistry) matchesFilterLocked(agentID string, filter *db.PreconditionFilter) bool {
	entry, exists := r.agentStates[agentID]
	if !exists {
		return false
	}

	// Check online filter
	if filter.OnlineOnly && !entry.IsOnline {
		return false
	}

	// Check executing filter
	if filter.ExecutingOnly && !entry.IsExecuting {
		return false
	}

	// Check graph filter
	if filter.GraphID != "" && entry.CurrentGraphID != filter.GraphID {
		return false
	}

	// Check tags filter (all must be present)
	for _, requiredTag := range filter.Tags {
		found := false
		for _, tag := range entry.SemanticTags {
			if tag == requiredTag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func (r *StateRegistry) evaluateCountCondition(count int, condition string) bool {
	if condition == "" {
		return count > 0 // Default: at least one
	}

	// Parse condition: ">= 1", "== 0", "< 3", etc.
	condition = strings.TrimSpace(condition)

	// Handle simple operators
	if strings.HasPrefix(condition, ">=") {
		var threshold int
		fmt.Sscanf(condition, ">= %d", &threshold)
		return count >= threshold
	}
	if strings.HasPrefix(condition, "<=") {
		var threshold int
		fmt.Sscanf(condition, "<= %d", &threshold)
		return count <= threshold
	}
	if strings.HasPrefix(condition, ">") {
		var threshold int
		fmt.Sscanf(condition, "> %d", &threshold)
		return count > threshold
	}
	if strings.HasPrefix(condition, "<") {
		var threshold int
		fmt.Sscanf(condition, "< %d", &threshold)
		return count < threshold
	}
	if strings.HasPrefix(condition, "==") {
		var threshold int
		fmt.Sscanf(condition, "== %d", &threshold)
		return count == threshold
	}
	if strings.HasPrefix(condition, "!=") {
		var threshold int
		fmt.Sscanf(condition, "!= %d", &threshold)
		return count != threshold
	}

	// Just a number means >= that number
	var threshold int
	fmt.Sscanf(condition, "%d", &threshold)
	return count >= threshold
}

// ============================================================
// Fleet State Broadcasting
// ============================================================

// FleetStateUpdate represents a state update to broadcast
type FleetStateUpdate struct {
	AgentID      string    `json:"agent_id"`
	StateCode    string    `json:"state_code"`
	SemanticTags []string  `json:"semantic_tags"`
	GraphID      string    `json:"graph_id"`
	IsOnline     bool      `json:"is_online"`
	IsExecuting  bool      `json:"is_executing"`
	Timestamp    time.Time `json:"timestamp"`
}

// GetFleetStateUpdates returns current state of all agents for broadcasting
func (r *StateRegistry) GetFleetStateUpdates() []FleetStateUpdate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	updates := make([]FleetStateUpdate, 0, len(r.agentStates))
	for _, entry := range r.agentStates {
		updates = append(updates, FleetStateUpdate{
			AgentID:      entry.AgentID,
			StateCode:    entry.StateCode,
			SemanticTags: append([]string{}, entry.SemanticTags...),
			GraphID:      entry.CurrentGraphID,
			IsOnline:     entry.IsOnline,
			IsExecuting:  entry.IsExecuting,
			Timestamp:    entry.UpdatedAt,
		})
	}
	return updates
}

// GetOnlineAgentIDs returns IDs of all online agents
func (r *StateRegistry) GetOnlineAgentIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]string, 0)
	for agentID, entry := range r.agentStates {
		if entry.IsOnline {
			result = append(result, agentID)
		}
	}
	return result
}

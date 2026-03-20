package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/executor"
	"central_server_go/internal/pddl"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type RealtimeGoalRequest struct {
	ID                   string                 `json:"id,omitempty"`
	Name                 string                 `json:"name"`
	Priority             int                    `json:"priority"`
	Enabled              bool                   `json:"enabled"`
	ActivationConditions []db.PlanningCondition `json:"activation_conditions,omitempty"`
	ResourceTypeID       string                 `json:"resource_type_id,omitempty"`
	GoalState            map[string]string      `json:"goal_state"`
}

type RealtimeSessionRequest struct {
	Name              string                `json:"name"`
	BehaviorTreeID    string                `json:"behavior_tree_id"`
	BehaviorTreeIDs   []string              `json:"behavior_tree_ids,omitempty"`
	TaskDistributorID string                `json:"task_distributor_id,omitempty"`
	InitialState      map[string]string     `json:"initial_state,omitempty"`
	AgentIDs          []string              `json:"agent_ids"`
	TickIntervalSec   float64               `json:"tick_interval_sec,omitempty"`
	Goals             []RealtimeGoalRequest `json:"goals"`
}

type RealtimeGoalResponse struct {
	ID                   string                 `json:"id"`
	Name                 string                 `json:"name"`
	Priority             int                    `json:"priority"`
	Enabled              bool                   `json:"enabled"`
	ActivationConditions []db.PlanningCondition `json:"activation_conditions,omitempty"`
	ResourceTypeID       string                 `json:"resource_type_id,omitempty"`
	GoalState            map[string]string      `json:"goal_state"`
}

type RealtimeSessionResponse struct {
	ID                    string                 `json:"id"`
	Name                  string                 `json:"name"`
	Status                string                 `json:"status"`
	BehaviorTreeIDs       []string               `json:"behavior_tree_ids"`
	TaskDistributorID     string                 `json:"task_distributor_id,omitempty"`
	AgentIDs              []string               `json:"agent_ids"`
	TickIntervalSec       float64                `json:"tick_interval_sec"`
	Goals                 []RealtimeGoalResponse `json:"goals"`
	EffectiveState        map[string]string      `json:"effective_state,omitempty"`
	CurrentState          map[string]string      `json:"current_state"`
	LiveState             map[string]string      `json:"live_state,omitempty"`
	SelectedGoalID        string                 `json:"selected_goal_id,omitempty"`
	SelectedGoalName      string                 `json:"selected_goal_name,omitempty"`
	SelectedAgentID       string                 `json:"selected_agent_id,omitempty"`
	SelectedAgentName     string                 `json:"selected_agent_name,omitempty"`
	SelectedResourceID    string                 `json:"selected_resource_id,omitempty"`
	SelectedResourceName  string                 `json:"selected_resource_name,omitempty"`
	ActiveExecutionID     string                 `json:"active_execution_id,omitempty"`
	ActiveExecutionIDs    []string               `json:"active_execution_ids,omitempty"`
	ActiveExecutionStatus string                 `json:"active_execution_status,omitempty"`
	LastError             string                 `json:"last_error,omitempty"`
	LastPlan              *pddl.Plan             `json:"last_plan,omitempty"`
	StartedAt             time.Time              `json:"started_at"`
	UpdatedAt             time.Time              `json:"updated_at"`
}

type RealtimeSessionStateResetRequest struct {
	Values        map[string]string `json:"values"`
	ClearLiveKeys []string          `json:"clear_live_keys,omitempty"`
}

type realtimeGoal struct {
	ID                   string
	Name                 string
	Priority             int
	Enabled              bool
	ActivationConditions []db.PlanningCondition
	ResourceTypeID       string
	GoalState            map[string]string
}

type realtimeGoalBinding struct {
	AgentID      string
	AgentName    string
	ResourceID   string
	ResourceName string
}

type realtimeExecutionContext struct {
	ExecutionID string
	Plan        *pddl.Plan
	Goal        *realtimeGoal
	Binding     realtimeGoalBinding
	StartedAt   time.Time
}

type realtimeDispatchCandidate struct {
	AgentID    string
	AgentOrder int
	Goal       *realtimeGoal
	Binding    realtimeGoalBinding
	Plan       *pddl.Plan
	FailureKey string
}

type realtimeSession struct {
	ID                   string
	Name                 string
	BehaviorTreeIDs      []string
	TaskDistributorID    string
	AgentIDs             []string
	Agents               []pddl.AgentInfo
	Resources            []pddl.ResourceInfo
	TickInterval         time.Duration
	Goals                []realtimeGoal
	CurrentState         map[string]string
	SelectedGoalID       string
	SelectedGoalName     string
	SelectedAgentID      string
	SelectedAgentName    string
	SelectedResourceID   string
	SelectedResourceName string
	LastSelectedAgentID  string
	ActiveExecutionID    string
	ActivePlan           *pddl.Plan
	ActiveGoal           *realtimeGoal
	ActiveExecutions     map[string]realtimeExecutionContext
	ActiveStatus         string
	LastError            string
	LastPlan             *pddl.Plan
	StartedAt            time.Time
	UpdatedAt            time.Time
	RuntimeStateSources  map[string]runtimeStateSource

	cancel         context.CancelFunc
	ctx            context.Context
	failedStateKey map[string]string
	mu             sync.RWMutex
}

type runtimeStateSource struct {
	Values    map[string]string
	UpdatedAt time.Time
	ExpiresAt *time.Time
}

type RealtimePddlManager struct {
	server   *Server
	sessions map[string]*realtimeSession
	mu       sync.RWMutex
}

func NewRealtimePddlManager(server *Server) *RealtimePddlManager {
	return &RealtimePddlManager{
		server:   server,
		sessions: make(map[string]*realtimeSession),
	}
}

func (m *RealtimePddlManager) Start(req RealtimeSessionRequest) (*realtimeSession, error) {
	behaviorTreeIDs := normalizeBehaviorTreeIDs(req.BehaviorTreeID, req.BehaviorTreeIDs)
	if len(behaviorTreeIDs) == 0 {
		return nil, fmt.Errorf("behavior_tree_id or behavior_tree_ids is required")
	}
	if strings.TrimSpace(req.TaskDistributorID) == "" {
		return nil, fmt.Errorf("task_distributor_id is required")
	}
	if len(req.AgentIDs) == 0 {
		return nil, fmt.Errorf("agent_ids is required")
	}
	if len(req.Goals) == 0 {
		return nil, fmt.Errorf("at least one realtime goal is required")
	}

	for _, btID := range behaviorTreeIDs {
		bt, err := m.server.repo.GetBehaviorTree(btID)
		if err != nil {
			return nil, fmt.Errorf("failed to get behavior tree %q: %w", btID, err)
		}
		if bt == nil {
			return nil, fmt.Errorf("behavior tree %q not found", btID)
		}
	}

	currentState, err := m.server.buildRealtimeInitialState(req.TaskDistributorID, req.InitialState)
	if err != nil {
		return nil, err
	}
	agents, err := m.server.loadRealtimeAgents(req.AgentIDs)
	if err != nil {
		return nil, err
	}
	resources, err := m.server.loadRealtimeResources(req.TaskDistributorID)
	if err != nil {
		return nil, err
	}

	tickInterval := 2 * time.Second
	if req.TickIntervalSec > 0 {
		tickInterval = time.Duration(req.TickIntervalSec * float64(time.Second))
	}
	if tickInterval < 500*time.Millisecond {
		tickInterval = 500 * time.Millisecond
	}

	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now().UTC()
	session := &realtimeSession{
		ID:                  uuid.New().String()[:8],
		Name:                strings.TrimSpace(req.Name),
		BehaviorTreeIDs:     behaviorTreeIDs,
		TaskDistributorID:   strings.TrimSpace(req.TaskDistributorID),
		AgentIDs:            append([]string{}, req.AgentIDs...),
		Agents:              append([]pddl.AgentInfo{}, agents...),
		Resources:           append([]pddl.ResourceInfo{}, resources...),
		TickInterval:        tickInterval,
		Goals:               normalizeRealtimeGoals(req.Goals),
		CurrentState:        cloneStringMap(currentState),
		ActiveStatus:        "starting",
		StartedAt:           now,
		UpdatedAt:           now,
		ctx:                 ctx,
		cancel:              cancel,
		failedStateKey:      map[string]string{},
		ActiveExecutions:    map[string]realtimeExecutionContext{},
		RuntimeStateSources: map[string]runtimeStateSource{},
	}
	if session.Name == "" {
		session.Name = fmt.Sprintf("realtime-%s", session.ID)
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	go m.run(session)

	return session, nil
}

func (m *RealtimePddlManager) Stop(id string) error {
	session, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("realtime session %s not found", id)
	}

	session.mu.Lock()
	session.ActiveStatus = "stopped"
	session.UpdatedAt = time.Now().UTC()
	activeExecutionIDs := make([]string, 0, len(session.ActiveExecutions)+1)
	for executionID := range session.ActiveExecutions {
		activeExecutionIDs = append(activeExecutionIDs, executionID)
	}
	if strings.TrimSpace(session.ActiveExecutionID) != "" {
		activeExecutionIDs = append(activeExecutionIDs, session.ActiveExecutionID)
	}
	session.ActiveExecutionID = ""
	session.ActivePlan = nil
	session.ActiveGoal = nil
	session.ActiveExecutions = map[string]realtimeExecutionContext{}
	session.mu.Unlock()

	session.cancel()

	for _, executionID := range uniqueNonEmptyStrings(activeExecutionIDs) {
		if strings.TrimSpace(executionID) == "" {
			continue
		}
		_ = m.server.planExecutor.CancelExecution(executionID)
	}
	m.cancelExecutionsByProblemPrefix("realtime:" + id + ":")
	return nil
}

func (m *RealtimePddlManager) ResetSessionState(
	id string,
	values map[string]string,
	clearLiveKeys []string,
) (*realtimeSession, error) {
	session, ok := m.Get(strings.TrimSpace(id))
	if !ok || session == nil {
		return nil, fmt.Errorf("realtime session %s not found", id)
	}

	normalizedValues := normalizeRuntimeStateValues(values)
	if len(normalizedValues) == 0 {
		return nil, fmt.Errorf("values is required")
	}

	clearKeySet := make(map[string]struct{}, len(clearLiveKeys))
	for _, key := range clearLiveKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		clearKeySet[trimmed] = struct{}{}
	}
	for key := range normalizedValues {
		clearKeySet[key] = struct{}{}
	}

	now := time.Now().UTC()
	session.mu.Lock()
	if session.CurrentState == nil {
		session.CurrentState = map[string]string{}
	}
	for key, value := range normalizedValues {
		session.CurrentState[key] = value
	}
	if len(clearKeySet) > 0 {
		for source, runtime := range session.RuntimeStateSources {
			if len(runtime.Values) == 0 {
				delete(session.RuntimeStateSources, source)
				continue
			}
			changed := false
			for key := range clearKeySet {
				if _, exists := runtime.Values[key]; exists {
					delete(runtime.Values, key)
					changed = true
				}
			}
			if len(runtime.Values) == 0 {
				delete(session.RuntimeStateSources, source)
				continue
			}
			if changed {
				runtime.UpdatedAt = now
				session.RuntimeStateSources[source] = runtime
			}
		}
	}
	session.UpdatedAt = now
	session.mu.Unlock()

	return session, nil
}

func (m *RealtimePddlManager) cancelExecutionsByProblemPrefix(prefix string) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return
	}

	executions := m.server.planExecutor.ListExecutions()
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		snapshot := execution.Snapshot()
		if !strings.HasPrefix(snapshot.ProblemID, prefix) {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(snapshot.Status))
		if status != string(executor.PlanExecRunning) && status != string(executor.PlanExecPending) {
			continue
		}
		_ = m.server.planExecutor.CancelExecution(snapshot.ID)
	}
}

// UpsertRuntimeStateByDistributor applies runtime state overlays to all realtime
// sessions bound to the given task distributor.
func (m *RealtimePddlManager) UpsertRuntimeStateByDistributor(
	taskDistributorID string,
	source string,
	values map[string]string,
	ttlSec float64,
) int {
	taskDistributorID = strings.TrimSpace(taskDistributorID)
	if taskDistributorID == "" {
		return 0
	}
	normalized := normalizeRuntimeStateValues(values)
	if len(normalized) == 0 {
		return 0
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "runtime"
	}

	now := time.Now().UTC()
	var expiresAt *time.Time
	if ttlSec > 0 {
		exp := now.Add(time.Duration(ttlSec * float64(time.Second)))
		expiresAt = &exp
	}

	updated := 0
	m.mu.RLock()
	for _, session := range m.sessions {
		if session == nil || session.TaskDistributorID != taskDistributorID {
			continue
		}
		session.mu.Lock()
		session.RuntimeStateSources[source] = runtimeStateSource{
			Values:    cloneStringMap(normalized),
			UpdatedAt: now,
			ExpiresAt: expiresAt,
		}
		session.UpdatedAt = now
		session.mu.Unlock()
		updated++
	}
	m.mu.RUnlock()

	return updated
}

// UpsertRuntimeStateByAgent applies runtime state overlays to all realtime sessions
// that include the given agent ID.
func (m *RealtimePddlManager) UpsertRuntimeStateByAgent(
	agentID string,
	source string,
	values map[string]string,
	ttlSec float64,
) int {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return 0
	}
	normalized := normalizeRuntimeStateValues(values)
	if len(normalized) == 0 {
		return 0
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "agent:" + agentID
	}

	now := time.Now().UTC()
	var expiresAt *time.Time
	if ttlSec > 0 {
		exp := now.Add(time.Duration(ttlSec * float64(time.Second)))
		expiresAt = &exp
	}

	updated := 0
	m.mu.RLock()
	for _, session := range m.sessions {
		if session == nil || !sessionContainsAgent(session, agentID) {
			continue
		}
		session.mu.Lock()
		session.RuntimeStateSources[source] = runtimeStateSource{
			Values:    cloneStringMap(normalized),
			UpdatedAt: now,
			ExpiresAt: expiresAt,
		}
		session.UpdatedAt = now
		session.mu.Unlock()
		updated++
	}
	m.mu.RUnlock()

	return updated
}

// ClearRuntimeStateByDistributor removes runtime overlay state from sessions of a distributor.
// If source is empty, all runtime sources are cleared.
func (m *RealtimePddlManager) ClearRuntimeStateByDistributor(taskDistributorID string, source string) int {
	taskDistributorID = strings.TrimSpace(taskDistributorID)
	if taskDistributorID == "" {
		return 0
	}
	source = strings.TrimSpace(source)
	now := time.Now().UTC()
	cleared := 0

	m.mu.RLock()
	for _, session := range m.sessions {
		if session == nil || session.TaskDistributorID != taskDistributorID {
			continue
		}
		session.mu.Lock()
		if source == "" {
			if len(session.RuntimeStateSources) > 0 {
				session.RuntimeStateSources = map[string]runtimeStateSource{}
				cleared++
			}
		} else {
			if _, ok := session.RuntimeStateSources[source]; ok {
				delete(session.RuntimeStateSources, source)
				cleared++
			}
		}
		session.UpdatedAt = now
		session.mu.Unlock()
	}
	m.mu.RUnlock()

	return cleared
}

func (m *RealtimePddlManager) Get(id string) (*realtimeSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[id]
	return session, ok
}

func (m *RealtimePddlManager) List() []*realtimeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*realtimeSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		result = append(result, session)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.After(result[j].StartedAt)
	})
	return result
}

func (m *RealtimePddlManager) run(session *realtimeSession) {
	ticker := time.NewTicker(session.TickInterval)
	defer ticker.Stop()

	m.tick(session)
	for {
		select {
		case <-session.ctx.Done():
			session.mu.Lock()
			session.ActiveStatus = "stopped"
			session.UpdatedAt = time.Now().UTC()
			session.mu.Unlock()
			return
		case <-ticker.C:
			m.tick(session)
		}
	}
}

func (m *RealtimePddlManager) tick(session *realtimeSession) {
	if session == nil {
		return
	}

	m.syncExecutions(session)
	currentState := m.sessionMergedState(session)

	session.mu.RLock()
	preferredAgentID := strings.TrimSpace(session.LastSelectedAgentID)
	agents := append([]pddl.AgentInfo{}, session.Agents...)
	resources := append([]pddl.ResourceInfo{}, session.Resources...)
	goals := make([]realtimeGoal, 0, len(session.Goals))
	for _, goal := range session.Goals {
		goals = append(goals, realtimeGoal{
			ID:                   goal.ID,
			Name:                 goal.Name,
			Priority:             goal.Priority,
			Enabled:              goal.Enabled,
			ActivationConditions: cloneConditions(goal.ActivationConditions),
			ResourceTypeID:       goal.ResourceTypeID,
			GoalState:            cloneStringMap(goal.GoalState),
		})
	}
	behaviorTreeIDs := append([]string{}, session.BehaviorTreeIDs...)
	taskDistributorID := strings.TrimSpace(session.TaskDistributorID)
	session.mu.RUnlock()

	if len(agents) == 0 || len(behaviorTreeIDs) == 0 {
		session.mu.Lock()
		if len(session.ActiveExecutions) == 0 {
			session.ActiveStatus = "idle"
			session.ActiveExecutionID = ""
			session.ActivePlan = nil
			session.ActiveGoal = nil
		}
		session.UpdatedAt = time.Now().UTC()
		session.mu.Unlock()
		return
	}

	agents = rotateAgentsByPreferred(agents, preferredAgentID)
	busyAgents, busyResources := m.collectBusyAgentsAndResources(session)
	startedExecution := false

	// Build dispatch candidates greedily with a provisional reservation set.
	// This lets later agents in the same tick solve with already-reserved
	// resources filtered out (e.g., agent2 can pick cnc02 instead of failing on
	// cnc01 selected by agent1).
	provisionalBusyResources := cloneBoolMap(busyResources)
	if provisionalBusyResources == nil {
		provisionalBusyResources = map[string]bool{}
	}
	provisionalBusyAgents := cloneBoolMap(busyAgents)
	if provisionalBusyAgents == nil {
		provisionalBusyAgents = map[string]bool{}
	}
	selectedCandidates := make([]realtimeDispatchCandidate, 0, len(agents))

	for agentOrder, agent := range agents {
		agentID := strings.TrimSpace(agent.ID)
		if agentID == "" || provisionalBusyAgents[agentID] {
			continue
		}
		if !isRealtimeAgentIdle(currentState, agent) {
			continue
		}
		candidate := m.buildDispatchCandidateForAgent(
			session,
			agent,
			agentOrder,
			currentState,
			goals,
			resources,
			behaviorTreeIDs,
			taskDistributorID,
			provisionalBusyResources,
		)
		if candidate == nil {
			continue
		}
		selectedCandidates = append(selectedCandidates, *candidate)
		provisionalBusyAgents[agentID] = true
		if resourceID := strings.TrimSpace(candidate.Binding.ResourceID); resourceID != "" {
			provisionalBusyResources[resourceID] = true
		}
		if resourceName := strings.TrimSpace(candidate.Binding.ResourceName); resourceName != "" {
			provisionalBusyResources[resourceName] = true
		}
		markBusyAgentsAndResourcesFromPlan(candidate.Plan, provisionalBusyAgents, provisionalBusyResources)
	}

	for _, candidate := range selectedCandidates {
		executionProblemID := fmt.Sprintf(
			"realtime:%s:%s:%d",
			session.ID,
			candidate.AgentID,
			time.Now().UnixNano(),
		)
		executionID, err := m.server.planExecutor.StartRuntimePlanExecution(
			session.ctx,
			executionProblemID,
			behaviorTreeIDs,
			taskDistributorID,
			currentState,
			candidate.Plan,
		)
		if err != nil {
			recordRealtimeGoalFailure(
				session,
				candidate.Goal,
				candidate.Binding,
				candidate.FailureKey,
				err.Error(),
				candidate.Plan,
			)
			continue
		}

		now := time.Now().UTC()
		session.mu.Lock()
		delete(session.failedStateKey, candidate.Goal.ID)
		if session.ActiveExecutions == nil {
			session.ActiveExecutions = map[string]realtimeExecutionContext{}
		}
		session.ActiveExecutions[executionID] = realtimeExecutionContext{
			ExecutionID: executionID,
			Plan:        clonePlan(candidate.Plan),
			Goal:        cloneRealtimeGoalPtr(candidate.Goal),
			Binding: realtimeGoalBinding{
				AgentID:      candidate.Binding.AgentID,
				AgentName:    candidate.Binding.AgentName,
				ResourceID:   candidate.Binding.ResourceID,
				ResourceName: candidate.Binding.ResourceName,
			},
			StartedAt: now,
		}
		session.ActiveExecutionID = executionID
		session.ActivePlan = clonePlan(candidate.Plan)
		session.ActiveGoal = cloneRealtimeGoalPtr(candidate.Goal)
		session.ActiveStatus = "running"
		session.SelectedGoalID = candidate.Goal.ID
		session.SelectedGoalName = candidate.Goal.Name
		session.SelectedAgentID = candidate.Binding.AgentID
		session.SelectedAgentName = candidate.Binding.AgentName
		session.SelectedResourceID = candidate.Binding.ResourceID
		session.SelectedResourceName = candidate.Binding.ResourceName
		if strings.TrimSpace(candidate.Binding.AgentID) != "" {
			session.LastSelectedAgentID = strings.TrimSpace(candidate.Binding.AgentID)
		}
		session.LastError = ""
		session.LastPlan = clonePlan(candidate.Plan)
		session.UpdatedAt = now
		session.mu.Unlock()

		busyAgents[candidate.AgentID] = true
		if resourceID := strings.TrimSpace(candidate.Binding.ResourceID); resourceID != "" {
			busyResources[resourceID] = true
		}
		if resourceName := strings.TrimSpace(candidate.Binding.ResourceName); resourceName != "" {
			busyResources[resourceName] = true
		}
		markBusyAgentsAndResourcesFromPlan(candidate.Plan, busyAgents, busyResources)
		startedExecution = true
	}

	session.mu.Lock()
	if len(session.ActiveExecutions) == 0 && !startedExecution {
		session.SelectedGoalID = ""
		session.SelectedGoalName = ""
		session.SelectedAgentID = ""
		session.SelectedAgentName = ""
		session.SelectedResourceID = ""
		session.SelectedResourceName = ""
		session.ActiveStatus = "idle"
		session.ActiveExecutionID = ""
		session.ActivePlan = nil
		session.ActiveGoal = nil
	} else if len(session.ActiveExecutions) > 0 && strings.TrimSpace(session.ActiveStatus) == "" {
		session.ActiveStatus = "running"
	}
	session.UpdatedAt = time.Now().UTC()
	session.mu.Unlock()
}

func (m *RealtimePddlManager) buildDispatchCandidateForAgent(
	session *realtimeSession,
	agent pddl.AgentInfo,
	agentOrder int,
	currentState map[string]string,
	goals []realtimeGoal,
	resources []pddl.ResourceInfo,
	behaviorTreeIDs []string,
	taskDistributorID string,
	busyResources map[string]bool,
) *realtimeDispatchCandidate {
	if session == nil {
		return nil
	}
	agentID := strings.TrimSpace(agent.ID)
	if agentID == "" {
		return nil
	}

	availableResources := filterRealtimeAvailableResources(resources, busyResources)
	availableResources = filterRealtimeAvailableResourcesByReservation(currentState, availableResources, agent)
	for goalIdx := range goals {
		goalCandidate := goals[goalIdx]
		if !goalCandidate.Enabled {
			continue
		}

		met, selectedBinding := realtimeActivationConditionsMet(
			currentState,
			goalCandidate.ActivationConditions,
			goalCandidate.GoalState,
			goalCandidate.ResourceTypeID,
			[]pddl.AgentInfo{agent},
			availableResources,
			agentID,
		)
		if !met {
			continue
		}
		if realtimeGoalSatisfied(currentState, applyRealtimeGoalBinding(goalCandidate.GoalState, selectedBinding)) {
			continue
		}

		selectedGoal := cloneRealtimeGoalPtr(&goalCandidate)
		if selectedGoal == nil {
			continue
		}

		failureKey := goalFailureKey(selectedGoal.ID, currentState, selectedBinding)
		session.mu.RLock()
		sameFailureState := session.failedStateKey[selectedGoal.ID] == failureKey
		session.mu.RUnlock()
		if sameFailureState {
			continue
		}

		goalStateForSolve := applyRealtimeGoalBinding(selectedGoal.GoalState, selectedBinding)
		plan, err := m.server.solveProblemSpec(planningSolveSpec{
			BehaviorTreeIDs:   behaviorTreeIDs,
			TaskDistributorID: taskDistributorID,
			InitialState:      currentState,
			GoalState:         goalStateForSolve,
			GoalBinding:       selectedBinding,
			AgentIDs:          []string{agentID},
		})
		if err != nil {
			recordRealtimeGoalFailure(session, selectedGoal, selectedBinding, failureKey, err.Error(), nil)
			continue
		}
		if plan == nil || !plan.IsValid {
			errMsg := "plan is invalid"
			if plan != nil && strings.TrimSpace(plan.ErrorMessage) != "" {
				errMsg = plan.ErrorMessage
			}
			recordRealtimeGoalFailure(session, selectedGoal, selectedBinding, failureKey, errMsg, plan)
			continue
		}
		if len(plan.Assignments) == 0 {
			recordRealtimeGoalFailure(session, selectedGoal, selectedBinding, failureKey, "plan has no assignments", plan)
			continue
		}

		conflicts := planBusyResourceConflicts(plan, busyResources)
		if len(conflicts) > 0 {
			// Busy-resource conflict is transient. Do not cache as failedStateKey,
			// so the goal can be retried once resources are released.
			session.mu.Lock()
			session.LastError = fmt.Sprintf(
				"skip goal %s for agent %s: busy resources %v",
				selectedGoal.ID,
				agentID,
				conflicts,
			)
			session.UpdatedAt = time.Now().UTC()
			session.mu.Unlock()
			continue
		}

		return &realtimeDispatchCandidate{
			AgentID:    agentID,
			AgentOrder: agentOrder,
			Goal:       selectedGoal,
			Binding: realtimeGoalBinding{
				AgentID:      selectedBinding.AgentID,
				AgentName:    selectedBinding.AgentName,
				ResourceID:   selectedBinding.ResourceID,
				ResourceName: selectedBinding.ResourceName,
			},
			Plan:       clonePlan(plan),
			FailureKey: failureKey,
		}
	}

	return nil
}

func selectRealtimeDispatchCandidates(
	candidates []realtimeDispatchCandidate,
	busyResources map[string]bool,
) []realtimeDispatchCandidate {
	if len(candidates) == 0 {
		return nil
	}

	ordered := make([]realtimeDispatchCandidate, len(candidates))
	copy(ordered, candidates)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := 0
		right := 0
		if ordered[i].Goal != nil {
			left = ordered[i].Goal.Priority
		}
		if ordered[j].Goal != nil {
			right = ordered[j].Goal.Priority
		}
		if left != right {
			return left < right
		}
		return ordered[i].AgentOrder < ordered[j].AgentOrder
	})

	reservedResources := cloneBoolMap(busyResources)
	if reservedResources == nil {
		reservedResources = map[string]bool{}
	}
	selectedAgents := map[string]bool{}
	dummyBusyAgents := map[string]bool{}
	selected := make([]realtimeDispatchCandidate, 0, len(ordered))

	for _, candidate := range ordered {
		agentID := strings.TrimSpace(candidate.AgentID)
		if agentID == "" || selectedAgents[agentID] {
			continue
		}
		if candidate.Plan == nil || candidate.Goal == nil {
			continue
		}

		if conflicts := planBusyResourceConflicts(candidate.Plan, reservedResources); len(conflicts) > 0 {
			continue
		}
		resourceID := strings.TrimSpace(candidate.Binding.ResourceID)
		resourceName := strings.TrimSpace(candidate.Binding.ResourceName)
		if (resourceID != "" && reservedResources[resourceID]) || (resourceName != "" && reservedResources[resourceName]) {
			continue
		}

		selected = append(selected, candidate)
		selectedAgents[agentID] = true
		if resourceID != "" {
			reservedResources[resourceID] = true
		}
		if resourceName != "" {
			reservedResources[resourceName] = true
		}
		markBusyAgentsAndResourcesFromPlan(candidate.Plan, dummyBusyAgents, reservedResources)
	}

	return selected
}

func (m *RealtimePddlManager) syncExecutions(session *realtimeSession) bool {
	session.mu.RLock()
	activeExecutions := make([]realtimeExecutionContext, 0, len(session.ActiveExecutions))
	for _, executionCtx := range session.ActiveExecutions {
		activeExecutions = append(activeExecutions, cloneRealtimeExecutionContext(executionCtx))
	}
	session.mu.RUnlock()

	if len(activeExecutions) == 0 {
		return false
	}

	sort.Slice(activeExecutions, func(i, j int) bool {
		if activeExecutions[i].StartedAt.Equal(activeExecutions[j].StartedAt) {
			return activeExecutions[i].ExecutionID < activeExecutions[j].ExecutionID
		}
		return activeExecutions[i].StartedAt.Before(activeExecutions[j].StartedAt)
	})

	nextState := sessionCurrentState(session)
	if nextState == nil {
		nextState = map[string]string{}
	}

	remainingExecutions := make(map[string]realtimeExecutionContext, len(activeExecutions))
	completedGoalIDs := map[string]bool{}
	runningExecutionID := ""
	runningExecutionStatus := ""
	var runningPlan *pddl.Plan
	var runningGoal *realtimeGoal
	runningBinding := realtimeGoalBinding{}
	lastError := ""

	for _, executionCtx := range activeExecutions {
		executionID := strings.TrimSpace(executionCtx.ExecutionID)
		if executionID == "" {
			continue
		}
		execState, ok := m.server.planExecutor.GetExecution(executionID)
		if !ok {
			continue
		}

		snapshot := execState.Snapshot()
		switch executor.PlanExecutionStatus(snapshot.Status) {
		case executor.PlanExecPending, executor.PlanExecRunning:
			remainingExecutions[executionID] = executionCtx
			if runningExecutionID == "" {
				runningExecutionID = executionID
				runningExecutionStatus = snapshot.Status
				runningPlan = clonePlan(executionCtx.Plan)
				runningGoal = cloneRealtimeGoalPtr(executionCtx.Goal)
				runningBinding = executionCtx.Binding
			}
		case executor.PlanExecCompleted:
			if executionCtx.Plan != nil {
				applyRealtimeExecutionEffects(nextState, executionCtx.Plan, snapshot.Steps)
			}
			if executionCtx.Goal != nil {
				completedGoalIDs[executionCtx.Goal.ID] = true
			}
		case executor.PlanExecFailed, executor.PlanExecCancelled:
			applyRealtimePartialEffects(nextState, executionCtx.Plan, snapshot.Steps)
			if strings.TrimSpace(snapshot.Error) != "" {
				lastError = snapshot.Error
			}
		default:
			if strings.TrimSpace(snapshot.Error) != "" {
				lastError = snapshot.Error
			}
		}
	}

	now := time.Now().UTC()
	session.mu.Lock()
	session.CurrentState = nextState
	session.ActiveExecutions = remainingExecutions
	if runningExecutionID != "" {
		session.ActiveExecutionID = runningExecutionID
		session.ActivePlan = runningPlan
		session.ActiveGoal = runningGoal
		session.ActiveStatus = runningExecutionStatus
		session.SelectedGoalID = ""
		session.SelectedGoalName = ""
		session.SelectedAgentID = ""
		session.SelectedAgentName = ""
		session.SelectedResourceID = ""
		session.SelectedResourceName = ""
		if runningGoal != nil {
			session.SelectedGoalID = runningGoal.ID
			session.SelectedGoalName = runningGoal.Name
		}
		session.SelectedAgentID = runningBinding.AgentID
		session.SelectedAgentName = runningBinding.AgentName
		session.SelectedResourceID = runningBinding.ResourceID
		session.SelectedResourceName = runningBinding.ResourceName
		if strings.TrimSpace(session.LastError) != "" && strings.TrimSpace(lastError) == "" {
			// keep previous last_error until a new plan starts or another error occurs
		}
	} else {
		session.ActiveExecutionID = ""
		session.ActivePlan = nil
		session.ActiveGoal = nil
		if strings.TrimSpace(lastError) == "" {
			session.ActiveStatus = "idle"
		} else {
			session.ActiveStatus = "error"
		}
	}
	if strings.TrimSpace(lastError) != "" {
		session.LastError = lastError
	}
	for goalID := range completedGoalIDs {
		delete(session.failedStateKey, goalID)
	}
	session.UpdatedAt = now
	session.mu.Unlock()

	return len(remainingExecutions) > 0
}

// applyRealtimePartialEffects applies only effects of assignments whose steps
// actually completed, even when the whole plan execution failed/cancelled.
// This keeps realtime current_state aligned with what was already executed
// (e.g., go_to_cnc_and_park completed but follow-up step failed).
func applyRealtimePartialEffects(
	state map[string]string,
	plan *pddl.Plan,
	stepSnapshots []executor.StepStatusSnapshot,
) {
	if state == nil || plan == nil || len(plan.Assignments) == 0 || len(stepSnapshots) == 0 {
		return
	}

	applyRealtimeExecutionEffects(state, plan, stepSnapshots)
}

func applyRealtimeExecutionEffects(
	state map[string]string,
	plan *pddl.Plan,
	stepSnapshots []executor.StepStatusSnapshot,
) {
	if state == nil || plan == nil || len(plan.Assignments) == 0 {
		return
	}

	stepByTaskID := make(map[string]executor.StepStatusSnapshot, len(stepSnapshots))
	for _, step := range stepSnapshots {
		taskID := strings.TrimSpace(step.TaskID)
		if taskID == "" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(step.Status))
		switch status {
		case "completed", "failed", "cancelled":
		default:
			continue
		}
		stepByTaskID[taskID] = step
	}
	if len(stepByTaskID) == 0 {
		return
	}

	for _, assignment := range plan.Assignments {
		step, ok := stepByTaskID[strings.TrimSpace(assignment.TaskID)]
		if !ok {
			continue
		}
		effects := realtimePlanningEffectsForStep(assignment, step)
		for _, effect := range effects {
			if key := strings.TrimSpace(effect.Variable); key != "" {
				state[key] = effect.Value
			}
		}
	}
}

func realtimePlanningEffectsForStep(
	assignment pddl.TaskAssignment,
	step executor.StepStatusSnapshot,
) []db.PlanningEffect {
	stepStatus := strings.ToLower(strings.TrimSpace(step.Status))
	rawMessage := strings.TrimSpace(step.Error)
	messageClass, normalizedMessage := classifyRealtimeTaskResultMessage(step.Error)
	baseEffects := cloneRealtimePlanningEffects(assignment.ResultStates)

	switch stepStatus {
	case "failed", "cancelled":
		errorEffects := append(baseEffects, cloneRealtimePlanningEffects(assignment.ErrorResultStates)...)
		message := normalizedMessage
		if strings.TrimSpace(message) == "" {
			message = rawMessage
		}
		return appendRealtimePlanningMessageEffect(
			errorEffects,
			assignment.ErrorMessageVariable,
			message,
		)
	}

	switch messageClass {
	case taskResultMessageWarning:
		warningEffects := append(baseEffects, cloneRealtimePlanningEffects(assignment.WarningResultStates)...)
		return appendRealtimePlanningMessageEffect(
			warningEffects,
			assignment.WarningMessageVariable,
			normalizedMessage,
		)
	case taskResultMessageError:
		errorEffects := append(baseEffects, cloneRealtimePlanningEffects(assignment.ErrorResultStates)...)
		return appendRealtimePlanningMessageEffect(
			errorEffects,
			assignment.ErrorMessageVariable,
			normalizedMessage,
		)
	default:
		return baseEffects
	}
}

type taskResultMessageClass string

const (
	taskResultMessageNone    taskResultMessageClass = "none"
	taskResultMessageWarning taskResultMessageClass = "warning"
	taskResultMessageError   taskResultMessageClass = "error"
)

func classifyRealtimeTaskResultMessage(message string) (taskResultMessageClass, string) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return taskResultMessageNone, ""
	}

	if len(trimmed) >= 2 {
		prefix := strings.ToLower(trimmed[:2])
		payload := strings.TrimSpace(trimmed[2:])
		switch prefix {
		case "w:":
			return taskResultMessageWarning, payload
		case "e:":
			return taskResultMessageError, payload
		}
	}

	return taskResultMessageNone, trimmed
}

func cloneRealtimePlanningEffects(values []db.PlanningEffect) []db.PlanningEffect {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]db.PlanningEffect, len(values))
	copy(cloned, values)
	return cloned
}

func appendRealtimePlanningMessageEffect(effectsInput []db.PlanningEffect, variable, message string) []db.PlanningEffect {
	effects := cloneRealtimePlanningEffects(effectsInput)
	trimmedVariable := strings.TrimSpace(variable)
	if trimmedVariable == "" {
		return effects
	}

	trimmedMessage := strings.TrimSpace(message)
	for i := range effects {
		if strings.TrimSpace(effects[i].Variable) == trimmedVariable {
			effects[i].Value = trimmedMessage
			return effects
		}
	}

	effects = append(effects, db.PlanningEffect{
		Variable: trimmedVariable,
		Value:    trimmedMessage,
	})
	return effects
}

func (m *RealtimePddlManager) collectBusyAgentsAndResources(session *realtimeSession) (map[string]bool, map[string]bool) {
	busyAgents := map[string]bool{}
	busyResources := map[string]bool{}
	if session == nil {
		return busyAgents, busyResources
	}

	session.mu.RLock()
	activeExecutions := make([]realtimeExecutionContext, 0, len(session.ActiveExecutions))
	for _, executionCtx := range session.ActiveExecutions {
		activeExecutions = append(activeExecutions, cloneRealtimeExecutionContext(executionCtx))
	}
	session.mu.RUnlock()

	for _, executionCtx := range activeExecutions {
		executionID := strings.TrimSpace(executionCtx.ExecutionID)
		if executionID == "" {
			continue
		}

		execState, ok := m.server.planExecutor.GetExecution(executionID)
		if !ok {
			continue
		}
		snapshot := execState.Snapshot()
		switch executor.PlanExecutionStatus(snapshot.Status) {
		case executor.PlanExecPending, executor.PlanExecRunning:
		default:
			continue
		}

		markBusyAgentsAndResourcesFromPlan(executionCtx.Plan, busyAgents, busyResources)
		if agentID := strings.TrimSpace(executionCtx.Binding.AgentID); agentID != "" {
			busyAgents[agentID] = true
		}
		if resourceID := strings.TrimSpace(executionCtx.Binding.ResourceID); resourceID != "" {
			busyResources[resourceID] = true
		}
		if resourceName := strings.TrimSpace(executionCtx.Binding.ResourceName); resourceName != "" {
			busyResources[resourceName] = true
		}
	}

	return busyAgents, busyResources
}

func markBusyAgentsAndResourcesFromPlan(plan *pddl.Plan, busyAgents map[string]bool, busyResources map[string]bool) {
	if plan == nil {
		return
	}
	for _, assignment := range plan.Assignments {
		if agentID := strings.TrimSpace(assignment.AgentID); agentID != "" {
			busyAgents[agentID] = true
		}

		if len(assignment.RuntimeParams) == 0 {
			continue
		}
		addBusyResourceValue(busyResources, assignment.RuntimeParams["resource_id"])
		addBusyResourceValue(busyResources, assignment.RuntimeParams["resource_name"])
		addBusyResourceValue(busyResources, assignment.RuntimeParams["resource.id"])
		addBusyResourceValue(busyResources, assignment.RuntimeParams["resource.name"])

		if rawBindings := strings.TrimSpace(assignment.RuntimeParams["__fleet_resource_bindings"]); rawBindings != "" {
			var bindings map[string]string
			if err := json.Unmarshal([]byte(rawBindings), &bindings); err == nil {
				for _, value := range bindings {
					addBusyResourceValue(busyResources, value)
				}
			}
		}
	}
}

func addBusyResourceValue(busyResources map[string]bool, value string) {
	key := strings.TrimSpace(value)
	if key == "" {
		return
	}
	busyResources[key] = true
}

func planBusyResourceConflicts(plan *pddl.Plan, busyResources map[string]bool) []string {
	if plan == nil || len(plan.Assignments) == 0 || len(busyResources) == 0 {
		return nil
	}

	conflicts := map[string]bool{}
	checkValue := func(value string) {
		key := strings.TrimSpace(value)
		if key == "" {
			return
		}
		if busyResources[key] {
			conflicts[key] = true
		}
	}

	for _, assignment := range plan.Assignments {
		if len(assignment.RuntimeParams) == 0 {
			continue
		}

		checkValue(assignment.RuntimeParams["resource_id"])
		checkValue(assignment.RuntimeParams["resource_name"])
		checkValue(assignment.RuntimeParams["resource.id"])
		checkValue(assignment.RuntimeParams["resource.name"])

		if rawBindings := strings.TrimSpace(assignment.RuntimeParams["__fleet_resource_bindings"]); rawBindings != "" {
			var bindings map[string]string
			if err := json.Unmarshal([]byte(rawBindings), &bindings); err == nil {
				for _, value := range bindings {
					checkValue(value)
				}
			}
		}
	}

	if len(conflicts) == 0 {
		return nil
	}

	result := make([]string, 0, len(conflicts))
	for key := range conflicts {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func cloneRealtimeExecutionContext(ctx realtimeExecutionContext) realtimeExecutionContext {
	cloned := ctx
	cloned.Plan = clonePlan(ctx.Plan)
	cloned.Goal = cloneRealtimeGoalPtr(ctx.Goal)
	cloned.Binding = realtimeGoalBinding{
		AgentID:      ctx.Binding.AgentID,
		AgentName:    ctx.Binding.AgentName,
		ResourceID:   ctx.Binding.ResourceID,
		ResourceName: ctx.Binding.ResourceName,
	}
	return cloned
}

func isRealtimeAgentIdle(current map[string]string, agent pddl.AgentInfo) bool {
	if len(current) == 0 {
		return true
	}
	agentID := strings.TrimSpace(agent.ID)
	agentName := strings.TrimSpace(agent.Name)

	// 1) Prefer explicit execution flags when available.
	// This avoids stale/non-critical mode=status overlays (e.g., "error") from
	// unnecessarily excluding an agent that is actually idle.
	execCandidates := uniqueNonEmptyStrings([]string{
		agentName + "_is_executing",
		agentID + "_is_executing",
		strings.ReplaceAll(agentID, "-", "_") + "_is_executing",
	})
	foundExec := false
	for _, key := range execCandidates {
		raw, ok := current[key]
		if !ok {
			continue
		}
		foundExec = true
		normalized := strings.ToLower(strings.TrimSpace(raw))
		if normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "executing" || normalized == "running" {
			return false
		}
	}
	if foundExec {
		return true
	}

	// 2) Fallback to status/mode keys.
	// Use precedence so mixed aliases do not over-constrain idle checks:
	// agent-name keys > normalized-agent-id keys > raw-agent-id keys.
	type candidateGroup struct {
		keys []string
	}
	groups := []candidateGroup{
		{keys: uniqueNonEmptyStrings([]string{agentName + "_status", agentName + "_mode"})},
		{keys: uniqueNonEmptyStrings([]string{
			strings.ReplaceAll(agentID, "-", "_") + "_status",
			strings.ReplaceAll(agentID, "-", "_") + "_mode",
		})},
		{keys: uniqueNonEmptyStrings([]string{agentID + "_status", agentID + "_mode"})},
	}
	for _, group := range groups {
		foundAny := false
		allIdleLike := true
		for _, key := range group.keys {
			value, ok := current[key]
			if !ok {
				continue
			}
			foundAny = true
			normalized := strings.ToLower(strings.TrimSpace(value))
			idleLike := normalized == "" || normalized == "idle" || normalized == "ready"
			if !idleLike {
				allIdleLike = false
			}
		}
		if foundAny {
			return allIdleLike
		}
	}
	return true
}

func agentLocationFromState(current map[string]string, agent pddl.AgentInfo) string {
	if len(current) == 0 {
		return ""
	}
	agentID := strings.TrimSpace(agent.ID)
	agentName := strings.TrimSpace(agent.Name)
	candidates := uniqueNonEmptyStrings([]string{
		agentName + "_location",
		agentID + "_location",
		strings.ReplaceAll(agentID, "-", "_") + "_location",
	})
	for _, key := range candidates {
		if value, ok := current[key]; ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func agentLocationStateKey(current map[string]string, agent pddl.AgentInfo) string {
	if len(current) == 0 {
		return ""
	}
	agentID := strings.TrimSpace(agent.ID)
	agentName := strings.TrimSpace(agent.Name)
	candidates := uniqueNonEmptyStrings([]string{
		agentName + "_location",
		agentID + "_location",
		strings.ReplaceAll(agentID, "-", "_") + "_location",
	})
	for _, key := range candidates {
		if _, ok := current[key]; ok {
			return key
		}
	}
	return ""
}

func inferGoalTargetResourceName(goal *realtimeGoal, binding realtimeGoalBinding, resources []pddl.ResourceInfo) string {
	if goal == nil {
		return ""
	}
	if name := strings.TrimSpace(binding.ResourceName); name != "" {
		return name
	}
	if resourceID := strings.TrimSpace(binding.ResourceID); resourceID != "" {
		for _, resource := range resources {
			if strings.TrimSpace(resource.ID) == resourceID {
				return strings.TrimSpace(resource.Name)
			}
		}
	}

	instanceNames := make([]string, 0, len(resources))
	for _, resource := range resources {
		if strings.EqualFold(strings.TrimSpace(resource.Kind), "type") {
			continue
		}
		name := strings.TrimSpace(resource.Name)
		if name == "" {
			continue
		}
		instanceNames = append(instanceNames, name)
	}
	if len(instanceNames) == 0 {
		return ""
	}

	matches := map[string]bool{}
	for variable := range goal.GoalState {
		key := strings.TrimSpace(variable)
		if key == "" {
			continue
		}
		for _, name := range instanceNames {
			if key == name || strings.HasPrefix(key, name+"_") {
				matches[name] = true
			}
		}
	}

	if len(matches) != 1 {
		return ""
	}
	for name := range matches {
		return name
	}
	return ""
}

func hasOtherIdleAgentAtResource(
	current map[string]string,
	agents []pddl.AgentInfo,
	busyAgents map[string]bool,
	resourceName string,
	excludeAgentID string,
) bool {
	target := strings.TrimSpace(resourceName)
	if target == "" {
		return false
	}
	exclude := strings.TrimSpace(excludeAgentID)
	for _, candidate := range agents {
		candidateID := strings.TrimSpace(candidate.ID)
		if candidateID == "" || candidateID == exclude {
			continue
		}
		if busyAgents[candidateID] {
			continue
		}
		if !isRealtimeAgentIdle(current, candidate) {
			continue
		}
		location := strings.TrimSpace(agentLocationFromState(current, candidate))
		if location != "" && strings.EqualFold(location, target) {
			return true
		}
	}
	return false
}

func filterRealtimeAvailableResources(resources []pddl.ResourceInfo, busyResources map[string]bool) []pddl.ResourceInfo {
	if len(resources) == 0 {
		return nil
	}
	filtered := make([]pddl.ResourceInfo, 0, len(resources))
	for _, resource := range resources {
		kind := strings.ToLower(strings.TrimSpace(resource.Kind))
		if kind == "type" {
			filtered = append(filtered, resource)
			continue
		}
		resourceID := strings.TrimSpace(resource.ID)
		resourceName := strings.TrimSpace(resource.Name)
		if (resourceID != "" && busyResources[resourceID]) || (resourceName != "" && busyResources[resourceName]) {
			continue
		}
		filtered = append(filtered, resource)
	}
	return filtered
}

func filterRealtimeAvailableResourcesByReservation(
	current map[string]string,
	resources []pddl.ResourceInfo,
	agent pddl.AgentInfo,
) []pddl.ResourceInfo {
	if len(resources) == 0 {
		return nil
	}

	allowed := make([]pddl.ResourceInfo, 0, len(resources))
	for _, resource := range resources {
		kind := strings.ToLower(strings.TrimSpace(resource.Kind))
		if kind == "type" {
			allowed = append(allowed, resource)
			continue
		}

		stateKey := reservationStateKeyForResource(resource, current)
		if stateKey == "" {
			allowed = append(allowed, resource)
			continue
		}

		holder := normalizeReservationHolder(current[stateKey])
		if holder == "" {
			allowed = append(allowed, resource)
			continue
		}
		if reservationHolderMatchesAgent(holder, agent) {
			allowed = append(allowed, resource)
			continue
		}
	}
	return allowed
}

func normalizeRealtimeGoals(goals []RealtimeGoalRequest) []realtimeGoal {
	result := make([]realtimeGoal, 0, len(goals))
	for index, goal := range goals {
		goalID := strings.TrimSpace(goal.ID)
		if goalID == "" {
			goalID = fmt.Sprintf("goal_%d", index+1)
		}
		result = append(result, realtimeGoal{
			ID:                   goalID,
			Name:                 strings.TrimSpace(goal.Name),
			Priority:             goal.Priority,
			Enabled:              goal.Enabled,
			ActivationConditions: cloneConditions(goal.ActivationConditions),
			ResourceTypeID:       strings.TrimSpace(goal.ResourceTypeID),
			GoalState:            cloneStringMap(goal.GoalState),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority < result[j].Priority
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func goalUsesAgentPlaceholders(conditions []db.PlanningCondition, goalState map[string]string) bool {
	for _, condition := range conditions {
		if containsAgentPlaceholder(condition.Variable) || containsAgentPlaceholder(condition.Value) {
			return true
		}
	}
	for variable, value := range goalState {
		if containsAgentPlaceholder(variable) || containsAgentPlaceholder(value) {
			return true
		}
	}
	return false
}

func goalUsesResourcePlaceholders(conditions []db.PlanningCondition, goalState map[string]string) bool {
	for _, condition := range conditions {
		if containsResourcePlaceholder(condition.Variable) || containsResourcePlaceholder(condition.Value) {
			return true
		}
	}
	for variable, value := range goalState {
		if containsResourcePlaceholder(variable) || containsResourcePlaceholder(value) {
			return true
		}
	}
	return false
}

func filterRealtimeResourcesByType(resources []pddl.ResourceInfo, resourceTypeID string) []pddl.ResourceInfo {
	resourceTypeID = strings.TrimSpace(resourceTypeID)
	if resourceTypeID == "" {
		return append([]pddl.ResourceInfo{}, resources...)
	}

	filtered := make([]pddl.ResourceInfo, 0, len(resources))
	for _, resource := range resources {
		if strings.EqualFold(strings.TrimSpace(resource.Kind), "type") {
			continue
		}
		if strings.TrimSpace(resource.ParentResourceID) != resourceTypeID {
			continue
		}
		filtered = append(filtered, resource)
	}
	return filtered
}

func selectRealtimeGoal(
	goals []realtimeGoal,
	currentState map[string]string,
	agents []pddl.AgentInfo,
	resources []pddl.ResourceInfo,
	preferredAgentID string,
) (*realtimeGoal, realtimeGoalBinding) {
	for index := range goals {
		goal := goals[index]
		if !goal.Enabled {
			continue
		}
		met, binding := realtimeActivationConditionsMet(
			currentState,
			goal.ActivationConditions,
			goal.GoalState,
			goal.ResourceTypeID,
			agents,
			resources,
			preferredAgentID,
		)
		if !met {
			continue
		}
		if realtimeGoalSatisfied(currentState, applyRealtimeGoalBinding(goal.GoalState, binding)) {
			continue
		}
		return cloneRealtimeGoalPtr(&goal), binding
	}
	return nil, realtimeGoalBinding{}
}

func realtimeActivationConditionsMet(
	current map[string]string,
	conditions []db.PlanningCondition,
	goalState map[string]string,
	resourceTypeID string,
	agents []pddl.AgentInfo,
	resources []pddl.ResourceInfo,
	preferredAgentID string,
) (bool, realtimeGoalBinding) {
	needsAgent := goalUsesAgentPlaceholders(conditions, goalState)
	needsResource := goalUsesResourcePlaceholders(conditions, goalState)

	if len(conditions) == 0 && !needsAgent && !needsResource {
		return true, realtimeGoalBinding{}
	}

	if !needsAgent && !needsResource {
		return planningConditionsMet(current, conditions), realtimeGoalBinding{}
	}

	agentCandidates := []pddl.AgentInfo{{}}
	if needsAgent {
		if len(agents) == 0 {
			return false, realtimeGoalBinding{}
		}
		agentCandidates = rotateAgentsByPreferred(agents, preferredAgentID)
	}

	resourceCandidates := []pddl.ResourceInfo{{}}
	if needsResource {
		instances := filterRealtimeResourcesByType(resourceInstances(resources), resourceTypeID)
		if len(instances) == 0 {
			return false, realtimeGoalBinding{}
		}
		resourceCandidates = instances
	}

	for _, agent := range agentCandidates {
		for _, resource := range resourceCandidates {
			expanded := expandRealtimeActivationConditions(conditions, agent, resource)
			if hasUnresolvedConditionPlaceholders(expanded) {
				continue
			}
			if planningConditionsMet(current, expanded) {
				binding := realtimeGoalBinding{}
				if needsAgent {
					binding.AgentID = strings.TrimSpace(agent.ID)
					binding.AgentName = strings.TrimSpace(agent.Name)
					if binding.AgentName == "" {
						binding.AgentName = binding.AgentID
					}
				}
				if needsResource {
					binding.ResourceID = strings.TrimSpace(resource.ID)
					binding.ResourceName = strings.TrimSpace(resource.Name)
					if binding.ResourceName == "" {
						binding.ResourceName = binding.ResourceID
					}
				}
				return true, binding
			}
		}
	}

	return false, realtimeGoalBinding{}
}

func expandRealtimeActivationConditions(
	conditions []db.PlanningCondition,
	agent pddl.AgentInfo,
	resource pddl.ResourceInfo,
) []db.PlanningCondition {
	expanded := make([]db.PlanningCondition, 0, len(conditions))
	for _, condition := range conditions {
		variable := strings.TrimSpace(condition.Variable)
		value := strings.TrimSpace(condition.Value)
		if resource.ID != "" || resource.Name != "" {
			variable = applyResourcePlaceholders(variable, resource)
			value = applyResourcePlaceholders(value, resource)
		}
		if agent.ID != "" || agent.Name != "" {
			variable = applyAgentPlaceholders(variable, agent)
			value = applyAgentPlaceholders(value, agent)
		}
		expanded = append(expanded, db.PlanningCondition{
			Variable: variable,
			Operator: condition.Operator,
			Value:    value,
		})
	}
	return expanded
}

func rotateAgentsByPreferred(agents []pddl.AgentInfo, preferredAgentID string) []pddl.AgentInfo {
	if len(agents) <= 1 {
		return append([]pddl.AgentInfo{}, agents...)
	}
	preferredAgentID = strings.TrimSpace(preferredAgentID)
	if preferredAgentID == "" {
		return append([]pddl.AgentInfo{}, agents...)
	}

	pivot := -1
	for index, agent := range agents {
		if strings.TrimSpace(agent.ID) == preferredAgentID {
			pivot = index
			break
		}
	}
	if pivot < 0 {
		return append([]pddl.AgentInfo{}, agents...)
	}

	rotated := make([]pddl.AgentInfo, 0, len(agents))
	for offset := 1; offset <= len(agents); offset++ {
		rotated = append(rotated, agents[(pivot+offset)%len(agents)])
	}
	return rotated
}

func hasUnresolvedConditionPlaceholders(conditions []db.PlanningCondition) bool {
	for _, condition := range conditions {
		if containsAgentPlaceholder(condition.Variable) || containsAgentPlaceholder(condition.Value) {
			return true
		}
		if containsResourcePlaceholder(condition.Variable) || containsResourcePlaceholder(condition.Value) {
			return true
		}
	}
	return false
}

func applyRealtimeGoalBinding(goalState map[string]string, binding realtimeGoalBinding) map[string]string {
	bound := cloneStringMap(goalState)
	if len(bound) == 0 {
		return bound
	}

	agent := pddl.AgentInfo{
		ID:   strings.TrimSpace(binding.AgentID),
		Name: strings.TrimSpace(binding.AgentName),
	}
	resource := pddl.ResourceInfo{
		ID:   strings.TrimSpace(binding.ResourceID),
		Name: strings.TrimSpace(binding.ResourceName),
	}

	result := make(map[string]string, len(bound))
	for variable, value := range bound {
		nextKey := strings.TrimSpace(variable)
		nextValue := strings.TrimSpace(value)

		if agent.ID != "" || agent.Name != "" {
			nextKey = applyAgentPlaceholders(nextKey, agent)
			nextValue = applyAgentPlaceholders(nextValue, agent)
		}
		if resource.ID != "" || resource.Name != "" {
			nextKey = applyResourcePlaceholders(nextKey, resource)
			nextValue = applyResourcePlaceholders(nextValue, resource)
		}

		result[nextKey] = nextValue
	}

	return result
}

func planningConditionsMet(current map[string]string, conditions []db.PlanningCondition) bool {
	for _, condition := range conditions {
		key := strings.TrimSpace(condition.Variable)
		if key == "" {
			continue
		}
		operator := strings.TrimSpace(condition.Operator)
		if operator == "" {
			operator = "=="
		}
		currentValue := normalizePlanningConditionCurrentValue(current, key, condition.Value)
		switch operator {
		case "!=":
			if currentValue == condition.Value {
				return false
			}
		default:
			if currentValue != condition.Value {
				return false
			}
		}
	}
	return true
}

// normalizePlanningConditionCurrentValue applies a small realtime-specific
// normalization to avoid stale agent status/mode overlays starving dispatch.
//
// If a condition expects "..._status == idle|ready" (or "..._mode == idle|ready")
// but telemetry still reports stale "error/warning" while "..._is_executing=false",
// treat it as idle for condition matching.
func normalizePlanningConditionCurrentValue(current map[string]string, key, expected string) string {
	currentValue := current[key]
	normalizedExpected := strings.ToLower(strings.TrimSpace(expected))
	if normalizedExpected != "idle" && normalizedExpected != "ready" {
		return currentValue
	}

	lowerKey := strings.ToLower(strings.TrimSpace(key))
	if !(strings.HasSuffix(lowerKey, "_status") || strings.HasSuffix(lowerKey, "_mode")) {
		return currentValue
	}

	prefix := strings.TrimSpace(key[:len(key)-len("_status")])
	if strings.HasSuffix(lowerKey, "_mode") {
		prefix = strings.TrimSpace(key[:len(key)-len("_mode")])
	}
	if prefix == "" {
		return currentValue
	}

	execKey := prefix + "_is_executing"
	execRaw, ok := current[execKey]
	if !ok {
		return currentValue
	}

	execNormalized := strings.ToLower(strings.TrimSpace(execRaw))
	isExecuting := execNormalized == "true" || execNormalized == "1" || execNormalized == "yes" || execNormalized == "running" || execNormalized == "executing"
	if isExecuting {
		return currentValue
	}

	valueNormalized := strings.ToLower(strings.TrimSpace(currentValue))
	if valueNormalized == "error" || valueNormalized == "warning" {
		return "idle"
	}
	return currentValue
}

func realtimeGoalSatisfied(current map[string]string, goal map[string]string) bool {
	for key, value := range goal {
		if current[key] != value {
			return false
		}
	}
	return true
}

func sessionCurrentState(session *realtimeSession) map[string]string {
	session.mu.RLock()
	defer session.mu.RUnlock()
	return cloneStringMap(session.CurrentState)
}

func (m *RealtimePddlManager) sessionLiveState(session *realtimeSession) map[string]string {
	if session == nil {
		return nil
	}
	now := time.Now().UTC()
	session.mu.Lock()
	defer session.mu.Unlock()

	live := map[string]string{}
	for source, runtime := range session.RuntimeStateSources {
		if runtime.ExpiresAt != nil && now.After(*runtime.ExpiresAt) {
			delete(session.RuntimeStateSources, source)
			continue
		}
		for key, value := range runtime.Values {
			live[key] = value
		}
	}
	if len(live) == 0 {
		return nil
	}
	return live
}

func (m *RealtimePddlManager) sessionMergedState(session *realtimeSession) map[string]string {
	if session == nil {
		return nil
	}
	now := time.Now().UTC()
	session.mu.Lock()
	defer session.mu.Unlock()

	merged := cloneStringMap(session.CurrentState)
	if merged == nil {
		merged = map[string]string{}
	}

	for source, runtime := range session.RuntimeStateSources {
		if runtime.ExpiresAt != nil && now.After(*runtime.ExpiresAt) {
			delete(session.RuntimeStateSources, source)
			continue
		}
		for key, value := range runtime.Values {
			merged[key] = value
		}
	}

	if reconcileRealtimeReservationState(merged, session.CurrentState, session.Agents, session.Resources) {
		for key, value := range merged {
			if _, exists := session.CurrentState[key]; exists {
				session.CurrentState[key] = value
			}
		}
	}

	return merged
}

func reconcileRealtimeReservationState(
	merged map[string]string,
	current map[string]string,
	agents []pddl.AgentInfo,
	resources []pddl.ResourceInfo,
) bool {
	if len(merged) == 0 || len(current) == 0 || len(agents) == 0 || len(resources) == 0 {
		return false
	}

	type agentAlias struct {
		Agent     pddl.AgentInfo
		Canonical string
	}

	aliases := make([]agentAlias, 0, len(agents))
	byToken := map[string]agentAlias{}
	for _, agent := range agents {
		agentID := strings.TrimSpace(agent.ID)
		if agentID == "" {
			continue
		}
		canonical := strings.TrimSpace(agent.Name)
		if canonical == "" {
			canonical = agentID
		}
		alias := agentAlias{
			Agent:     agent,
			Canonical: canonical,
		}
		aliases = append(aliases, alias)

		for _, token := range reservationHolderTokens(agent) {
			byToken[token] = alias
		}
	}
	if len(aliases) == 0 {
		return false
	}

	changed := false
	locationByCanonical := map[string]string{}
	for _, alias := range aliases {
		mergedLocation := strings.TrimSpace(agentLocationFromState(merged, alias.Agent))
		currentLocation := strings.TrimSpace(agentLocationFromState(current, alias.Agent))
		if (mergedLocation == "" || strings.EqualFold(mergedLocation, "none")) &&
			currentLocation != "" &&
			!strings.EqualFold(currentLocation, "none") {
			mergedLocation = currentLocation
			if key := agentLocationStateKey(merged, alias.Agent); key != "" {
				merged[key] = currentLocation
				changed = true
			}
		}
		locationByCanonical[alias.Canonical] = mergedLocation
	}

	for _, resource := range resources {
		if strings.EqualFold(strings.TrimSpace(resource.Kind), "type") {
			continue
		}

		resourceName := strings.TrimSpace(resource.Name)
		if resourceName == "" {
			continue
		}
		stateKey := reservationStateKeyForResource(resource, current)
		if stateKey == "" {
			continue
		}

		holderRaw := strings.TrimSpace(merged[stateKey])
		holder := normalizeReservationHolder(holderRaw)

		// 1) Release stale reservation when holder moved away.
		if holder != "" {
			if alias, ok := byToken[holder]; ok {
				holderLocation := strings.TrimSpace(locationByCanonical[alias.Canonical])
				if holderLocation != "" && !strings.EqualFold(holderLocation, "none") && !strings.EqualFold(holderLocation, resourceName) {
					merged[stateKey] = "none"
					holder = ""
					holderRaw = "none"
					changed = true
				} else if !strings.EqualFold(holderRaw, alias.Canonical) {
					merged[stateKey] = alias.Canonical
					holderRaw = alias.Canonical
					changed = true
				}
			}
		}

		// 2) If not reserved, auto-claim when exactly one agent is currently there.
		if holder == "" {
			candidates := make([]string, 0, 2)
			for canonical, location := range locationByCanonical {
				if strings.EqualFold(strings.TrimSpace(location), resourceName) {
					candidates = append(candidates, canonical)
				}
			}
			if len(candidates) == 1 {
				if !strings.EqualFold(holderRaw, candidates[0]) {
					merged[stateKey] = candidates[0]
					changed = true
				}
			}
		}
	}

	return changed
}

func reservationStateKeyForResource(resource pddl.ResourceInfo, current map[string]string) string {
	candidates := uniqueNonEmptyStrings([]string{
		strings.TrimSpace(resource.Name) + "_reserved_by",
		strings.TrimSpace(resource.ID) + "_reserved_by",
	})
	for _, candidate := range candidates {
		if _, ok := current[candidate]; ok {
			return candidate
		}
	}
	return ""
}

func normalizeReservationHolder(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "none", "null", "nil", "-", "na", "n/a", "false":
		return ""
	default:
		return normalized
	}
}

func reservationHolderMatchesAgent(holder string, agent pddl.AgentInfo) bool {
	holder = normalizeReservationHolder(holder)
	if holder == "" {
		return true
	}
	for _, token := range reservationHolderTokens(agent) {
		if token == holder {
			return true
		}
	}
	return false
}

func reservationHolderTokens(agent pddl.AgentInfo) []string {
	agentID := strings.TrimSpace(agent.ID)
	agentName := strings.TrimSpace(agent.Name)
	if agentName == "" {
		agentName = agentID
	}
	normalizedID := strings.ReplaceAll(agentID, "-", "_")
	return uniqueNonEmptyStrings([]string{
		strings.ToLower(agentID),
		strings.ToLower(agentName),
		strings.ToLower(normalizedID),
		strings.ToLower(strings.ReplaceAll(agentName, "-", "_")),
	})
}

func normalizeRuntimeStateValues(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		normalized[trimmedKey] = strings.TrimSpace(value)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func sessionContainsAgent(session *realtimeSession, agentID string) bool {
	if session == nil || agentID == "" {
		return false
	}
	for _, id := range session.AgentIDs {
		if strings.TrimSpace(id) == agentID {
			return true
		}
	}
	return false
}

func goalFailureKey(goalID string, state map[string]string, binding realtimeGoalBinding) string {
	bindingRaw, _ := json.Marshal(binding)
	raw, _ := json.Marshal(state)
	return goalID + "::" + string(bindingRaw) + "::" + string(raw)
}

func recordRealtimeGoalFailure(session *realtimeSession, goal *realtimeGoal, binding realtimeGoalBinding, failureKey, errMsg string, plan *pddl.Plan) {
	if session == nil || goal == nil {
		return
	}
	session.mu.Lock()
	session.failedStateKey[goal.ID] = failureKey
	session.SelectedGoalID = goal.ID
	session.SelectedGoalName = goal.Name
	session.SelectedAgentID = binding.AgentID
	session.SelectedAgentName = binding.AgentName
	session.SelectedResourceID = binding.ResourceID
	session.SelectedResourceName = binding.ResourceName
	session.ActiveStatus = "error"
	session.LastError = errMsg
	session.LastPlan = clonePlan(plan)
	session.UpdatedAt = time.Now().UTC()
	session.mu.Unlock()
}

func cloneConditions(values []db.PlanningCondition) []db.PlanningCondition {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]db.PlanningCondition, 0, len(values))
	for _, value := range values {
		cloned = append(cloned, db.PlanningCondition{
			Variable: value.Variable,
			Operator: value.Operator,
			Value:    value.Value,
		})
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneBoolMap(values map[string]bool) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]bool, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}

func clonePlan(plan *pddl.Plan) *pddl.Plan {
	if plan == nil {
		return nil
	}
	cloned := &pddl.Plan{
		IsValid:        plan.IsValid,
		ErrorMessage:   plan.ErrorMessage,
		TotalTasks:     plan.TotalTasks,
		TotalSteps:     plan.TotalSteps,
		ParallelGroups: plan.ParallelGroups,
	}
	if len(plan.Assignments) > 0 {
		cloned.Assignments = make([]pddl.TaskAssignment, 0, len(plan.Assignments))
		for _, assignment := range plan.Assignments {
			next := assignment
			next.RuntimeParams = cloneStringMap(assignment.RuntimeParams)
			if len(assignment.ResultStates) > 0 {
				next.ResultStates = make([]db.PlanningEffect, 0, len(assignment.ResultStates))
				for _, effect := range assignment.ResultStates {
					next.ResultStates = append(next.ResultStates, db.PlanningEffect{
						Variable: effect.Variable,
						Value:    effect.Value,
					})
				}
			}
			cloned.Assignments = append(cloned.Assignments, next)
		}
	}
	return cloned
}

func cloneRealtimeGoalPtr(goal *realtimeGoal) *realtimeGoal {
	if goal == nil {
		return nil
	}
	cloned := *goal
	cloned.ActivationConditions = cloneConditions(goal.ActivationConditions)
	cloned.GoalState = cloneStringMap(goal.GoalState)
	return &cloned
}

func (m *RealtimePddlManager) toResponse(session *realtimeSession) RealtimeSessionResponse {
	session.mu.RLock()
	id := session.ID
	name := session.Name
	status := session.ActiveStatus
	behaviorTreeIDs := append([]string{}, session.BehaviorTreeIDs...)
	taskDistributorID := session.TaskDistributorID
	agentIDs := append([]string{}, session.AgentIDs...)
	tickIntervalSec := session.TickInterval.Seconds()
	selectedGoalID := session.SelectedGoalID
	selectedGoalName := session.SelectedGoalName
	selectedAgentID := session.SelectedAgentID
	selectedAgentName := session.SelectedAgentName
	selectedResourceID := session.SelectedResourceID
	selectedResourceName := session.SelectedResourceName
	activeExecutionID := session.ActiveExecutionID
	activeExecutionIDs := make([]string, 0, len(session.ActiveExecutions))
	for executionID := range session.ActiveExecutions {
		activeExecutionIDs = append(activeExecutionIDs, executionID)
	}
	sort.Strings(activeExecutionIDs)
	activeExecutionStatus := session.ActiveStatus
	lastError := session.LastError
	lastPlan := clonePlan(session.LastPlan)
	startedAt := session.StartedAt
	updatedAt := session.UpdatedAt
	goals := make([]realtimeGoal, 0, len(session.Goals))
	for _, goal := range session.Goals {
		goals = append(goals, realtimeGoal{
			ID:                   goal.ID,
			Name:                 goal.Name,
			Priority:             goal.Priority,
			Enabled:              goal.Enabled,
			ActivationConditions: cloneConditions(goal.ActivationConditions),
			ResourceTypeID:       goal.ResourceTypeID,
			GoalState:            cloneStringMap(goal.GoalState),
		})
	}
	session.mu.RUnlock()

	if strings.TrimSpace(activeExecutionID) == "" && len(activeExecutionIDs) > 0 {
		activeExecutionID = activeExecutionIDs[0]
	}

	response := RealtimeSessionResponse{
		ID:                    id,
		Name:                  name,
		Status:                status,
		BehaviorTreeIDs:       behaviorTreeIDs,
		TaskDistributorID:     taskDistributorID,
		AgentIDs:              agentIDs,
		TickIntervalSec:       tickIntervalSec,
		SelectedGoalID:        selectedGoalID,
		SelectedGoalName:      selectedGoalName,
		SelectedAgentID:       selectedAgentID,
		SelectedAgentName:     selectedAgentName,
		SelectedResourceID:    selectedResourceID,
		SelectedResourceName:  selectedResourceName,
		ActiveExecutionID:     activeExecutionID,
		ActiveExecutionIDs:    append([]string{}, activeExecutionIDs...),
		ActiveExecutionStatus: activeExecutionStatus,
		LastError:             lastError,
		LastPlan:              lastPlan,
		StartedAt:             startedAt,
		UpdatedAt:             updatedAt,
	}

	effectiveState := m.sessionMergedState(session)
	currentState := sessionCurrentState(session)
	liveState := m.sessionLiveState(session)
	response.CurrentState = currentState

	for _, executionID := range uniqueNonEmptyStrings(append([]string{activeExecutionID}, activeExecutionIDs...)) {
		execState, ok := m.server.planExecutor.GetExecution(executionID)
		if !ok {
			continue
		}
		snapshot := execState.Snapshot()
		if executionID == activeExecutionID {
			response.ActiveExecutionStatus = snapshot.Status
		}
		if planningLive := m.server.stateManager.GetPlanningState(snapshot.ProblemID); len(planningLive) > 0 {
			if liveState == nil {
				liveState = map[string]string{}
			}
			if effectiveState == nil {
				effectiveState = cloneStringMap(currentState)
			}
			for key, value := range planningLive {
				liveState[key] = value
				effectiveState[key] = value
			}
		}
	}
	if len(effectiveState) > 0 {
		response.EffectiveState = effectiveState
	}
	if len(liveState) > 0 {
		response.LiveState = liveState
	}

	response.Goals = make([]RealtimeGoalResponse, 0, len(goals))
	for _, goal := range goals {
		response.Goals = append(response.Goals, RealtimeGoalResponse{
			ID:                   goal.ID,
			Name:                 goal.Name,
			Priority:             goal.Priority,
			Enabled:              goal.Enabled,
			ActivationConditions: cloneConditions(goal.ActivationConditions),
			ResourceTypeID:       goal.ResourceTypeID,
			GoalState:            cloneStringMap(goal.GoalState),
		})
	}
	return response
}

func (s *Server) buildRealtimeInitialState(taskDistributorID string, overrides map[string]string) (map[string]string, error) {
	initial := make(map[string]string)
	tdStates, err := s.repo.ListTaskDistributorStates(taskDistributorID)
	if err != nil {
		return nil, fmt.Errorf("failed to load task distributor states: %w", err)
	}
	for _, stateVar := range tdStates {
		if stateVar.InitialValue != "" {
			initial[stateVar.Name] = stateVar.InitialValue
		}
	}
	for key, value := range overrides {
		initial[key] = value
	}
	return initial, nil
}

func (s *Server) loadRealtimeAgents(agentIDs []string) ([]pddl.AgentInfo, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	agents := make([]pddl.AgentInfo, 0, len(agentIDs))
	for _, rawID := range agentIDs {
		agentID := strings.TrimSpace(rawID)
		if agentID == "" || seen[agentID] {
			continue
		}
		agent, err := s.repo.GetAgent(agentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get agent %s: %w", agentID, err)
		}
		if agent == nil {
			return nil, fmt.Errorf("agent %s not found", agentID)
		}
		agents = append(agents, pddl.AgentInfo{
			ID:   agentID,
			Name: strings.TrimSpace(agent.Name),
		})
		seen[agentID] = true
	}
	return agents, nil
}

func (s *Server) loadRealtimeResources(taskDistributorID string) ([]pddl.ResourceInfo, error) {
	taskDistributorID = strings.TrimSpace(taskDistributorID)
	if taskDistributorID == "" {
		return nil, nil
	}

	tdResources, err := s.repo.ListTaskDistributorResources(taskDistributorID)
	if err != nil {
		return nil, fmt.Errorf("failed to load task distributor resources: %w", err)
	}

	resources := make([]pddl.ResourceInfo, 0, len(tdResources))
	for _, resource := range tdResources {
		resources = append(resources, pddl.ResourceInfo{
			ID:               resource.ID,
			Name:             resource.Name,
			Kind:             resource.Kind,
			ParentResourceID: resource.ParentResourceID,
		})
	}
	return resources, nil
}

func (s *Server) solveProblemSpec(spec planningSolveSpec) (*pddl.Plan, error) {
	behaviorTreeIDs := normalizeBehaviorTreeIDs(spec.BehaviorTreeID, spec.BehaviorTreeIDs)
	behaviorTreeIDsJSON, _ := json.Marshal(behaviorTreeIDs)
	filteredInitialState, err := s.filterInitialStateByTaskDistributor(spec.TaskDistributorID, spec.InitialState)
	if err != nil {
		return nil, err
	}
	initialStateJSON, _ := json.Marshal(filteredInitialState)
	goalStateForSolve := cloneStringMap(spec.GoalState)
	if goalStateForSolve == nil {
		goalStateForSolve = map[string]string{}
	}
	if strings.TrimSpace(spec.GoalBinding.AgentID) != "" ||
		strings.TrimSpace(spec.GoalBinding.AgentName) != "" ||
		strings.TrimSpace(spec.GoalBinding.ResourceID) != "" ||
		strings.TrimSpace(spec.GoalBinding.ResourceName) != "" {
		goalStateForSolve = applyRealtimeGoalBinding(goalStateForSolve, spec.GoalBinding)
	}
	for variable, value := range goalStateForSolve {
		if containsAgentPlaceholder(variable) || containsAgentPlaceholder(value) ||
			containsResourcePlaceholder(variable) || containsResourcePlaceholder(value) {
			return nil, fmt.Errorf(
				"realtime solve received unresolved goal placeholder after binding (goal=%q value=%q agent=%q resource=%q)",
				variable,
				value,
				strings.TrimSpace(spec.GoalBinding.AgentName),
				strings.TrimSpace(spec.GoalBinding.ResourceName),
			)
		}
	}
	goalStateJSON, _ := json.Marshal(goalStateForSolve)
	agentIDsJSON, _ := json.Marshal(spec.AgentIDs)

	pp := &db.PlanningProblem{
		ID:                uuid.New().String()[:8],
		Name:              "realtime-solve",
		BehaviorTreeID:    behaviorTreeIDs[0],
		BehaviorTreeIDs:   datatypes.JSON(behaviorTreeIDsJSON),
		TaskDistributorID: sql.NullString{String: spec.TaskDistributorID, Valid: spec.TaskDistributorID != ""},
		InitialState:      datatypes.JSON(initialStateJSON),
		GoalState:         datatypes.JSON(goalStateJSON),
		AgentIDs:          datatypes.JSON(agentIDsJSON),
	}
	return s.solveProblem(pp)
}

func (s *Server) filterInitialStateByTaskDistributor(taskDistributorID string, initial map[string]string) (map[string]string, error) {
	copied := cloneStringMap(initial)
	if copied == nil {
		copied = map[string]string{}
	}
	if strings.TrimSpace(taskDistributorID) == "" {
		return copied, nil
	}

	states, err := s.repo.ListTaskDistributorStates(taskDistributorID)
	if err != nil {
		return nil, fmt.Errorf("failed to load task distributor states for realtime solve: %w", err)
	}

	allowed := make(map[string]struct{}, len(states))
	for _, state := range states {
		name := strings.TrimSpace(state.Name)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}

	filtered := make(map[string]string, len(copied))
	for key, value := range copied {
		if _, ok := allowed[key]; ok {
			filtered[key] = value
		}
	}
	return filtered, nil
}

type planningSolveSpec struct {
	BehaviorTreeID    string
	BehaviorTreeIDs   []string
	TaskDistributorID string
	InitialState      map[string]string
	GoalState         map[string]string
	GoalBinding       realtimeGoalBinding
	AgentIDs          []string
}

func (s *Server) ListRealtimeSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.realtimePddl.List()
	response := make([]RealtimeSessionResponse, 0, len(sessions))
	for _, session := range sessions {
		response = append(response, s.realtimePddl.toResponse(session))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) GetRealtimeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "sessionID")
	session, ok := s.realtimePddl.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "realtime session not found")
		return
	}
	writeJSON(w, http.StatusOK, s.realtimePddl.toResponse(session))
}

func (s *Server) StartRealtimeSession(w http.ResponseWriter, r *http.Request) {
	var req RealtimeSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := s.realtimePddl.Start(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s.realtimePddl.toResponse(session))
}

func (s *Server) StopRealtimeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "sessionID")
	if err := s.realtimePddl.Stop(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, _ := s.realtimePddl.Get(id)
	if session == nil {
		writeJSON(w, http.StatusOK, map[string]string{"message": "realtime session stopped"})
		return
	}
	writeJSON(w, http.StatusOK, s.realtimePddl.toResponse(session))
}

func (s *Server) ResetRealtimeSessionState(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "sessionID is required")
		return
	}

	var req RealtimeSessionStateResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Values) == 0 {
		writeError(w, http.StatusBadRequest, "values is required")
		return
	}

	session, err := s.realtimePddl.ResetSessionState(id, req.Values, req.ClearLiveKeys)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.realtimePddl.toResponse(session))
}

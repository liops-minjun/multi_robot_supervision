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
	ID                  string                `json:"id,omitempty"`
	Name                string                `json:"name"`
	Priority            int                   `json:"priority"`
	Enabled             bool                  `json:"enabled"`
	ActivationConditions []db.PlanningCondition `json:"activation_conditions,omitempty"`
	GoalState           map[string]string     `json:"goal_state"`
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
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	Priority             int                   `json:"priority"`
	Enabled              bool                  `json:"enabled"`
	ActivationConditions []db.PlanningCondition `json:"activation_conditions,omitempty"`
	GoalState            map[string]string     `json:"goal_state"`
}

type RealtimeSessionResponse struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Status           string                 `json:"status"`
	BehaviorTreeIDs  []string               `json:"behavior_tree_ids"`
	TaskDistributorID string                `json:"task_distributor_id,omitempty"`
	AgentIDs         []string               `json:"agent_ids"`
	TickIntervalSec  float64                `json:"tick_interval_sec"`
	Goals            []RealtimeGoalResponse `json:"goals"`
	CurrentState     map[string]string      `json:"current_state"`
	LiveState        map[string]string      `json:"live_state,omitempty"`
	SelectedGoalID   string                 `json:"selected_goal_id,omitempty"`
	SelectedGoalName string                 `json:"selected_goal_name,omitempty"`
	ActiveExecutionID string                `json:"active_execution_id,omitempty"`
	ActiveExecutionStatus string            `json:"active_execution_status,omitempty"`
	LastError        string                 `json:"last_error,omitempty"`
	LastPlan         *pddl.Plan             `json:"last_plan,omitempty"`
	StartedAt        time.Time              `json:"started_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

type realtimeGoal struct {
	ID                   string
	Name                 string
	Priority             int
	Enabled              bool
	ActivationConditions []db.PlanningCondition
	GoalState            map[string]string
}

type realtimeSession struct {
	ID                string
	Name              string
	BehaviorTreeIDs   []string
	TaskDistributorID string
	AgentIDs          []string
	TickInterval      time.Duration
	Goals             []realtimeGoal
	CurrentState      map[string]string
	SelectedGoalID    string
	SelectedGoalName  string
	ActiveExecutionID string
	ActivePlan        *pddl.Plan
	ActiveGoal        *realtimeGoal
	ActiveStatus      string
	LastError         string
	LastPlan          *pddl.Plan
	StartedAt         time.Time
	UpdatedAt         time.Time

	cancel         context.CancelFunc
	ctx            context.Context
	failedStateKey map[string]string
	mu             sync.RWMutex
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
		ID:                uuid.New().String()[:8],
		Name:              strings.TrimSpace(req.Name),
		BehaviorTreeIDs:   behaviorTreeIDs,
		TaskDistributorID: strings.TrimSpace(req.TaskDistributorID),
		AgentIDs:          append([]string{}, req.AgentIDs...),
		TickInterval:      tickInterval,
		Goals:             normalizeRealtimeGoals(req.Goals),
		CurrentState:      cloneStringMap(currentState),
		ActiveStatus:      "starting",
		StartedAt:         now,
		UpdatedAt:         now,
		ctx:               ctx,
		cancel:            cancel,
		failedStateKey:    map[string]string{},
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
	activeExecutionID := session.ActiveExecutionID
	session.mu.Unlock()

	if activeExecutionID != "" {
		_ = m.server.planExecutor.CancelExecution(activeExecutionID)
	}
	session.cancel()
	return nil
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

	if m.syncExecution(session) {
		return
	}

	currentState := sessionCurrentState(session)
	selectedGoal := selectRealtimeGoal(session.Goals, currentState)
	if selectedGoal == nil {
		session.mu.Lock()
		session.SelectedGoalID = ""
		session.SelectedGoalName = ""
		session.ActiveStatus = "idle"
		session.UpdatedAt = time.Now().UTC()
		session.mu.Unlock()
		return
	}

	failureKey := goalFailureKey(selectedGoal.ID, currentState)

	session.mu.RLock()
	if session.failedStateKey[selectedGoal.ID] == failureKey {
		session.mu.RUnlock()
		return
	}
	session.mu.RUnlock()

	plan, err := m.server.solveProblemSpec(planningSolveSpec{
		BehaviorTreeIDs:   session.BehaviorTreeIDs,
		TaskDistributorID: session.TaskDistributorID,
		InitialState:      currentState,
		GoalState:         cloneStringMap(selectedGoal.GoalState),
		AgentIDs:          append([]string{}, session.AgentIDs...),
	})
	if err != nil {
		recordRealtimeGoalFailure(session, selectedGoal, failureKey, err.Error(), nil)
		return
	}
	if plan == nil || !plan.IsValid {
		errMsg := "plan is invalid"
		if plan != nil && strings.TrimSpace(plan.ErrorMessage) != "" {
			errMsg = plan.ErrorMessage
		}
		recordRealtimeGoalFailure(session, selectedGoal, failureKey, errMsg, plan)
		return
	}
	if len(plan.Assignments) == 0 {
		recordRealtimeGoalFailure(session, selectedGoal, failureKey, "plan has no assignments", plan)
		return
	}

	executionProblemID := fmt.Sprintf("realtime:%s:%d", session.ID, time.Now().UnixNano())
	executionID, err := m.server.planExecutor.StartRuntimePlanExecution(
		session.ctx,
		executionProblemID,
		session.BehaviorTreeIDs,
		session.TaskDistributorID,
		currentState,
		plan,
	)
	if err != nil {
		recordRealtimeGoalFailure(session, selectedGoal, failureKey, err.Error(), plan)
		return
	}

	session.mu.Lock()
	delete(session.failedStateKey, selectedGoal.ID)
	session.ActiveExecutionID = executionID
	session.ActivePlan = clonePlan(plan)
	session.ActiveGoal = cloneRealtimeGoalPtr(selectedGoal)
	session.ActiveStatus = "running"
	session.SelectedGoalID = selectedGoal.ID
	session.SelectedGoalName = selectedGoal.Name
	session.LastError = ""
	session.LastPlan = clonePlan(plan)
	session.UpdatedAt = time.Now().UTC()
	session.mu.Unlock()
}

func (m *RealtimePddlManager) syncExecution(session *realtimeSession) bool {
	session.mu.RLock()
	activeExecutionID := session.ActiveExecutionID
	activePlan := clonePlan(session.ActivePlan)
	activeGoal := cloneRealtimeGoalPtr(session.ActiveGoal)
	session.mu.RUnlock()

	if activeExecutionID == "" {
		return false
	}

	execState, ok := m.server.planExecutor.GetExecution(activeExecutionID)
	if !ok {
		session.mu.Lock()
		session.ActiveExecutionID = ""
		session.ActivePlan = nil
		session.ActiveGoal = nil
		session.ActiveStatus = "idle"
		session.UpdatedAt = time.Now().UTC()
		session.mu.Unlock()
		return false
	}

	snapshot := execState.Snapshot()
	session.mu.Lock()
	session.ActiveStatus = snapshot.Status
	session.UpdatedAt = time.Now().UTC()
	session.mu.Unlock()

	switch executor.PlanExecutionStatus(snapshot.Status) {
	case executor.PlanExecPending, executor.PlanExecRunning:
		return true
	case executor.PlanExecCompleted:
		nextState := sessionCurrentState(session)
		for _, assignment := range activePlan.Assignments {
			for _, effect := range assignment.ResultStates {
				if strings.TrimSpace(effect.Variable) == "" {
					continue
				}
				nextState[effect.Variable] = effect.Value
			}
		}
		session.mu.Lock()
		session.CurrentState = nextState
		session.ActiveExecutionID = ""
		session.ActivePlan = nil
		session.ActiveGoal = nil
		session.ActiveStatus = "idle"
		session.LastError = ""
		if activeGoal != nil {
			delete(session.failedStateKey, activeGoal.ID)
		}
		session.UpdatedAt = time.Now().UTC()
		session.mu.Unlock()
		return false
	case executor.PlanExecFailed, executor.PlanExecCancelled:
		session.mu.Lock()
		if activeGoal != nil {
			session.failedStateKey[activeGoal.ID] = goalFailureKey(activeGoal.ID, session.CurrentState)
		}
		session.ActiveExecutionID = ""
		session.ActivePlan = nil
		session.ActiveGoal = nil
		session.ActiveStatus = snapshot.Status
		session.LastError = snapshot.Error
		session.UpdatedAt = time.Now().UTC()
		session.mu.Unlock()
		return false
	default:
		return false
	}
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

func selectRealtimeGoal(goals []realtimeGoal, currentState map[string]string) *realtimeGoal {
	for index := range goals {
		goal := goals[index]
		if !goal.Enabled {
			continue
		}
		if !planningConditionsMet(currentState, goal.ActivationConditions) {
			continue
		}
		if realtimeGoalSatisfied(currentState, goal.GoalState) {
			continue
		}
		return cloneRealtimeGoalPtr(&goal)
	}
	return nil
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
		currentValue := current[key]
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

func goalFailureKey(goalID string, state map[string]string) string {
	raw, _ := json.Marshal(state)
	return goalID + "::" + string(raw)
}

func recordRealtimeGoalFailure(session *realtimeSession, goal *realtimeGoal, failureKey, errMsg string, plan *pddl.Plan) {
	if session == nil || goal == nil {
		return
	}
	session.mu.Lock()
	session.failedStateKey[goal.ID] = failureKey
	session.SelectedGoalID = goal.ID
	session.SelectedGoalName = goal.Name
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
	defer session.mu.RUnlock()

	response := RealtimeSessionResponse{
		ID:                 session.ID,
		Name:               session.Name,
		Status:             session.ActiveStatus,
		BehaviorTreeIDs:    append([]string{}, session.BehaviorTreeIDs...),
		TaskDistributorID:  session.TaskDistributorID,
		AgentIDs:           append([]string{}, session.AgentIDs...),
		TickIntervalSec:    session.TickInterval.Seconds(),
		CurrentState:       cloneStringMap(session.CurrentState),
		SelectedGoalID:     session.SelectedGoalID,
		SelectedGoalName:   session.SelectedGoalName,
		ActiveExecutionID:  session.ActiveExecutionID,
		ActiveExecutionStatus: session.ActiveStatus,
		LastError:          session.LastError,
		LastPlan:           clonePlan(session.LastPlan),
		StartedAt:          session.StartedAt,
		UpdatedAt:          session.UpdatedAt,
	}

	if session.ActiveExecutionID != "" {
		if execState, ok := m.server.planExecutor.GetExecution(session.ActiveExecutionID); ok {
			snapshot := execState.Snapshot()
			response.ActiveExecutionStatus = snapshot.Status
			if live := m.server.stateManager.GetPlanningState(snapshot.ProblemID); len(live) > 0 {
				response.LiveState = live
			}
		}
	}

	response.Goals = make([]RealtimeGoalResponse, 0, len(session.Goals))
	for _, goal := range session.Goals {
		response.Goals = append(response.Goals, RealtimeGoalResponse{
			ID:                   goal.ID,
			Name:                 goal.Name,
			Priority:             goal.Priority,
			Enabled:              goal.Enabled,
			ActivationConditions: cloneConditions(goal.ActivationConditions),
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

func (s *Server) solveProblemSpec(spec planningSolveSpec) (*pddl.Plan, error) {
	behaviorTreeIDs := normalizeBehaviorTreeIDs(spec.BehaviorTreeID, spec.BehaviorTreeIDs)
	behaviorTreeIDsJSON, _ := json.Marshal(behaviorTreeIDs)
	initialStateJSON, _ := json.Marshal(spec.InitialState)
	goalStateJSON, _ := json.Marshal(spec.GoalState)
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

type planningSolveSpec struct {
	BehaviorTreeID    string
	BehaviorTreeIDs   []string
	TaskDistributorID string
	InitialState      map[string]string
	GoalState         map[string]string
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

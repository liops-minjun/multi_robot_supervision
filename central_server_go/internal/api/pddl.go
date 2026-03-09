package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/executor"
	"central_server_go/internal/pddl"
	"central_server_go/internal/state"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ============================================================
// PDDL Request/Response Models
// ============================================================

// PlanningProblemCreateRequest represents a request to create a planning problem
type PlanningProblemCreateRequest struct {
	Name              string            `json:"name"`
	BehaviorTreeID    string            `json:"behavior_tree_id"`
	TaskDistributorID string            `json:"task_distributor_id,omitempty"`
	InitialState      map[string]string `json:"initial_state,omitempty"`
	GoalState         map[string]string `json:"goal_state"`
	AgentIDs          []string          `json:"agent_ids"`
}

// PlanningProblemResponse represents a planning problem in API responses
type PlanningProblemResponse struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	BehaviorTreeID    string            `json:"behavior_tree_id"`
	TaskDistributorID string            `json:"task_distributor_id,omitempty"`
	InitialState      map[string]string `json:"initial_state,omitempty"`
	GoalState         map[string]string `json:"goal_state"`
	AgentIDs          []string          `json:"agent_ids"`
	Status            string            `json:"status"`
	PlanResult        *pddl.Plan        `json:"plan_result,omitempty"`
	ErrorMessage      string            `json:"error_message,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// PreviewDistributionRequest represents a request to preview task distribution without saving
type PreviewDistributionRequest struct {
	BehaviorTreeID    string            `json:"behavior_tree_id"`
	TaskDistributorID string            `json:"task_distributor_id,omitempty"`
	InitialState      map[string]string `json:"initial_state,omitempty"`
	GoalState         map[string]string `json:"goal_state"`
	AgentIDs          []string          `json:"agent_ids"`
}

// ============================================================
// PDDL Handlers
// ============================================================

// ListPlanningProblems lists planning problems with optional BT filter
func (s *Server) ListPlanningProblems(w http.ResponseWriter, r *http.Request) {
	btID := r.URL.Query().Get("behavior_tree_id")
	problems, err := s.repo.ListPlanningProblems(btID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list problems: %v", err))
		return
	}

	responses := make([]PlanningProblemResponse, 0, len(problems))
	for _, p := range problems {
		responses = append(responses, toPlanningProblemResponse(&p))
	}
	writeJSON(w, http.StatusOK, responses)
}

// CreatePlanningProblem creates a new planning problem
func (s *Server) CreatePlanningProblem(w http.ResponseWriter, r *http.Request) {
	var req PlanningProblemCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.BehaviorTreeID == "" {
		writeError(w, http.StatusBadRequest, "behavior_tree_id is required")
		return
	}
	if len(req.GoalState) == 0 {
		writeError(w, http.StatusBadRequest, "goal_state is required")
		return
	}
	if len(req.AgentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "agent_ids is required")
		return
	}

	// Verify BT exists
	bt, err := s.repo.GetBehaviorTree(req.BehaviorTreeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get behavior tree: %v", err))
		return
	}
	if bt == nil {
		writeError(w, http.StatusNotFound, "behavior tree not found")
		return
	}

	initialStateJSON, _ := json.Marshal(req.InitialState)
	goalStateJSON, _ := json.Marshal(req.GoalState)
	agentIDsJSON, _ := json.Marshal(req.AgentIDs)

	now := time.Now().UTC()
	pp := &db.PlanningProblem{
		ID:                uuid.New().String()[:8],
		Name:              req.Name,
		BehaviorTreeID:    req.BehaviorTreeID,
		TaskDistributorID: sql.NullString{String: req.TaskDistributorID, Valid: req.TaskDistributorID != ""},
		InitialState:      datatypes.JSON(initialStateJSON),
		GoalState:         datatypes.JSON(goalStateJSON),
		AgentIDs:          datatypes.JSON(agentIDsJSON),
		Status:            "draft",
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.repo.CreatePlanningProblem(pp); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create problem: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, toPlanningProblemResponse(pp))
}

// GetPlanningProblem retrieves a planning problem by ID
func (s *Server) GetPlanningProblem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "problemID")
	pp, err := s.repo.GetPlanningProblem(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get problem: %v", err))
		return
	}
	if pp == nil {
		writeError(w, http.StatusNotFound, "planning problem not found")
		return
	}
	writeJSON(w, http.StatusOK, toPlanningProblemResponse(pp))
}

// DeletePlanningProblem deletes a planning problem
func (s *Server) DeletePlanningProblem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "problemID")
	if err := s.repo.DeletePlanningProblem(id); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete problem: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// SolvePlanningProblem runs the planner on a saved problem
func (s *Server) SolvePlanningProblem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "problemID")
	pp, err := s.repo.GetPlanningProblem(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get problem: %v", err))
		return
	}
	if pp == nil {
		writeError(w, http.StatusNotFound, "planning problem not found")
		return
	}

	plan, err := s.solveProblem(pp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to solve: %v", err))
		return
	}

	// Save result
	planJSON, _ := json.Marshal(plan)
	status := "planned"
	errMsg := ""
	if !plan.IsValid {
		status = "failed"
		errMsg = plan.ErrorMessage
	}
	if err := s.repo.UpdatePlanningProblemStatus(id, status, planJSON, errMsg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update problem: %v", err))
		return
	}

	pp.Status = status
	pp.PlanResult = datatypes.JSON(planJSON)
	writeJSON(w, http.StatusOK, toPlanningProblemResponse(pp))
}

// ExecutePlan executes a solved plan by dispatching tasks to agents
func (s *Server) ExecutePlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "problemID")
	pp, err := s.repo.GetPlanningProblem(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get problem: %v", err))
		return
	}
	if pp == nil {
		writeError(w, http.StatusNotFound, "planning problem not found")
		return
	}
	if pp.Status != "planned" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("problem status is %q, must be 'planned'", pp.Status))
		return
	}

	var plan pddl.Plan
	if err := json.Unmarshal(pp.PlanResult, &plan); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse plan result")
		return
	}
	if !plan.IsValid {
		writeError(w, http.StatusBadRequest, "plan is not valid")
		return
	}

	// Update status to executing
	if err := s.repo.UpdatePlanningProblemStatus(id, "executing", pp.PlanResult, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update status: %v", err))
		return
	}

	// Start plan execution via PlanExecutor.
	// Do not bind long-running plan execution lifetime to the HTTP request context,
	// otherwise the execution is cancelled immediately after the response is sent.
	executionID, err := s.planExecutor.StartPlanExecution(
		context.WithoutCancel(r.Context()),
		id,
		pp.BehaviorTreeID,
		&plan,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to start execution: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":         "plan execution started",
		"execution_id":    executionID,
		"problem_id":      id,
		"total_tasks":     plan.TotalTasks,
		"total_steps":     plan.TotalSteps,
		"parallel_groups": plan.ParallelGroups,
		"assignments":     plan.Assignments,
	})
}

// ListPlanExecutions returns all active plan executions
func (s *Server) ListPlanExecutions(w http.ResponseWriter, r *http.Request) {
	executions := s.planExecutor.ListExecutions()
	responses := make([]PlanExecutionResponse, 0, len(executions))
	for _, exec := range executions {
		responses = append(responses, toPlanExecutionResponse(exec, s.stateManager))
	}
	writeJSON(w, http.StatusOK, responses)
}

// GetPlanExecution returns a specific plan execution
func (s *Server) GetPlanExecution(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "executionID")
	exec, ok := s.planExecutor.GetExecution(id)
	if !ok {
		writeError(w, http.StatusNotFound, "execution not found")
		return
	}
	writeJSON(w, http.StatusOK, toPlanExecutionResponse(exec, s.stateManager))
}

// CancelPlanExecution cancels a running plan execution
func (s *Server) CancelPlanExecution(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "executionID")
	if err := s.planExecutor.CancelExecution(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "execution cancelled"})
}

// GetPlanResources returns current resource allocations
func (s *Server) GetPlanResources(w http.ResponseWriter, r *http.Request) {
	allocations := s.planExecutor.GetResourceAllocations()
	writeJSON(w, http.StatusOK, allocations)
}

// PreviewDistribution previews task distribution without saving
func (s *Server) PreviewDistribution(w http.ResponseWriter, r *http.Request) {
	var req PreviewDistributionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.BehaviorTreeID == "" {
		writeError(w, http.StatusBadRequest, "behavior_tree_id is required")
		return
	}
	if len(req.GoalState) == 0 {
		writeError(w, http.StatusBadRequest, "goal_state is required")
		return
	}
	if len(req.AgentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "agent_ids is required")
		return
	}

	// Create a temporary problem
	initialStateJSON, _ := json.Marshal(req.InitialState)
	goalStateJSON, _ := json.Marshal(req.GoalState)
	agentIDsJSON, _ := json.Marshal(req.AgentIDs)

	pp := &db.PlanningProblem{
		BehaviorTreeID:    req.BehaviorTreeID,
		TaskDistributorID: sql.NullString{String: req.TaskDistributorID, Valid: req.TaskDistributorID != ""},
		InitialState:      datatypes.JSON(initialStateJSON),
		GoalState:         datatypes.JSON(goalStateJSON),
		AgentIDs:          datatypes.JSON(agentIDsJSON),
	}

	plan, err := s.solveProblem(pp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to solve: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, plan)
}

// ============================================================
// Internal helpers
// ============================================================

// solveProblem builds a PlanProblem from a PlanningProblem and runs the planner
func (s *Server) solveProblem(pp *db.PlanningProblem) (*pddl.Plan, error) {
	// Load behavior tree
	bt, err := s.repo.GetBehaviorTree(pp.BehaviorTreeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get behavior tree: %w", err)
	}
	if bt == nil {
		return nil, fmt.Errorf("behavior tree not found")
	}

	// Parse planning state vars: prefer TaskDistributor, fallback to BT.PlanningStates
	var stateVars []db.PlanningStateVar
	selectedTDID := ""
	if pp.TaskDistributorID.Valid && pp.TaskDistributorID.String != "" {
		selectedTDID = pp.TaskDistributorID.String
	} else if bt.TaskDistributorID.Valid && bt.TaskDistributorID.String != "" {
		selectedTDID = bt.TaskDistributorID.String
	}

	if selectedTDID != "" {
		tdStates, err := s.repo.ListTaskDistributorStates(selectedTDID)
		if err == nil {
			for _, ts := range tdStates {
				stateVars = append(stateVars, db.PlanningStateVar{
					Name:         ts.Name,
					Type:         ts.Type,
					InitialValue: ts.InitialValue,
					Description:  ts.Description,
				})
			}
		}
	} else if bt.PlanningStates != nil {
		json.Unmarshal(bt.PlanningStates, &stateVars)
	}

	resources := make([]pddl.ResourceInfo, 0)
	if selectedTDID != "" {
		tdResources, err := s.repo.ListTaskDistributorResources(selectedTDID)
		if err != nil {
			return nil, fmt.Errorf("failed to load task distributor resources: %w", err)
		}
		for _, resource := range tdResources {
			resources = append(resources, pddl.ResourceInfo{
				ID:               resource.ID,
				Name:             resource.Name,
				Kind:             resource.Kind,
				ParentResourceID: resource.ParentResourceID,
			})
		}
	}

	// Parse agent IDs
	var agentIDs []string
	if pp.AgentIDs != nil {
		json.Unmarshal(pp.AgentIDs, &agentIDs)
	}

	// Build agent info with capabilities
	agents := make([]pddl.AgentInfo, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		caps, err := s.repo.GetAgentCapabilities(agentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get capabilities for agent %s: %w", agentID, err)
		}
		agent, err := s.repo.GetAgent(agentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get agent %s: %w", agentID, err)
		}
		if agent == nil {
			return nil, fmt.Errorf("agent %s not found", agentID)
		}

		capTypes := make([]string, 0, len(caps))
		for _, c := range caps {
			capTypes = append(capTypes, c.ActionType)
		}

		isOnline := s.stateManager.IsAgentOnline(agentID)
		agents = append(agents, pddl.AgentInfo{
			ID:           agentID,
			Name:         agent.Name,
			Capabilities: capTypes,
			IsOnline:     isOnline,
		})
	}

	taskSpec, err := db.DecodePlanningTaskSpec(bt.PlanningTask)
	if err != nil {
		return nil, fmt.Errorf("failed to parse task planning metadata: %w", err)
	}
	if !taskSpec.HasData() {
		return nil, fmt.Errorf("behavior tree %q does not define task planning metadata", bt.ID)
	}
	if len(taskSpec.ResultStates) == 0 {
		return nil, fmt.Errorf("behavior tree %q does not define task result states", bt.ID)
	}

	tasks := []pddl.PlanTask{{
		TaskID:              bt.ID,
		TaskName:            bt.Name,
		BehaviorTreeID:      bt.ID,
		RequiredActionTypes: decodeStringSlice(bt.RequiredActionTypes),
		RequiredResources:   append([]string{}, taskSpec.RequiredResources...),
		ResultStates:        append([]db.PlanningEffect{}, taskSpec.ResultStates...),
		DuringState:         append([]db.PlanningEffect{}, taskSpec.DuringState...),
	}}

	// Parse initial and goal states
	var initialState, goalState map[string]string
	if pp.InitialState != nil {
		json.Unmarshal(pp.InitialState, &initialState)
	}
	if pp.GoalState != nil {
		json.Unmarshal(pp.GoalState, &goalState)
	}

	problem := &pddl.PlanProblem{
		StateVars:    stateVars,
		InitialState: initialState,
		GoalState:    goalState,
		Tasks:        tasks,
		Agents:       agents,
		Resources:    resources,
	}

	return pddl.Solve(problem), nil
}

func toPlanExecutionResponse(exec *executor.PlanExecution, stateManager *state.GlobalStateManager) PlanExecutionResponse {
	snapshot := exec.Snapshot()

	resp := PlanExecutionResponse{
		ID:             snapshot.ID,
		ProblemID:      snapshot.ProblemID,
		BehaviorTreeID: snapshot.BehaviorTreeID,
		Status:         snapshot.Status,
		CurrentOrder:   snapshot.CurrentOrder,
		TotalOrders:    snapshot.TotalOrders,
		StartedAt:      snapshot.StartedAt,
		Error:          snapshot.Error,
	}

	if snapshot.CompletedAt != nil {
		resp.CompletedAt = snapshot.CompletedAt
	}

	resp.Steps = make([]PlanExecutionStepResponse, 0, len(snapshot.Steps))
	agentNames := make(map[string]string, len(snapshot.Steps))
	for _, ss := range snapshot.Steps {
		agentNames[ss.AgentID] = ss.AgentName
		resp.Steps = append(resp.Steps, PlanExecutionStepResponse{
			TaskID:        ss.TaskID,
			TaskName:      ss.TaskName,
			RuntimeTaskID: ss.RuntimeTaskID,
			StepID:        ss.StepID,
			StepName:      ss.StepName,
			AgentID:       ss.AgentID,
			AgentName:     ss.AgentName,
			Order:         ss.Order,
			Status:        ss.Status,
			Error:         ss.Error,
		})
	}

	if stateManager != nil {
		resp.PlanningState = stateManager.GetPlanningState(snapshot.ProblemID)
		for _, hold := range stateManager.GetPlanResources(snapshot.ProblemID) {
			holderName := agentNames[hold.AgentID]
			if holderName == "" {
				holderName = hold.AgentID
			}
			resp.Resources = append(resp.Resources, PlanExecutionResourceResponse{
				Resource:        hold.ResourceID,
				HolderAgent:     holderName,
				HolderAgentID:   hold.AgentID,
				HolderAgentName: holderName,
				PlanID:          snapshot.ProblemID,
				ProblemID:       snapshot.ProblemID,
				PlanExecutionID: snapshot.ID,
				TaskID:          hold.TaskID,
				StepID:          hold.StepID,
				AcquiredAt:      hold.AcquiredAt,
			})
		}
	}

	return resp
}
func toPlanningProblemResponse(pp *db.PlanningProblem) PlanningProblemResponse {
	resp := PlanningProblemResponse{
		ID:                pp.ID,
		Name:              pp.Name,
		BehaviorTreeID:    pp.BehaviorTreeID,
		TaskDistributorID: pp.TaskDistributorID.String,
		Status:            pp.Status,
		ErrorMessage:      pp.ErrorMessage.String,
		CreatedAt:         pp.CreatedAt,
		UpdatedAt:         pp.UpdatedAt,
	}

	if pp.InitialState != nil {
		json.Unmarshal(pp.InitialState, &resp.InitialState)
	}
	if pp.GoalState != nil {
		json.Unmarshal(pp.GoalState, &resp.GoalState)
	}
	if pp.AgentIDs != nil {
		json.Unmarshal(pp.AgentIDs, &resp.AgentIDs)
	}
	if pp.PlanResult != nil && len(pp.PlanResult) > 0 {
		var plan pddl.Plan
		if err := json.Unmarshal(pp.PlanResult, &plan); err == nil {
			resp.PlanResult = &plan
		}
	}

	return resp
}

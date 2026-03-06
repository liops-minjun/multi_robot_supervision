package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/pddl"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ============================================================
// PDDL Request/Response Models
// ============================================================

// PlanningProblemCreateRequest represents a request to create a planning problem
type PlanningProblemCreateRequest struct {
	Name           string            `json:"name"`
	BehaviorTreeID string            `json:"behavior_tree_id"`
	InitialState   map[string]string `json:"initial_state,omitempty"`
	GoalState      map[string]string `json:"goal_state"`
	AgentIDs       []string          `json:"agent_ids"`
}

// PlanningProblemResponse represents a planning problem in API responses
type PlanningProblemResponse struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	BehaviorTreeID string            `json:"behavior_tree_id"`
	InitialState   map[string]string `json:"initial_state,omitempty"`
	GoalState      map[string]string `json:"goal_state"`
	AgentIDs       []string          `json:"agent_ids"`
	Status         string            `json:"status"`
	PlanResult     *pddl.Plan        `json:"plan_result,omitempty"`
	ErrorMessage   string            `json:"error_message,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// PreviewDistributionRequest represents a request to preview task distribution without saving
type PreviewDistributionRequest struct {
	BehaviorTreeID string            `json:"behavior_tree_id"`
	InitialState   map[string]string `json:"initial_state,omitempty"`
	GoalState      map[string]string `json:"goal_state"`
	AgentIDs       []string          `json:"agent_ids"`
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
		ID:             uuid.New().String()[:8],
		Name:           req.Name,
		BehaviorTreeID: req.BehaviorTreeID,
		InitialState:   datatypes.JSON(initialStateJSON),
		GoalState:      datatypes.JSON(goalStateJSON),
		AgentIDs:       datatypes.JSON(agentIDsJSON),
		Status:         "draft",
		CreatedAt:      now,
		UpdatedAt:      now,
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

	// Initialize planning state tracking
	var initialState map[string]string
	if pp.InitialState != nil {
		json.Unmarshal(pp.InitialState, &initialState)
	}
	s.stateManager.InitPlanningState(id, initialState)

	// Update status to executing
	if err := s.repo.UpdatePlanningProblemStatus(id, "executing", pp.PlanResult, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update status: %v", err))
		return
	}

	// Group assignments by order for sequential/parallel execution
	orderGroups := make(map[int][]pddl.StepAssignment)
	for _, a := range plan.Assignments {
		orderGroups[a.Order] = append(orderGroups[a.Order], a)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":         "plan execution started",
		"problem_id":      id,
		"total_steps":     plan.TotalSteps,
		"parallel_groups": plan.ParallelGroups,
		"assignments":     plan.Assignments,
	})
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
		BehaviorTreeID: req.BehaviorTreeID,
		InitialState:   datatypes.JSON(initialStateJSON),
		GoalState:      datatypes.JSON(goalStateJSON),
		AgentIDs:       datatypes.JSON(agentIDsJSON),
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

	// Parse steps
	var steps []db.BehaviorTreeStep
	if bt.Steps != nil {
		if err := json.Unmarshal(bt.Steps, &steps); err != nil {
			return nil, fmt.Errorf("failed to parse steps: %w", err)
		}
	}

	// Parse planning state vars
	var stateVars []db.PlanningStateVar
	if bt.PlanningStates != nil {
		json.Unmarshal(bt.PlanningStates, &stateVars)
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

	// Build plan actions from steps
	actions := make([]pddl.PlanAction, 0, len(steps))
	for _, step := range steps {
		if step.Type == "terminal" {
			continue
		}
		actionType := ""
		if step.Action != nil {
			actionType = step.Action.Type
		}
		actions = append(actions, pddl.PlanAction{
			StepID:          step.ID,
			StepName:        step.Name,
			ActionType:      actionType,
			ResourceAcquire: step.ResourceAcquire,
			ResourceRelease: step.ResourceRelease,
			Preconditions:   step.PlanningPreconditions,
			Effects:         step.PlanningEffects,
		})
	}

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
		Actions:      actions,
		Agents:       agents,
	}

	return pddl.Solve(problem), nil
}

func toPlanningProblemResponse(pp *db.PlanningProblem) PlanningProblemResponse {
	resp := PlanningProblemResponse{
		ID:             pp.ID,
		Name:           pp.Name,
		BehaviorTreeID: pp.BehaviorTreeID,
		Status:         pp.Status,
		ErrorMessage:   pp.ErrorMessage.String,
		CreatedAt:      pp.CreatedAt,
		UpdatedAt:      pp.UpdatedAt,
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

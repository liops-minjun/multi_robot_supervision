package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"central_server_go/internal/db"
	graphpkg "central_server_go/internal/graph"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ListBehaviorTrees returns all behavior trees
func (s *Server) ListBehaviorTrees(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	includeTemplates := r.URL.Query().Get("include_templates") == "true"

	graphs, err := s.repo.GetBehaviorTrees(agentID, includeTemplates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]BehaviorTreeListResponse, len(graphs))
	for i, graph := range graphs {
		responses[i] = behaviorTreeToListResponse(&graph, s.repo)
	}

	writeJSON(w, http.StatusOK, responses)
}

// CreateBehaviorTree creates a new behavior tree
func (s *Server) CreateBehaviorTree(w http.ResponseWriter, r *http.Request) {
	var req BehaviorTreeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}

	// Check if ID already exists
	existing, _ := s.repo.GetBehaviorTree(req.ID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Behavior Tree already exists")
		return
	}

	// Validate agent exists if provided
	if req.AgentID != "" {
		agent, _ := s.repo.GetAgent(req.AgentID)
		if agent == nil {
			writeError(w, http.StatusNotFound, "Agent not found")
			return
		}
	}

	// Marshal JSON fields
	preconditionsJSON, _ := json.Marshal(req.Preconditions)
	normalizedSteps := normalizeBehaviorTreeSteps(req.Steps)
	stepsJSON, _ := json.Marshal(normalizedSteps)

	// Handle auto_generate_states (default: true if not specified)
	autoGenerateStates := true
	if req.AutoGenerateStates != nil {
		autoGenerateStates = *req.AutoGenerateStates
	}

	// Convert states from request to DB format
	var statesJSON []byte
	if len(req.States) > 0 {
		dbStates := make([]db.GraphState, len(req.States))
		for i, s := range req.States {
			dbStates[i] = db.GraphState{
				Code:         s.Code,
				Name:         s.Name,
				Type:         s.Type,
				StepID:       s.StepID,
				Phase:        s.Phase,
				Color:        s.Color,
				Description:  s.Description,
				SemanticTags: s.SemanticTags,
			}
		}
		statesJSON, _ = json.Marshal(dbStates)
	}

	// Extract required action types from steps
	var dbStepsForExtract []db.BehaviorTreeStep
	stepsJSONForExtract, _ := json.Marshal(normalizedSteps)
	json.Unmarshal(stepsJSONForExtract, &dbStepsForExtract)
	requiredActionTypes := db.ExtractActionTypesFromSteps(dbStepsForExtract)
	requiredActionTypesJSON, _ := json.Marshal(requiredActionTypes)

	// Convert planning states from request to DB format
	var planningStatesJSON []byte
	if len(req.PlanningStates) > 0 {
		dbPlanningStates := make([]db.PlanningStateVar, len(req.PlanningStates))
		for i, ps := range req.PlanningStates {
			dbPlanningStates[i] = db.PlanningStateVar{
				Name:         ps.Name,
				Type:         ps.Type,
				InitialValue: ps.InitialValue,
				Description:  ps.Description,
			}
		}
		planningStatesJSON, _ = json.Marshal(dbPlanningStates)
	}

	graph := &db.BehaviorTree{
		ID:                  req.ID,
		Name:                req.Name,
		Preconditions:       datatypes.JSON(preconditionsJSON),
		Steps:               datatypes.JSON(stepsJSON),
		States:              datatypes.JSON(statesJSON),
		PlanningStates:      datatypes.JSON(planningStatesJSON),
		RequiredActionTypes: datatypes.JSON(requiredActionTypesJSON),
		AutoGenerateStates:  autoGenerateStates,
		Version:             1,
		IsTemplate:          req.AgentID == "",
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	if req.Description != "" {
		graph.Description = sql.NullString{String: req.Description, Valid: true}
	}
	if req.AgentID != "" {
		graph.AgentID = sql.NullString{String: req.AgentID, Valid: true}
	}
	if req.EntryPoint != "" {
		graph.EntryPoint = sql.NullString{String: req.EntryPoint, Valid: true}
	}

	if err := s.repo.CreateBehaviorTree(graph); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Cache the newly created graph
	canonicalGraph, err := graphpkg.FromDBModel(graph)
	if err == nil {
		if req.AgentID != "" {
			s.stateManager.GraphCache().SetDeployed(req.AgentID, graph.ID, canonicalGraph)
		} else {
			s.stateManager.GraphCache().SetTemplate(graph.ID, canonicalGraph)
		}
	}

	// Auto-create AgentBehaviorTree if agent_id is provided
	if req.AgentID != "" {
		abt := &db.AgentBehaviorTree{
			ID:               uuid.New().String(),
			AgentID:          req.AgentID,
			BehaviorTreeID:   graph.ID,
			ServerVersion:    graph.Version,
			DeployedVersion:  0,
			DeploymentStatus: "pending",
			Enabled:          true,
			Priority:         0,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		s.repo.CreateAgentBehaviorTree(abt)
	}

	writeJSON(w, http.StatusCreated, behaviorTreeToResponse(graph, s.repo))
}

// GetBehaviorTree returns a single behavior tree
func (s *Server) GetBehaviorTree(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	writeJSON(w, http.StatusOK, behaviorTreeToResponse(graph, s.repo))
}

// UpdateBehaviorTree updates a behavior tree
func (s *Server) UpdateBehaviorTree(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")
	sessionID := r.Header.Get("X-Session-ID")

	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	// Validate lock ownership if session ID is provided
	if sessionID != "" {
		isOwner, lockedBy, err := s.ValidateLockOwnership(graphID, sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isOwner {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":     "locked",
				"message":   "Behavior Tree is locked by another user",
				"locked_by": lockedBy,
			})
			return
		}
	}

	var req BehaviorTreeUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != "" {
		graph.Name = req.Name
	}
	if req.Description != "" {
		graph.Description = sql.NullString{String: req.Description, Valid: true}
	}
	if req.Preconditions != nil {
		preconditionsJSON, _ := json.Marshal(req.Preconditions)
		graph.Preconditions = datatypes.JSON(preconditionsJSON)
	}
	if req.EntryPoint != "" {
		graph.EntryPoint = sql.NullString{String: req.EntryPoint, Valid: true}
	}
	if req.Steps != nil {
		normalizedSteps := normalizeBehaviorTreeSteps(req.Steps)
		stepsJSON, _ := json.Marshal(normalizedSteps)
		graph.Steps = datatypes.JSON(stepsJSON)

		// Update required action types when steps change
		var dbStepsForExtract []db.BehaviorTreeStep
		json.Unmarshal(stepsJSON, &dbStepsForExtract)
		requiredActionTypes := db.ExtractActionTypesFromSteps(dbStepsForExtract)
		requiredActionTypesJSON, _ := json.Marshal(requiredActionTypes)
		graph.RequiredActionTypes = datatypes.JSON(requiredActionTypesJSON)
	}

	// Handle auto_generate_states
	if req.AutoGenerateStates != nil {
		graph.AutoGenerateStates = *req.AutoGenerateStates
	}

	// Handle states
	if req.States != nil {
		dbStates := make([]db.GraphState, len(req.States))
		for i, s := range req.States {
			dbStates[i] = db.GraphState{
				Code:         s.Code,
				Name:         s.Name,
				Type:         s.Type,
				StepID:       s.StepID,
				Phase:        s.Phase,
				Color:        s.Color,
				Description:  s.Description,
				SemanticTags: s.SemanticTags,
			}
		}
		statesJSON, _ := json.Marshal(dbStates)
		graph.States = datatypes.JSON(statesJSON)
	}

	// Handle planning states
	if req.PlanningStates != nil {
		dbPlanningStates := make([]db.PlanningStateVar, len(req.PlanningStates))
		for i, ps := range req.PlanningStates {
			dbPlanningStates[i] = db.PlanningStateVar{
				Name:         ps.Name,
				Type:         ps.Type,
				InitialValue: ps.InitialValue,
				Description:  ps.Description,
			}
		}
		planningStatesJSON, _ := json.Marshal(dbPlanningStates)
		graph.PlanningStates = datatypes.JSON(planningStatesJSON)
	}

	graph.Version++
	graph.UpdatedAt = time.Now()

	if err := s.repo.UpdateBehaviorTree(graph); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Invalidate all cached versions and re-cache the updated graph
	affectedAgents := s.stateManager.GraphCache().InvalidateAllDeployments(graph.ID)

	// Convert to canonical and cache the updated version
	canonicalGraph, err := graphpkg.FromDBModel(graph)
	if err == nil {
		if graph.AgentID.Valid {
			s.stateManager.GraphCache().SetDeployed(graph.AgentID.String, graph.ID, canonicalGraph)
		} else {
			s.stateManager.GraphCache().SetTemplate(graph.ID, canonicalGraph)
		}
	}

	// Update server_version in AgentBehaviorTree if exists
	if graph.AgentID.Valid {
		abt, _ := s.repo.GetAgentBehaviorTree(graph.AgentID.String, graph.ID)
		if abt != nil {
			abt.ServerVersion = graph.Version
			if abt.DeployedVersion > 0 && abt.DeployedVersion < graph.Version {
				abt.DeploymentStatus = "outdated"
			}
			abt.UpdatedAt = time.Now()
			s.repo.UpdateAgentBehaviorTree(abt)
		}
	}

	// Notify affected agents about the graph update via QUIC
	if len(affectedAgents) > 0 && s.quicHandler != nil {
		// Send graph update notification to each affected agent
		for _, agentID := range affectedAgents {
			// Convert to canonical format without RTM namespace substitution.
			// Legacy fallback: strip {namespace} token if it still exists in old templates.
			agent, _ := s.repo.GetAgent(agentID)
			if agent != nil && canonicalGraph != nil {
				agentGraph := *canonicalGraph
				agentGraph.SubstituteServerPatterns("")
				graphJSON, _ := json.Marshal(agentGraph)

				// Send async notification (don't block the response)
				go s.quicHandler.SendGraphUpdateNotification(
					agentID,
					graph.ID,
					graph.Version,
					"MODIFIED",
					graphJSON,
				)
			}
		}
	}

	// Broadcast graph sync event via WebSocket
	s.wsHub.BroadcastGraphSync(graph.ID, "", "updated", graph.Version)

	writeJSON(w, http.StatusOK, behaviorTreeToResponse(graph, s.repo))
}

// DeleteBehaviorTree deletes a behavior tree
func (s *Server) DeleteBehaviorTree(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	// Invalidate cache before deletion
	s.stateManager.GraphCache().InvalidateAllDeployments(graphID)

	if err := s.repo.DeleteBehaviorTree(graphID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Behavior Tree deleted",
	})
}

// CheckExecutability validates if a behavior tree can be executed on an agent
// This is a safety check that verifies all required capabilities are available
func (s *Server) CheckExecutability(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")
	agentID := r.URL.Query().Get("agent_id")

	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id query parameter is required")
		return
	}

	// Validate capabilities
	result, err := s.scheduler.ValidateCapabilities(graphID, agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Check if agent is online
	isOnline := s.stateManager.IsAgentOnline(agentID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"behavior_tree_id":      graphID,
		"agent_id":              agentID,
		"can_execute":           result.Valid && isOnline,
		"capabilities_valid":    result.Valid,
		"agent_online":          isOnline,
		"missing_capabilities":  result.MissingCapabilities,
		"unavailable_servers":   result.UnavailableServers,
		"message":               result.Message,
	})
}

// ExecuteBehaviorTree starts executing a behavior tree on a robot
func (s *Server) ExecuteBehaviorTree(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	var req BehaviorTreeExecuteRequest
	// Decode body if present (ignore error if body is empty/null)
	json.NewDecoder(r.Body).Decode(&req)

	// Also check query parameter for agent_id
	if req.AgentID == "" {
		req.AgentID = r.URL.Query().Get("agent_id")
	}

	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required (in body or query param)")
		return
	}

	// Validate that the graph is assigned and deployed to this agent
	abt, err := s.repo.GetAgentBehaviorTree(req.AgentID, graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check graph assignment: "+err.Error())
		return
	}
	if abt == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("behavior tree '%s' is not assigned to agent '%s'. Please assign the graph first.", graphID, req.AgentID))
		return
	}
	// Allow execution if deployed or outdated (server will use latest version)
	// "outdated" means template was updated but agent has a previous deployment
	if abt.DeploymentStatus != "deployed" && abt.DeploymentStatus != "outdated" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("behavior tree '%s' is not deployed to agent '%s' (status: %s). Please deploy the graph first.", graphID, req.AgentID, abt.DeploymentStatus))
		return
	}
	if abt.DeployedVersion == 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("behavior tree '%s' has never been successfully deployed to agent '%s'", graphID, req.AgentID))
		return
	}

	taskID, err := s.scheduler.StartTask(r.Context(), graphID, req.AgentID, req.Params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"task_id":           taskID,
		"behavior_tree_id":  graphID,
		"agent_id":          req.AgentID,
		"status":            "running",
		"message":           "Behavior Tree execution started",
	})
}

// ExecuteMultiBehaviorTree starts executing a behavior tree on multiple agents simultaneously
func (s *Server) ExecuteMultiBehaviorTree(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	var req MultiAgentExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.AgentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "agent_ids is required and must not be empty")
		return
	}

	if len(req.AgentIDs) < 2 {
		writeError(w, http.StatusBadRequest, "Multi-agent execution requires at least 2 agents; use /execute for single agent")
		return
	}

	// Default sync mode to barrier
	if req.SyncMode == "" {
		req.SyncMode = "barrier"
	}

	// Validate sync mode
	if req.SyncMode != "barrier" && req.SyncMode != "best_effort" {
		writeError(w, http.StatusBadRequest, "sync_mode must be 'barrier' or 'best_effort'")
		return
	}

	// Call scheduler to start multi-agent task
	result, err := s.scheduler.StartMultiAgentTask(
		r.Context(),
		graphID,
		req.AgentIDs,
		req.Params,
		req.AgentParams,
		req.SyncMode,
	)

	if err != nil {
		// Check if it's a validation error with details
		if result != nil && result.FailedAgentID != "" {
			writeJSON(w, http.StatusBadRequest, MultiAgentExecuteErrorResponse{
				Error:   "validation_failed",
				Message: err.Error(),
				FailedAgents: []MultiAgentFailedAgent{
					{
						AgentID: result.FailedAgentID,
						Reason:  result.ErrorMessage,
					},
				},
			})
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Build success response
	tasks := make([]MultiAgentTaskInfo, 0, len(result.Tasks))
	for _, taskInfo := range result.Tasks {
		agentName := ""
		if agent, _ := s.repo.GetAgent(taskInfo.AgentID); agent != nil {
			agentName = agent.Name
		}
		tasks = append(tasks, MultiAgentTaskInfo{
			AgentID:   taskInfo.AgentID,
			AgentName: agentName,
			TaskID:    taskInfo.TaskID,
			Status:    taskInfo.Status,
		})
	}

	writeJSON(w, http.StatusOK, MultiAgentExecuteResponse{
		ExecutionGroupID: result.ExecutionGroupID,
		Tasks:            tasks,
		StartedAt:        time.Now(),
		SyncMode:         req.SyncMode,
		Message:          "Multi-agent execution started",
	})
}

// ValidateBehaviorTree validates a behavior tree configuration
func (s *Server) ValidateBehaviorTree(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	errors := []string{}
	warnings := []string{}

	var steps []map[string]interface{}
	json.Unmarshal(graph.Steps, &steps)

	stepIDs := make(map[string]bool)
	for _, step := range steps {
		if id, ok := step["id"].(string); ok {
			stepIDs[id] = true
		}
	}

	// Validate each step
	for _, step := range steps {
		stepID, _ := step["id"].(string)

		// Check transitions reference valid steps
		if transition, ok := step["transition"].(map[string]interface{}); ok {
			for _, key := range []string{"on_success", "on_failure", "on_confirm", "on_cancel", "on_timeout"} {
				if target, ok := transition[key]; ok {
					switch v := target.(type) {
					case string:
						if !stepIDs[v] {
							errors = append(errors, "Step '"+stepID+"': transition '"+key+"' references unknown step '"+v+"'")
						}
					case map[string]interface{}:
						for _, subKey := range []string{"next", "else", "fallback"} {
							if subTarget, ok := v[subKey].(string); ok {
								if !stepIDs[subTarget] {
									errors = append(errors, "Step '"+stepID+"': transition '"+key+"."+subKey+"' references unknown step '"+subTarget+"'")
								}
							}
						}
					}
				}
			}
		}
	}

	// Check for terminal steps
	hasTerminal := false
	for _, step := range steps {
		if stepType, ok := step["type"].(string); ok && stepType == "terminal" {
			hasTerminal = true
			break
		}
	}
	if !hasTerminal {
		warnings = append(warnings, "Behavior Tree has no terminal steps")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":    len(errors) == 0,
		"errors":   errors,
		"warnings": warnings,
	})
}

// Helper functions

func behaviorTreeToListResponse(graph *db.BehaviorTree, repo *db.Repository) BehaviorTreeListResponse {
	response := BehaviorTreeListResponse{
		ID:         graph.ID,
		Name:       graph.Name,
		Version:    graph.Version,
		IsTemplate: graph.IsTemplate,
		CreatedAt:  graph.CreatedAt,
		UpdatedAt:  graph.UpdatedAt,
	}

	if graph.Description.Valid {
		response.Description = graph.Description.String
	}
	if graph.AgentID.Valid {
		response.AgentID = graph.AgentID.String
		// Get agent name
		agent, _ := repo.GetAgent(graph.AgentID.String)
		if agent != nil {
			response.AgentName = agent.Name
		}
	}
	if graph.EntryPoint.Valid {
		response.EntryPoint = graph.EntryPoint.String
	}

	// Count steps
	var steps []interface{}
	json.Unmarshal(graph.Steps, &steps)
	response.StepCount = len(steps)

	// Count states
	if graph.States != nil && len(graph.States) > 0 {
		var states []interface{}
		json.Unmarshal(graph.States, &states)
		response.StateCount = len(states)
	}

	// Get deployment status
	if graph.AgentID.Valid {
		abt, _ := repo.GetAgentBehaviorTree(graph.AgentID.String, graph.ID)
		if abt != nil {
			response.DeploymentStatus = abt.DeploymentStatus
		}
	}

	return response
}

func behaviorTreeToResponse(graph *db.BehaviorTree, repo *db.Repository) BehaviorTreeResponse {
	response := BehaviorTreeResponse{
		ID:                 graph.ID,
		Name:               graph.Name,
		Version:            graph.Version,
		IsTemplate:         graph.IsTemplate,
		AutoGenerateStates: graph.AutoGenerateStates,
		CreatedAt:          graph.CreatedAt,
		UpdatedAt:          graph.UpdatedAt,
	}

	if graph.Description.Valid {
		response.Description = graph.Description.String
	}
	if graph.AgentID.Valid {
		response.AgentID = graph.AgentID.String
		agent, _ := repo.GetAgent(graph.AgentID.String)
		if agent != nil {
			response.AgentName = agent.Name
		}
	}
	if graph.EntryPoint.Valid {
		response.EntryPoint = graph.EntryPoint.String
	}

	// Parse JSON fields
	if graph.Preconditions != nil {
		json.Unmarshal(graph.Preconditions, &response.Preconditions)
	}
	if graph.Steps != nil {
		json.Unmarshal(graph.Steps, &response.Steps)
	}
	// Ensure steps is always an empty array, not null
	if response.Steps == nil {
		response.Steps = []map[string]interface{}{}
	}

	// Parse states
	if graph.States != nil && len(graph.States) > 0 {
		var dbStates []db.GraphState
		if err := json.Unmarshal(graph.States, &dbStates); err == nil {
			response.States = make([]GraphStateResponse, len(dbStates))
			for i, s := range dbStates {
				response.States[i] = GraphStateResponse{
					Code:         s.Code,
					Name:         s.Name,
					Type:         s.Type,
					StepID:       s.StepID,
					Phase:        s.Phase,
					Color:        s.Color,
					Description:  s.Description,
					SemanticTags: s.SemanticTags,
				}
			}
		}
	}

	// Parse planning states
	if graph.PlanningStates != nil && len(graph.PlanningStates) > 0 {
		var dbPlanningStates []db.PlanningStateVar
		if err := json.Unmarshal(graph.PlanningStates, &dbPlanningStates); err == nil {
			response.PlanningStates = make([]PlanningStateVarResponse, len(dbPlanningStates))
			for i, ps := range dbPlanningStates {
				response.PlanningStates[i] = PlanningStateVarResponse{
					Name:         ps.Name,
					Type:         ps.Type,
					InitialValue: ps.InitialValue,
					Description:  ps.Description,
				}
			}
		}
	}

	// Get deployment status
	if graph.AgentID.Valid {
		abt, _ := repo.GetAgentBehaviorTree(graph.AgentID.String, graph.ID)
		if abt != nil {
			response.DeploymentStatus = abt.DeploymentStatus
		}
	}

	return response
}

// =============================================================================
// Canonical Graph Endpoints
// =============================================================================

// GetCanonicalGraph returns a behavior tree in canonical format
// This format is optimized for graph operations and Agent communication
func (s *Server) GetCanonicalGraph(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	dbGraph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dbGraph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	// Convert to canonical format
	canonicalGraph, err := graphpkg.FromDBModel(dbGraph)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to convert to canonical format: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, canonicalGraph)
}

// ValidateCanonicalGraph performs advanced graph validation using graph algorithms
func (s *Server) ValidateCanonicalGraph(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	dbGraph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dbGraph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	// Convert to canonical format
	canonicalGraph, err := graphpkg.FromDBModel(dbGraph)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to convert to canonical format: "+err.Error())
		return
	}

	errors := []string{}
	warnings := []string{}

	// Basic validation
	if err := canonicalGraph.Validate(); err != nil {
		errors = append(errors, err.Error())
	}

	// Check for cycles
	if canonicalGraph.HasCycle() {
		errors = append(errors, "Graph contains cycles")
	}

	// Check for unreachable vertices
	unreachable := canonicalGraph.FindUnreachableVertices()
	if len(unreachable) > 0 {
		for _, u := range unreachable {
			warnings = append(warnings, "Unreachable vertex: "+u)
		}
	}

	// Check for terminal vertices
	terminals := canonicalGraph.FindTerminals()
	if len(terminals) == 0 {
		warnings = append(warnings, "Graph has no terminal vertices")
	}

	// Check each vertex has at least one outgoing edge (except terminals)
	for _, v := range canonicalGraph.Vertices {
		if v.Type == graphpkg.VertexTypeTerminal {
			continue
		}
		edges := canonicalGraph.GetOutgoingEdges(v.ID)
		if len(edges) == 0 {
			warnings = append(warnings, "Step '"+v.ID+"' has no outgoing transitions")
		}
	}

	// Check success path exists
	hasSuccessTerminal := false
	for _, t := range terminals {
		if t.Terminal != nil && t.Terminal.TerminalType == graphpkg.TerminalTypeSuccess {
			hasSuccessTerminal = true
			break
		}
	}
	if !hasSuccessTerminal {
		warnings = append(warnings, "Graph has no success terminal")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":         len(errors) == 0,
		"errors":        errors,
		"warnings":      warnings,
		"vertex_count":  len(canonicalGraph.Vertices),
		"edge_count":    len(canonicalGraph.Edges),
		"checksum":      canonicalGraph.Checksum,
		"entry_point":   canonicalGraph.EntryPoint,
		"terminal_count": len(terminals),
	})
}

// DeployBehaviorTreeToAgent deploys a behavior tree to an agent using canonical format
func (s *Server) DeployBehaviorTreeToAgent(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")
	agentID := chi.URLParam(r, "agentID")

	// Get the behavior tree
	dbGraph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dbGraph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	// Get the agent
	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	// Validate agent capability compatibility
	var requiredTypes []string
	if dbGraph.RequiredActionTypes != nil {
		json.Unmarshal(dbGraph.RequiredActionTypes, &requiredTypes)
	}

	if len(requiredTypes) > 0 {
		agentCaps, err := s.repo.GetAgentCapabilities(agentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to get agent capabilities: "+err.Error())
			return
		}

		// Build set of agent's action types
		agentActionTypes := make(map[string]bool)
		for _, cap := range agentCaps {
			agentActionTypes[cap.ActionType] = true
		}

		// Check for missing capabilities
		var missingTypes []string
		for _, reqType := range requiredTypes {
			if !agentActionTypes[reqType] {
				missingTypes = append(missingTypes, reqType)
			}
		}

		if len(missingTypes) > 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"Agent does not support required action types: %v", missingTypes))
			return
		}
	}

	// Convert to canonical format
	canonicalGraph, err := graphpkg.FromDBModel(dbGraph)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to convert to canonical format: "+err.Error())
		return
	}

	// Validate before deployment
	if err := canonicalGraph.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "Graph validation failed: "+err.Error())
		return
	}

	if canonicalGraph.HasCycle() {
		writeError(w, http.StatusBadRequest, "Cannot deploy graph with cycles")
		return
	}

	// Set the target agent
	canonicalGraph.BehaviorTree.AgentID = agentID

	// Legacy fallback only: strip {namespace} token from old templates.
	canonicalGraph.SubstituteServerPatterns("")

	// Serialize the graph to JSON
	graphJSON, err := json.Marshal(canonicalGraph)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to serialize graph: "+err.Error())
		return
	}

	// Check if QUIC handler is available
	if s.quicHandler == nil {
		writeError(w, http.StatusServiceUnavailable, "QUIC transport not available")
		return
	}

	// Get or create AgentBehaviorTree record first
	abt, _ := s.repo.GetAgentBehaviorTree(agentID, graphID)
	if abt == nil {
		// Create new assignment
		abt = &db.AgentBehaviorTree{
			ID:               uuid.New().String(),
			AgentID:          agentID,
			BehaviorTreeID:   graphID,
			ServerVersion:    dbGraph.Version,
			DeployedVersion:  0,
			DeploymentStatus: "deploying",
			Enabled:          true,
			Priority:         0,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		s.repo.CreateAgentBehaviorTree(abt)
	} else {
		// Update existing assignment before QUIC deploy
		abt.DeploymentStatus = "deploying"
		abt.ServerVersion = dbGraph.Version
		abt.DeploymentError = sql.NullString{}
		abt.UpdatedAt = time.Now()
		s.repo.UpdateAgentBehaviorTree(abt)
	}

	// Deploy via QUIC
	result, err := s.quicHandler.DeployCanonicalGraph(r.Context(), agentID, graphJSON)
	if err != nil {
		// Update database to reflect failed status
		abt.DeploymentStatus = "failed"
		abt.DeploymentError = sql.NullString{String: err.Error(), Valid: true}
		abt.UpdatedAt = time.Now()
		s.repo.UpdateAgentBehaviorTree(abt)

		// Create deployment log for the failure
		deployLog := &db.BehaviorTreeDeploymentLog{
			ID:                  uuid.New().String(),
			AgentBehaviorTreeID: abt.ID,
			Action:              "deploy",
			Version:             dbGraph.Version,
			Status:              "failed",
			ErrorMessage:        sql.NullString{String: err.Error(), Valid: true},
			InitiatedAt:         time.Now(),
			CompletedAt:         sql.NullTime{Time: time.Now(), Valid: true},
		}
		s.repo.CreateDeploymentLog(deployLog)

		// Return 200 with success=false so frontend can handle it properly
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":           false,
			"behavior_tree_id":  graphID,
			"agent_id":          agentID,
			"version":           dbGraph.Version,
			"error":             err.Error(),
			"deployment_status": "failed",
		})
		return
	}

	if result.Success {
		abt.DeployedVersion = dbGraph.Version
		abt.DeploymentStatus = "deployed"
		abt.DeployedAt = sql.NullTime{Time: time.Now(), Valid: true}

		// Cache the deployed graph for fast lookup during task execution
		s.stateManager.GraphCache().SetDeployed(agentID, graphID, canonicalGraph)
	} else {
		abt.DeploymentStatus = "failed"
		abt.DeploymentError = sql.NullString{String: result.Error, Valid: true}
	}
	abt.UpdatedAt = time.Now()
	s.repo.UpdateAgentBehaviorTree(abt)

	// Create deployment log
	deployLog := &db.BehaviorTreeDeploymentLog{
		ID:                  uuid.New().String(),
		AgentBehaviorTreeID: abt.ID,
		Action:              "deploy",
		Version:             dbGraph.Version,
		Status:              "success",
		InitiatedAt:         time.Now(),
		CompletedAt:         sql.NullTime{Time: time.Now(), Valid: true},
	}
	if !result.Success {
		deployLog.Status = "failed"
		deployLog.ErrorMessage = sql.NullString{String: result.Error, Valid: true}
	}
	s.repo.CreateDeploymentLog(deployLog)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":           result.Success,
		"behavior_tree_id":  graphID,
		"agent_id":          agentID,
		"version":           dbGraph.Version,
		"checksum":          canonicalGraph.Checksum,
		"error":             result.Error,
		"deployment_status": abt.DeploymentStatus,
	})
}

// ExportBehaviorTree exports a behavior tree in canonical JSON format
// GET /api/behavior-trees/{graphID}/export
func (s *Server) ExportBehaviorTree(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")
	if graphID == "" {
		writeError(w, http.StatusBadRequest, "Graph ID is required")
		return
	}

	// Fetch the behavior tree from DB
	dbGraph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Behavior tree not found")
		return
	}

	// Convert to canonical format
	canonicalGraph, err := graphpkg.FromDBModel(dbGraph)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to convert graph: %v", err))
		return
	}

	// Compute fresh checksum
	canonicalGraph.Checksum = canonicalGraph.ComputeChecksum()

	// Set headers for file download
	filename := fmt.Sprintf("%s_v%d.json", graphID, dbGraph.Version)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	// Write JSON response
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ") // Pretty print for readability
	if err := encoder.Encode(canonicalGraph); err != nil {
		log.Printf("Failed to encode export: %v", err)
	}
}

// ImportBehaviorTree imports a behavior tree from canonical JSON format
// POST /api/behavior-trees/import
func (s *Server) ImportBehaviorTree(w http.ResponseWriter, r *http.Request) {
	var canonicalGraph graphpkg.CanonicalGraph
	if err := json.NewDecoder(r.Body).Decode(&canonicalGraph); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate basic structure
	if err := canonicalGraph.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid graph structure: %v", err))
		return
	}

	// Verify checksum if provided
	if canonicalGraph.Checksum != "" {
		expectedChecksum := canonicalGraph.ComputeChecksum()
		if canonicalGraph.Checksum != expectedChecksum {
			writeError(w, http.StatusBadRequest, "Checksum mismatch: graph may have been modified")
			return
		}
	}

	// Check if graph already exists
	existingGraph, _ := s.repo.GetBehaviorTree(canonicalGraph.BehaviorTree.ID)
	if existingGraph != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("Behavior tree '%s' already exists. Use update endpoint or delete first.", canonicalGraph.BehaviorTree.ID))
		return
	}

	// Convert to DB model
	dbModel, err := graphpkg.ToDBModel(&canonicalGraph)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to convert graph: %v", err))
		return
	}

	// Set timestamps
	now := time.Now()
	dbModel.CreatedAt = now
	dbModel.UpdatedAt = now

	// Save to database
	if err := s.repo.CreateBehaviorTree(dbModel); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save graph: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":       dbModel.ID,
		"name":     dbModel.Name,
		"version":  dbModel.Version,
		"message":  "Behavior tree imported successfully",
		"checksum": canonicalGraph.Checksum,
	})
}

package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"central_server_go/internal/db"
	graphpkg "central_server_go/internal/graph"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ListActionGraphs returns all action graphs
func (s *Server) ListActionGraphs(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	includeTemplates := r.URL.Query().Get("include_templates") == "true"

	graphs, err := s.repo.GetActionGraphs(agentID, includeTemplates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]ActionGraphListResponse, len(graphs))
	for i, graph := range graphs {
		responses[i] = actionGraphToListResponse(&graph, s.repo)
	}

	writeJSON(w, http.StatusOK, responses)
}

// CreateActionGraph creates a new action graph
func (s *Server) CreateActionGraph(w http.ResponseWriter, r *http.Request) {
	var req ActionGraphCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}

	// Check if ID already exists
	existing, _ := s.repo.GetActionGraph(req.ID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Action Graph already exists")
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
	normalizedSteps := normalizeActionGraphSteps(req.Steps)
	stepsJSON, _ := json.Marshal(normalizedSteps)

	graph := &db.ActionGraph{
		ID:            req.ID,
		Name:          req.Name,
		Preconditions: datatypes.JSON(preconditionsJSON),
		Steps:         datatypes.JSON(stepsJSON),
		Version:       1,
		IsTemplate:    req.AgentID == "",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if req.Description != "" {
		graph.Description = sql.NullString{String: req.Description, Valid: true}
	}
	if req.AgentID != "" {
		graph.AgentID = sql.NullString{String: req.AgentID, Valid: true}
	}

	if err := s.repo.CreateActionGraph(graph); err != nil {
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

	// Auto-create AgentActionGraph if agent_id is provided
	if req.AgentID != "" {
		aag := &db.AgentActionGraph{
			ID:               uuid.New().String(),
			AgentID:          req.AgentID,
			ActionGraphID:    graph.ID,
			ServerVersion:    graph.Version,
			DeployedVersion:  0,
			DeploymentStatus: "pending",
			Enabled:          true,
			Priority:         0,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		s.repo.CreateAgentActionGraph(aag)
	}

	writeJSON(w, http.StatusCreated, actionGraphToResponse(graph, s.repo))
}

// GetActionGraph returns a single action graph
func (s *Server) GetActionGraph(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	graph, err := s.repo.GetActionGraph(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found")
		return
	}

	writeJSON(w, http.StatusOK, actionGraphToResponse(graph, s.repo))
}

// UpdateActionGraph updates an action graph
func (s *Server) UpdateActionGraph(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	graph, err := s.repo.GetActionGraph(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found")
		return
	}

	var req ActionGraphUpdateRequest
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
	if req.Steps != nil {
		normalizedSteps := normalizeActionGraphSteps(req.Steps)
		stepsJSON, _ := json.Marshal(normalizedSteps)
		graph.Steps = datatypes.JSON(stepsJSON)
	}

	graph.Version++
	graph.UpdatedAt = time.Now()

	if err := s.repo.UpdateActionGraph(graph); err != nil {
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

	// Update server_version in AgentActionGraph if exists
	if graph.AgentID.Valid {
		aag, _ := s.repo.GetAgentActionGraph(graph.AgentID.String, graph.ID)
		if aag != nil {
			aag.ServerVersion = graph.Version
			if aag.DeployedVersion > 0 && aag.DeployedVersion < graph.Version {
				aag.DeploymentStatus = "outdated"
			}
			aag.UpdatedAt = time.Now()
			s.repo.UpdateAgentActionGraph(aag)
		}
	}

	// Log affected agents for visibility
	if len(affectedAgents) > 0 {
		// These agents need to be notified about the graph update
		// TODO: Send version mismatch notification via QUIC
		_ = affectedAgents
	}

	writeJSON(w, http.StatusOK, actionGraphToResponse(graph, s.repo))
}

// DeleteActionGraph deletes an action graph
func (s *Server) DeleteActionGraph(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	// Invalidate cache before deletion
	s.stateManager.GraphCache().InvalidateAllDeployments(graphID)

	if err := s.repo.DeleteActionGraph(graphID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Action Graph deleted",
	})
}

// ExecuteActionGraph starts executing an action graph on a robot
func (s *Server) ExecuteActionGraph(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	var req ActionGraphExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.RobotID == "" {
		writeError(w, http.StatusBadRequest, "robot_id is required")
		return
	}

	taskID, err := s.scheduler.StartTask(r.Context(), graphID, req.RobotID, req.Params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"task_id":         taskID,
		"action_graph_id": graphID,
		"robot_id":        req.RobotID,
		"status":          "running",
		"message":         "Action Graph execution started",
	})
}

// ValidateActionGraph validates an action graph configuration
func (s *Server) ValidateActionGraph(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	graph, err := s.repo.GetActionGraph(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found")
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
		warnings = append(warnings, "Action Graph has no terminal steps")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":    len(errors) == 0,
		"errors":   errors,
		"warnings": warnings,
	})
}

// Helper functions

func actionGraphToListResponse(graph *db.ActionGraph, repo *db.Repository) ActionGraphListResponse {
	response := ActionGraphListResponse{
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

	// Count steps
	var steps []interface{}
	json.Unmarshal(graph.Steps, &steps)
	response.StepCount = len(steps)

	// Get deployment status
	if graph.AgentID.Valid {
		aag, _ := repo.GetAgentActionGraph(graph.AgentID.String, graph.ID)
		if aag != nil {
			response.DeploymentStatus = aag.DeploymentStatus
		}
	}

	return response
}

func actionGraphToResponse(graph *db.ActionGraph, repo *db.Repository) ActionGraphResponse {
	response := ActionGraphResponse{
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
		agent, _ := repo.GetAgent(graph.AgentID.String)
		if agent != nil {
			response.AgentName = agent.Name
		}
	}

	// Parse JSON fields
	if graph.Preconditions != nil {
		json.Unmarshal(graph.Preconditions, &response.Preconditions)
	}
	if graph.Steps != nil {
		json.Unmarshal(graph.Steps, &response.Steps)
	}

	// Get deployment status
	if graph.AgentID.Valid {
		aag, _ := repo.GetAgentActionGraph(graph.AgentID.String, graph.ID)
		if aag != nil {
			response.DeploymentStatus = aag.DeploymentStatus
		}
	}

	return response
}

// =============================================================================
// Canonical Graph Endpoints
// =============================================================================

// GetCanonicalGraph returns an action graph in canonical format
// This format is optimized for graph operations and Agent communication
func (s *Server) GetCanonicalGraph(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	dbGraph, err := s.repo.GetActionGraph(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dbGraph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found")
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

	dbGraph, err := s.repo.GetActionGraph(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dbGraph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found")
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

// DeployActionGraphToAgent deploys an action graph to an agent using canonical format
func (s *Server) DeployActionGraphToAgent(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")
	agentID := chi.URLParam(r, "agentID")

	// Get the action graph
	dbGraph, err := s.repo.GetActionGraph(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dbGraph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found")
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
	canonicalGraph.ActionGraph.AgentID = agentID

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

	// Deploy via QUIC
	result, err := s.quicHandler.DeployCanonicalGraph(r.Context(), agentID, graphJSON)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Deployment failed: "+err.Error())
		return
	}

	// Update AgentActionGraph record
	aag, _ := s.repo.GetAgentActionGraph(agentID, graphID)
	if aag == nil {
		// Create new assignment
		aag = &db.AgentActionGraph{
			ID:               uuid.New().String(),
			AgentID:          agentID,
			ActionGraphID:    graphID,
			ServerVersion:    dbGraph.Version,
			DeployedVersion:  0,
			DeploymentStatus: "deploying",
			Enabled:          true,
			Priority:         0,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		s.repo.CreateAgentActionGraph(aag)
	}

	if result.Success {
		aag.DeployedVersion = dbGraph.Version
		aag.DeploymentStatus = "deployed"
		aag.DeployedAt = sql.NullTime{Time: time.Now(), Valid: true}

		// Cache the deployed graph for fast lookup during task execution
		s.stateManager.GraphCache().SetDeployed(agentID, graphID, canonicalGraph)
	} else {
		aag.DeploymentStatus = "failed"
		aag.DeploymentError = sql.NullString{String: result.Error, Valid: true}
	}
	aag.UpdatedAt = time.Now()
	s.repo.UpdateAgentActionGraph(aag)

	// Create deployment log
	deployLog := &db.ActionGraphDeploymentLog{
		ID:                 uuid.New().String(),
		AgentActionGraphID: aag.ID,
		Action:             "deploy",
		Version:            dbGraph.Version,
		Status:             "success",
		InitiatedAt:        time.Now(),
		CompletedAt:        sql.NullTime{Time: time.Now(), Valid: true},
	}
	if !result.Success {
		deployLog.Status = "failed"
		deployLog.ErrorMessage = sql.NullString{String: result.Error, Valid: true}
	}
	s.repo.CreateDeploymentLog(deployLog)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":          result.Success,
		"action_graph_id":  graphID,
		"agent_id":         agentID,
		"version":          dbGraph.Version,
		"checksum":         canonicalGraph.Checksum,
		"error":            result.Error,
		"deployment_status": aag.DeploymentStatus,
	})
}

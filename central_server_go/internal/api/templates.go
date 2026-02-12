package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"central_server_go/internal/db"
	graphpkg "central_server_go/internal/graph"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ============================================
// Response Models
// ============================================

// TemplateListItem represents template summary for list view
type TemplateListItem struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Description         string     `json:"description,omitempty"`
	RequiredActionTypes []string   `json:"required_action_types,omitempty"`
	StepCount           int        `json:"step_count"`
	Version             int        `json:"version"`
	AssignmentCount     int        `json:"assignment_count"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
}

// TemplateAssignmentInfo represents a template assignment to an agent
type TemplateAssignmentInfo struct {
	ID               string     `json:"id"`
	AgentID          string     `json:"agent_id"`
	AgentName        string     `json:"agent_name"`
	BehaviorTreeID   string     `json:"behavior_tree_id"`
	BehaviorTreeName string     `json:"behavior_tree_name"`
	ServerVersion    int        `json:"server_version"`
	DeployedVersion  *int       `json:"deployed_version,omitempty"`
	DeploymentStatus string     `json:"deployment_status"`
	Enabled          bool       `json:"enabled"`
	DeployedAt       *time.Time `json:"deployed_at,omitempty"`
}

// AssignTemplateRequest represents request to assign a template
type AssignTemplateRequest struct {
	AgentID  string `json:"agent_id"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`
}

// ============================================
// Template Endpoints
// ============================================

// ListTemplates returns all templates
func (s *Server) ListTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.repo.GetTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result := make([]TemplateListItem, 0, len(templates))

	for _, t := range templates {
		// Count assignments
		assignmentCount := s.repo.CountTemplateAssignments(t.ID)

		// Parse steps to count
		var steps []interface{}
		if t.Steps != nil {
			json.Unmarshal(t.Steps, &steps)
		}

		// Parse required action types
		var requiredActionTypes []string
		if t.RequiredActionTypes != nil {
			json.Unmarshal(t.RequiredActionTypes, &requiredActionTypes)
		}

		var updatedAt *time.Time
		if !t.UpdatedAt.IsZero() {
			updatedAt = &t.UpdatedAt
		}

		result = append(result, TemplateListItem{
			ID:                  t.ID,
			Name:                t.Name,
			Description:         t.Description.String,
			RequiredActionTypes: requiredActionTypes,
			StepCount:           len(steps),
			Version:             t.Version,
			AssignmentCount:     assignmentCount,
			CreatedAt:           t.CreatedAt,
			UpdatedAt:           updatedAt,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// CreateTemplate creates a new behavior tree template
func (s *Server) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req BehaviorTreeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate: templates should not have agent_id
	if req.AgentID != "" {
		writeError(w, http.StatusBadRequest, "Templates cannot have agent_id")
		return
	}

	// Check if ID already exists
	existing, _ := s.repo.GetBehaviorTree(req.ID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Behavior Tree already exists: "+req.ID)
		return
	}

	// Create behavior tree as template
	graph := &db.BehaviorTree{
		ID:          req.ID,
		Name:        req.Name,
		Version:     1,
		IsTemplate:  true,
	}

	if req.Description != "" {
		graph.Description.String = req.Description
		graph.Description.Valid = true
	}
	if req.EntryPoint != "" {
		graph.EntryPoint = sql.NullString{String: req.EntryPoint, Valid: true}
	}

	if req.Steps != nil {
		normalizedSteps := normalizeBehaviorTreeSteps(req.Steps)
		stepsJSON, _ := json.Marshal(normalizedSteps)
		graph.Steps = stepsJSON

		// Extract required action types from steps (capability-based)
		var steps []db.BehaviorTreeStep
		json.Unmarshal(stepsJSON, &steps)
		requiredTypes := db.ExtractActionTypesFromSteps(steps)
		if len(requiredTypes) > 0 {
			typesJSON, _ := json.Marshal(requiredTypes)
			graph.RequiredActionTypes = typesJSON
		}
	}

	if req.Preconditions != nil {
		preconJSON, _ := json.Marshal(req.Preconditions)
		graph.Preconditions = preconJSON
	}

	if err := s.repo.CreateBehaviorTree(graph); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, behaviorTreeToResponse(graph, s.repo))
}

// GetTemplate returns a specific template
func (s *Server) GetTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")

	template, err := s.repo.GetTemplate(templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if template == nil {
		writeError(w, http.StatusNotFound, "Template not found: "+templateID)
		return
	}

	writeJSON(w, http.StatusOK, behaviorTreeToResponse(template, s.repo))
}

// UpdateTemplate updates a template
func (s *Server) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")

	template, err := s.repo.GetTemplate(templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if template == nil {
		writeError(w, http.StatusNotFound, "Template not found: "+templateID)
		return
	}

	var req BehaviorTreeUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update fields
	if req.Name != "" {
		template.Name = req.Name
	}
	if req.Description != "" {
		template.Description.String = req.Description
		template.Description.Valid = true
	}
	if req.EntryPoint != "" {
		template.EntryPoint = sql.NullString{String: req.EntryPoint, Valid: true}
	}
	if req.Steps != nil {
		normalizedSteps := normalizeBehaviorTreeSteps(req.Steps)
		stepsJSON, _ := json.Marshal(normalizedSteps)
		template.Steps = stepsJSON

		// Re-extract required action types from updated steps (capability-based)
		var steps []db.BehaviorTreeStep
		json.Unmarshal(stepsJSON, &steps)
		requiredTypes := db.ExtractActionTypesFromSteps(steps)
		if len(requiredTypes) > 0 {
			typesJSON, _ := json.Marshal(requiredTypes)
			template.RequiredActionTypes = typesJSON
		} else {
			template.RequiredActionTypes = []byte("[]")
		}
	}
	if req.Preconditions != nil {
		preconJSON, _ := json.Marshal(req.Preconditions)
		template.Preconditions = preconJSON
	}

	template.Version++
	template.UpdatedAt = time.Now()

	if err := s.repo.UpdateBehaviorTree(template); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Mark all assignments as outdated
	s.repo.MarkTemplateAssignmentsOutdated(templateID, template.Version)

	writeJSON(w, http.StatusOK, behaviorTreeToResponse(template, s.repo))
}

// DeleteTemplate deletes a template and all its assignments
func (s *Server) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")

	template, err := s.repo.GetTemplate(templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if template == nil {
		writeError(w, http.StatusNotFound, "Template not found: "+templateID)
		return
	}

	// Delete assignments first
	s.repo.DeleteTemplateAssignments(templateID)

	// Delete template
	if err := s.repo.DeleteBehaviorTree(templateID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Template " + templateID + " deleted",
	})
}

// ============================================
// Assignment Endpoints
// ============================================

// GetTemplateAssignments returns all agents that have this template assigned
func (s *Server) GetTemplateAssignments(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")

	template, err := s.repo.GetTemplate(templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if template == nil {
		writeError(w, http.StatusNotFound, "Template not found: "+templateID)
		return
	}

	assignments, err := s.repo.GetAgentBehaviorTreesByGraphID(templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result := make([]TemplateAssignmentInfo, 0, len(assignments))

	for _, a := range assignments {
		agent, _ := s.repo.GetAgent(a.AgentID)
		agentName := a.AgentID
		if agent != nil {
			agentName = agent.Name
		}

		var deployedVersion *int
		if a.DeployedVersion > 0 {
			v := a.DeployedVersion
			deployedVersion = &v
		}

		var deployedAt *time.Time
		if a.DeployedAt.Valid {
			deployedAt = &a.DeployedAt.Time
		}

		result = append(result, TemplateAssignmentInfo{
			ID:               a.ID,
			AgentID:          a.AgentID,
			AgentName:        agentName,
			BehaviorTreeID:    templateID,
			BehaviorTreeName:  template.Name,
			ServerVersion:    a.ServerVersion,
			DeployedVersion:  deployedVersion,
			DeploymentStatus: a.DeploymentStatus,
			Enabled:          a.Enabled,
			DeployedAt:       deployedAt,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// GetTemplateCompatibleAgents returns all agents with their compatibility status for a template
func (s *Server) GetTemplateCompatibleAgents(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")

	template, err := s.repo.GetTemplate(templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if template == nil {
		writeError(w, http.StatusNotFound, "Template not found: "+templateID)
		return
	}

	// Extract required action types from template
	var requiredTypes []string
	if template.RequiredActionTypes != nil {
		json.Unmarshal(template.RequiredActionTypes, &requiredTypes)
	}

	// Get all agents with their compatibility status
	agentInfos, err := s.repo.FindAgentsWithCompatibility(requiredTypes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get already assigned agent IDs
	assignments, _ := s.repo.GetAgentBehaviorTreesByGraphID(templateID)
	assignedAgents := make(map[string]bool)
	for _, a := range assignments {
		assignedAgents[a.AgentID] = true
	}

	result := make([]CompatibleAgentResponse, 0, len(agentInfos))
	for _, info := range agentInfos {
		// Skip already assigned agents
		if assignedAgents[info.Agent.ID] {
			continue
		}
		// Only include currently online agents in compatibility list.
		if !s.stateManager.IsAgentOnline(info.Agent.ID) {
			continue
		}

		result = append(result, CompatibleAgentResponse{
			AgentID:             info.Agent.ID,
			AgentName:           info.Agent.Name,
			Status:              "online",
			HasAllCapabilities:  info.HasAllCapabilities,
			MissingCapabilities: info.MissingCapabilities,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"template_id":           templateID,
		"required_action_types": requiredTypes,
		"agents":                result,
	})
}

// AssignTemplateToAgent assigns a template to an agent
func (s *Server) AssignTemplateToAgent(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")

	template, err := s.repo.GetTemplate(templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if template == nil {
		writeError(w, http.StatusNotFound, "Template not found: "+templateID)
		return
	}

	var req AssignTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	agent, err := s.repo.GetAgent(req.AgentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found: "+req.AgentID)
		return
	}

	// Extract required action types from template
	var requiredTypes []string
	if template.RequiredActionTypes != nil {
		json.Unmarshal(template.RequiredActionTypes, &requiredTypes)
	}

	// Check if agent has all required capabilities
	if len(requiredTypes) > 0 {
		agentTypes, err := s.repo.GetAgentActionTypes(req.AgentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		agentTypeSet := make(map[string]bool)
		for _, at := range agentTypes {
			agentTypeSet[at] = true
		}

		missing := make([]string, 0)
		for _, required := range requiredTypes {
			if !agentTypeSet[required] {
				missing = append(missing, required)
			}
		}

		if len(missing) > 0 {
			writeError(w, http.StatusBadRequest, "Agent missing required capabilities: "+stringSliceToString(missing))
			return
		}
	}

	// Check if already assigned
	existing, _ := s.repo.GetAgentBehaviorTree(req.AgentID, templateID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Template already assigned to agent")
		return
	}

	// Create assignment
	assignment := &db.AgentBehaviorTree{
		ID:               uuid.New().String(),
		AgentID:          req.AgentID,
		BehaviorTreeID:    templateID,
		ServerVersion:    template.Version,
		DeploymentStatus: "pending",
		Enabled:          req.Enabled,
		Priority:         req.Priority,
	}

	if err := s.repo.CreateAgentBehaviorTree(assignment); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Trigger deployment if agent is online and enabled
	if agent.Status == "online" && req.Enabled {
		go s.deployBehaviorTreeToAgent(assignment.ID)
	}

	writeJSON(w, http.StatusCreated, TemplateAssignmentInfo{
		ID:               assignment.ID,
		AgentID:          assignment.AgentID,
		AgentName:        agent.Name,
		BehaviorTreeID:    templateID,
		BehaviorTreeName:  template.Name,
		ServerVersion:    assignment.ServerVersion,
		DeploymentStatus: assignment.DeploymentStatus,
		Enabled:          assignment.Enabled,
	})
}

// stringSliceToString converts string slice to comma-separated string
func stringSliceToString(slice []string) string {
	result := ""
	for i, s := range slice {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

// DeployTemplateAssignment deploys a template assignment
func (s *Server) DeployTemplateAssignment(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	agentID := chi.URLParam(r, "agentID")

	assignment, err := s.repo.GetAgentBehaviorTree(agentID, templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if assignment == nil {
		writeError(w, http.StatusNotFound, "Assignment not found")
		return
	}

	// Trigger deployment with request context
	result := s.deployBehaviorTreeToAgentSync(r.Context(), assignment.ID)

	writeJSON(w, http.StatusOK, result)
}

// UnassignTemplate removes template assignment from an agent
func (s *Server) UnassignTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	agentID := chi.URLParam(r, "agentID")

	assignment, err := s.repo.GetAgentBehaviorTree(agentID, templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if assignment == nil {
		writeError(w, http.StatusNotFound, "Assignment not found")
		return
	}

	// Delete deployment logs
	s.repo.DeleteDeploymentLogsForAssignment(assignment.ID)

	// Delete assignment
	if err := s.repo.DeleteAgentBehaviorTree(agentID, templateID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Template unassigned from agent",
	})
}

// ============================================
// Agent View Endpoints
// ============================================

// AgentOverviewActionServer represents an individual action server in agent overview
type AgentOverviewActionServer struct {
	ActionServer string `json:"action_server"` // e.g., "/test_A_action"
	ActionType   string `json:"action_type"`   // e.g., "test_msgs/TestAction"
	IsAvailable  bool   `json:"is_available"`
	Status       string `json:"status"`
}

// AgentOverviewInfo represents agent overview information
type AgentOverviewInfo struct {
	AgentID           string                      `json:"agent_id"`
	AgentName         string                      `json:"agent_name"`
	Status            string                      `json:"status"`
	RobotCount        int                         `json:"robot_count"`
	ActionTypes       []string                    `json:"action_types"`        // Grouped by type (backward compat)
	ActionServers     []AgentOverviewActionServer `json:"action_servers"`      // Individual servers
	AssignedTemplates []map[string]interface{}    `json:"assigned_templates"`
}

// GetAgentsOverview returns all agents with their capabilities and assigned templates
// Optimized: Uses batch queries to avoid N+1 query problem
func (s *Server) GetAgentsOverview(w http.ResponseWriter, r *http.Request) {
	// Step 1: Batch load all data in 4 queries instead of N+1
	agents, err := s.repo.GetAllAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get all capabilities in one query
	allCapabilities, err := s.repo.GetAllAgentCapabilities()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get all agent behavior tree assignments in one query
	allAssignments, err := s.repo.GetAllAgentBehaviorTrees()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Collect all behavior tree IDs we need to fetch
	graphIDSet := make(map[string]bool)
	for _, a := range allAssignments {
		graphIDSet[a.BehaviorTreeID] = true
	}
	graphIDs := make([]string, 0, len(graphIDSet))
	for id := range graphIDSet {
		graphIDs = append(graphIDs, id)
	}

	// Get all behavior trees by IDs in one query
	graphsMap, err := s.repo.GetBehaviorTreesByIDs(graphIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Step 2: Build lookup maps for O(1) access
	// Capabilities by agent ID
	capsByAgent := make(map[string][]db.AgentCapability)
	for _, cap := range allCapabilities {
		capsByAgent[cap.AgentID] = append(capsByAgent[cap.AgentID], cap)
	}

	// Assignments by agent ID
	assignmentsByAgent := make(map[string][]db.AgentBehaviorTree)
	for _, a := range allAssignments {
		assignmentsByAgent[a.AgentID] = append(assignmentsByAgent[a.AgentID], a)
	}

	// Step 3: Build result using lookup maps (no more DB queries in loop)
	result := make([]AgentOverviewInfo, 0, len(agents))

	for _, agent := range agents {
		// Get capabilities from map
		agentCaps := capsByAgent[agent.ID]

		// Build action types (deduplicated) and action servers
		typeSet := make(map[string]bool)
		actionServers := make([]AgentOverviewActionServer, 0, len(agentCaps))
		for _, cap := range agentCaps {
			typeSet[cap.ActionType] = true
			actionServers = append(actionServers, AgentOverviewActionServer{
				ActionServer: cap.ActionServer,
				ActionType:   cap.ActionType,
				IsAvailable:  cap.IsAvailable,
				Status:       cap.Status,
			})
		}
		actionTypes := make([]string, 0, len(typeSet))
		for t := range typeSet {
			actionTypes = append(actionTypes, t)
		}

		// Get assignments from map
		assignments := assignmentsByAgent[agent.ID]

		assignedTemplates := make([]map[string]interface{}, 0, len(assignments))
		for _, a := range assignments {
			graph := graphsMap[a.BehaviorTreeID]
			if graph != nil {
				var deployedVersion interface{}
				if a.DeployedVersion > 0 {
					deployedVersion = a.DeployedVersion
				}

				assignedTemplates = append(assignedTemplates, map[string]interface{}{
					"assignment_id":    a.ID,
					"template_id":      graph.ID,
					"template_name":    graph.Name,
					"version":          graph.Version,
					"deployed_version": deployedVersion,
					"status":           a.DeploymentStatus,
					"enabled":          a.Enabled,
				})
			}
		}

		result = append(result, AgentOverviewInfo{
			AgentID:           agent.ID,
			AgentName:         agent.Name,
			Status:            agent.Status,
			RobotCount:        1, // 1 Agent = 1 Robot in this architecture
			ActionTypes:       actionTypes,
			ActionServers:     actionServers,
			AssignedTemplates: assignedTemplates,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// GetAvailableTemplatesForAgent returns templates that can be assigned to an agent based on capabilities
func (s *Server) GetAvailableTemplatesForAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found: "+agentID)
		return
	}

	// Get agent's action types (capabilities)
	agentActionTypes, _ := s.repo.GetAgentActionTypes(agentID)
	agentTypeSet := make(map[string]bool)
	for _, at := range agentActionTypes {
		agentTypeSet[at] = true
	}

	// Get all templates
	templates, _ := s.repo.GetTemplates()

	// Get already assigned template IDs
	assignedIDs := s.repo.GetAssignedTemplateIDs(agentID)

	result := make([]TemplateListItem, 0)

	for _, t := range templates {
		// Skip already assigned
		if _, assigned := assignedIDs[t.ID]; assigned {
			continue
		}

		// Parse required action types
		var requiredTypes []string
		if t.RequiredActionTypes != nil {
			json.Unmarshal(t.RequiredActionTypes, &requiredTypes)
		}

		// Check if agent has all required capabilities
		compatible := true
		for _, required := range requiredTypes {
			if !agentTypeSet[required] {
				compatible = false
				break
			}
		}

		// Only include compatible templates
		if !compatible {
			continue
		}

		var steps []interface{}
		if t.Steps != nil {
			json.Unmarshal(t.Steps, &steps)
		}

		var updatedAt *time.Time
		if !t.UpdatedAt.IsZero() {
			updatedAt = &t.UpdatedAt
		}

		result = append(result, TemplateListItem{
			ID:                  t.ID,
			Name:                t.Name,
			Description:         t.Description.String,
			RequiredActionTypes: requiredTypes,
			StepCount:           len(steps),
			Version:             t.Version,
			CreatedAt:           t.CreatedAt,
			UpdatedAt:           updatedAt,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// deployBehaviorTreeToAgent triggers async deployment
func (s *Server) deployBehaviorTreeToAgent(assignmentID string) {
	ctx := context.Background()
	s.deployBehaviorTreeToAgentSync(ctx, assignmentID)
}

// deployBehaviorTreeToAgentSync performs synchronous deployment
func (s *Server) deployBehaviorTreeToAgentSync(ctx context.Context, assignmentID string) map[string]interface{} {
	assignment, err := s.repo.GetAgentBehaviorTreeByID(assignmentID)
	if err != nil || assignment == nil {
		return map[string]interface{}{
			"status": "failed",
			"error":  "Assignment not found",
		}
	}

	graph, err := s.repo.GetBehaviorTree(assignment.BehaviorTreeID)
	if err != nil || graph == nil {
		return map[string]interface{}{
			"status": "failed",
			"error":  "Behavior tree not found",
		}
	}

	agent, err := s.repo.GetAgent(assignment.AgentID)
	if err != nil || agent == nil {
		return map[string]interface{}{
			"status": "failed",
			"error":  "Agent not found",
		}
	}

	// Check if agent is online
	if agent.Status != "online" {
		assignment.DeploymentStatus = "pending"
		s.repo.UpdateAgentBehaviorTree(assignment)
		return map[string]interface{}{
			"status": "queued",
		}
	}

	// Update status to deploying
	assignment.DeploymentStatus = "deploying"
	assignment.ServerVersion = graph.Version
	s.repo.UpdateAgentBehaviorTree(assignment)

	// Create deployment log
	correlationID := uuid.New().String()
	deployLog := &db.BehaviorTreeDeploymentLog{
		ID:                 uuid.New().String(),
		AgentBehaviorTreeID: assignmentID,
		Action:             "deploy",
		Version:            graph.Version,
		Status:             "initiated",
	}
	s.repo.CreateDeploymentLog(deployLog)

	// Deploy via QUIC if handler is available
	if s.quicHandler != nil {
		// Convert to canonical format
		canonicalGraph, err := graphpkg.FromDBModel(graph)
		if err != nil {
			assignment.DeploymentStatus = "failed"
			assignment.DeploymentError.String = "Failed to convert to canonical format: " + err.Error()
			assignment.DeploymentError.Valid = true
			deployLog.Status = "failed"
			deployLog.ErrorMessage.String = assignment.DeploymentError.String
			deployLog.ErrorMessage.Valid = true
			deployLog.CompletedAt.Time = time.Now()
			deployLog.CompletedAt.Valid = true
			s.repo.UpdateAgentBehaviorTree(assignment)
			s.repo.UpdateDeploymentLog(deployLog)
			return map[string]interface{}{
				"correlation_id": correlationID,
				"status":         "failed",
				"error":          assignment.DeploymentError.String,
			}
		}

		// Set the target agent
		canonicalGraph.BehaviorTree.AgentID = assignment.AgentID

		// Legacy fallback only: strip {namespace} token from old templates.
		canonicalGraph.SubstituteServerPatterns("")

		// Serialize to JSON
		graphJSON, err := json.Marshal(canonicalGraph)
		if err != nil {
			assignment.DeploymentStatus = "failed"
			assignment.DeploymentError.String = "Failed to serialize graph: " + err.Error()
			assignment.DeploymentError.Valid = true
			deployLog.Status = "failed"
			deployLog.ErrorMessage.String = assignment.DeploymentError.String
			deployLog.ErrorMessage.Valid = true
			deployLog.CompletedAt.Time = time.Now()
			deployLog.CompletedAt.Valid = true
			s.repo.UpdateAgentBehaviorTree(assignment)
			s.repo.UpdateDeploymentLog(deployLog)
			return map[string]interface{}{
				"correlation_id": correlationID,
				"status":         "failed",
				"error":          assignment.DeploymentError.String,
			}
		}

		result, err := s.quicHandler.DeployCanonicalGraph(ctx, assignment.AgentID, graphJSON)

		if err != nil {
			assignment.DeploymentStatus = "failed"
			assignment.DeploymentError.String = err.Error()
			assignment.DeploymentError.Valid = true
			deployLog.Status = "failed"
			deployLog.ErrorMessage.String = err.Error()
			deployLog.ErrorMessage.Valid = true
		} else if result.Success {
			assignment.DeploymentStatus = "deployed"
			assignment.DeployedVersion = graph.Version
			assignment.DeployedAt.Time = time.Now()
			assignment.DeployedAt.Valid = true
			deployLog.Status = "success"
		} else {
			assignment.DeploymentStatus = "failed"
			assignment.DeploymentError.String = result.Error
			assignment.DeploymentError.Valid = true
			deployLog.Status = "failed"
			deployLog.ErrorMessage.String = result.Error
			deployLog.ErrorMessage.Valid = true
		}

		deployLog.CompletedAt.Time = time.Now()
		deployLog.CompletedAt.Valid = true
		s.repo.UpdateAgentBehaviorTree(assignment)
		s.repo.UpdateDeploymentLog(deployLog)

		return map[string]interface{}{
			"correlation_id": correlationID,
			"status":         assignment.DeploymentStatus,
			"version":        graph.Version,
		}
	}

	// Fallback: simulate success if no QUIC handler
	assignment.DeploymentStatus = "deployed"
	assignment.DeployedVersion = graph.Version
	assignment.DeployedAt.Time = time.Now()
	assignment.DeployedAt.Valid = true
	deployLog.Status = "success"
	deployLog.CompletedAt.Time = time.Now()
	deployLog.CompletedAt.Valid = true

	s.repo.UpdateAgentBehaviorTree(assignment)
	s.repo.UpdateDeploymentLog(deployLog)

	return map[string]interface{}{
		"correlation_id": correlationID,
		"status":         "deployed",
		"version":        graph.Version,
	}
}

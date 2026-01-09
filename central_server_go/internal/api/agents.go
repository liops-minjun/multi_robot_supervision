package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/state"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ListAgents returns all agents
func (s *Server) ListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.repo.GetAllAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]AgentResponse, len(agents))
	for i, agent := range agents {
		responses[i] = agentToResponse(&agent, s.stateManager)
	}

	writeJSON(w, http.StatusOK, responses)
}

// GetAgent returns a single agent
func (s *Server) GetAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(agent, s.stateManager))
}

// CreateAgentRequest represents a request to create an agent
type CreateAgentRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateAgent creates a new agent
func (s *Server) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if req.Name == "" {
		req.Name = req.ID
	}

	// Check if agent already exists
	existing, _ := s.repo.GetAgent(req.ID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Agent already exists")
		return
	}

	agent := &db.Agent{
		ID:        req.ID,
		Name:      req.Name,
		Status:    "offline",
		CreatedAt: time.Now(),
	}

	if err := s.repo.CreateAgent(agent); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, agentToResponse(agent, s.stateManager))
}

// DeleteAgent deletes an agent
func (s *Server) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	// Delete related agent_action_graphs first
	if err := s.repo.DeleteAgentActionGraphsByAgent(agentID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.repo.DeleteAgent(agentID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Agent deleted",
	})
}

// ListAgentActionGraphs returns all action graphs assigned to an agent
func (s *Server) ListAgentActionGraphs(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	aags, err := s.repo.GetAgentActionGraphs(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]AgentActionGraphResponse, len(aags))
	for i, aag := range aags {
		responses[i] = agentActionGraphToResponse(&aag, s.repo)
	}

	writeJSON(w, http.StatusOK, responses)
}

// AssignActionGraph assigns an action graph to an agent
func (s *Server) AssignActionGraph(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	var req AssignActionGraphRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ActionGraphID == "" {
		writeError(w, http.StatusBadRequest, "action_graph_id is required")
		return
	}

	// Check if agent exists
	agent, _ := s.repo.GetAgent(agentID)
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	// Check if action graph exists
	graph, _ := s.repo.GetActionGraph(req.ActionGraphID)
	if graph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found")
		return
	}

	// Check if already assigned
	existing, _ := s.repo.GetAgentActionGraph(agentID, req.ActionGraphID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Action Graph already assigned to this agent")
		return
	}

	aag := &db.AgentActionGraph{
		ID:               uuid.New().String(),
		AgentID:          agentID,
		ActionGraphID:    req.ActionGraphID,
		ServerVersion:    graph.Version,
		DeployedVersion:  0,
		DeploymentStatus: "pending",
		Enabled:          req.Enabled,
		Priority:         req.Priority,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := s.repo.CreateAgentActionGraph(aag); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, agentActionGraphToResponse(aag, s.repo))
}

// GetAgentActionGraph returns a specific agent-action graph assignment
func (s *Server) GetAgentActionGraph(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	graphID := chi.URLParam(r, "graphID")

	aag, err := s.repo.GetAgentActionGraph(agentID, graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if aag == nil {
		writeError(w, http.StatusNotFound, "Assignment not found")
		return
	}

	writeJSON(w, http.StatusOK, agentActionGraphToResponse(aag, s.repo))
}

// RemoveAgentActionGraph removes an action graph assignment
func (s *Server) RemoveAgentActionGraph(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	graphID := chi.URLParam(r, "graphID")

	if err := s.repo.DeleteAgentActionGraph(agentID, graphID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Assignment removed",
	})
}

// DeployActionGraph deploys an action graph to an agent
func (s *Server) DeployActionGraph(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	graphID := chi.URLParam(r, "graphID")

	// Get assignment
	aag, err := s.repo.GetAgentActionGraph(agentID, graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if aag == nil {
		writeError(w, http.StatusNotFound, "Assignment not found")
		return
	}

	// Get action graph
	graph, err := s.repo.GetActionGraph(graphID)
	if err != nil || graph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found")
		return
	}

	// Check if agent is online
	if !s.stateManager.IsAgentOnline(agentID) {
		// Mark as pending for when agent comes online
		aag.DeploymentStatus = "pending"
		aag.UpdatedAt = time.Now()
		s.repo.UpdateAgentActionGraph(aag)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "pending",
			"message": "Agent is offline, deployment queued",
		})
		return
	}

	// Update status to deploying
	aag.DeploymentStatus = "deploying"
	aag.UpdatedAt = time.Now()
	s.repo.UpdateAgentActionGraph(aag)

	// Create deployment log
	logID := uuid.New().String()
	deployLog := &db.ActionGraphDeploymentLog{
		ID:                 logID,
		AgentActionGraphID: aag.ID,
		Action:             "deploy",
		Version:            graph.Version,
		Status:             "pending",
		InitiatedAt:        time.Now(),
	}
	s.repo.CreateDeploymentLog(deployLog)

	// TODO: Send deploy message via QUIC
	// For now, simulate immediate deployment success
	aag.DeploymentStatus = "deployed"
	aag.DeployedVersion = graph.Version
	aag.DeployedAt = sql.NullTime{Time: time.Now(), Valid: true}
	aag.UpdatedAt = time.Now()
	s.repo.UpdateAgentActionGraph(aag)

	// Update deployment log
	deployLog.Status = "success"
	completedAt := time.Now()
	deployLog.CompletedAt = sql.NullTime{Time: completedAt, Valid: true}
	s.repo.UpdateDeploymentLog(deployLog)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "deployed",
		"deployed_version": graph.Version,
		"message":          "Action Graph deployed successfully",
	})
}

// GetDeploymentLogs returns deployment logs for an agent-action graph
func (s *Server) GetDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	graphID := chi.URLParam(r, "graphID")

	// Get assignment first
	aag, err := s.repo.GetAgentActionGraph(agentID, graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if aag == nil {
		writeError(w, http.StatusNotFound, "Assignment not found")
		return
	}

	logs, err := s.repo.GetDeploymentLogs(aag.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]DeploymentLogResponse, len(logs))
	for i, log := range logs {
		responses[i] = DeploymentLogResponse{
			ID:                 log.ID,
			AgentActionGraphID: log.AgentActionGraphID,
			Action:             log.Action,
			Version:            log.Version,
			Status:             log.Status,
			InitiatedAt:        log.InitiatedAt,
		}
		if log.ErrorMessage.Valid {
			responses[i].ErrorMessage = log.ErrorMessage.String
		}
		if log.CompletedAt.Valid {
			responses[i].CompletedAt = &log.CompletedAt.Time
		}
	}

	writeJSON(w, http.StatusOK, responses)
}

// ============================================
// Agent Connection Status (Heartbeat Monitoring)
// ============================================

// GetAgentConnectionStatus returns real-time connection status for all agents
func (s *Server) GetAgentConnectionStatus(w http.ResponseWriter, r *http.Request) {
	// Get live connection status from state manager
	liveStatus := s.stateManager.GetAllAgentStatus()

	// Build response with database info merged with live status
	responses := make([]AgentConnectionStatusResponse, 0)

	// Create map of live agents for quick lookup
	liveMap := make(map[string]state.AgentStatus)
	for _, status := range liveStatus {
		liveMap[status.ID] = status
	}

	// Get all agents from database
	dbAgents, err := s.repo.GetAllAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, agent := range dbAgents {
		status := AgentConnectionStatusResponse{
			ID:       agent.ID,
			Name:     agent.Name,
			Status:   "offline",
			IsOnline: false,
			RobotIDs: make([]string, 0),
		}

		if agent.IPAddress.Valid {
			status.IPAddress = agent.IPAddress.String
		}

		// Merge with live status if available
		if live, exists := liveMap[agent.ID]; exists {
			status.Status = "online"
			status.IsOnline = true
			status.ConnectedAt = &live.ConnectedAt
			status.LastHeartbeat = &live.LastHeartbeat
			status.HeartbeatAgeMs = int64(live.HeartbeatAge.Milliseconds())
			status.HeartbeatHealth = live.HeartbeatHealth
			status.RobotIDs = live.RobotIDs
			status.IPAddress = live.IPAddress
			if !live.LastPing.IsZero() {
				status.LastPing = &live.LastPing
				pingMs := int64(live.PingLatency.Milliseconds())
				status.PingLatencyMs = &pingMs
				pingUs := int64(live.PingLatency / time.Microsecond)
				status.PingLatencyUs = &pingUs
			}
		}

		responses = append(responses, status)
	}

	// Also include any live agents not in database (auto-registered)
	for agentID, live := range liveMap {
		found := false
		for _, resp := range responses {
			if resp.ID == agentID {
				found = true
				break
			}
		}
		if !found {
			responses = append(responses, AgentConnectionStatusResponse{
				ID:              live.ID,
				Name:            live.Name,
				IPAddress:       live.IPAddress,
				Status:          "online",
				IsOnline:        true,
				ConnectedAt:     &live.ConnectedAt,
				LastHeartbeat:   &live.LastHeartbeat,
				HeartbeatAgeMs:  int64(live.HeartbeatAge.Milliseconds()),
				HeartbeatHealth: live.HeartbeatHealth,
				LastPing: func() *time.Time {
					if live.LastPing.IsZero() {
						return nil
					}
					return &live.LastPing
				}(),
				PingLatencyMs: func() *int64 {
					if live.LastPing.IsZero() {
						return nil
					}
					v := int64(live.PingLatency.Milliseconds())
					return &v
				}(),
				PingLatencyUs: func() *int64 {
					if live.LastPing.IsZero() {
						return nil
					}
					v := int64(live.PingLatency / time.Microsecond)
					return &v
				}(),
				RobotIDs: live.RobotIDs,
			})
		}
	}

	writeJSON(w, http.StatusOK, responses)
}

// GetSingleAgentConnectionStatus returns connection status for a specific agent
func (s *Server) GetSingleAgentConnectionStatus(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	// Get from database first
	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := AgentConnectionStatusResponse{
		ID:       agentID,
		Status:   "offline",
		IsOnline: false,
		RobotIDs: make([]string, 0),
	}

	if agent != nil {
		status.Name = agent.Name
		if agent.IPAddress.Valid {
			status.IPAddress = agent.IPAddress.String
		}
	}

	// Get live status
	if live, exists := s.stateManager.GetAgentStatus(agentID); exists {
		status.Status = "online"
		status.IsOnline = true
		status.Name = live.Name
		status.IPAddress = live.IPAddress
		status.ConnectedAt = &live.ConnectedAt
		status.LastHeartbeat = &live.LastHeartbeat
		status.HeartbeatAgeMs = int64(live.HeartbeatAge.Milliseconds())
		status.HeartbeatHealth = live.HeartbeatHealth
		status.RobotIDs = live.RobotIDs
		if !live.LastPing.IsZero() {
			status.LastPing = &live.LastPing
			pingMs := int64(live.PingLatency.Milliseconds())
			status.PingLatencyMs = &pingMs
			pingUs := int64(live.PingLatency / time.Microsecond)
			status.PingLatencyUs = &pingUs
		}
	}

	if agent == nil && !status.IsOnline {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// ============================================
// Agent Compatible Templates
// ============================================

// GetAgentCompatibleTemplates returns all templates with compatibility info for an agent
func (s *Server) GetAgentCompatibleTemplates(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	// Check if agent exists
	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found: "+agentID)
		return
	}

	// Get agent's action types
	agentActionTypes, err := s.repo.GetAgentActionTypes(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get templates with compatibility info
	templateInfos, err := s.repo.FindTemplatesCompatibleWithAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build response
	templates := make([]TemplateCompatibilityResponse, 0, len(templateInfos))
	for _, info := range templateInfos {
		description := ""
		if info.Template.Description.Valid {
			description = info.Template.Description.String
		}

		templates = append(templates, TemplateCompatibilityResponse{
			TemplateID:          info.Template.ID,
			TemplateName:        info.Template.Name,
			Description:         description,
			RequiredActionTypes: info.RequiredActionTypes,
			IsFullyCompatible:   info.IsFullyCompatible,
			MissingCapabilities: info.MissingCapabilities,
			AlreadyAssigned:     info.AlreadyAssigned,
		})
	}

	writeJSON(w, http.StatusOK, AgentCompatibleTemplatesResponse{
		AgentID:          agentID,
		AgentName:        agent.Name,
		AgentActionTypes: agentActionTypes,
		Templates:        templates,
	})
}

// ============================================
// Block Copy/Paste
// ============================================

// BlockCopyRequest represents a request to copy blocks
type BlockCopyRequest struct {
	StepIDs []string `json:"step_ids"`
}

// BlockCopyResponse represents the response from copying blocks
type BlockCopyResponse struct {
	SourceActionGraphID   string                   `json:"source_action_graph_id"`
	SourceActionGraphName string                   `json:"source_action_graph_name"`
	Blocks                []map[string]interface{} `json:"blocks"`
	Connections           []map[string]interface{} `json:"connections"`
}

// BlockPasteRequest represents a request to paste blocks
type BlockPasteRequest struct {
	Blocks      []map[string]interface{} `json:"blocks"`
	IDPrefix    string                   `json:"id_prefix,omitempty"`
	InsertAfter string                   `json:"insert_after,omitempty"`
}

// BlockPasteResponse represents the response from pasting blocks
type BlockPasteResponse struct {
	ActionGraphID string            `json:"action_graph_id"`
	NewVersion    int               `json:"new_version"`
	PastedStepIDs []string          `json:"pasted_step_ids"`
	IDMapping     map[string]string `json:"id_mapping"`
}

// CopyActionGraphBlocks copies selected blocks from an action graph
func (s *Server) CopyActionGraphBlocks(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	graph, err := s.repo.GetActionGraph(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found: "+graphID)
		return
	}

	var req BlockCopyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Parse steps
	var steps []map[string]interface{}
	if graph.Steps != nil {
		json.Unmarshal(graph.Steps, &steps)
	}

	// Build step map
	stepMap := make(map[string]map[string]interface{})
	for _, step := range steps {
		if id, ok := step["id"].(string); ok {
			stepMap[id] = step
		}
	}

	// Get selected blocks
	selectedBlocks := make([]map[string]interface{}, 0)
	for _, stepID := range req.StepIDs {
		if step, exists := stepMap[stepID]; exists {
			selectedBlocks = append(selectedBlocks, step)
		} else {
			writeError(w, http.StatusBadRequest, "Step not found: "+stepID)
			return
		}
	}

	// Extract connections between selected blocks
	selectedIDs := make(map[string]bool)
	for _, id := range req.StepIDs {
		selectedIDs[id] = true
	}

	connections := make([]map[string]interface{}, 0)
	transitionTypes := []string{"on_success", "on_failure", "on_confirm", "on_cancel", "on_timeout"}

	for _, step := range selectedBlocks {
		stepID, _ := step["id"].(string)
		transition, _ := step["transition"].(map[string]interface{})

		if transition != nil {
			for _, transType := range transitionTypes {
				target := transition[transType]
				if targetStr, ok := target.(string); ok && selectedIDs[targetStr] {
					connections = append(connections, map[string]interface{}{
						"from": stepID,
						"to":   targetStr,
						"type": transType,
					})
				} else if targetMap, ok := target.(map[string]interface{}); ok {
					for _, subKey := range []string{"next", "else", "fallback"} {
						if subTarget, ok := targetMap[subKey].(string); ok && selectedIDs[subTarget] {
							connections = append(connections, map[string]interface{}{
								"from": stepID,
								"to":   subTarget,
								"type": transType + "." + subKey,
							})
						}
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, BlockCopyResponse{
		SourceActionGraphID:   graphID,
		SourceActionGraphName: graph.Name,
		Blocks:                selectedBlocks,
		Connections:           connections,
	})
}

// PasteActionGraphBlocks pastes blocks into an action graph
func (s *Server) PasteActionGraphBlocks(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	graph, err := s.repo.GetActionGraph(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Action Graph not found: "+graphID)
		return
	}

	var req BlockPasteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Parse existing steps
	var steps []map[string]interface{}
	if graph.Steps != nil {
		json.Unmarshal(graph.Steps, &steps)
	}

	existingIDs := make(map[string]bool)
	for _, step := range steps {
		if id, ok := step["id"].(string); ok {
			existingIDs[id] = true
		}
	}

	// Generate new IDs for pasted blocks
	idPrefix := req.IDPrefix
	if idPrefix == "" {
		idPrefix = "pasted_" + uuid.New().String()[:6] + "_"
	}

	idMapping := make(map[string]string)
	newSteps := make([]map[string]interface{}, 0, len(req.Blocks))

	for _, block := range req.Blocks {
		oldID, _ := block["id"].(string)
		newID := idPrefix + oldID

		// Ensure unique ID
		counter := 1
		for existingIDs[newID] {
			newID = idPrefix + oldID + "_" + string(rune('0'+counter))
			counter++
		}

		idMapping[oldID] = newID
		existingIDs[newID] = true

		// Create new step with new ID
		newStep := make(map[string]interface{})
		for k, v := range block {
			newStep[k] = v
		}
		newStep["id"] = newID
		newSteps = append(newSteps, newStep)
	}

	// Update transitions to use new IDs
	for _, step := range newSteps {
		if transition, ok := step["transition"].(map[string]interface{}); ok {
			updateTransitionIDs(transition, idMapping)
		}
	}

	// Find insert position
	insertIndex := len(steps)
	if req.InsertAfter != "" {
		for i, step := range steps {
			if id, _ := step["id"].(string); id == req.InsertAfter {
				insertIndex = i + 1
				break
			}
		}
	}

	// Insert new steps
	newStepsList := make([]map[string]interface{}, 0, len(steps)+len(newSteps))
	newStepsList = append(newStepsList, steps[:insertIndex]...)
	newStepsList = append(newStepsList, newSteps...)
	newStepsList = append(newStepsList, steps[insertIndex:]...)

	// Update action graph
	stepsJSON, _ := json.Marshal(newStepsList)
	graph.Steps = stepsJSON
	graph.Version++
	graph.UpdatedAt = time.Now()

	if err := s.repo.UpdateActionGraph(graph); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Mark assignments as outdated
	s.repo.MarkTemplateAssignmentsOutdated(graphID, graph.Version)

	pastedIDs := make([]string, 0, len(idMapping))
	for _, newID := range idMapping {
		pastedIDs = append(pastedIDs, newID)
	}

	writeJSON(w, http.StatusOK, BlockPasteResponse{
		ActionGraphID: graphID,
		NewVersion:    graph.Version,
		PastedStepIDs: pastedIDs,
		IDMapping:     idMapping,
	})
}

// ============================================
// Action Graph Copy
// ============================================

// ActionGraphCopyRequest represents a request to copy an entire action graph
type ActionGraphCopyRequest struct {
	NewID         string `json:"new_id"`
	NewName       string `json:"new_name"`
	TargetAgentID string `json:"target_agent_id,omitempty"`
}

// ActionGraphCopyResponse represents the response from copying an action graph
type ActionGraphCopyResponse struct {
	NewActionGraphID   string  `json:"new_action_graph_id"`
	NewActionGraphName string  `json:"new_action_graph_name"`
	StepCount          int     `json:"step_count"`
	AssignedToAgent    *string `json:"assigned_to_agent,omitempty"`
}

// CopyActionGraph copies an entire action graph
func (s *Server) CopyActionGraph(w http.ResponseWriter, r *http.Request) {
	sourceGraphID := r.URL.Query().Get("source_action_graph_id")
	if sourceGraphID == "" {
		writeError(w, http.StatusBadRequest, "source_action_graph_id is required")
		return
	}

	sourceGraph, err := s.repo.GetActionGraph(sourceGraphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sourceGraph == nil {
		writeError(w, http.StatusNotFound, "Source action graph not found: "+sourceGraphID)
		return
	}

	var req ActionGraphCopyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Check new ID doesn't exist
	existing, _ := s.repo.GetActionGraph(req.NewID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Action Graph ID already exists: "+req.NewID)
		return
	}

	// Create new action graph
	newGraph := &db.ActionGraph{
		ID:            req.NewID,
		Name:          req.NewName,
		Description:   sourceGraph.Description,
		Preconditions: sourceGraph.Preconditions,
		Steps:         sourceGraph.Steps,
		Version:       1,
		IsTemplate:    false,
	}

	if err := s.repo.CreateActionGraph(newGraph); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var assignedAgent *string

	// Optionally assign to agent
	if req.TargetAgentID != "" {
		agent, _ := s.repo.GetAgent(req.TargetAgentID)
		if agent != nil {
			aag := &db.AgentActionGraph{
				ID:               uuid.New().String(),
				AgentID:          req.TargetAgentID,
				ActionGraphID:    req.NewID,
				ServerVersion:    1,
				DeploymentStatus: "pending",
				Enabled:          true,
				Priority:         0,
				CreatedAt:        time.Now(),
				UpdatedAt:        time.Now(),
			}
			s.repo.CreateAgentActionGraph(aag)
			assignedAgent = &req.TargetAgentID
		}
	}

	// Count steps
	var steps []interface{}
	if newGraph.Steps != nil {
		json.Unmarshal(newGraph.Steps, &steps)
	}

	writeJSON(w, http.StatusCreated, ActionGraphCopyResponse{
		NewActionGraphID:   req.NewID,
		NewActionGraphName: req.NewName,
		StepCount:          len(steps),
		AssignedToAgent:    assignedAgent,
	})
}

// updateTransitionIDs updates transition target IDs using mapping
func updateTransitionIDs(transition map[string]interface{}, idMapping map[string]string) {
	transitionTypes := []string{"on_success", "on_failure", "on_confirm", "on_cancel", "on_timeout"}

	for _, key := range transitionTypes {
		target := transition[key]
		if targetStr, ok := target.(string); ok {
			if newID, exists := idMapping[targetStr]; exists {
				transition[key] = newID
			}
		} else if targetMap, ok := target.(map[string]interface{}); ok {
			for _, subKey := range []string{"next", "else", "fallback"} {
				if subTarget, ok := targetMap[subKey].(string); ok {
					if newID, exists := idMapping[subTarget]; exists {
						targetMap[subKey] = newID
					}
				}
			}
		}
	}
}

// Helper functions

func agentToResponse(agent *db.Agent, sm *state.GlobalStateManager) AgentResponse {
	response := AgentResponse{
		ID:         agent.ID,
		Name:       agent.Name,
		Status:     agent.Status,
		RobotCount: len(agent.Robots),
		CreatedAt:  agent.CreatedAt,
	}

	if agent.IPAddress.Valid {
		response.IPAddress = agent.IPAddress.String
	}
	if agent.LastSeen.Valid {
		response.LastSeen = &agent.LastSeen.Time
	}

	// Get robot IDs
	for _, robot := range agent.Robots {
		response.Robots = append(response.Robots, robot.ID)
	}

	return response
}

func agentActionGraphToResponse(aag *db.AgentActionGraph, repo *db.Repository) AgentActionGraphResponse {
	response := AgentActionGraphResponse{
		ID:               aag.ID,
		AgentID:          aag.AgentID,
		ActionGraphID:    aag.ActionGraphID,
		ServerVersion:    aag.ServerVersion,
		DeployedVersion:  aag.DeployedVersion,
		DeploymentStatus: aag.DeploymentStatus,
		Enabled:          aag.Enabled,
		Priority:         aag.Priority,
		CreatedAt:        aag.CreatedAt,
		UpdatedAt:        aag.UpdatedAt,
	}

	if aag.DeploymentError.Valid {
		response.DeploymentError = aag.DeploymentError.String
	}
	if aag.DeployedAt.Valid {
		response.DeployedAt = &aag.DeployedAt.Time
	}

	// Get action graph name
	if aag.ActionGraph != nil {
		response.ActionGraphName = aag.ActionGraph.Name
	} else {
		graph, _ := repo.GetActionGraph(aag.ActionGraphID)
		if graph != nil {
			response.ActionGraphName = graph.Name
		}
	}

	return response
}

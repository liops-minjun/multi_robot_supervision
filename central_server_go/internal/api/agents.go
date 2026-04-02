package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
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

	offlineMode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("offline_mode")))
	if offlineMode == "" {
		offlineMode = "all"
	}
	cleanupStale := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("cleanup_stale")), "true")

	needsAssignmentLookup := offlineMode == "template_only" || cleanupStale
	assignedAgentIDs := make(map[string]struct{})
	if needsAssignmentLookup {
		assignments, err := s.repo.GetAllAgentBehaviorTrees()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		graphIDs := make([]string, 0, len(assignments))
		graphIDSet := make(map[string]struct{})
		for _, assignment := range assignments {
			graphID := strings.TrimSpace(assignment.BehaviorTreeID)
			if graphID == "" {
				continue
			}
			if _, exists := graphIDSet[graphID]; exists {
				continue
			}
			graphIDSet[graphID] = struct{}{}
			graphIDs = append(graphIDs, graphID)
		}
		graphsMap, err := s.repo.GetBehaviorTreesByIDs(graphIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		for _, assignment := range assignments {
			agentID := strings.TrimSpace(assignment.AgentID)
			if agentID == "" {
				continue
			}

			graphID := strings.TrimSpace(assignment.BehaviorTreeID)
			graph := graphsMap[graphID]
			if graph == nil || !graph.IsTemplate {
				continue
			}
			assignedAgentIDs[agentID] = struct{}{}
		}
	}

	responses := make([]AgentResponse, 0, len(agents))
	staleAgentIDs := make([]string, 0)
	for _, agent := range agents {
		response := agentToResponse(&agent, s.stateManager)
		isOnline := response.Status == "online"
		_, hasAssignedTemplate := assignedAgentIDs[agent.ID]
		hasTemplate := hasAssignedTemplate || response.HasCapabilityTemplate

		include := true
		switch offlineMode {
		case "none":
			include = isOnline
		case "template_only":
			include = isOnline || hasTemplate
		case "all":
			include = true
		default:
			include = true
		}

		if !isOnline && !hasTemplate && cleanupStale {
			staleAgentIDs = append(staleAgentIDs, agent.ID)
			include = false
		}

		if include {
			responses = append(responses, response)
		}
	}

	if cleanupStale {
		for _, agentID := range staleAgentIDs {
			if err := s.repo.DeleteAgentBehaviorTreesByAgent(agentID); err != nil {
				log.Printf("[Agents] cleanup_stale failed to remove assignments for %s: %v", agentID, err)
				continue
			}
			if err := s.repo.DeleteAgent(agentID); err != nil {
				log.Printf("[Agents] cleanup_stale failed to remove agent %s: %v", agentID, err)
			}
		}
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

// UpdateAgentRequest represents a request to update an agent
type UpdateAgentRequest struct {
	Name string `json:"name,omitempty"`
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

// UpdateAgent updates an existing agent (primarily for renaming)
func (s *Server) UpdateAgent(w http.ResponseWriter, r *http.Request) {
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

	var req UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update name if provided
	if req.Name != "" {
		agent.Name = req.Name
	}

	if err := s.repo.UpdateAgent(agent); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(agent, s.stateManager))
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

	// Delete related agent_behavior_trees first
	if err := s.repo.DeleteAgentBehaviorTreesByAgent(agentID); err != nil {
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

// SaveAgentCapabilityTemplate persists current capabilities as an offline RTM template snapshot.
func (s *Server) SaveAgentCapabilityTemplate(w http.ResponseWriter, r *http.Request) {
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

	capabilityCount, err := s.repo.SaveAgentCapabilityTemplate(agentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updatedAgent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updatedAgent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"agent_id":         agentID,
		"capability_count": capabilityCount,
		"saved_at":         time.Now().UTC(),
		"agent":            agentToResponse(updatedAgent, s.stateManager),
	})
}

// ListAgentBehaviorTrees returns all behavior trees assigned to an agent
func (s *Server) ListAgentBehaviorTrees(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	abts, err := s.repo.GetAgentBehaviorTrees(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for i := range abts {
		s.normalizeStaleDeployingStatus(&abts[i])
	}

	responses := make([]AgentBehaviorTreeResponse, len(abts))
	for i, abt := range abts {
		responses[i] = agentBehaviorTreeToResponse(&abt, s.repo)
	}

	writeJSON(w, http.StatusOK, responses)
}

// AssignBehaviorTree assigns a behavior tree to an agent
func (s *Server) AssignBehaviorTree(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	var req AssignBehaviorTreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.BehaviorTreeID == "" {
		writeError(w, http.StatusBadRequest, "behavior_tree_id is required")
		return
	}

	// Check if agent exists
	agent, _ := s.repo.GetAgent(agentID)
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	// Check if behavior tree exists
	graph, _ := s.repo.GetBehaviorTree(req.BehaviorTreeID)
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	// Check if already assigned
	existing, _ := s.repo.GetAgentBehaviorTree(agentID, req.BehaviorTreeID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Behavior Tree already assigned to this agent")
		return
	}

	abt := &db.AgentBehaviorTree{
		ID:               uuid.New().String(),
		AgentID:          agentID,
		BehaviorTreeID:   req.BehaviorTreeID,
		ServerVersion:    graph.Version,
		DeployedVersion:  0,
		DeploymentStatus: "pending",
		Enabled:          req.Enabled,
		Priority:         req.Priority,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := s.repo.CreateAgentBehaviorTree(abt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, agentBehaviorTreeToResponse(abt, s.repo))
}

// GetAgentBehaviorTree returns a specific agent-behavior tree assignment
func (s *Server) GetAgentBehaviorTree(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	graphID := chi.URLParam(r, "graphID")

	abt, err := s.repo.GetAgentBehaviorTree(agentID, graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if abt == nil {
		writeError(w, http.StatusNotFound, "Assignment not found")
		return
	}

	s.normalizeStaleDeployingStatus(abt)

	writeJSON(w, http.StatusOK, agentBehaviorTreeToResponse(abt, s.repo))
}

// RemoveAgentBehaviorTree removes a behavior tree assignment
func (s *Server) RemoveAgentBehaviorTree(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	graphID := chi.URLParam(r, "graphID")

	if err := s.repo.DeleteAgentBehaviorTree(agentID, graphID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Assignment removed",
	})
}

// DeployBehaviorTree deploys a behavior tree to an agent
func (s *Server) DeployBehaviorTree(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	graphID := chi.URLParam(r, "graphID")

	// Get assignment
	abt, err := s.repo.GetAgentBehaviorTree(agentID, graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if abt == nil {
		writeError(w, http.StatusNotFound, "Assignment not found")
		return
	}

	result := s.deployBehaviorTreeToAgentSync(r.Context(), abt.ID)
	status, _ := result["status"].(string)
	errorMessage, _ := result["error"].(string)

	if status == "failed" && errorMessage == "" {
		errorMessage = "Behavior Tree deployment failed"
	}
	if status == "" {
		status = "failed"
	}

	response := map[string]interface{}{
		"status":           status,
		"behavior_tree_id": graphID,
		"agent_id":         agentID,
	}
	if version, ok := result["version"]; ok {
		response["deployed_version"] = version
	}
	if errorMessage != "" {
		response["error"] = errorMessage
		response["message"] = errorMessage
	} else {
		switch status {
		case "queued":
			response["message"] = "Agent is offline, deployment queued"
		case "deployed":
			response["message"] = "Behavior Tree deployed successfully"
		default:
			response["message"] = "Behavior Tree deployment requested"
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// GetDeploymentLogs returns deployment logs for an agent-behavior tree
func (s *Server) GetDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	graphID := chi.URLParam(r, "graphID")

	// Get assignment first
	abt, err := s.repo.GetAgentBehaviorTree(agentID, graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if abt == nil {
		writeError(w, http.StatusNotFound, "Assignment not found")
		return
	}

	logs, err := s.repo.GetDeploymentLogs(abt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]DeploymentLogResponse, len(logs))
	for i, log := range logs {
		responses[i] = DeploymentLogResponse{
			ID:                  log.ID,
			AgentBehaviorTreeID: log.AgentBehaviorTreeID,
			Action:              log.Action,
			Version:             log.Version,
			Status:              log.Status,
			InitiatedAt:         log.InitiatedAt,
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
	SourceBehaviorTreeID   string                   `json:"source_behavior_tree_id"`
	SourceBehaviorTreeName string                   `json:"source_behavior_tree_name"`
	Blocks                 []map[string]interface{} `json:"blocks"`
	Connections            []map[string]interface{} `json:"connections"`
}

// BlockPasteRequest represents a request to paste blocks
type BlockPasteRequest struct {
	Blocks      []map[string]interface{} `json:"blocks"`
	IDPrefix    string                   `json:"id_prefix,omitempty"`
	InsertAfter string                   `json:"insert_after,omitempty"`
}

// BlockPasteResponse represents the response from pasting blocks
type BlockPasteResponse struct {
	BehaviorTreeID string            `json:"behavior_tree_id"`
	NewVersion     int               `json:"new_version"`
	PastedStepIDs  []string          `json:"pasted_step_ids"`
	IDMapping      map[string]string `json:"id_mapping"`
}

// CopyBehaviorTreeBlocks copies selected blocks from a behavior tree
func (s *Server) CopyBehaviorTreeBlocks(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found: "+graphID)
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
		SourceBehaviorTreeID:   graphID,
		SourceBehaviorTreeName: graph.Name,
		Blocks:                 selectedBlocks,
		Connections:            connections,
	})
}

// PasteBehaviorTreeBlocks pastes blocks into a behavior tree
func (s *Server) PasteBehaviorTreeBlocks(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found: "+graphID)
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

	// Update behavior tree
	stepsJSON, _ := json.Marshal(newStepsList)
	graph.Steps = stepsJSON
	graph.Version++
	graph.UpdatedAt = time.Now()

	if err := s.repo.UpdateBehaviorTree(graph); err != nil {
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
		BehaviorTreeID: graphID,
		NewVersion:     graph.Version,
		PastedStepIDs:  pastedIDs,
		IDMapping:      idMapping,
	})
}

// ============================================
// Behavior Tree Copy
// ============================================

// BehaviorTreeCopyRequest represents a request to copy an entire behavior tree
type BehaviorTreeCopyRequest struct {
	NewID         string `json:"new_id"`
	NewName       string `json:"new_name"`
	TargetAgentID string `json:"target_agent_id,omitempty"`
}

// BehaviorTreeCopyResponse represents the response from copying a behavior tree
type BehaviorTreeCopyResponse struct {
	NewBehaviorTreeID   string  `json:"new_behavior_tree_id"`
	NewBehaviorTreeName string  `json:"new_behavior_tree_name"`
	StepCount           int     `json:"step_count"`
	AssignedToAgent     *string `json:"assigned_to_agent,omitempty"`
}

// CopyBehaviorTree copies an entire behavior tree
func (s *Server) CopyBehaviorTree(w http.ResponseWriter, r *http.Request) {
	sourceGraphID := r.URL.Query().Get("source_behavior_tree_id")
	if sourceGraphID == "" {
		writeError(w, http.StatusBadRequest, "source_behavior_tree_id is required")
		return
	}

	sourceGraph, err := s.repo.GetBehaviorTree(sourceGraphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sourceGraph == nil {
		writeError(w, http.StatusNotFound, "Source behavior tree not found: "+sourceGraphID)
		return
	}

	var req BehaviorTreeCopyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Check new ID doesn't exist
	existing, _ := s.repo.GetBehaviorTree(req.NewID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Behavior Tree ID already exists: "+req.NewID)
		return
	}

	// Create new behavior tree
	newGraph := &db.BehaviorTree{
		ID:            req.NewID,
		Name:          req.NewName,
		Description:   sourceGraph.Description,
		Preconditions: sourceGraph.Preconditions,
		Steps:         sourceGraph.Steps,
		EntryPoint:    sourceGraph.EntryPoint,
		Version:       1,
		IsTemplate:    false,
	}

	if err := s.repo.CreateBehaviorTree(newGraph); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var assignedAgent *string

	// Optionally assign to agent
	if req.TargetAgentID != "" {
		agent, _ := s.repo.GetAgent(req.TargetAgentID)
		if agent != nil {
			abt := &db.AgentBehaviorTree{
				ID:               uuid.New().String(),
				AgentID:          req.TargetAgentID,
				BehaviorTreeID:   req.NewID,
				ServerVersion:    1,
				DeploymentStatus: "pending",
				Enabled:          true,
				Priority:         0,
				CreatedAt:        time.Now(),
				UpdatedAt:        time.Now(),
			}
			s.repo.CreateAgentBehaviorTree(abt)
			assignedAgent = &req.TargetAgentID
		}
	}

	// Count steps
	var steps []interface{}
	if newGraph.Steps != nil {
		json.Unmarshal(newGraph.Steps, &steps)
	}

	writeJSON(w, http.StatusCreated, BehaviorTreeCopyResponse{
		NewBehaviorTreeID:   req.NewID,
		NewBehaviorTreeName: req.NewName,
		StepCount:           len(steps),
		AssignedToAgent:     assignedAgent,
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
		Namespace:  agent.Namespace,
		Status:     agent.Status,
		RobotCount: 1, // 1 Agent = 1 Robot in this architecture
		CreatedAt:  agent.CreatedAt,
		HasCapabilityTemplate:         agent.CapabilityTemplateSavedAt.Valid,
		CapabilityTemplateCapabilityCount: agent.CapabilityTemplateCapabilityCount,
	}

	if agent.IPAddress.Valid {
		response.IPAddress = agent.IPAddress.String
	}
	if agent.LastSeen.Valid {
		response.LastSeen = &agent.LastSeen.Time
	}
	if agent.CapabilityTemplateSavedAt.Valid {
		response.CapabilityTemplateSavedAt = &agent.CapabilityTemplateSavedAt.Time
	}

	// In 1:1 model, agent ID is also the robot ID
	response.Robots = []string{agent.ID}

	// Get current state from state manager
	if robotState, exists := sm.GetRobotState(agent.ID); exists {
		response.CurrentState = effectiveAgentCurrentState(robotState.CurrentState, robotState.IsOnline, robotState.IsExecuting)
	} else {
		response.CurrentState = effectiveAgentCurrentState(agent.CurrentState, sm.IsAgentOnline(agent.ID), false)
	}

	// Check real-time online status from state manager
	// This overrides the database status with live connection status
	if sm.IsAgentOnline(agent.ID) {
		response.Status = "online"
		// Update IP address from live connection if available
		if liveStatus, exists := sm.GetAgentStatus(agent.ID); exists {
			if liveStatus.IPAddress != "" {
				response.IPAddress = liveStatus.IPAddress
			}
			response.LastSeen = &liveStatus.LastHeartbeat
		}
	} else {
		response.Status = "offline"
	}

	return response
}

func agentBehaviorTreeToResponse(abt *db.AgentBehaviorTree, repo *db.Repository) AgentBehaviorTreeResponse {
	response := AgentBehaviorTreeResponse{
		ID:               abt.ID,
		AgentID:          abt.AgentID,
		BehaviorTreeID:   abt.BehaviorTreeID,
		ServerVersion:    abt.ServerVersion,
		DeployedVersion:  abt.DeployedVersion,
		DeploymentStatus: abt.DeploymentStatus,
		Enabled:          abt.Enabled,
		Priority:         abt.Priority,
		CreatedAt:        abt.CreatedAt,
		UpdatedAt:        abt.UpdatedAt,
	}

	if abt.DeploymentError.Valid {
		response.DeploymentError = abt.DeploymentError.String
	}
	if abt.DeployedAt.Valid {
		response.DeployedAt = &abt.DeployedAt.Time
	}

	// Get behavior tree name
	if abt.BehaviorTree != nil {
		response.BehaviorTreeName = abt.BehaviorTree.Name
	} else {
		graph, _ := repo.GetBehaviorTree(abt.BehaviorTreeID)
		if graph != nil {
			response.BehaviorTreeName = graph.Name
		}
	}

	return response
}

func (s *Server) normalizeStaleDeployingStatus(abt *db.AgentBehaviorTree) {
	if abt == nil {
		return
	}
	if abt.DeploymentStatus != "deploying" {
		return
	}
	if time.Since(abt.UpdatedAt) < 45*time.Second {
		return
	}

	abt.DeploymentStatus = "failed"
	if !abt.DeploymentError.Valid || strings.TrimSpace(abt.DeploymentError.String) == "" {
		abt.DeploymentError = sql.NullString{
			String: "deployment timed out (no response from RTM)",
			Valid:  true,
		}
	}
	abt.UpdatedAt = time.Now()
	if err := s.repo.UpdateAgentBehaviorTree(abt); err != nil {
		log.Printf("[AgentAPI] failed to normalize stale deploying status for %s/%s: %v", abt.AgentID, abt.BehaviorTreeID, err)
	}
}

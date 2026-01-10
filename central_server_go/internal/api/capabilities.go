package api

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"central_server_go/internal/db"

	"github.com/go-chi/chi/v5"
)

// ============================================================
// Capability Registration API (Zero-Config Architecture)
// ============================================================

// RegisterCapabilities registers capabilities for an agent
// PUT /api/agents/{agentID}/capabilities or PUT /api/robots/{robotID}/capabilities (legacy)
func (s *Server) RegisterCapabilities(w http.ResponseWriter, r *http.Request) {
	// Support both agentID and robotID URL params (1 Agent = 1 Robot)
	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		agentID = chi.URLParam(r, "robotID") // Legacy compatibility
	}

	var req CapabilityRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Verify agent exists
	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	// Convert request to DB models
	capabilities := make([]db.AgentCapability, len(req.Capabilities))
	for i, cap := range req.Capabilities {
		// Generate unique ID from agent_id + action_type
		idHash := md5.Sum([]byte(agentID + ":" + cap.ActionType))
		capID := hex.EncodeToString(idHash[:])

		var goalSchema, resultSchema, feedbackSchema, successCriteria []byte
		if cap.GoalSchema != nil {
			goalSchema, _ = json.Marshal(cap.GoalSchema)
		}
		if cap.ResultSchema != nil {
			resultSchema, _ = json.Marshal(cap.ResultSchema)
		}
		if cap.FeedbackSchema != nil {
			feedbackSchema, _ = json.Marshal(cap.FeedbackSchema)
		}
		if cap.SuccessCriteria != nil {
			successCriteria, _ = json.Marshal(cap.SuccessCriteria)
		}

		capabilities[i] = db.AgentCapability{
			ID:              capID,
			AgentID:         agentID,
			ActionType:      cap.ActionType,
			ActionServer:    cap.ActionServer,
			GoalSchema:      goalSchema,
			ResultSchema:    resultSchema,
			FeedbackSchema:  feedbackSchema,
			SuccessCriteria: successCriteria,
			Status:          "idle",
			IsAvailable:     true,
			DiscoveredAt:    time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		}
	}

	// Sync capabilities (delete old, add new)
	if err := s.repo.SyncAgentCapabilities(agentID, capabilities); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":          "Capabilities registered",
		"agent_id":         agentID,
		"capability_count": len(capabilities),
	})
}

// GetRobotCapabilities returns capabilities for an agent (legacy robot endpoint)
// GET /api/robots/{robotID}/capabilities or GET /api/agents/{agentID}/capabilities
func (s *Server) GetRobotCapabilities(w http.ResponseWriter, r *http.Request) {
	// Support both agentID and robotID URL params (1 Agent = 1 Robot)
	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		agentID = chi.URLParam(r, "robotID") // Legacy compatibility
	}

	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	capabilities, err := s.repo.GetAgentCapabilities(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := AgentCapabilitiesListResponse{
		AgentID:      agentID,
		AgentName:    agent.Name,
		Namespace:    agent.Namespace,
		Capabilities: make([]CapabilityResponse, len(capabilities)),
		LastUpdated:  time.Now().UTC(),
	}

	for i, cap := range capabilities {
		var goalSchema, resultSchema, feedbackSchema, successCriteria map[string]interface{}
		if cap.GoalSchema != nil {
			json.Unmarshal(cap.GoalSchema, &goalSchema)
		}
		if cap.ResultSchema != nil {
			json.Unmarshal(cap.ResultSchema, &resultSchema)
		}
		if cap.FeedbackSchema != nil {
			json.Unmarshal(cap.FeedbackSchema, &feedbackSchema)
		}
		if cap.SuccessCriteria != nil {
			json.Unmarshal(cap.SuccessCriteria, &successCriteria)
		}

		response.Capabilities[i] = CapabilityResponse{
			ActionType:      cap.ActionType,
			ActionServer:    cap.ActionServer,
			GoalSchema:      goalSchema,
			ResultSchema:    resultSchema,
			FeedbackSchema:  feedbackSchema,
			SuccessCriteria: successCriteria,
			Status:          cap.Status,
			IsAvailable:     cap.IsAvailable,
			DiscoveredAt:    cap.DiscoveredAt,
		}

		if cap.UpdatedAt.After(response.LastUpdated) {
			response.LastUpdated = cap.UpdatedAt
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// UpdateCapabilityStatus updates capability status for an agent
// PATCH /api/robots/{robotID}/capabilities/status or PATCH /api/agents/{agentID}/capabilities/status
func (s *Server) UpdateCapabilityStatus(w http.ResponseWriter, r *http.Request) {
	// Support both agentID and robotID URL params (1 Agent = 1 Robot)
	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		agentID = chi.URLParam(r, "robotID") // Legacy compatibility
	}

	var req CapabilityStatusUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Verify agent exists
	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	// Update each capability status
	for actionType, status := range req.Status {
		if err := s.repo.UpdateAgentCapabilityStatus(agentID, actionType, status.Status, status.Available); err != nil {
			// Log but don't fail - capability might not exist yet
			continue
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Capability status updated",
		"agent_id": agentID,
	})
}

// ============================================================
// Fleet-wide Capability Queries
// ============================================================

// ListAllCapabilities returns all capabilities across all agents
// GET /api/capabilities
func (s *Server) ListAllCapabilities(w http.ResponseWriter, r *http.Request) {
	// Query from agent_capabilities table (where Fleet Agent registers capabilities)
	allCaps, err := s.repo.GetAllAgentCapabilities()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get all agents for count and name lookup
	agents, err := s.repo.GetAllAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build agent name map for lookup
	agentNameMap := make(map[string]string)
	for _, agent := range agents {
		agentNameMap[agent.ID] = agent.Name
	}

	// Group capabilities by action type (for backward compatibility)
	actionTypeMap := make(map[string]*ActionTypeInfo)
	// Also collect individual action servers
	actionServers := make([]ActionServerInfo, 0, len(allCaps))

	for _, cap := range allCaps {
		// Group by action type (existing behavior)
		info, exists := actionTypeMap[cap.ActionType]
		if !exists {
			info = &ActionTypeInfo{
				ActionType: cap.ActionType,
				AgentIDs:   make([]string, 0),
			}
			actionTypeMap[cap.ActionType] = info
		}
		info.AgentIDs = append(info.AgentIDs, cap.AgentID)
		info.TotalCount++
		if cap.IsAvailable {
			info.AvailableCount++
		}

		// Add individual action server (NEW)
		actionServers = append(actionServers, ActionServerInfo{
			ActionType:   cap.ActionType,
			ActionServer: cap.ActionServer,
			AgentID:      cap.AgentID,
			AgentName:    agentNameMap[cap.AgentID],
			IsAvailable:  cap.IsAvailable,
			Status:       cap.Status,
		})
	}

	actionTypeInfos := make([]ActionTypeInfo, 0, len(actionTypeMap))
	for _, info := range actionTypeMap {
		actionTypeInfos = append(actionTypeInfos, *info)
	}

	writeJSON(w, http.StatusOK, AllCapabilitiesResponse{
		ActionTypes:   actionTypeInfos,
		ActionServers: actionServers,
		TotalAgents:   len(agents),
	})
}

// GetCapabilitiesByActionType returns agents with a specific action type
// GET /api/capabilities/{actionType}
func (s *Server) GetCapabilitiesByActionType(w http.ResponseWriter, r *http.Request) {
	actionType := chi.URLParam(r, "*")
	if actionType == "" {
		writeError(w, http.StatusBadRequest, "action_type is required")
		return
	}

	// Get all capabilities and filter by action type
	allCaps, err := s.repo.GetAllAgentCapabilities()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get agent name map for lookup
	agents, _ := s.repo.GetAllAgents()
	agentNameMap := make(map[string]string)
	for _, agent := range agents {
		agentNameMap[agent.ID] = agent.Name
	}

	// Filter by action type
	var caps []db.AgentCapability
	for _, cap := range allCaps {
		if cap.ActionType == actionType {
			caps = append(caps, cap)
		}
	}

	responses := make([]struct {
		AgentID      string                 `json:"agent_id"`
		AgentName    string                 `json:"agent_name,omitempty"`
		ActionServer string                 `json:"action_server"`
		Status       string                 `json:"status"`
		IsAvailable  bool                   `json:"is_available"`
		GoalSchema   map[string]interface{} `json:"goal_schema,omitempty"`
	}, len(caps))

	for i, cap := range caps {
		var goalSchema map[string]interface{}
		if cap.GoalSchema != nil {
			json.Unmarshal(cap.GoalSchema, &goalSchema)
		}

		responses[i].AgentID = cap.AgentID
		responses[i].AgentName = agentNameMap[cap.AgentID]
		responses[i].ActionServer = cap.ActionServer
		responses[i].Status = cap.Status
		responses[i].IsAvailable = cap.IsAvailable
		responses[i].GoalSchema = goalSchema
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"action_type": actionType,
		"agents":      responses,
		"total":       len(responses),
	})
}

// ============================================================
// Agent Registration with Capabilities (Legacy Robot endpoint)
// ============================================================

// RegisterRobot registers a new agent (legacy robot endpoint, 1 Agent = 1 Robot)
// POST /api/robots (legacy) or POST /api/agents
func (s *Server) RegisterRobot(w http.ResponseWriter, r *http.Request) {
	var req RobotRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}

	// In 1:1 model, agent_id is same as robot id if not specified
	agentID := req.AgentID
	if agentID == "" {
		agentID = req.ID
	}

	// Check if agent already exists
	existing, _ := s.repo.GetAgent(agentID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Agent already exists")
		return
	}

	// Marshal tags
	var tags []byte
	if req.Tags != nil {
		tags, _ = json.Marshal(req.Tags)
	}

	// Create agent (1 Agent = 1 Robot)
	agent := &db.Agent{
		ID:           agentID,
		Name:         req.Name,
		Namespace:    req.Namespace,
		Tags:         tags,
		CurrentState: "idle",
		Status:       "online",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if req.IPAddress != "" {
		agent.IPAddress.String = req.IPAddress
		agent.IPAddress.Valid = true
	}

	agent.LastSeen.Time = time.Now().UTC()
	agent.LastSeen.Valid = true

	if err := s.repo.CreateOrUpdateAgent(agent); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Register in state manager (agent ID is also robot ID in 1:1 model)
	s.stateManager.RegisterRobot(
		agentID,
		agent.Name,
		agentID,
		"idle",
	)

	// Register capabilities if provided
	if len(req.Capabilities) > 0 {
		capabilities := make([]db.AgentCapability, len(req.Capabilities))
		for i, cap := range req.Capabilities {
			idHash := md5.Sum([]byte(agentID + ":" + cap.ActionType))
			capID := hex.EncodeToString(idHash[:])

			var goalSchema, resultSchema, feedbackSchema, successCriteria []byte
			if cap.GoalSchema != nil {
				goalSchema, _ = json.Marshal(cap.GoalSchema)
			}
			if cap.ResultSchema != nil {
				resultSchema, _ = json.Marshal(cap.ResultSchema)
			}
			if cap.FeedbackSchema != nil {
				feedbackSchema, _ = json.Marshal(cap.FeedbackSchema)
			}
			if cap.SuccessCriteria != nil {
				successCriteria, _ = json.Marshal(cap.SuccessCriteria)
			}

			capabilities[i] = db.AgentCapability{
				ID:              capID,
				AgentID:         agentID,
				ActionType:      cap.ActionType,
				ActionServer:    cap.ActionServer,
				GoalSchema:      goalSchema,
				ResultSchema:    resultSchema,
				FeedbackSchema:  feedbackSchema,
				SuccessCriteria: successCriteria,
				Status:          "idle",
				IsAvailable:     true,
				DiscoveredAt:    time.Now().UTC(),
				UpdatedAt:       time.Now().UTC(),
			}
		}

		s.repo.SyncAgentCapabilities(agentID, capabilities)
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":               agent.ID,
		"name":             agent.Name,
		"namespace":        agent.Namespace,
		"agent_id":         agentID,
		"capability_count": len(req.Capabilities),
		"message":          "Agent registered successfully",
	})
}

// ============================================================
// Agent Capability Aggregation API
// ============================================================

// GetAgentCapabilities returns capabilities directly registered by an agent
// GET /api/agents/{agentID}/capabilities
func (s *Server) GetAgentCapabilities(w http.ResponseWriter, r *http.Request) {
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

	// Get capabilities directly from agent_capabilities table
	caps, err := s.repo.GetAgentCapabilities(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	capabilities := make([]CapabilityResponse, len(caps))
	for i, cap := range caps {
		var goalSchema, resultSchema, feedbackSchema, successCriteria map[string]interface{}
		if cap.GoalSchema != nil {
			json.Unmarshal(cap.GoalSchema, &goalSchema)
		}
		if cap.ResultSchema != nil {
			json.Unmarshal(cap.ResultSchema, &resultSchema)
		}
		if cap.FeedbackSchema != nil {
			json.Unmarshal(cap.FeedbackSchema, &feedbackSchema)
		}
		if cap.SuccessCriteria != nil {
			json.Unmarshal(cap.SuccessCriteria, &successCriteria)
		}

		capabilities[i] = CapabilityResponse{
			ActionType:      cap.ActionType,
			ActionServer:    cap.ActionServer,
			GoalSchema:      goalSchema,
			ResultSchema:    resultSchema,
			FeedbackSchema:  feedbackSchema,
			SuccessCriteria: successCriteria,
			Status:          cap.Status,
			IsAvailable:     cap.IsAvailable,
			DiscoveredAt:    cap.DiscoveredAt,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agent_id":     agentID,
		"agent_name":   agent.Name,
		"status":       agent.Status,
		"capabilities": capabilities,
		"total":        len(capabilities),
	})
}

// GetAllActionTypesWithStats returns all action types with agent/robot counts
// GET /api/capabilities/action-types
func (s *Server) GetAllActionTypesWithStats(w http.ResponseWriter, r *http.Request) {
	results, err := s.repo.GetAllActionTypesWithAgentCount()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := make([]ActionTypeStats, len(results))
	for i, result := range results {
		response[i] = ActionTypeStats{
			ActionType: result.ActionType,
			AgentCount: result.AgentCount,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"action_types": response,
		"total":        len(response),
	})
}

// UpdateRobot updates an agent's metadata (legacy robot endpoint)
// PATCH /api/robots/{robotID} or PATCH /api/agents/{agentID}
func (s *Server) UpdateRobot(w http.ResponseWriter, r *http.Request) {
	// Support both agentID and robotID URL params (1 Agent = 1 Robot)
	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		agentID = chi.URLParam(r, "robotID") // Legacy compatibility
	}

	var req RobotUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found")
		return
	}

	// Update fields
	if req.Name != "" {
		agent.Name = req.Name
	}
	if req.Namespace != "" {
		agent.Namespace = req.Namespace
	}
	if req.Tags != nil {
		tags, _ := json.Marshal(req.Tags)
		agent.Tags = tags
	}

	agent.UpdatedAt = time.Now().UTC()

	if err := s.repo.CreateOrUpdateAgent(agent); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(agent, s.stateManager))
}

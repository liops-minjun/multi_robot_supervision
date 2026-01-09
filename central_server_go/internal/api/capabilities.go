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

// RegisterCapabilities registers capabilities for a robot
// PUT /api/robots/{robotID}/capabilities
func (s *Server) RegisterCapabilities(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	var req CapabilityRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Verify robot exists
	robot, err := s.repo.GetRobot(robotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if robot == nil {
		writeError(w, http.StatusNotFound, "Robot not found")
		return
	}

	// Convert request to DB models
	capabilities := make([]db.RobotCapability, len(req.Capabilities))
	for i, cap := range req.Capabilities {
		// Generate unique ID from robot_id + action_type
		idHash := md5.Sum([]byte(robotID + ":" + cap.ActionType))
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

		capabilities[i] = db.RobotCapability{
			ID:              capID,
			RobotID:         robotID,
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
	if err := s.repo.SyncRobotCapabilities(robotID, capabilities); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":          "Capabilities registered",
		"robot_id":         robotID,
		"capability_count": len(capabilities),
	})
}

// GetRobotCapabilities returns capabilities for a robot
// GET /api/robots/{robotID}/capabilities
func (s *Server) GetRobotCapabilities(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	robot, err := s.repo.GetRobot(robotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if robot == nil {
		writeError(w, http.StatusNotFound, "Robot not found")
		return
	}

	capabilities, err := s.repo.GetRobotCapabilities(robotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := RobotCapabilitiesResponse{
		RobotID:      robotID,
		RobotName:    robot.Name,
		Namespace:    robot.Namespace,
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

// UpdateCapabilityStatus updates capability status for a robot
// PATCH /api/robots/{robotID}/capabilities/status
func (s *Server) UpdateCapabilityStatus(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	var req CapabilityStatusUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Verify robot exists
	robot, err := s.repo.GetRobot(robotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if robot == nil {
		writeError(w, http.StatusNotFound, "Robot not found")
		return
	}

	// Update each capability status
	for actionType, status := range req.Status {
		if err := s.repo.UpdateCapabilityStatus(robotID, actionType, status.Status, status.Available); err != nil {
			// Log but don't fail - capability might not exist yet
			continue
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Capability status updated",
		"robot_id": robotID,
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
				RobotIDs:   make([]string, 0),
			}
			actionTypeMap[cap.ActionType] = info
		}
		// Use agent_id as "robot_id" for compatibility with frontend
		info.RobotIDs = append(info.RobotIDs, cap.AgentID)
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
		TotalRobots:   len(agents),
	})
}

// GetCapabilitiesByActionType returns robots with a specific action type
// GET /api/capabilities/{actionType}
func (s *Server) GetCapabilitiesByActionType(w http.ResponseWriter, r *http.Request) {
	actionType := chi.URLParam(r, "*")
	if actionType == "" {
		writeError(w, http.StatusBadRequest, "action_type is required")
		return
	}

	caps, err := s.repo.GetCapabilitiesByActionType(actionType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]struct {
		RobotID      string                 `json:"robot_id"`
		RobotName    string                 `json:"robot_name,omitempty"`
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

		responses[i].RobotID = cap.RobotID
		responses[i].ActionServer = cap.ActionServer
		responses[i].Status = cap.Status
		responses[i].IsAvailable = cap.IsAvailable
		responses[i].GoalSchema = goalSchema

		if cap.Robot != nil {
			responses[i].RobotName = cap.Robot.Name
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"action_type": actionType,
		"robots":      responses,
		"total":       len(responses),
	})
}

// ============================================================
// Robot Registration with Capabilities (Updated)
// ============================================================

// RegisterRobot registers a new robot with capabilities
// POST /api/robots
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

	// Check if robot already exists
	existing, _ := s.repo.GetRobot(req.ID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Robot already exists")
		return
	}

	// Marshal tags
	var tags []byte
	if req.Tags != nil {
		tags, _ = json.Marshal(req.Tags)
	}

	// Create robot
	robot := &db.Robot{
		ID:           req.ID,
		Name:         req.Name,
		Namespace:    req.Namespace,
		Tags:         tags,
		CurrentState: "idle",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if req.AgentID != "" {
		robot.AgentID.String = req.AgentID
		robot.AgentID.Valid = true

		// Auto-create agent if not exists
		agent, _ := s.repo.GetAgent(req.AgentID)
		if agent == nil {
			newAgent := &db.Agent{
				ID:        req.AgentID,
				Name:      req.AgentID,
				Status:    "online",
				CreatedAt: time.Now().UTC(),
			}
			s.repo.CreateOrUpdateAgent(newAgent)
		}
	}

	if req.IPAddress != "" {
		robot.IPAddress.String = req.IPAddress
		robot.IPAddress.Valid = true
	}

	robot.LastSeen.Time = time.Now().UTC()
	robot.LastSeen.Valid = true

	if err := s.repo.CreateOrUpdateRobot(robot); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Register in state manager
	s.stateManager.RegisterRobot(
		robot.ID,
		robot.Name,
		req.AgentID,
		"idle",
	)

	// Register capabilities if provided
	if len(req.Capabilities) > 0 {
		capabilities := make([]db.RobotCapability, len(req.Capabilities))
		for i, cap := range req.Capabilities {
			idHash := md5.Sum([]byte(req.ID + ":" + cap.ActionType))
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

			capabilities[i] = db.RobotCapability{
				ID:              capID,
				RobotID:         req.ID,
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

		s.repo.SyncRobotCapabilities(req.ID, capabilities)
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":               robot.ID,
		"name":             robot.Name,
		"namespace":        robot.Namespace,
		"agent_id":         req.AgentID,
		"capability_count": len(req.Capabilities),
		"message":          "Robot registered successfully",
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

// UpdateRobot updates a robot's metadata
// PATCH /api/robots/{robotID}
func (s *Server) UpdateRobot(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	var req RobotUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	robot, err := s.repo.GetRobot(robotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if robot == nil {
		writeError(w, http.StatusNotFound, "Robot not found")
		return
	}

	// Update fields
	if req.Name != "" {
		robot.Name = req.Name
	}
	if req.Namespace != "" {
		robot.Namespace = req.Namespace
	}
	if req.Tags != nil {
		tags, _ := json.Marshal(req.Tags)
		robot.Tags = tags
	}

	robot.UpdatedAt = time.Now().UTC()

	if err := s.repo.CreateOrUpdateRobot(robot); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, robotToResponse(robot, s.stateManager))
}

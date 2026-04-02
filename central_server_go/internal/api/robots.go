package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/state"

	"github.com/go-chi/chi/v5"
)

// ListRobots returns all agents (legacy robot endpoint, 1 Agent = 1 Robot)
func (s *Server) ListRobots(w http.ResponseWriter, r *http.Request) {
	agents, err := s.repo.GetAllAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]RobotResponse, len(agents))
	for i, agent := range agents {
		responses[i] = agentToRobotResponse(&agent, s.stateManager)
	}

	writeJSON(w, http.StatusOK, responses)
}

// ConnectRobot registers or reconnects an agent (legacy robot endpoint)
func (s *Server) ConnectRobot(w http.ResponseWriter, r *http.Request) {
	var req RobotConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}

	// In 1:1 model, use ID as agent ID if AgentID not specified
	agentID := req.AgentID
	if agentID == "" {
		agentID = req.ID
	}

	// Create or update agent in database (1 Agent = 1 Robot)
	agent := &db.Agent{
		ID:           agentID,
		Name:         req.Name,
		CurrentState: "idle",
		Status:       "online",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if req.IPAddress != "" {
		agent.IPAddress = sql.NullString{String: req.IPAddress, Valid: true}
	}

	agent.LastSeen = sql.NullTime{Time: time.Now(), Valid: true}

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

	response := agentToRobotResponse(agent, s.stateManager)
	writeJSON(w, http.StatusOK, response)
}

// GetRobot returns a single agent (legacy robot endpoint)
func (s *Server) GetRobot(w http.ResponseWriter, r *http.Request) {
	// Support both agentID and robotID URL params
	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		agentID = chi.URLParam(r, "robotID")
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

	response := RobotDetailResponse{
		RobotResponse: agentToRobotResponse(agent, s.stateManager),
	}

	// Add auto-discovered capabilities
	capabilities, err := s.repo.GetAgentCapabilities(agentID)
	if err == nil && len(capabilities) > 0 {
		response.Capabilities = make([]CapabilityResponse, len(capabilities))
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
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// DeleteRobot deletes an agent (legacy robot endpoint)
func (s *Server) DeleteRobot(w http.ResponseWriter, r *http.Request) {
	// Support both agentID and robotID URL params
	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		agentID = chi.URLParam(r, "robotID")
	}

	if err := s.repo.DeleteAgent(agentID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.stateManager.UnregisterRobot(agentID)

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Agent deleted",
	})
}


// GetCommands returns pending commands for an agent
// In 1:1 model, agentID = robotID, URL param kept for backward compatibility
func (s *Server) GetCommands(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "robotID") // URL param name kept for backward compatibility

	commands, err := s.repo.GetPendingCommands(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := CommandPollResponse{
		Commands: make([]CommandResponse, len(commands)),
	}

	for i, cmd := range commands {
		var payload map[string]interface{}
		if cmd.Payload != nil {
			json.Unmarshal(cmd.Payload, &payload)
		}

		response.Commands[i] = CommandResponse{
			ID:          cmd.ID,
			CommandType: cmd.CommandType,
			Payload:     payload,
			CreatedAt:   cmd.CreatedAt,
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// ReportCommandResult handles command result reports
func (s *Server) ReportCommandResult(w http.ResponseWriter, r *http.Request) {
	commandID := chi.URLParam(r, "commandID")

	var req CommandResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update command status
	if err := s.repo.UpdateCommandStatus(commandID, req.Status, req.Result); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get command to check if it's a step execution
	cmd, _ := s.repo.GetCommand(commandID)
	if cmd != nil && cmd.CommandType == "EXECUTE_STEP" && cmd.Payload != nil {
		var payload map[string]interface{}
		json.Unmarshal(cmd.Payload, &payload)

		taskID, _ := payload["task_id"].(string)
		stepID, _ := payload["step_id"].(string)

		if taskID != "" && stepID != "" {
			// Handle step result in scheduler
			// The scheduler will pick up this result through its own mechanisms
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Result recorded",
	})
}

// Helper function to convert db.Agent to RobotResponse (legacy support, 1 Agent = 1 Robot)
func agentToRobotResponse(agent *db.Agent, sm *state.GlobalStateManager) RobotResponse {
	currentState := effectiveAgentCurrentState(agent.CurrentState, sm.IsAgentOnline(agent.ID), false)
	if robotState, exists := sm.GetRobotState(agent.ID); exists {
		currentState = effectiveAgentCurrentState(robotState.CurrentState, robotState.IsOnline, robotState.IsExecuting)
	}

	response := RobotResponse{
		ID:           agent.ID,
		Name:         agent.Name,
		Namespace:    agent.Namespace,
		CurrentState: currentState,
		AgentID:      agent.ID, // In 1:1 model, agent ID is also robot ID
		CreatedAt:    agent.CreatedAt,
	}

	if agent.IPAddress.Valid {
		response.IPAddress = agent.IPAddress.String
	}
	if agent.LastSeen.Valid {
		response.LastSeen = &agent.LastSeen.Time
	}
	if agent.Tags != nil {
		json.Unmarshal(agent.Tags, &response.Tags)
	}

	now := time.Now()

	// Get online status from state manager
	if robotState, exists := sm.GetRobotState(agent.ID); exists {
		response.IsOnline = robotState.IsOnline
		response.CurrentState = robotState.CurrentState
		if !robotState.LastSeen.IsZero() {
			response.StalenessSec = now.Sub(robotState.LastSeen).Seconds()
		}
	} else if agent.LastSeen.Valid {
		response.StalenessSec = now.Sub(agent.LastSeen.Time).Seconds()
	}

	if response.StalenessSec < 0 {
		response.StalenessSec = 0
	}

	return response
}

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

// ListRobots returns all robots
func (s *Server) ListRobots(w http.ResponseWriter, r *http.Request) {
	robots, err := s.repo.GetAllRobots()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]RobotResponse, len(robots))
	for i, robot := range robots {
		responses[i] = robotToResponse(&robot, s.stateManager)
	}

	writeJSON(w, http.StatusOK, responses)
}

// ConnectRobot registers or reconnects a robot
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

	// Create or update robot in database
	robot := &db.Robot{
		ID:           req.ID,
		Name:         req.Name,
		CurrentState: "idle",
		CreatedAt:    time.Now(),
	}

	if req.AgentID != "" {
		robot.AgentID = sql.NullString{String: req.AgentID, Valid: true}

		// Auto-create agent if not exists
		agent, _ := s.repo.GetAgent(req.AgentID)
		if agent == nil {
			newAgent := &db.Agent{
				ID:        req.AgentID,
				Name:      req.AgentID,
				Status:    "online",
				CreatedAt: time.Now(),
			}
			s.repo.CreateOrUpdateAgent(newAgent)
		}
	}
	if req.IPAddress != "" {
		robot.IPAddress = sql.NullString{String: req.IPAddress, Valid: true}
	}

	robot.LastSeen = sql.NullTime{Time: time.Now(), Valid: true}

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

	response := robotToResponse(robot, s.stateManager)
	writeJSON(w, http.StatusOK, response)
}

// GetRobot returns a single robot
func (s *Server) GetRobot(w http.ResponseWriter, r *http.Request) {
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

	response := RobotDetailResponse{
		RobotResponse: robotToResponse(robot, s.stateManager),
	}

	// Add auto-discovered capabilities (capability-based model)
	capabilities, err := s.repo.GetRobotCapabilities(robotID)
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

// DeleteRobot deletes a robot
func (s *Server) DeleteRobot(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	if err := s.repo.DeleteRobot(robotID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.stateManager.UnregisterRobot(robotID)

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Robot deleted",
	})
}


// GetCommands returns pending commands for a robot
func (s *Server) GetCommands(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	commands, err := s.repo.GetPendingCommands(robotID)
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

// Helper function to convert db.Robot to RobotResponse
func robotToResponse(robot *db.Robot, sm *state.GlobalStateManager) RobotResponse {
	response := RobotResponse{
		ID:           robot.ID,
		Name:         robot.Name,
		Namespace:    robot.Namespace,
		CurrentState: robot.CurrentState,
		CreatedAt:    robot.CreatedAt,
	}

	if robot.AgentID.Valid {
		response.AgentID = robot.AgentID.String
	}
	if robot.IPAddress.Valid {
		response.IPAddress = robot.IPAddress.String
	}
	if robot.LastSeen.Valid {
		response.LastSeen = &robot.LastSeen.Time
	}
	if robot.Tags != nil {
		json.Unmarshal(robot.Tags, &response.Tags)
	}

	now := time.Now()

	// Get online status from state manager
	if robotState, exists := sm.GetRobotState(robot.ID); exists {
		response.IsOnline = robotState.IsOnline
		response.CurrentState = robotState.CurrentState
		if !robotState.LastSeen.IsZero() {
			response.StalenessSec = now.Sub(robotState.LastSeen).Seconds()
		}
	} else if robot.LastSeen.Valid {
		response.StalenessSec = now.Sub(robot.LastSeen.Time).Seconds()
	}

	if response.StalenessSec < 0 {
		response.StalenessSec = 0
	}

	return response
}

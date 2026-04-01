package api

import (
	"encoding/json"
	"net/http"
	"time"

	"central_server_go/internal/state"

	"github.com/go-chi/chi/v5"
)

// GetFleetState returns the current state of the fleet
func (s *Server) GetFleetState(w http.ResponseWriter, r *http.Request) {
	snapshot := s.stateManager.GetSnapshot()

	response := FleetStateResponse{
		Timestamp: snapshot.Timestamp.UnixMilli(),
		Robots:    make(map[string]*RobotStateSnapshot),
		Agents:    make(map[string]*AgentStateSnapshot),
		Zones:     make(map[string]*ZoneReservationState),
	}

	now := time.Now()

	// Convert robots
	for id, robot := range snapshot.Robots {
		// Determine execution phase
		executionPhase := "idle"
		if !robot.IsOnline {
			executionPhase = "offline"
		} else if robot.IsExecuting {
			if robot.CurrentStepID == "" {
				executionPhase = "starting"
			} else {
				executionPhase = "executing"
			}
		}

		response.Robots[id] = &RobotStateSnapshot{
			ID:             robot.ID,
			Name:           robot.Name,
			AgentID:        robot.AgentID,
			CurrentState:   robot.CurrentState,
			StateCode:      robot.CurrentStateCode,
			CurrentGraphID: robot.CurrentGraphID,
			ExecutionPhase: executionPhase,
			SemanticTags:   robot.SemanticTags,
			IsOnline:       robot.IsOnline,
			IsExecuting:    robot.IsExecuting,
			CurrentTaskID:  robot.CurrentTaskID,
			CurrentStepID:  robot.CurrentStepID,
			StalenessSec:   now.Sub(robot.LastSeen).Seconds(),
		}
	}

	// Convert agents
	for id, agent := range snapshot.Agents {
		response.Agents[id] = &AgentStateSnapshot{
			ID:           agent.ID,
			Name:         agent.Name,
			IsOnline:     true, // Agents in snapshot are online
			StalenessSec: now.Sub(agent.LastHeartbeat).Seconds(),
		}
	}

	// Convert zones
	for id, zone := range snapshot.Zones {
		response.Zones[id] = &ZoneReservationState{
			ZoneID:     zone.ZoneID,
			AgentID:    zone.AgentID,
			ReservedAt: zone.ReservedAt.UnixMilli(),
			ExpiresAt:  zone.ExpiresAt.UnixMilli(),
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// ValidatePreconditions validates preconditions for an agent
func (s *Server) ValidatePreconditions(w http.ResponseWriter, r *http.Request) {
	var req ValidatePreconditionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	// Convert preconditions to state.Precondition
	preconditions := make([]state.Precondition, len(req.Preconditions))
	for i, p := range req.Preconditions {
		preconditions[i] = state.Precondition{
			Type:      getString(p, "type"),
			Condition: getString(p, "condition"),
			Message:   getString(p, "message"),
		}
	}

	// Evaluate preconditions (1:1 model: agent_id = robot_id)
	passed, errorMsg := s.stateManager.EvaluatePreconditions(req.AgentID, preconditions)

	response := ValidatePreconditionsResponse{
		Valid:        passed,
		ErrorMessage: errorMsg,
	}

	// Add detailed results
	if !passed {
		robotState, exists := s.stateManager.GetRobotState(req.AgentID)
		if exists {
			response.Details = map[string]interface{}{
				"agent_state":  robotState.CurrentState,
				"is_online":    robotState.IsOnline,
				"is_executing": robotState.IsExecuting,
			}
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// Helper function to get string from map
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// GetRobotState returns a single robot's state snapshot
// In 1:1 model, agentID = robotID, URL param kept for backward compatibility
func (s *Server) GetRobotState(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "robotID") // URL param name kept for backward compatibility

	robotState, exists := s.stateManager.GetRobotState(agentID)
	if !exists {
		writeError(w, http.StatusNotFound, "Agent not found: "+agentID)
		return
	}

	now := time.Now()

	// Determine execution phase
	executionPhase := "idle"
	if !robotState.IsOnline {
		executionPhase = "offline"
	} else if robotState.IsWaitingForPrecondition {
		executionPhase = "waiting_for_precondition"
	} else if robotState.IsExecuting {
		if robotState.CurrentStepID == "" {
			executionPhase = "starting"
		} else {
			executionPhase = "executing"
		}
	}

	response := &RobotStateSnapshot{
		ID:             robotState.ID,
		Name:           robotState.Name,
		AgentID:        robotState.AgentID,
		CurrentState:   robotState.CurrentState,
		StateCode:      robotState.CurrentStateCode,
		CurrentGraphID: robotState.CurrentGraphID,
		ExecutionPhase: executionPhase,
		SemanticTags:   robotState.SemanticTags,
		IsOnline:       robotState.IsOnline,
		IsExecuting:    robotState.IsExecuting,
		CurrentTaskID:  robotState.CurrentTaskID,
		CurrentStepID:  robotState.CurrentStepID,
		StalenessSec:   now.Sub(robotState.LastSeen).Seconds(),
	}

	// Add precondition waiting info
	if robotState.IsWaitingForPrecondition {
		response.IsWaitingForPrecondition = true
		if !robotState.WaitingForPreconditionSince.IsZero() {
			response.WaitingForPreconditionSince = robotState.WaitingForPreconditionSince.Format(time.RFC3339)
		}
		if len(robotState.BlockingConditions) > 0 {
			response.BlockingConditions = make([]BlockingConditionInfoResponse, len(robotState.BlockingConditions))
			for i, bc := range robotState.BlockingConditions {
				response.BlockingConditions[i] = BlockingConditionInfoResponse{
					ConditionID:     bc.ConditionID,
					Description:     bc.Description,
					TargetAgentID:   bc.TargetAgentID,
					TargetAgentName: bc.TargetAgentName,
					RequiredState:   bc.RequiredState,
					CurrentState:    bc.CurrentState,
					Reason:          bc.Reason,
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// GetAgentRobotsState returns all robots' states for a specific agent
func (s *Server) GetAgentRobotsState(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")

	snapshot := s.stateManager.GetSnapshot()
	now := time.Now()

	robots := make([]*RobotStateSnapshot, 0)
	onlineCount := 0

	for _, robot := range snapshot.Robots {
		if robot.AgentID == agentID {
			// Determine execution phase
			executionPhase := "idle"
			if !robot.IsOnline {
				executionPhase = "offline"
			} else if robot.IsWaitingForPrecondition {
				executionPhase = "waiting_for_precondition"
			} else if robot.IsExecuting {
				if robot.CurrentStepID == "" {
					executionPhase = "starting"
				} else {
					executionPhase = "executing"
				}
			}

			robotSnapshot := &RobotStateSnapshot{
				ID:             robot.ID,
				Name:           robot.Name,
				AgentID:        robot.AgentID,
				CurrentState:   robot.CurrentState,
				StateCode:      robot.CurrentStateCode,
				CurrentGraphID: robot.CurrentGraphID,
				ExecutionPhase: executionPhase,
				SemanticTags:   robot.SemanticTags,
				IsOnline:       robot.IsOnline,
				IsExecuting:    robot.IsExecuting,
				CurrentTaskID:  robot.CurrentTaskID,
				CurrentStepID:  robot.CurrentStepID,
				StalenessSec:   now.Sub(robot.LastSeen).Seconds(),
			}

			// Add precondition waiting info
			if robot.IsWaitingForPrecondition {
				robotSnapshot.IsWaitingForPrecondition = true
				if !robot.WaitingForPreconditionSince.IsZero() {
					robotSnapshot.WaitingForPreconditionSince = robot.WaitingForPreconditionSince.Format(time.RFC3339)
				}
				if len(robot.BlockingConditions) > 0 {
					robotSnapshot.BlockingConditions = make([]BlockingConditionInfoResponse, len(robot.BlockingConditions))
					for i, bc := range robot.BlockingConditions {
						robotSnapshot.BlockingConditions[i] = BlockingConditionInfoResponse{
							ConditionID:     bc.ConditionID,
							Description:     bc.Description,
							TargetAgentID:   bc.TargetAgentID,
							TargetAgentName: bc.TargetAgentName,
							RequiredState:   bc.RequiredState,
							CurrentState:    bc.CurrentState,
							Reason:          bc.Reason,
						}
					}
				}
			}

			robots = append(robots, robotSnapshot)
			if robot.IsOnline {
				onlineCount++
			}
		}
	}

	response := map[string]interface{}{
		"agent_id":  agentID,
		"timestamp": snapshot.Timestamp.UnixMilli(),
		"robots":    robots,
		"total":     len(robots),
		"online":    onlineCount,
	}

	writeJSON(w, http.StatusOK, response)
}

// ResetAgentState resets an agent's state to the initial "idle" state
// This is useful for recovering from stuck states or starting fresh
func (s *Server) ResetAgentState(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	agentCommandSent := false
	if s.quicHandler != nil {
		if err := s.quicHandler.SendResetAgentStateCommand(agentID, "manual reset from MCS"); err == nil {
			agentCommandSent = true
		}
	}

	// Reset state in memory
	err := s.stateManager.ResetAgentState(agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Clear live planning overlay for this agent to avoid stale error/warning
	// values immediately re-appearing in realtime PDDL monitors.
	clearedSessions := 0
	if s.realtimePddl != nil {
		clearedSessions = s.realtimePddl.ClearRuntimeStateByAgent(agentID, "agent:"+agentID)
	}

	// Update DB if agent exists there
	if s.repo != nil {
		// Reset enhanced state in DB (state_code, semantic_tags, graph_id)
		s.repo.UpdateAgentEnhancedState(agentID, "idle", []string{}, "")
		// Also update status to idle
		s.repo.UpdateAgentStatus(agentID, "idle", "")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"agent_id":         agentID,
		"state":            "idle",
		"agent_command_sent": agentCommandSent,
		"message":          "Agent state reset to idle",
		"cleared_sessions": clearedSessions,
	})
}

// GetFleetSummary returns fleet summary statistics
func (s *Server) GetFleetSummary(w http.ResponseWriter, r *http.Request) {
	snapshot := s.stateManager.GetSnapshot()
	now := time.Now()

	// Calculate state distribution
	stateCounts := make(map[string]int)
	onlineRobots := 0
	freshCount := 0
	staleCount := 0

	for _, robot := range snapshot.Robots {
		stateCounts[robot.CurrentState]++
		if robot.IsOnline {
			onlineRobots++
		}
		if now.Sub(robot.LastSeen).Seconds() < 30.0 {
			freshCount++
		} else {
			staleCount++
		}
	}

	// Calculate agent distribution (1:1 model: each agent = 1 robot)
	agentCounts := make(map[string]int)
	for agentID := range snapshot.Agents {
		agentCounts[agentID] = 1 // In 1:1 model, agent_id = robot_id
	}

	response := map[string]interface{}{
		"timestamp":      snapshot.Timestamp.UnixMilli(),
		"total_robots":   len(snapshot.Robots),
		"online_robots":  onlineRobots,
		"offline_robots": len(snapshot.Robots) - onlineRobots,
		"fresh_robots":   freshCount,
		"stale_robots":   staleCount,
		"by_state":       stateCounts,
		"by_agent":       agentCounts,
	}

	writeJSON(w, http.StatusOK, response)
}

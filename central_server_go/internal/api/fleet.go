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
		response.Robots[id] = &RobotStateSnapshot{
			ID:            robot.ID,
			Name:          robot.Name,
			AgentID:       robot.AgentID,
			CurrentState:  robot.CurrentState,
			IsOnline:      robot.IsOnline,
			IsExecuting:   robot.IsExecuting,
			CurrentTaskID: robot.CurrentTaskID,
			CurrentStepID: robot.CurrentStepID,
			StalenessSec:  now.Sub(robot.LastSeen).Seconds(),
		}
	}

	// Convert agents
	for id, agent := range snapshot.Agents {
		response.Agents[id] = &AgentStateSnapshot{
			ID:           agent.ID,
			Name:         agent.Name,
			IsOnline:     true, // Agents in snapshot are online
			RobotIDs:     agent.RobotIDs,
			StalenessSec: now.Sub(agent.LastHeartbeat).Seconds(),
		}
	}

	// Convert zones
	for id, zone := range snapshot.Zones {
		response.Zones[id] = &ZoneReservationState{
			ZoneID:     zone.ZoneID,
			RobotID:    zone.RobotID,
			ReservedAt: zone.ReservedAt.UnixMilli(),
			ExpiresAt:  zone.ExpiresAt.UnixMilli(),
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// ValidatePreconditions validates preconditions for a robot
func (s *Server) ValidatePreconditions(w http.ResponseWriter, r *http.Request) {
	var req ValidatePreconditionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.RobotID == "" {
		writeError(w, http.StatusBadRequest, "robot_id is required")
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

	// Evaluate preconditions
	passed, errorMsg := s.stateManager.EvaluatePreconditions(req.RobotID, preconditions)

	response := ValidatePreconditionsResponse{
		Valid:        passed,
		ErrorMessage: errorMsg,
	}

	// Add detailed results
	if !passed {
		robotState, exists := s.stateManager.GetRobotState(req.RobotID)
		if exists {
			response.Details = map[string]interface{}{
				"robot_state":  robotState.CurrentState,
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
func (s *Server) GetRobotState(w http.ResponseWriter, r *http.Request) {
	robotID := chi.URLParam(r, "robotID")

	robotState, exists := s.stateManager.GetRobotState(robotID)
	if !exists {
		writeError(w, http.StatusNotFound, "Robot not found: "+robotID)
		return
	}

	now := time.Now()
	response := &RobotStateSnapshot{
		ID:            robotState.ID,
		Name:          robotState.Name,
		AgentID:       robotState.AgentID,
		CurrentState:  robotState.CurrentState,
		IsOnline:      robotState.IsOnline,
		IsExecuting:   robotState.IsExecuting,
		CurrentTaskID: robotState.CurrentTaskID,
		CurrentStepID: robotState.CurrentStepID,
		StalenessSec:  now.Sub(robotState.LastSeen).Seconds(),
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
			robotSnapshot := &RobotStateSnapshot{
				ID:            robot.ID,
				Name:          robot.Name,
				AgentID:       robot.AgentID,
				CurrentState:  robot.CurrentState,
				IsOnline:      robot.IsOnline,
				IsExecuting:   robot.IsExecuting,
				CurrentTaskID: robot.CurrentTaskID,
				CurrentStepID: robot.CurrentStepID,
				StalenessSec:  now.Sub(robot.LastSeen).Seconds(),
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

	// Calculate agent distribution
	agentCounts := make(map[string]int)
	for agentID, agent := range snapshot.Agents {
		agentCounts[agentID] = len(agent.RobotIDs)
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

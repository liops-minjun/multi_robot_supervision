package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"central_server_go/internal/db"

	"github.com/go-chi/chi/v5"
)

// ListTasks returns all tasks
func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	status := r.URL.Query().Get("status")

	tasks, err := s.repo.GetTasks(agentID, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]TaskResponse, len(tasks))
	for i, task := range tasks {
		responses[i] = taskToResponse(&task, s.repo)
	}

	writeJSON(w, http.StatusOK, responses)
}

// GetTask returns a single task
func (s *Server) GetTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	task, err := s.repo.GetTask(taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(task, s.repo))
}

// CancelTask cancels a running task
func (s *Server) CancelTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	var req TaskControlRequest
	json.NewDecoder(r.Body).Decode(&req)

	reason := req.Reason
	if reason == "" {
		reason = "User requested cancellation"
	}

	if err := s.scheduler.CancelTask(taskID, reason); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Task cancelled",
	})
}

// PauseTask pauses a running task
func (s *Server) PauseTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	if err := s.scheduler.PauseTask(taskID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Task paused",
	})
}

// ResumeTask resumes a paused task
func (s *Server) ResumeTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	if err := s.scheduler.ResumeTask(taskID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Task resumed",
	})
}


// GetTaskPreconditionStatus returns the precondition waiting status for a task
func (s *Server) GetTaskPreconditionStatus(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	task, err := s.repo.GetTask(taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	response := PreconditionStatusResponse{
		TaskID:                      taskID,
		IsWaitingForPrecondition:    task.WaitingForPreconditionSince.Valid,
		PreconditionTimeoutSec:      task.PreconditionTimeoutSec,
	}

	if task.WaitingForPreconditionSince.Valid {
		response.WaitingForPreconditionSince = &task.WaitingForPreconditionSince.Time

		// Calculate remaining timeout
		elapsed := int(time.Since(task.WaitingForPreconditionSince.Time).Seconds())
		if task.PreconditionTimeoutSec > 0 {
			response.RemainingTimeoutSec = task.PreconditionTimeoutSec - elapsed
			if response.RemainingTimeoutSec < 0 {
				response.RemainingTimeoutSec = 0
			}
		}
	}

	if task.BlockingConditions != nil {
		var blockingInfos []db.BlockingConditionInfo
		if err := json.Unmarshal(task.BlockingConditions, &blockingInfos); err == nil && len(blockingInfos) > 0 {
			response.BlockingConditions = make([]BlockingConditionInfoResponse, len(blockingInfos))
			for i, bc := range blockingInfos {
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

	// Also get real-time status from state manager
	if task.AgentID.Valid {
		if robotState, ok := s.stateManager.GetRobotState(task.AgentID.String); ok {
			response.AgentID = task.AgentID.String
			response.AgentName = robotState.Name
			response.CurrentState = robotState.CurrentState
			response.IsAgentOnline = robotState.IsOnline
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// PreconditionStatusResponse represents detailed precondition status
type PreconditionStatusResponse struct {
	TaskID                      string                          `json:"task_id"`
	AgentID                     string                          `json:"agent_id,omitempty"`
	AgentName                   string                          `json:"agent_name,omitempty"`
	CurrentState                string                          `json:"current_state,omitempty"`
	IsAgentOnline               bool                            `json:"is_agent_online"`
	IsWaitingForPrecondition    bool                            `json:"is_waiting_for_precondition"`
	WaitingForPreconditionSince *time.Time                      `json:"waiting_for_precondition_since,omitempty"`
	BlockingConditions          []BlockingConditionInfoResponse `json:"blocking_conditions,omitempty"`
	PreconditionTimeoutSec      int                             `json:"precondition_timeout_sec"`
	RemainingTimeoutSec         int                             `json:"remaining_timeout_sec,omitempty"`
}

// GetTaskLogs returns execution logs for a specific task
func (s *Server) GetTaskLogs(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "Task ID is required")
		return
	}

	// Parse limit from query params
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := parseInt(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	logs := s.stateManager.TaskLogManager().GetTaskLogs(taskID, limit)
	writeJSON(w, http.StatusOK, logs)
}

// GetAgentLogs returns execution logs for a specific agent
func (s *Server) GetAgentLogs(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "Agent ID is required")
		return
	}

	// Parse limit from query params
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := parseInt(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	logs := s.stateManager.TaskLogManager().GetAgentLogs(agentID, limit)
	writeJSON(w, http.StatusOK, logs)
}

// GetRecentLogs returns recent logs across all agents
func (s *Server) GetRecentLogs(w http.ResponseWriter, r *http.Request) {
	// Parse limit from query params
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := parseInt(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	logs := s.stateManager.TaskLogManager().GetRecentLogs(limit)
	writeJSON(w, http.StatusOK, logs)
}

// GetLogStats returns task log manager statistics
func (s *Server) GetLogStats(w http.ResponseWriter, r *http.Request) {
	stats := s.stateManager.TaskLogManager().GetStats()
	writeJSON(w, http.StatusOK, stats)
}

// parseInt helper for parsing query parameters
func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// Helper function to convert db.Task to TaskResponse
func taskToResponse(task *db.Task, repo *db.Repository) TaskResponse {
	response := TaskResponse{
		ID:               task.ID,
		Status:           task.Status,
		CurrentStepIndex: task.CurrentStepIndex,
		CreatedAt:        task.CreatedAt,
	}

	if task.ActionGraphID.Valid {
		response.ActionGraphID = task.ActionGraphID.String
		// Get action graph name
		graph, _ := repo.GetActionGraph(task.ActionGraphID.String)
		if graph != nil {
			response.ActionGraphName = graph.Name
		}
	}
	if task.AgentID.Valid {
		response.AgentID = task.AgentID.String
		// Get agent name
		agent, _ := repo.GetAgent(task.AgentID.String)
		if agent != nil {
			response.AgentName = agent.Name
		}
	}
	if task.CurrentStepID.Valid {
		response.CurrentStepID = task.CurrentStepID.String
	}
	if task.ErrorMessage.Valid {
		response.ErrorMessage = task.ErrorMessage.String
	}
	if task.StartedAt.Valid {
		response.StartedAt = &task.StartedAt.Time
	}
	if task.CompletedAt.Valid {
		response.CompletedAt = &task.CompletedAt.Time
	}
	if task.StepResults != nil {
		json.Unmarshal(task.StepResults, &response.StepResults)
	}

	// Precondition waiting status
	if task.WaitingForPreconditionSince.Valid {
		response.IsWaitingForPrecondition = true
		response.WaitingForPreconditionSince = &task.WaitingForPreconditionSince.Time
	}
	if task.BlockingConditions != nil {
		var blockingInfos []db.BlockingConditionInfo
		if err := json.Unmarshal(task.BlockingConditions, &blockingInfos); err == nil && len(blockingInfos) > 0 {
			response.BlockingConditions = make([]BlockingConditionInfoResponse, len(blockingInfos))
			for i, bc := range blockingInfos {
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
	if task.PreconditionTimeoutSec > 0 {
		response.PreconditionTimeoutSec = task.PreconditionTimeoutSec
	}

	return response
}

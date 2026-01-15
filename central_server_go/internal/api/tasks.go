package api

import (
	"encoding/json"
	"fmt"
	"net/http"

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

	return response
}

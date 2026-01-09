package api

import (
	"encoding/json"
	"net/http"

	"central_server_go/internal/db"
	"central_server_go/internal/executor"

	"github.com/go-chi/chi/v5"
)

// ListTasks returns all tasks
func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request) {
	robotID := r.URL.Query().Get("robot_id")
	status := r.URL.Query().Get("status")

	tasks, err := s.repo.GetTasks(robotID, status)
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

// ConfirmTask handles manual confirmation for a task
func (s *Server) ConfirmTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	// Get the task from scheduler
	task, exists := s.scheduler.GetTask(taskID)
	if !exists {
		writeError(w, http.StatusNotFound, "Task not found or not active")
		return
	}

	// Check if task is waiting for confirmation
	if task.Status != executor.TaskRunning {
		writeError(w, http.StatusBadRequest, "Task is not running")
		return
	}

	// TODO: Implement confirmation signaling to the running task
	// For now, this would need to integrate with the scheduler's step execution

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Confirmation received",
	})
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
	if task.RobotID.Valid {
		response.RobotID = task.RobotID.String
		// Get robot name
		robot, _ := repo.GetRobot(task.RobotID.String)
		if robot != nil {
			response.RobotName = robot.Name
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

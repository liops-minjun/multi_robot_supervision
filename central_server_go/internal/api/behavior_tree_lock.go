package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Lock configuration
const (
	LockDuration    = 5 * time.Minute  // Lock expires after 5 minutes
	HeartbeatExtend = 2 * time.Minute  // Heartbeat extends lock by 2 minutes
)

// LockAcquireRequest represents a request to acquire a lock
type LockAcquireRequest struct {
	SessionID string `json:"session_id"`
	UserName  string `json:"user_name"`
}

// LockStatusResponse represents the lock status of a behavior tree
type LockStatusResponse struct {
	IsLocked  bool   `json:"is_locked"`
	LockedBy  string `json:"locked_by,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"` // Unix timestamp in milliseconds
	IsOwnLock bool   `json:"is_own_lock"`
}

// AcquireBehaviorTreeLock attempts to acquire an edit lock on a behavior tree
// POST /api/behavior-trees/{graphID}/lock
func (s *Server) AcquireBehaviorTreeLock(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")

	var req LockAcquireRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	// Get the behavior tree
	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	// Check if any agent is currently executing this behavior tree
	executingAgents := s.stateManager.GetAgentsExecutingGraph(graphID)
	if len(executingAgents) > 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":            "executing",
			"message":          "Behavior Tree is currently being executed by agent(s)",
			"executing_agents": executingAgents,
		})
		return
	}

	now := time.Now()

	// Check if already locked by someone else
	if graph.LockSessionID.Valid && graph.LockExpiresAt.Valid {
		if graph.LockExpiresAt.Time.After(now) {
			// Lock is still valid
			if graph.LockSessionID.String != req.SessionID {
				// Locked by someone else
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"error":     "locked",
					"message":   "Behavior Tree is locked by another user",
					"locked_by": graph.LockedBy.String,
					"expires_at": graph.LockExpiresAt.Time.UnixMilli(),
				})
				return
			}
			// Same session already has the lock - extend it
		}
	}

	// Acquire or extend the lock
	userName := req.UserName
	if userName == "" {
		userName = "Unknown User"
	}

	expiresAt := now.Add(LockDuration)

	graph.LockedBy = sql.NullString{String: userName, Valid: true}
	graph.LockedAt = sql.NullTime{Time: now, Valid: true}
	graph.LockExpiresAt = sql.NullTime{Time: expiresAt, Valid: true}
	graph.LockSessionID = sql.NullString{String: req.SessionID, Valid: true}

	if err := s.repo.UpdateBehaviorTree(graph); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to acquire lock: "+err.Error())
		return
	}

	// Broadcast lock acquired event via WebSocket
	s.wsHub.BroadcastBehaviorTreeLock(graphID, "acquired", userName, expiresAt.UnixMilli())

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"locked_by":  userName,
		"expires_at": expiresAt.UnixMilli(),
	})
}

// ReleaseBehaviorTreeLock releases an edit lock on a behavior tree
// DELETE /api/behavior-trees/{graphID}/lock
func (s *Server) ReleaseBehaviorTreeLock(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")
	sessionID := r.Header.Get("X-Session-ID")

	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "X-Session-ID header is required")
		return
	}

	// Get the behavior tree
	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	// Verify ownership
	if !graph.LockSessionID.Valid || graph.LockSessionID.String != sessionID {
		writeError(w, http.StatusForbidden, "You do not own this lock")
		return
	}

	// Release the lock
	graph.LockedBy = sql.NullString{}
	graph.LockedAt = sql.NullTime{}
	graph.LockExpiresAt = sql.NullTime{}
	graph.LockSessionID = sql.NullString{}

	if err := s.repo.UpdateBehaviorTree(graph); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to release lock: "+err.Error())
		return
	}

	// Broadcast lock released event via WebSocket
	s.wsHub.BroadcastBehaviorTreeLock(graphID, "released", "", 0)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Lock released",
	})
}

// GetBehaviorTreeLockStatus returns the lock status of a behavior tree
// GET /api/behavior-trees/{graphID}/lock
func (s *Server) GetBehaviorTreeLockStatus(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")
	sessionID := r.Header.Get("X-Session-ID")

	// Get the behavior tree
	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	now := time.Now()
	response := LockStatusResponse{
		IsLocked:  false,
		IsOwnLock: false,
	}

	if graph.LockSessionID.Valid && graph.LockExpiresAt.Valid {
		if graph.LockExpiresAt.Time.After(now) {
			// Lock is still valid
			response.IsLocked = true
			response.LockedBy = graph.LockedBy.String
			response.ExpiresAt = graph.LockExpiresAt.Time.UnixMilli()
			response.IsOwnLock = sessionID != "" && graph.LockSessionID.String == sessionID
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// HeartbeatBehaviorTreeLock extends a lock on a behavior tree
// POST /api/behavior-trees/{graphID}/lock/heartbeat
func (s *Server) HeartbeatBehaviorTreeLock(w http.ResponseWriter, r *http.Request) {
	graphID := chi.URLParam(r, "graphID")
	sessionID := r.Header.Get("X-Session-ID")

	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "X-Session-ID header is required")
		return
	}

	// Get the behavior tree
	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if graph == nil {
		writeError(w, http.StatusNotFound, "Behavior Tree not found")
		return
	}

	now := time.Now()

	// Verify the lock exists and is owned by this session
	if !graph.LockSessionID.Valid {
		writeError(w, http.StatusConflict, "No active lock")
		return
	}

	if graph.LockSessionID.String != sessionID {
		writeError(w, http.StatusForbidden, "You do not own this lock")
		return
	}

	// Check if lock has already expired
	if graph.LockExpiresAt.Valid && graph.LockExpiresAt.Time.Before(now) {
		writeError(w, http.StatusConflict, "Lock has expired")
		return
	}

	// Extend the lock
	newExpiresAt := now.Add(LockDuration)
	graph.LockExpiresAt = sql.NullTime{Time: newExpiresAt, Valid: true}

	if err := s.repo.UpdateBehaviorTree(graph); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to extend lock: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"expires_at": newExpiresAt.UnixMilli(),
	})
}

// ValidateLockOwnership checks if a session owns the lock on a behavior tree
// Returns true if the lock is owned by the session or if there is no lock
func (s *Server) ValidateLockOwnership(graphID, sessionID string) (bool, string, error) {
	graph, err := s.repo.GetBehaviorTree(graphID)
	if err != nil {
		return false, "", err
	}
	if graph == nil {
		return false, "", nil
	}

	now := time.Now()

	// No lock or lock expired - allow edit
	if !graph.LockSessionID.Valid || !graph.LockExpiresAt.Valid || graph.LockExpiresAt.Time.Before(now) {
		return true, "", nil
	}

	// Lock exists and is valid - check ownership
	if graph.LockSessionID.String == sessionID {
		return true, "", nil
	}

	// Locked by someone else
	return false, graph.LockedBy.String, nil
}

// StartLockCleanup starts a background goroutine that periodically cleans up expired locks
func (s *Server) StartLockCleanup() {
	go s.runLockCleanup()
}

// runLockCleanup periodically checks for and clears expired locks
func (s *Server) runLockCleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupExpiredLocks()
	}
}

// cleanupExpiredLocks finds and clears all expired locks
func (s *Server) cleanupExpiredLocks() {
	// Get all behavior trees with expired locks
	expiredGraphs, err := s.repo.GetBehaviorTreesWithExpiredLocks()
	if err != nil {
		// Log error but don't crash
		return
	}

	now := time.Now()
	for _, graph := range expiredGraphs {
		// Double check the lock is actually expired
		if !graph.LockExpiresAt.Valid || graph.LockExpiresAt.Time.After(now) {
			continue
		}

		// Clear the lock
		graph.LockedBy = sql.NullString{}
		graph.LockedAt = sql.NullTime{}
		graph.LockExpiresAt = sql.NullTime{}
		graph.LockSessionID = sql.NullString{}

		if err := s.repo.UpdateBehaviorTree(&graph); err != nil {
			continue
		}

		// Broadcast lock expired event via WebSocket
		s.wsHub.BroadcastBehaviorTreeLock(graph.ID, "expired", "", 0)
	}
}

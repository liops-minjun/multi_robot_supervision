package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// TeachRequest represents a request to create a waypoint from robot's current state
type TeachRequest struct {
	Name         string `json:"name"`
	WaypointType string `json:"waypoint_type,omitempty"`
	Description  string `json:"description,omitempty"`
}

// TeachWaypoint creates a waypoint from robot's current state
func (s *Server) TeachWaypoint(w http.ResponseWriter, r *http.Request) {
	// Support both agentID and robotID (1 Agent = 1 Robot)
	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		agentID = chi.URLParam(r, "robotID")
	}

	// Get agent (1 Agent = 1 Robot)
	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "Agent not found: "+agentID)
		return
	}

	var req TeachRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "Waypoint name is required")
		return
	}

	writeError(w, http.StatusBadRequest, "Waypoint teach requires manual data")
	return
}

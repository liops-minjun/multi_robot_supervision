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
	robotID := chi.URLParam(r, "robotID")

	// Get robot
	robot, err := s.repo.GetRobot(robotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if robot == nil {
		writeError(w, http.StatusNotFound, "Robot not found: "+robotID)
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

package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"central_server_go/internal/db"

	"github.com/go-chi/chi/v5"
	"gorm.io/datatypes"
)

// ListWaypoints returns all waypoints
func (s *Server) ListWaypoints(w http.ResponseWriter, r *http.Request) {
	waypointType := r.URL.Query().Get("waypoint_type")

	waypoints, err := s.repo.GetWaypoints(waypointType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]WaypointResponse, len(waypoints))
	for i, wp := range waypoints {
		responses[i] = waypointToResponse(&wp)
	}

	writeJSON(w, http.StatusOK, responses)
}

// CreateWaypoint creates a new waypoint
func (s *Server) CreateWaypoint(w http.ResponseWriter, r *http.Request) {
	var req WaypointCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" || req.Name == "" || req.WaypointType == "" {
		writeError(w, http.StatusBadRequest, "id, name, and waypoint_type are required")
		return
	}

	// Check if ID already exists
	existing, _ := s.repo.GetWaypoint(req.ID)
	if existing != nil {
		writeError(w, http.StatusConflict, "Waypoint already exists")
		return
	}

	dataJSON, _ := json.Marshal(req.Data)
	tagsJSON, _ := json.Marshal(req.Tags)

	wp := &db.Waypoint{
		ID:           req.ID,
		Name:         req.Name,
		WaypointType: req.WaypointType,
		Data:         datatypes.JSON(dataJSON),
		Tags:         datatypes.JSON(tagsJSON),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if req.CreatedBy != "" {
		wp.CreatedBy = sql.NullString{String: req.CreatedBy, Valid: true}
	}
	if req.Description != "" {
		wp.Description = sql.NullString{String: req.Description, Valid: true}
	}

	if err := s.repo.CreateWaypoint(wp); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, waypointToResponse(wp))
}

// GetWaypoint returns a single waypoint
func (s *Server) GetWaypoint(w http.ResponseWriter, r *http.Request) {
	waypointID := chi.URLParam(r, "waypointID")

	wp, err := s.repo.GetWaypoint(waypointID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wp == nil {
		writeError(w, http.StatusNotFound, "Waypoint not found")
		return
	}

	writeJSON(w, http.StatusOK, waypointToResponse(wp))
}

// UpdateWaypoint updates a waypoint
func (s *Server) UpdateWaypoint(w http.ResponseWriter, r *http.Request) {
	waypointID := chi.URLParam(r, "waypointID")

	wp, err := s.repo.GetWaypoint(waypointID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wp == nil {
		writeError(w, http.StatusNotFound, "Waypoint not found")
		return
	}

	var req WaypointUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != "" {
		wp.Name = req.Name
	}
	if req.Data != nil {
		dataJSON, _ := json.Marshal(req.Data)
		wp.Data = datatypes.JSON(dataJSON)
	}
	if req.Description != "" {
		wp.Description = sql.NullString{String: req.Description, Valid: true}
	}
	if req.Tags != nil {
		tagsJSON, _ := json.Marshal(req.Tags)
		wp.Tags = datatypes.JSON(tagsJSON)
	}

	wp.UpdatedAt = time.Now()

	if err := s.repo.UpdateWaypoint(wp); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, waypointToResponse(wp))
}

// DeleteWaypoint deletes a waypoint
func (s *Server) DeleteWaypoint(w http.ResponseWriter, r *http.Request) {
	waypointID := chi.URLParam(r, "waypointID")

	if err := s.repo.DeleteWaypoint(waypointID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Waypoint deleted",
	})
}

// Helper function
func waypointToResponse(wp *db.Waypoint) WaypointResponse {
	response := WaypointResponse{
		ID:           wp.ID,
		Name:         wp.Name,
		WaypointType: wp.WaypointType,
		CreatedAt:    wp.CreatedAt,
		UpdatedAt:    wp.UpdatedAt,
	}

	if wp.CreatedBy.Valid {
		response.CreatedBy = wp.CreatedBy.String
	}
	if wp.Description.Valid {
		response.Description = wp.Description.String
	}
	if wp.Data != nil {
		json.Unmarshal(wp.Data, &response.Data)
	}
	if wp.Tags != nil {
		json.Unmarshal(wp.Tags, &response.Tags)
	}

	return response
}

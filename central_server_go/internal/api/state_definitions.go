package api

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"central_server_go/internal/db"

	"github.com/go-chi/chi/v5"
)

type StateDefinitionResponse struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description,omitempty"`
	States            []string               `json:"states"`
	DefaultState      string                 `json:"default_state"`
	ActionMappings    []db.StateActionMapping `json:"action_mappings,omitempty"`
	TeachableWaypoints []string              `json:"teachable_waypoints,omitempty"`
	Version           int                    `json:"version"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

type StateDefinitionCreateRequest struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description,omitempty"`
	States            []string               `json:"states"`
	DefaultState      string                 `json:"default_state"`
	ActionMappings    []db.StateActionMapping `json:"action_mappings,omitempty"`
	TeachableWaypoints []string              `json:"teachable_waypoints,omitempty"`
}

type StateDefinitionUpdateRequest struct {
	Name               *string                 `json:"name,omitempty"`
	Description        *string                 `json:"description,omitempty"`
	States             *[]string               `json:"states,omitempty"`
	DefaultState       *string                 `json:"default_state,omitempty"`
	ActionMappings     *[]db.StateActionMapping `json:"action_mappings,omitempty"`
	TeachableWaypoints *[]string               `json:"teachable_waypoints,omitempty"`
}

func (s *Server) ListStateDefinitions(w http.ResponseWriter, r *http.Request) {
	defs, err := s.repo.GetStateDefinitions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]StateDefinitionResponse, len(defs))
	for i := range defs {
		responses[i] = stateDefinitionToResponse(&defs[i])
	}

	writeJSON(w, http.StatusOK, responses)
}

func (s *Server) GetStateDefinition(w http.ResponseWriter, r *http.Request) {
	defID := chi.URLParam(r, "stateDefID")
	def, err := s.repo.GetStateDefinition(defID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if def == nil {
		writeError(w, http.StatusNotFound, "State definition not found")
		return
	}

	writeJSON(w, http.StatusOK, stateDefinitionToResponse(def))
}

func (s *Server) CreateStateDefinition(w http.ResponseWriter, r *http.Request) {
	var req StateDefinitionCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	if len(req.States) == 0 {
		writeError(w, http.StatusBadRequest, "states must not be empty")
		return
	}
	if req.DefaultState == "" {
		req.DefaultState = req.States[0]
	}
	if !stringInSlice(req.DefaultState, req.States) {
		writeError(w, http.StatusBadRequest, "default_state must be in states")
		return
	}

	existing, _ := s.repo.GetStateDefinition(req.ID)
	if existing != nil {
		writeError(w, http.StatusConflict, "State definition already exists")
		return
	}

	def := &db.StateDefinition{
		ID:                req.ID,
		Name:              req.Name,
		States:            req.States,
		DefaultState:      req.DefaultState,
		ActionMappings:    req.ActionMappings,
		TeachableWaypoints: req.TeachableWaypoints,
		Version:           1,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	if req.Description != "" {
		def.Description = sql.NullString{String: req.Description, Valid: true}
	}

	if err := s.repo.CreateStateDefinition(def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, stateDefinitionToResponse(def))
}

func (s *Server) UpdateStateDefinition(w http.ResponseWriter, r *http.Request) {
	defID := chi.URLParam(r, "stateDefID")
	def, err := s.repo.GetStateDefinition(defID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if def == nil {
		writeError(w, http.StatusNotFound, "State definition not found")
		return
	}

	var req StateDefinitionUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != nil {
		def.Name = *req.Name
	}
	if req.Description != nil {
		if *req.Description == "" {
			def.Description = sql.NullString{}
		} else {
			def.Description = sql.NullString{String: *req.Description, Valid: true}
		}
	}
	if req.States != nil {
		def.States = *req.States
	}
	if req.DefaultState != nil {
		def.DefaultState = *req.DefaultState
	}
	if req.ActionMappings != nil {
		def.ActionMappings = *req.ActionMappings
	}
	if req.TeachableWaypoints != nil {
		def.TeachableWaypoints = *req.TeachableWaypoints
	}

	if def.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(def.States) == 0 {
		writeError(w, http.StatusBadRequest, "states must not be empty")
		return
	}
	if def.DefaultState == "" {
		writeError(w, http.StatusBadRequest, "default_state is required")
		return
	}
	if !stringInSlice(def.DefaultState, def.States) {
		writeError(w, http.StatusBadRequest, "default_state must be in states")
		return
	}

	def.Version += 1
	def.UpdatedAt = time.Now()

	if err := s.repo.UpdateStateDefinition(def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stateDefinitionToResponse(def))
}

func (s *Server) DeleteStateDefinition(w http.ResponseWriter, r *http.Request) {
	defID := chi.URLParam(r, "stateDefID")
	def, err := s.repo.GetStateDefinition(defID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if def == nil {
		writeError(w, http.StatusNotFound, "State definition not found")
		return
	}

	if err := s.repo.DeleteStateDefinition(defID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "State definition deleted"})
}

func (s *Server) DeployStateDefinition(w http.ResponseWriter, r *http.Request) {
	defID := chi.URLParam(r, "stateDefID")
	def, err := s.repo.GetStateDefinition(defID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if def == nil {
		writeError(w, http.StatusNotFound, "State definition not found")
		return
	}

	var robotIDs []string
	if err := json.NewDecoder(r.Body).Decode(&robotIDs); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if s.quicHandler == nil {
		writeError(w, http.StatusServiceUnavailable, "QUIC handler not available")
		return
	}

	if len(robotIDs) == 0 {
		robots, err := s.repo.GetAllRobots()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, robot := range robots {
			robotIDs = append(robotIDs, robot.ID)
		}
	}
	if len(robotIDs) == 0 {
		writeError(w, http.StatusBadRequest, "robot_ids must not be empty")
		return
	}

	states := def.States
	if states == nil {
		states = []string{}
	}
	actionMappings := def.ActionMappings
	if actionMappings == nil {
		actionMappings = []db.StateActionMapping{}
	}
	teachableWaypoints := def.TeachableWaypoints
	if teachableWaypoints == nil {
		teachableWaypoints = []string{}
	}

	payload := struct {
		ID                 string                 `json:"id"`
		Name               string                 `json:"name"`
		Description        string                 `json:"description,omitempty"`
		States             []string               `json:"states"`
		DefaultState       string                 `json:"default_state"`
		ActionMappings     []db.StateActionMapping `json:"action_mappings,omitempty"`
		TeachableWaypoints []string               `json:"teachable_waypoints,omitempty"`
		Version            int                    `json:"version"`
	}{
		ID:                 def.ID,
		Name:               def.Name,
		States:             states,
		DefaultState:       def.DefaultState,
		ActionMappings:     actionMappings,
		TeachableWaypoints: teachableWaypoints,
		Version:            def.Version,
	}
	if def.Description.Valid {
		payload.Description = def.Description.String
	}

	stateDefJSON, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to encode state definition")
		return
	}

	type deployResult struct {
		RobotID string `json:"robot_id"`
		AgentID string `json:"agent_id,omitempty"`
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}

	results := make([]deployResult, 0, len(robotIDs))
	for _, robotID := range robotIDs {
		result := deployResult{RobotID: robotID}

		robot, err := s.repo.GetRobot(robotID)
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		if robot == nil {
			result.Error = "robot not found"
			results = append(results, result)
			continue
		}
		if !robot.AgentID.Valid || robot.AgentID.String == "" {
			result.Error = "robot not assigned to an agent"
			results = append(results, result)
			continue
		}

		agentID := robot.AgentID.String
		result.AgentID = agentID
		resp, err := s.quicHandler.SendConfigUpdate(r.Context(), agentID, robotID, def.ID, int32(def.Version), stateDefJSON)
		if err != nil {
			result.Error = err.Error()
			if resp != nil && resp.Error != "" {
				result.Error = resp.Error
			}
			results = append(results, result)
			continue
		}

		result.Success = resp.Success
		if !resp.Success && resp.Error != "" {
			result.Error = resp.Error
		}
		results = append(results, result)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"state_definition_id": def.ID,
		"version":             def.Version,
		"results":             results,
	})
}

func stateDefinitionToResponse(def *db.StateDefinition) StateDefinitionResponse {
	states := def.States
	if states == nil {
		states = []string{}
	}
	actionMappings := def.ActionMappings
	if actionMappings == nil {
		actionMappings = []db.StateActionMapping{}
	}
	teachableWaypoints := def.TeachableWaypoints
	if teachableWaypoints == nil {
		teachableWaypoints = []string{}
	}
	response := StateDefinitionResponse{
		ID:                def.ID,
		Name:              def.Name,
		States:            states,
		DefaultState:      def.DefaultState,
		ActionMappings:    actionMappings,
		TeachableWaypoints: teachableWaypoints,
		Version:           def.Version,
		CreatedAt:         def.CreatedAt,
		UpdatedAt:         def.UpdatedAt,
	}
	if def.Description.Valid {
		response.Description = def.Description.String
	}
	return response
}

func stringInSlice(target string, list []string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

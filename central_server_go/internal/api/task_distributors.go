package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"central_server_go/internal/db"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ============================================================
// Task Distributor Request/Response Models
// ============================================================

type TaskDistributorCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type TaskDistributorUpdateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type TaskDistributorResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type TaskDistributorFullResponse struct {
	TaskDistributorResponse
	States    []TaskDistributorStateResponse    `json:"states"`
	Resources []TaskDistributorResourceResponse `json:"resources"`
}

type TaskDistributorStateCreateRequest struct {
	Name         string `json:"name"`
	Type         string `json:"type,omitempty"`
	InitialValue string `json:"initial_value,omitempty"`
	Description  string `json:"description,omitempty"`
}

type TaskDistributorStateUpdateRequest struct {
	Name         string `json:"name"`
	Type         string `json:"type,omitempty"`
	InitialValue string `json:"initial_value,omitempty"`
	Description  string `json:"description,omitempty"`
}

type TaskDistributorStateResponse struct {
	ID                string `json:"id"`
	TaskDistributorID string `json:"task_distributor_id"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	InitialValue      string `json:"initial_value,omitempty"`
	Description       string `json:"description,omitempty"`
}

type TaskDistributorResourceCreateRequest struct {
	Name             string `json:"name"`
	Kind             string `json:"kind,omitempty"`
	ParentResourceID string `json:"parent_resource_id,omitempty"`
	Description      string `json:"description,omitempty"`
}

type TaskDistributorResourceUpdateRequest struct {
	Name             string `json:"name"`
	Kind             string `json:"kind,omitempty"`
	ParentResourceID string `json:"parent_resource_id,omitempty"`
	Description      string `json:"description,omitempty"`
}

type TaskDistributorResourceResponse struct {
	ID                string `json:"id"`
	TaskDistributorID string `json:"task_distributor_id"`
	Name              string `json:"name"`
	Kind              string `json:"kind"`
	ParentResourceID  string `json:"parent_resource_id,omitempty"`
	Description       string `json:"description,omitempty"`
}

// ============================================================
// Task Distributor Handlers
// ============================================================

func (s *Server) ListTaskDistributors(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.ListTaskDistributors()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list task distributors: %v", err))
		return
	}
	responses := make([]TaskDistributorResponse, 0, len(items))
	for _, td := range items {
		responses = append(responses, toTaskDistributorResponse(&td))
	}
	writeJSON(w, http.StatusOK, responses)
}

func (s *Server) CreateTaskDistributor(w http.ResponseWriter, r *http.Request) {
	var req TaskDistributorCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	now := time.Now().UTC()
	td := &db.TaskDistributor{
		ID:          uuid.New().String()[:8],
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.CreateTaskDistributor(td); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create task distributor: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, toTaskDistributorResponse(td))
}

func (s *Server) GetTaskDistributor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "distributorID")
	td, err := s.repo.GetTaskDistributor(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get task distributor: %v", err))
		return
	}
	if td == nil {
		writeError(w, http.StatusNotFound, "task distributor not found")
		return
	}
	writeJSON(w, http.StatusOK, toTaskDistributorResponse(td))
}

func (s *Server) GetTaskDistributorFull(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "distributorID")
	td, states, resources, err := s.repo.GetTaskDistributorFull(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get task distributor: %v", err))
		return
	}
	if td == nil {
		writeError(w, http.StatusNotFound, "task distributor not found")
		return
	}

	stateResponses := make([]TaskDistributorStateResponse, 0, len(states))
	for _, s := range states {
		stateResponses = append(stateResponses, toTaskDistributorStateResponse(&s))
	}
	resourceResponses := make([]TaskDistributorResourceResponse, 0, len(resources))
	for _, r := range resources {
		resourceResponses = append(resourceResponses, toTaskDistributorResourceResponse(&r))
	}

	writeJSON(w, http.StatusOK, TaskDistributorFullResponse{
		TaskDistributorResponse: toTaskDistributorResponse(td),
		States:                  stateResponses,
		Resources:               resourceResponses,
	})
}

func (s *Server) UpdateTaskDistributor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "distributorID")
	var req TaskDistributorUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.repo.UpdateTaskDistributor(id, req.Name, req.Description); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update task distributor: %v", err))
		return
	}
	td, _ := s.repo.GetTaskDistributor(id)
	if td == nil {
		writeError(w, http.StatusNotFound, "task distributor not found")
		return
	}
	writeJSON(w, http.StatusOK, toTaskDistributorResponse(td))
}

func (s *Server) DeleteTaskDistributor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "distributorID")
	if err := s.repo.DeleteTaskDistributor(id); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete task distributor: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ============================================================
// Task Distributor State Handlers
// ============================================================

func (s *Server) ListTaskDistributorStates(w http.ResponseWriter, r *http.Request) {
	tdID := chi.URLParam(r, "distributorID")
	states, err := s.repo.ListTaskDistributorStates(tdID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list states: %v", err))
		return
	}
	responses := make([]TaskDistributorStateResponse, 0, len(states))
	for _, st := range states {
		responses = append(responses, toTaskDistributorStateResponse(&st))
	}
	writeJSON(w, http.StatusOK, responses)
}

func (s *Server) CreateTaskDistributorState(w http.ResponseWriter, r *http.Request) {
	tdID := chi.URLParam(r, "distributorID")
	var req TaskDistributorStateCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	typ := req.Type
	if typ == "" {
		typ = "string"
	}

	st := &db.TaskDistributorState{
		ID:                uuid.New().String()[:8],
		TaskDistributorID: tdID,
		Name:              req.Name,
		Type:              typ,
		InitialValue:      req.InitialValue,
		Description:       req.Description,
	}
	if err := s.repo.CreateTaskDistributorState(st); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create state: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, toTaskDistributorStateResponse(st))
}

func (s *Server) UpdateTaskDistributorState(w http.ResponseWriter, r *http.Request) {
	stateID := chi.URLParam(r, "stateID")
	var req TaskDistributorStateUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	typ := req.Type
	if typ == "" {
		typ = "string"
	}
	if err := s.repo.UpdateTaskDistributorState(stateID, req.Name, typ, req.InitialValue, req.Description); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update state: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}

func (s *Server) DeleteTaskDistributorState(w http.ResponseWriter, r *http.Request) {
	stateID := chi.URLParam(r, "stateID")
	if err := s.repo.DeleteTaskDistributorState(stateID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete state: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ============================================================
// Task Distributor Resource Handlers
// ============================================================

func (s *Server) ListTaskDistributorResources(w http.ResponseWriter, r *http.Request) {
	tdID := chi.URLParam(r, "distributorID")
	resources, err := s.repo.ListTaskDistributorResources(tdID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list resources: %v", err))
		return
	}
	responses := make([]TaskDistributorResourceResponse, 0, len(resources))
	for _, res := range resources {
		responses = append(responses, toTaskDistributorResourceResponse(&res))
	}
	writeJSON(w, http.StatusOK, responses)
}

func (s *Server) CreateTaskDistributorResource(w http.ResponseWriter, r *http.Request) {
	tdID := chi.URLParam(r, "distributorID")
	var req TaskDistributorResourceCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	res := &db.TaskDistributorResource{
		ID:                uuid.New().String()[:8],
		TaskDistributorID: tdID,
		Name:              req.Name,
		Kind:              req.Kind,
		ParentResourceID:  req.ParentResourceID,
		Description:       req.Description,
	}
	if err := s.repo.CreateTaskDistributorResource(res); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create resource: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, toTaskDistributorResourceResponse(res))
}

func (s *Server) UpdateTaskDistributorResource(w http.ResponseWriter, r *http.Request) {
	resourceID := chi.URLParam(r, "resourceID")
	var req TaskDistributorResourceUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.repo.UpdateTaskDistributorResource(resourceID, req.Name, req.Kind, req.ParentResourceID, req.Description); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update resource: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}

func (s *Server) DeleteTaskDistributorResource(w http.ResponseWriter, r *http.Request) {
	resourceID := chi.URLParam(r, "resourceID")
	if err := s.repo.DeleteTaskDistributorResource(resourceID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete resource: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ============================================================
// Converters
// ============================================================

func toTaskDistributorResponse(td *db.TaskDistributor) TaskDistributorResponse {
	return TaskDistributorResponse{
		ID:          td.ID,
		Name:        td.Name,
		Description: td.Description,
		CreatedAt:   td.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   td.UpdatedAt.Format(time.RFC3339),
	}
}

func toTaskDistributorStateResponse(s *db.TaskDistributorState) TaskDistributorStateResponse {
	return TaskDistributorStateResponse{
		ID:                s.ID,
		TaskDistributorID: s.TaskDistributorID,
		Name:              s.Name,
		Type:              s.Type,
		InitialValue:      s.InitialValue,
		Description:       s.Description,
	}
}

func toTaskDistributorResourceResponse(r *db.TaskDistributorResource) TaskDistributorResourceResponse {
	return TaskDistributorResourceResponse{
		ID:                r.ID,
		TaskDistributorID: r.TaskDistributorID,
		Name:              r.Name,
		Kind:              r.Kind,
		ParentResourceID:  r.ParentResourceID,
		Description:       r.Description,
	}
}

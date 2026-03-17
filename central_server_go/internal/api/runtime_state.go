package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type RuntimeStateUpsertRequest struct {
	Source string            `json:"source"`
	Values map[string]string `json:"values"`
	TTLSec float64           `json:"ttl_sec,omitempty"`
}

type RuntimeStateUpsertResponse struct {
	TaskDistributorID string            `json:"task_distributor_id"`
	Source            string            `json:"source"`
	Values            map[string]string `json:"values"`
	TTLSec            float64           `json:"ttl_sec,omitempty"`
	AppliedSessions   int               `json:"applied_sessions"`
}

// UpsertTaskDistributorRuntimeState applies runtime state values to all active
// realtime PDDL sessions of the selected task distributor.
// POST /api/task-distributors/{distributorID}/runtime-state
func (s *Server) UpsertTaskDistributorRuntimeState(w http.ResponseWriter, r *http.Request) {
	distributorID := strings.TrimSpace(chiURLParam(r, "distributorID"))
	if distributorID == "" {
		writeError(w, http.StatusBadRequest, "distributorID is required")
		return
	}

	var req RuntimeStateUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = "runtime-api"
	}
	if len(req.Values) == 0 {
		writeError(w, http.StatusBadRequest, "values is required")
		return
	}

	applied := s.realtimePddl.UpsertRuntimeStateByDistributor(
		distributorID,
		req.Source,
		req.Values,
		req.TTLSec,
	)

	writeJSON(w, http.StatusOK, RuntimeStateUpsertResponse{
		TaskDistributorID: distributorID,
		Source:            req.Source,
		Values:            req.Values,
		TTLSec:            req.TTLSec,
		AppliedSessions:   applied,
	})
}

// ClearTaskDistributorRuntimeState clears runtime state overlays from active realtime sessions.
// DELETE /api/task-distributors/{distributorID}/runtime-state?source=xxx
func (s *Server) ClearTaskDistributorRuntimeState(w http.ResponseWriter, r *http.Request) {
	distributorID := strings.TrimSpace(chiURLParam(r, "distributorID"))
	if distributorID == "" {
		writeError(w, http.StatusBadRequest, "distributorID is required")
		return
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	cleared := s.realtimePddl.ClearRuntimeStateByDistributor(distributorID, source)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"task_distributor_id": distributorID,
		"source":              source,
		"cleared_sessions":    cleared,
	})
}

// ListTaskDistributorRuntimeState returns active realtime sessions and their merged live state.
// GET /api/task-distributors/{distributorID}/runtime-state
func (s *Server) ListTaskDistributorRuntimeState(w http.ResponseWriter, r *http.Request) {
	distributorID := strings.TrimSpace(chiURLParam(r, "distributorID"))
	if distributorID == "" {
		writeError(w, http.StatusBadRequest, "distributorID is required")
		return
	}

	sessions := s.realtimePddl.List()
	resp := make([]RealtimeSessionResponse, 0)
	for _, session := range sessions {
		if session == nil || session.TaskDistributorID != distributorID {
			continue
		}
		resp = append(resp, s.realtimePddl.toResponse(session))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"task_distributor_id": distributorID,
		"sessions":            resp,
	})
}

func chiURLParam(r *http.Request, key string) string {
	return strings.TrimSpace(chi.URLParam(r, key))
}

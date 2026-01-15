package api

import (
	"encoding/json"
	"net/http"
	"time"

	"central_server_go/internal/db"
)

// GetCacheStats returns cache statistics
func (s *Server) GetCacheStats(w http.ResponseWriter, r *http.Request) {
	stats := s.stateManager.GraphCache().GetStats()

	response := map[string]interface{}{
		"graph_cache": map[string]interface{}{
			"total_entries":   stats.TotalEntries,
			"template_count":  stats.TemplateCount,
			"deployed_count":  stats.DeployedCount,
			"total_hits":      stats.TotalHits,
			"total_misses":    stats.TotalMisses,
			"oldest_entry":    stats.OldestEntry,
			"newest_entry":    stats.NewestEntry,
			"templates":       s.stateManager.GraphCache().ListTemplates(),
			"deployed":        s.stateManager.GraphCache().ListDeployed(),
		},
		"timestamp": time.Now(),
	}

	// Calculate hit rate
	if stats.TotalHits+stats.TotalMisses > 0 {
		response["graph_cache"].(map[string]interface{})["hit_rate"] =
			float64(stats.TotalHits) / float64(stats.TotalHits+stats.TotalMisses) * 100
	} else {
		response["graph_cache"].(map[string]interface{})["hit_rate"] = 0.0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// EvictStaleCacheRequest represents a cache eviction request
type EvictStaleCacheRequest struct {
	MaxAgeMinutes int `json:"max_age_minutes"`
}

// EvictStaleCache removes stale entries from cache
func (s *Server) EvictStaleCache(w http.ResponseWriter, r *http.Request) {
	var req EvictStaleCacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Default to 60 minutes if not specified
		req.MaxAgeMinutes = 60
	}

	if req.MaxAgeMinutes <= 0 {
		req.MaxAgeMinutes = 60
	}

	maxAge := time.Duration(req.MaxAgeMinutes) * time.Minute
	evicted := s.stateManager.GraphCache().EvictStale(maxAge)

	response := map[string]interface{}{
		"evicted_count":   evicted,
		"max_age_minutes": req.MaxAgeMinutes,
		"timestamp":       time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetSystemStates returns all predefined system states
func (s *Server) GetSystemStates(w http.ResponseWriter, r *http.Request) {
	response := make([]GraphStateResponse, len(db.SystemStates))
	for i, state := range db.SystemStates {
		response[i] = GraphStateResponse{
			Code:         state.Code,
			Name:         state.Name,
			Type:         state.Type,
			StepID:       state.StepID,
			Phase:        state.Phase,
			Color:        state.Color,
			Description:  state.Description,
			SemanticTags: state.SemanticTags,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"system_states": response,
		"count":         len(response),
	})
}

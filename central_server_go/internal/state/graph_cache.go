package state

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"central_server_go/internal/graph"
)

// CachedGraph represents an in-memory cached action graph
type CachedGraph struct {
	Graph     *graph.CanonicalGraph
	GraphID   string
	AgentID   string // Empty for templates
	Version   int
	Checksum  string
	LoadedAt  time.Time
	LastUsedAt time.Time
	HitCount  int64
}

// GraphCacheStats provides cache statistics
type GraphCacheStats struct {
	TotalEntries     int
	TemplateCount    int
	DeployedCount    int
	TotalHits        int64
	TotalMisses      int64
	OldestEntry      time.Time
	NewestEntry      time.Time
}

// GraphCache manages in-memory cache of deployed action graphs
// This cache provides:
// - Fast lookup for task execution (avoids DB I/O)
// - Version tracking for deployment sync
// - Checksum verification for integrity
type GraphCache struct {
	mu sync.RWMutex

	// Template graphs (not deployed to any specific agent)
	// Key: graphID
	templates map[string]*CachedGraph

	// Deployed graphs per agent
	// Key: "agentID:graphID"
	deployed map[string]*CachedGraph

	// Reverse index: graphID -> list of agentIDs that have this graph deployed
	graphToAgents map[string]map[string]bool

	// Statistics
	hits   int64
	misses int64
}

// NewGraphCache creates a new graph cache
func NewGraphCache() *GraphCache {
	return &GraphCache{
		templates:     make(map[string]*CachedGraph),
		deployed:      make(map[string]*CachedGraph),
		graphToAgents: make(map[string]map[string]bool),
	}
}

// =============================================================================
// Key Generation
// =============================================================================

// deployedKey generates a cache key for deployed graphs
func deployedKey(agentID, graphID string) string {
	return agentID + ":" + graphID
}

// =============================================================================
// Cache Operations - Templates
// =============================================================================

// SetTemplate caches a template (non-deployed) graph
func (c *GraphCache) SetTemplate(graphID string, g *graph.CanonicalGraph) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.templates[graphID] = &CachedGraph{
		Graph:      g,
		GraphID:    graphID,
		AgentID:    "",
		Version:    g.ActionGraph.Version,
		Checksum:   g.Checksum,
		LoadedAt:   now,
		LastUsedAt: now,
		HitCount:   0,
	}
}

// GetTemplate retrieves a template graph from cache
func (c *GraphCache) GetTemplate(graphID string) (*CachedGraph, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cached, exists := c.templates[graphID]
	if exists {
		cached.LastUsedAt = time.Now()
		cached.HitCount++
		c.hits++
		return cached, true
	}
	c.misses++
	return nil, false
}

// InvalidateTemplate removes a template from cache
func (c *GraphCache) InvalidateTemplate(graphID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.templates, graphID)
}

// =============================================================================
// Cache Operations - Deployed Graphs
// =============================================================================

// SetDeployed caches a deployed graph for a specific agent
func (c *GraphCache) SetDeployed(agentID, graphID string, g *graph.CanonicalGraph) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := deployedKey(agentID, graphID)
	now := time.Now()

	c.deployed[key] = &CachedGraph{
		Graph:      g,
		GraphID:    graphID,
		AgentID:    agentID,
		Version:    g.ActionGraph.Version,
		Checksum:   g.Checksum,
		LoadedAt:   now,
		LastUsedAt: now,
		HitCount:   0,
	}

	// Update reverse index
	if c.graphToAgents[graphID] == nil {
		c.graphToAgents[graphID] = make(map[string]bool)
	}
	c.graphToAgents[graphID][agentID] = true
}

// GetDeployed retrieves a deployed graph for a specific agent
func (c *GraphCache) GetDeployed(agentID, graphID string) (*CachedGraph, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := deployedKey(agentID, graphID)
	cached, exists := c.deployed[key]
	if exists {
		cached.LastUsedAt = time.Now()
		cached.HitCount++
		c.hits++
		return cached, true
	}
	c.misses++
	return nil, false
}

// InvalidateDeployed removes a deployed graph from cache for a specific agent
func (c *GraphCache) InvalidateDeployed(agentID, graphID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := deployedKey(agentID, graphID)
	delete(c.deployed, key)

	// Update reverse index
	if agents, exists := c.graphToAgents[graphID]; exists {
		delete(agents, agentID)
		if len(agents) == 0 {
			delete(c.graphToAgents, graphID)
		}
	}
}

// InvalidateAllDeployments removes a graph from all agent caches
// Called when graph is updated or deleted
func (c *GraphCache) InvalidateAllDeployments(graphID string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	affectedAgents := make([]string, 0)
	affectedSet := make(map[string]bool) // Track unique agents

	// Method 1: Use reverse index (fast path)
	if agents, exists := c.graphToAgents[graphID]; exists {
		for agentID := range agents {
			if !affectedSet[agentID] {
				affectedAgents = append(affectedAgents, agentID)
				affectedSet[agentID] = true
			}
			key := deployedKey(agentID, graphID)
			delete(c.deployed, key)
		}
		delete(c.graphToAgents, graphID)
	}

	// Method 2: Direct scan of deployed map (fallback for any missed entries)
	// This catches entries where graphToAgents index might be out of sync
	suffix := ":" + graphID
	keysToDelete := make([]string, 0)
	for key := range c.deployed {
		if strings.HasSuffix(key, suffix) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		// Extract agentID from key (format: "agentID:graphID")
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 {
			agentID := parts[0]
			if !affectedSet[agentID] {
				affectedAgents = append(affectedAgents, agentID)
				affectedSet[agentID] = true
				log.Printf("[GraphCache] WARNING: Found orphaned cache entry %s (not in graphToAgents index)", key)
			}
		}
		delete(c.deployed, key)
	}

	// Also remove from templates
	delete(c.templates, graphID)

	if len(affectedAgents) > 0 {
		log.Printf("[GraphCache] Invalidated graph %s for agents: %v", graphID, affectedAgents)
	}

	return affectedAgents
}

// =============================================================================
// Unified Lookup
// =============================================================================

// Get retrieves a graph from cache with priority:
// 1. If agentID provided, check deployed cache first
// 2. Fall back to template cache
func (c *GraphCache) Get(agentID, graphID string) (*CachedGraph, bool) {
	// Try deployed cache first if agent specified
	if agentID != "" {
		if cached, exists := c.GetDeployed(agentID, graphID); exists {
			return cached, true
		}
	}

	// Fall back to template
	return c.GetTemplate(graphID)
}

// Set caches a graph appropriately based on whether it's deployed
func (c *GraphCache) Set(agentID, graphID string, g *graph.CanonicalGraph) {
	if agentID != "" {
		c.SetDeployed(agentID, graphID, g)
	} else {
		c.SetTemplate(graphID, g)
	}
}

// =============================================================================
// Version Management
// =============================================================================

// GetVersion returns the cached version for a graph
// Returns (version, exists)
func (c *GraphCache) GetVersion(agentID, graphID string) (int, bool) {
	cached, exists := c.Get(agentID, graphID)
	if exists {
		return cached.Version, true
	}
	return 0, false
}

// GetChecksum returns the cached checksum for a graph
func (c *GraphCache) GetChecksum(agentID, graphID string) (string, bool) {
	cached, exists := c.Get(agentID, graphID)
	if exists {
		return cached.Checksum, true
	}
	return "", false
}

// IsVersionCurrent checks if cached version matches expected
func (c *GraphCache) IsVersionCurrent(agentID, graphID string, expectedVersion int) bool {
	version, exists := c.GetVersion(agentID, graphID)
	return exists && version == expectedVersion
}

// =============================================================================
// Agent Operations
// =============================================================================

// GetDeployedGraphsForAgent returns all cached graphs for an agent
func (c *GraphCache) GetDeployedGraphsForAgent(agentID string) []*CachedGraph {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*CachedGraph
	prefix := agentID + ":"

	for key, cached := range c.deployed {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			result = append(result, cached)
		}
	}

	return result
}

// InvalidateAgentCache removes all cached graphs for an agent
// Called when agent disconnects or is removed
func (c *GraphCache) InvalidateAgentCache(agentID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	prefix := agentID + ":"

	// Find and remove all deployed graphs for this agent
	keysToDelete := make([]string, 0)
	for key := range c.deployed {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		cached := c.deployed[key]
		delete(c.deployed, key)
		count++

		// Update reverse index
		if agents, exists := c.graphToAgents[cached.GraphID]; exists {
			delete(agents, agentID)
			if len(agents) == 0 {
				delete(c.graphToAgents, cached.GraphID)
			}
		}
	}

	return count
}

// GetAgentsWithGraph returns all agents that have a graph deployed
func (c *GraphCache) GetAgentsWithGraph(graphID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	agents := make([]string, 0)
	if agentSet, exists := c.graphToAgents[graphID]; exists {
		for agentID := range agentSet {
			agents = append(agents, agentID)
		}
	}
	return agents
}

// =============================================================================
// Cache Management
// =============================================================================

// Clear removes all entries from cache
func (c *GraphCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.templates = make(map[string]*CachedGraph)
	c.deployed = make(map[string]*CachedGraph)
	c.graphToAgents = make(map[string]map[string]bool)
	c.hits = 0
	c.misses = 0
}

// GetStats returns cache statistics
func (c *GraphCache) GetStats() GraphCacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := GraphCacheStats{
		TotalEntries:  len(c.templates) + len(c.deployed),
		TemplateCount: len(c.templates),
		DeployedCount: len(c.deployed),
		TotalHits:     c.hits,
		TotalMisses:   c.misses,
	}

	// Find oldest and newest entries
	var oldest, newest time.Time
	for _, cached := range c.templates {
		if oldest.IsZero() || cached.LoadedAt.Before(oldest) {
			oldest = cached.LoadedAt
		}
		if newest.IsZero() || cached.LoadedAt.After(newest) {
			newest = cached.LoadedAt
		}
	}
	for _, cached := range c.deployed {
		if oldest.IsZero() || cached.LoadedAt.Before(oldest) {
			oldest = cached.LoadedAt
		}
		if newest.IsZero() || cached.LoadedAt.After(newest) {
			newest = cached.LoadedAt
		}
	}
	stats.OldestEntry = oldest
	stats.NewestEntry = newest

	return stats
}

// EvictStale removes entries not used within the given duration
func (c *GraphCache) EvictStale(maxAge time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	evicted := 0

	// Evict stale templates
	for id, cached := range c.templates {
		if cached.LastUsedAt.Before(cutoff) {
			delete(c.templates, id)
			evicted++
		}
	}

	// Evict stale deployed graphs
	keysToDelete := make([]string, 0)
	for key, cached := range c.deployed {
		if cached.LastUsedAt.Before(cutoff) {
			keysToDelete = append(keysToDelete, key)
		}
	}
	for _, key := range keysToDelete {
		cached := c.deployed[key]
		delete(c.deployed, key)
		evicted++

		// Update reverse index
		if agents, exists := c.graphToAgents[cached.GraphID]; exists {
			delete(agents, cached.AgentID)
			if len(agents) == 0 {
				delete(c.graphToAgents, cached.GraphID)
			}
		}
	}

	return evicted
}

// =============================================================================
// Debug/Inspection
// =============================================================================

// ListTemplates returns all template graph IDs
func (c *GraphCache) ListTemplates() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.templates))
	for id := range c.templates {
		ids = append(ids, id)
	}
	return ids
}

// ListDeployed returns all deployed graph info as "agentID:graphID"
func (c *GraphCache) ListDeployed() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.deployed))
	for key := range c.deployed {
		keys = append(keys, key)
	}
	return keys
}

// String returns a summary string for debugging
func (c *GraphCache) String() string {
	stats := c.GetStats()
	hitRate := float64(0)
	if stats.TotalHits+stats.TotalMisses > 0 {
		hitRate = float64(stats.TotalHits) / float64(stats.TotalHits+stats.TotalMisses) * 100
	}
	return fmt.Sprintf("GraphCache{templates=%d, deployed=%d, hits=%d, misses=%d, hitRate=%.1f%%}",
		stats.TemplateCount, stats.DeployedCount, stats.TotalHits, stats.TotalMisses, hitRate)
}

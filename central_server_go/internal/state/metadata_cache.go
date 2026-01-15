package state

import (
	"sync"
	"time"
)

// CachedAgent represents cached agent metadata from DB
type CachedAgent struct {
	ID        string
	Name      string
	Status    string
	IPAddress string
	LastSeen  time.Time
	LoadedAt  time.Time
}

// CachedCapability represents cached capability metadata
type CachedCapability struct {
	AgentID      string
	ActionType   string
	ActionServer string
	IsAvailable  bool
	Status       string
}

// CachedActionGraphMeta represents cached action graph metadata (lightweight)
type CachedActionGraphMeta struct {
	ID          string
	Name        string
	Description string
	Version     int
	IsTemplate  bool
	AgentID     string // Empty for templates
	UpdatedAt   time.Time
	LoadedAt    time.Time
}

// MetadataCache provides in-memory caching for DB metadata to avoid N+1 queries
type MetadataCache struct {
	mu sync.RWMutex

	// Agent metadata by ID
	agents map[string]*CachedAgent

	// Capabilities by agent ID
	capabilitiesByAgent map[string][]*CachedCapability

	// Action types by agent ID (extracted from capabilities)
	actionTypesByAgent map[string][]string

	// Action graph metadata by ID
	graphs map[string]*CachedActionGraphMeta

	// Graph IDs by agent ID (for assigned graphs)
	graphsByAgent map[string][]string

	// Cache settings
	ttl        time.Duration
	lastReload time.Time
	needsReload bool

	// Stats
	hits   int64
	misses int64
}

// NewMetadataCache creates a new metadata cache
func NewMetadataCache(ttl time.Duration) *MetadataCache {
	if ttl == 0 {
		ttl = 30 * time.Second // Default TTL
	}
	return &MetadataCache{
		agents:              make(map[string]*CachedAgent),
		capabilitiesByAgent: make(map[string][]*CachedCapability),
		actionTypesByAgent:  make(map[string][]string),
		graphs:              make(map[string]*CachedActionGraphMeta),
		graphsByAgent:       make(map[string][]string),
		ttl:                 ttl,
		needsReload:         true,
	}
}

// NeedsReload returns true if cache needs to be reloaded
func (c *MetadataCache) NeedsReload() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.needsReload {
		return true
	}
	return time.Since(c.lastReload) > c.ttl
}

// MarkNeedsReload marks the cache as needing reload (call on DB writes)
func (c *MetadataCache) MarkNeedsReload() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.needsReload = true
}

// SetAgents bulk loads agents into cache
func (c *MetadataCache) SetAgents(agents []*CachedAgent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.agents = make(map[string]*CachedAgent, len(agents))
	for _, a := range agents {
		a.LoadedAt = now
		c.agents[a.ID] = a
	}
}

// SetCapabilities bulk loads capabilities into cache
func (c *MetadataCache) SetCapabilities(capabilities []*CachedCapability) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.capabilitiesByAgent = make(map[string][]*CachedCapability)
	c.actionTypesByAgent = make(map[string][]string)

	for _, cap := range capabilities {
		c.capabilitiesByAgent[cap.AgentID] = append(c.capabilitiesByAgent[cap.AgentID], cap)
	}

	// Extract action types per agent
	for agentID, caps := range c.capabilitiesByAgent {
		typeSet := make(map[string]bool)
		for _, cap := range caps {
			typeSet[cap.ActionType] = true
		}
		types := make([]string, 0, len(typeSet))
		for t := range typeSet {
			types = append(types, t)
		}
		c.actionTypesByAgent[agentID] = types
	}
}

// SetGraphs bulk loads action graph metadata into cache
func (c *MetadataCache) SetGraphs(graphs []*CachedActionGraphMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.graphs = make(map[string]*CachedActionGraphMeta, len(graphs))
	c.graphsByAgent = make(map[string][]string)

	for _, g := range graphs {
		g.LoadedAt = now
		c.graphs[g.ID] = g
		if g.AgentID != "" {
			c.graphsByAgent[g.AgentID] = append(c.graphsByAgent[g.AgentID], g.ID)
		}
	}
}

// MarkReloaded marks the cache as freshly reloaded
func (c *MetadataCache) MarkReloaded() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastReload = time.Now()
	c.needsReload = false
}

// GetAgent returns cached agent or nil if not found
func (c *MetadataCache) GetAgent(id string) *CachedAgent {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if agent, ok := c.agents[id]; ok {
		c.hits++
		return agent
	}
	c.misses++
	return nil
}

// GetAllAgents returns all cached agents
func (c *MetadataCache) GetAllAgents() []*CachedAgent {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*CachedAgent, 0, len(c.agents))
	for _, a := range c.agents {
		result = append(result, a)
	}
	return result
}

// GetAgentCapabilities returns cached capabilities for an agent
func (c *MetadataCache) GetAgentCapabilities(agentID string) []*CachedCapability {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if caps, ok := c.capabilitiesByAgent[agentID]; ok {
		c.hits++
		return caps
	}
	c.misses++
	return nil
}

// GetAgentActionTypes returns cached action types for an agent
func (c *MetadataCache) GetAgentActionTypes(agentID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if types, ok := c.actionTypesByAgent[agentID]; ok {
		return types
	}
	return nil
}

// GetGraph returns cached graph metadata or nil
func (c *MetadataCache) GetGraph(id string) *CachedActionGraphMeta {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if g, ok := c.graphs[id]; ok {
		c.hits++
		return g
	}
	c.misses++
	return nil
}

// GetAgentGraphIDs returns graph IDs assigned to an agent
func (c *MetadataCache) GetAgentGraphIDs(agentID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.graphsByAgent[agentID]
}

// GetAllGraphs returns all cached graph metadata
func (c *MetadataCache) GetAllGraphs() []*CachedActionGraphMeta {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*CachedActionGraphMeta, 0, len(c.graphs))
	for _, g := range c.graphs {
		result = append(result, g)
	}
	return result
}

// InvalidateAgent removes an agent from cache
func (c *MetadataCache) InvalidateAgent(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.agents, id)
	delete(c.capabilitiesByAgent, id)
	delete(c.actionTypesByAgent, id)
}

// InvalidateGraph removes a graph from cache
func (c *MetadataCache) InvalidateGraph(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if g, ok := c.graphs[id]; ok {
		// Remove from agent's graph list
		if g.AgentID != "" {
			newList := make([]string, 0)
			for _, gid := range c.graphsByAgent[g.AgentID] {
				if gid != id {
					newList = append(newList, gid)
				}
			}
			c.graphsByAgent[g.AgentID] = newList
		}
		delete(c.graphs, id)
	}
}

// Stats returns cache statistics
type MetadataCacheStats struct {
	AgentCount      int           `json:"agent_count"`
	CapabilityCount int           `json:"capability_count"`
	GraphCount      int           `json:"graph_count"`
	Hits            int64         `json:"hits"`
	Misses          int64         `json:"misses"`
	HitRate         float64       `json:"hit_rate"`
	LastReload      time.Time     `json:"last_reload"`
	TTL             time.Duration `json:"ttl"`
	NeedsReload     bool          `json:"needs_reload"`
}

func (c *MetadataCache) Stats() MetadataCacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	capCount := 0
	for _, caps := range c.capabilitiesByAgent {
		capCount += len(caps)
	}

	total := c.hits + c.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return MetadataCacheStats{
		AgentCount:      len(c.agents),
		CapabilityCount: capCount,
		GraphCount:      len(c.graphs),
		Hits:            c.hits,
		Misses:          c.misses,
		HitRate:         hitRate,
		LastReload:      c.lastReload,
		TTL:             c.ttl,
		NeedsReload:     c.needsReload || time.Since(c.lastReload) > c.ttl,
	}
}

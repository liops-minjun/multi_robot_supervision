// Package graph defines the canonical Action Graph schema
// shared between Central Server and Robot Agents.
//
// This schema is the single source of truth for Action Graph structure.
// Both the server's Neo4j storage and the agent's NetworkX graph
// use this canonical format for serialization/deserialization.
package graph

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// SchemaVersion is the current canonical schema version
const SchemaVersion = "1.0.0"

// =============================================================================
// Vertex Types
// =============================================================================

// VertexType defines the type of vertex in the action graph
type VertexType string

const (
	VertexTypeStep     VertexType = "step"
	VertexTypeTerminal VertexType = "terminal"
)

// StepType defines the sub-type of a step vertex
type StepType string

const (
	StepTypeAction    StepType = "action"
	StepTypeWait      StepType = "wait"
	StepTypeCondition StepType = "condition"
)

// TerminalType defines the outcome of a terminal vertex
type TerminalType string

const (
	TerminalTypeSuccess TerminalType = "success"
	TerminalTypeFailure TerminalType = "failure"
)

// WaitType defines the type of wait step
type WaitType string

const (
	WaitTypeManualConfirm WaitType = "manual_confirm"
	WaitTypeTimer         WaitType = "timer"
	WaitTypeCondition     WaitType = "condition"
)

// =============================================================================
// Edge Types
// =============================================================================

// EdgeType defines the type of edge (transition) in the action graph
type EdgeType string

const (
	EdgeTypeOnSuccess EdgeType = "on_success"
	EdgeTypeOnFailure EdgeType = "on_failure"
	EdgeTypeOnTimeout EdgeType = "on_timeout"
	EdgeTypeOnConfirm EdgeType = "on_confirm"
	EdgeTypeOnCancel  EdgeType = "on_cancel"
	EdgeTypeConditional EdgeType = "conditional"
)

// =============================================================================
// Canonical Graph Structure
// =============================================================================

// CanonicalGraph is the complete action graph in canonical format
// This is the format used for QUIC transport and storage serialization
type CanonicalGraph struct {
	SchemaVersion string `json:"schema_version"`

	// Graph metadata
	ActionGraph ActionGraphMeta `json:"action_graph"`

	// Graph structure
	Vertices   []Vertex `json:"vertices"`
	Edges      []Edge   `json:"edges"`
	EntryPoint string   `json:"entry_point"`

	// Integrity
	Checksum string `json:"checksum,omitempty"`
}

// ActionGraphMeta contains action graph metadata
type ActionGraphMeta struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Version     int                  `json:"version"`
	Description string               `json:"description,omitempty"`
	AgentID     string               `json:"agent_id,omitempty"` // Target agent (empty = template)
	Requirements *RobotRequirements  `json:"robot_requirements,omitempty"`
	CreatedAt   time.Time            `json:"created_at,omitempty"`
	UpdatedAt   time.Time            `json:"updated_at,omitempty"`
}

// RobotRequirements specifies robot capability requirements
type RobotRequirements struct {
	Capabilities []string `json:"capabilities,omitempty"` // Required action types
	Tags         []string `json:"tags,omitempty"`         // Required robot tags
}

// =============================================================================
// Vertex Definitions
// =============================================================================

// Vertex represents a node in the action graph
type Vertex struct {
	ID   string     `json:"id"`
	Type VertexType `json:"type"`
	Name string     `json:"name,omitempty"`

	// Step-specific fields (when Type == "step")
	Step *StepData `json:"step,omitempty"`

	// Terminal-specific fields (when Type == "terminal")
	Terminal *TerminalData `json:"terminal,omitempty"`

	// UI positioning
	UI *UIPosition `json:"ui,omitempty"`
}

// StepData contains step-specific data
type StepData struct {
	StepType StepType `json:"step_type"` // action, wait, condition

	// Action configuration (when StepType == "action")
	Action *ActionConfig `json:"action,omitempty"`

	// Wait configuration (when StepType == "wait")
	Wait *WaitConfig `json:"wait,omitempty"`

	// Condition configuration (when StepType == "condition")
	Condition *ConditionConfig `json:"condition,omitempty"`

	// Start conditions (AND/OR list)
	StartConditions []StartCondition `json:"start_conditions,omitempty"`

	// State management
	States *StateConfig `json:"states,omitempty"`

	// End states (action outcomes)
	EndStates []EndState `json:"end_states,omitempty"`
}

// ActionConfig defines an action to execute
type ActionConfig struct {
	Type       string        `json:"type"`              // ROS2 action type (e.g., "nav2_msgs/action/NavigateToPose")
	Server     string        `json:"server"`            // Action server name
	Params     *ActionParams `json:"params,omitempty"`  // Action parameters
	TimeoutSec float64       `json:"timeout_sec,omitempty"`
}

// ActionParams defines how to resolve action parameters
type ActionParams struct {
	Source     string                 `json:"source"` // "waypoint", "inline", "dynamic"
	WaypointID string                 `json:"waypoint_id,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Fields     []string               `json:"fields,omitempty"` // For dynamic: fields to request
}

// WaitConfig defines a wait step
type WaitConfig struct {
	Type       WaitType `json:"type"` // manual_confirm, timer, condition
	Message    string   `json:"message,omitempty"`
	TimeoutSec float64  `json:"timeout_sec,omitempty"`
	Condition  string   `json:"condition,omitempty"` // Expression for condition type
}

// ConditionConfig defines a conditional branch
type ConditionConfig struct {
	Expression string            `json:"expression"` // Condition expression
	Branches   map[string]string `json:"branches"`   // result -> next_step_id
}

// StateConfig defines state transitions for a step
type StateConfig struct {
	Pre     []string `json:"pre,omitempty"`     // Required states before execution
	During  []string `json:"during,omitempty"`  // States during execution
	Success []string `json:"success,omitempty"` // States on success
	Failure []string `json:"failure,omitempty"` // States on failure
}

// StartCondition defines a structured start condition.
type StartCondition struct {
	ID string `json:"id"`

	Operator   string `json:"operator,omitempty"`    // and, or
	Quantifier string `json:"quantifier,omitempty"`  // self, all, any, none, specific
	TargetType string `json:"target_type,omitempty"` // self, agent, all
	AgentID    string `json:"agent_id,omitempty"`

	State         string   `json:"state,omitempty"`
	StateOperator string   `json:"state_operator,omitempty"` // ==, !=, in, not_in
	AllowedStates []string `json:"allowed_states,omitempty"`

	MaxStalenessSec float64 `json:"max_staleness_sec,omitempty"`
	RequireOnline   bool    `json:"require_online,omitempty"`

	Message string `json:"message,omitempty"`
}

// EndState defines an outcome-specific state for a step.
type EndState struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Label     string `json:"label,omitempty"`
	Color     string `json:"color,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Condition string `json:"condition,omitempty"`
}

// TerminalData contains terminal-specific data
type TerminalData struct {
	TerminalType TerminalType `json:"terminal_type"` // success, failure
	Alert        bool         `json:"alert,omitempty"`
	Message      string       `json:"message,omitempty"`
}

// UIPosition contains UI rendering information
type UIPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// =============================================================================
// Edge Definitions
// =============================================================================

// Edge represents a transition between vertices
type Edge struct {
	From   string     `json:"from"`
	To     string     `json:"to"`
	Type   EdgeType   `json:"type"`
	Config *EdgeConfig `json:"config,omitempty"`
}

// EdgeConfig contains edge-specific configuration
type EdgeConfig struct {
	// For on_failure edges
	Retry    int    `json:"retry,omitempty"`    // Number of retries before following edge
	Fallback string `json:"fallback,omitempty"` // Final fallback if retries exhausted

	// For conditional edges
	Condition string `json:"condition,omitempty"` // Condition expression
}

// =============================================================================
// Helper Methods
// =============================================================================

// ComputeChecksum computes SHA256 checksum of the graph
func (g *CanonicalGraph) ComputeChecksum() string {
	// Create a copy without checksum for hashing
	copy := *g
	copy.Checksum = ""

	data, _ := json.Marshal(copy)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", hash)
}

// Validate performs basic validation on the graph structure
func (g *CanonicalGraph) Validate() error {
	if g.SchemaVersion == "" {
		return fmt.Errorf("schema_version is required")
	}
	if g.ActionGraph.ID == "" {
		return fmt.Errorf("action_graph.id is required")
	}
	if g.EntryPoint == "" {
		return fmt.Errorf("entry_point is required")
	}

	// Build vertex ID set
	vertexIDs := make(map[string]bool)
	for _, v := range g.Vertices {
		if v.ID == "" {
			return fmt.Errorf("vertex id is required")
		}
		if vertexIDs[v.ID] {
			return fmt.Errorf("duplicate vertex id: %s", v.ID)
		}
		vertexIDs[v.ID] = true
	}

	// Validate entry point exists
	if !vertexIDs[g.EntryPoint] {
		return fmt.Errorf("entry_point '%s' does not exist in vertices", g.EntryPoint)
	}

	// Validate edges reference existing vertices
	for _, e := range g.Edges {
		if !vertexIDs[e.From] {
			return fmt.Errorf("edge from vertex '%s' does not exist", e.From)
		}
		if !vertexIDs[e.To] {
			return fmt.Errorf("edge to vertex '%s' does not exist", e.To)
		}
	}

	return nil
}

// GetVertex returns a vertex by ID
func (g *CanonicalGraph) GetVertex(id string) *Vertex {
	for i := range g.Vertices {
		if g.Vertices[i].ID == id {
			return &g.Vertices[i]
		}
	}
	return nil
}

// GetOutgoingEdges returns all edges from a vertex
func (g *CanonicalGraph) GetOutgoingEdges(vertexID string) []Edge {
	var edges []Edge
	for _, e := range g.Edges {
		if e.From == vertexID {
			edges = append(edges, e)
		}
	}
	return edges
}

// GetEdgeByType returns the first edge of a specific type from a vertex
func (g *CanonicalGraph) GetEdgeByType(vertexID string, edgeType EdgeType) *Edge {
	for i := range g.Edges {
		if g.Edges[i].From == vertexID && g.Edges[i].Type == edgeType {
			return &g.Edges[i]
		}
	}
	return nil
}

// FindTerminals returns all terminal vertices
func (g *CanonicalGraph) FindTerminals() []Vertex {
	var terminals []Vertex
	for _, v := range g.Vertices {
		if v.Type == VertexTypeTerminal {
			terminals = append(terminals, v)
		}
	}
	return terminals
}

// HasCycle detects if the graph contains cycles using DFS
func (g *CanonicalGraph) HasCycle() bool {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(id string) bool
	dfs = func(id string) bool {
		visited[id] = true
		recStack[id] = true

		for _, edge := range g.GetOutgoingEdges(id) {
			if !visited[edge.To] {
				if dfs(edge.To) {
					return true
				}
			} else if recStack[edge.To] {
				return true
			}
		}

		recStack[id] = false
		return false
	}

	for _, v := range g.Vertices {
		if !visited[v.ID] {
			if dfs(v.ID) {
				return true
			}
		}
	}

	return false
}

// FindUnreachableVertices finds vertices not reachable from entry point
func (g *CanonicalGraph) FindUnreachableVertices() []string {
	reachable := make(map[string]bool)

	var bfs func(id string)
	bfs = func(id string) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		for _, edge := range g.GetOutgoingEdges(id) {
			bfs(edge.To)
		}
	}

	bfs(g.EntryPoint)

	var unreachable []string
	for _, v := range g.Vertices {
		if !reachable[v.ID] {
			unreachable = append(unreachable, v.ID)
		}
	}
	return unreachable
}

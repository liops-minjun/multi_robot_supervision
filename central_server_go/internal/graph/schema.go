// Package graph defines the canonical Behavior Tree schema
// shared between Central Server and Robot Agents.
//
// This schema is the single source of truth for Behavior Tree structure.
// Both the server's Neo4j storage and the agent's graph executor
// use this canonical format for serialization/deserialization.
package graph

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SchemaVersion is the current canonical schema version
const SchemaVersion = "1.0.0"

// =============================================================================
// Vertex Types
// =============================================================================

// VertexType defines the type of vertex in the behavior tree
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

// EdgeType defines the type of edge (transition) in the behavior tree
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

// CanonicalGraph is the complete behavior tree in canonical format
// This is the format used for QUIC transport and storage serialization
type CanonicalGraph struct {
	SchemaVersion string `json:"schema_version"`

	// Graph metadata
	BehaviorTree BehaviorTreeMeta `json:"behavior_tree"`

	// Graph structure
	Vertices   []Vertex `json:"vertices"`
	Edges      []Edge   `json:"edges"`
	EntryPoint string   `json:"entry_point"`

	// Integrity
	Checksum string `json:"checksum,omitempty"`
}

// BehaviorTreeMeta contains behavior tree metadata
type BehaviorTreeMeta struct {
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

// Vertex represents a node in the behavior tree
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

	// Job name (user-defined name for this step)
	JobName            string `json:"job_name,omitempty"`
	AutoGenerateStates bool   `json:"auto_generate_states,omitempty"`

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
	Type         string            `json:"type"`                    // ROS2 action type (e.g., "nav2_msgs/action/NavigateToPose")
	Server       string            `json:"server"`                  // Action server name
	Params       *ActionParams     `json:"params,omitempty"`        // Action parameters
	TimeoutSec   float64           `json:"timeout_sec,omitempty"`
	ResultSchema *StepResultSchema `json:"result_schema,omitempty"` // Expected result schema (for other steps to reference)
}

// ParameterSourceType defines how a parameter field gets its value
type ParameterSourceType string

const (
	ParameterSourceConstant   ParameterSourceType = "constant"
	ParameterSourceStepResult ParameterSourceType = "step_result"
	ParameterSourceDynamic    ParameterSourceType = "dynamic"
	ParameterSourceExpression ParameterSourceType = "expression"
)

// =============================================================================
// Canonical Data Type System
// =============================================================================

// CanonicalDataType defines the canonical data types for parameter binding
type CanonicalDataType string

const (
	// Primitive types
	DataTypeBool    CanonicalDataType = "bool"
	DataTypeInt8    CanonicalDataType = "int8"
	DataTypeInt16   CanonicalDataType = "int16"
	DataTypeInt32   CanonicalDataType = "int32"
	DataTypeInt64   CanonicalDataType = "int64"
	DataTypeUint8   CanonicalDataType = "uint8"
	DataTypeUint16  CanonicalDataType = "uint16"
	DataTypeUint32  CanonicalDataType = "uint32"
	DataTypeUint64  CanonicalDataType = "uint64"
	DataTypeFloat32 CanonicalDataType = "float32"
	DataTypeFloat64 CanonicalDataType = "float64"
	DataTypeString  CanonicalDataType = "string"
	// Complex types
	DataTypeObject CanonicalDataType = "object"
	DataTypeArray  CanonicalDataType = "array"
	// Any type (for dynamic/expression sources)
	DataTypeAny CanonicalDataType = "any"
)

// TypeCategory groups canonical types for compatibility checking
type TypeCategory string

const (
	TypeCategoryBoolean TypeCategory = "boolean"
	TypeCategoryInteger TypeCategory = "integer"
	TypeCategoryFloat   TypeCategory = "float"
	TypeCategoryString  TypeCategory = "string"
	TypeCategoryObject  TypeCategory = "object"
	TypeCategoryArray   TypeCategory = "array"
	TypeCategoryAny     TypeCategory = "any"
)

// DataTypeInfo contains full type information including array/object structure
type DataTypeInfo struct {
	Type             CanonicalDataType        `json:"type"`
	Category         TypeCategory             `json:"category"`
	IsArray          bool                     `json:"is_array"`
	ArrayElementType *DataTypeInfo            `json:"array_element_type,omitempty"`
	ObjectFields     map[string]*DataTypeInfo `json:"object_fields,omitempty"`
	ROSType          string                   `json:"ros_type,omitempty"`
}

// TypeConversionConfig configures how type conversion should be performed
type TypeConversionConfig struct {
	Enabled          bool   `json:"enabled"`
	Mode             string `json:"mode"` // implicit, explicit, custom
	CustomExpression string `json:"custom_expression,omitempty"`
}

// GetTypeCategory returns the category for a canonical data type
func GetTypeCategory(t CanonicalDataType) TypeCategory {
	switch t {
	case DataTypeBool:
		return TypeCategoryBoolean
	case DataTypeInt8, DataTypeInt16, DataTypeInt32, DataTypeInt64,
		DataTypeUint8, DataTypeUint16, DataTypeUint32, DataTypeUint64:
		return TypeCategoryInteger
	case DataTypeFloat32, DataTypeFloat64:
		return TypeCategoryFloat
	case DataTypeString:
		return TypeCategoryString
	case DataTypeObject:
		return TypeCategoryObject
	case DataTypeArray:
		return TypeCategoryArray
	default:
		return TypeCategoryAny
	}
}

// ROSTypeToCanonical converts a ROS2 type string to canonical type
func ROSTypeToCanonical(rosType string) *DataTypeInfo {
	// Handle array types
	isArray := false
	baseType := rosType
	if len(rosType) > 2 && rosType[len(rosType)-2:] == "[]" {
		isArray = true
		baseType = rosType[:len(rosType)-2]
	}

	// Primitive mappings
	var canonicalType CanonicalDataType
	switch baseType {
	case "bool", "boolean":
		canonicalType = DataTypeBool
	case "int8":
		canonicalType = DataTypeInt8
	case "int16":
		canonicalType = DataTypeInt16
	case "int32", "int":
		canonicalType = DataTypeInt32
	case "int64":
		canonicalType = DataTypeInt64
	case "uint8", "byte":
		canonicalType = DataTypeUint8
	case "uint16":
		canonicalType = DataTypeUint16
	case "uint32":
		canonicalType = DataTypeUint32
	case "uint64":
		canonicalType = DataTypeUint64
	case "float32", "float":
		canonicalType = DataTypeFloat32
	case "float64", "double":
		canonicalType = DataTypeFloat64
	case "string":
		canonicalType = DataTypeString
	default:
		canonicalType = DataTypeObject
	}

	typeInfo := &DataTypeInfo{
		Type:     canonicalType,
		Category: GetTypeCategory(canonicalType),
		IsArray:  isArray,
		ROSType:  rosType,
	}

	if isArray {
		typeInfo.ArrayElementType = &DataTypeInfo{
			Type:     canonicalType,
			Category: GetTypeCategory(canonicalType),
			IsArray:  false,
			ROSType:  baseType,
		}
	}

	return typeInfo
}

// TypeCompatibilityResult describes the result of a type compatibility check
type TypeCompatibilityResult struct {
	Compatible         bool   `json:"compatible"`
	RequiresConversion bool   `json:"requires_conversion"`
	ConversionType     string `json:"conversion_type,omitempty"` // implicit, explicit, lossy
	WarningMessage     string `json:"warning_message,omitempty"`
}

// CheckTypeCompatibility checks if source type is compatible with target type
func CheckTypeCompatibility(sourceType, targetType *DataTypeInfo) TypeCompatibilityResult {
	// Any type is always compatible
	if sourceType.Type == DataTypeAny || targetType.Type == DataTypeAny {
		return TypeCompatibilityResult{Compatible: true, RequiresConversion: false}
	}

	// Exact type match
	if sourceType.Type == targetType.Type && sourceType.IsArray == targetType.IsArray {
		return TypeCompatibilityResult{Compatible: true, RequiresConversion: false}
	}

	// Array compatibility
	if sourceType.IsArray != targetType.IsArray {
		// Array to non-array: need index access
		if sourceType.IsArray && !targetType.IsArray && sourceType.ArrayElementType != nil {
			elementCompat := CheckTypeCompatibility(sourceType.ArrayElementType, targetType)
			if elementCompat.Compatible {
				return TypeCompatibilityResult{
					Compatible:         true,
					RequiresConversion: true,
					ConversionType:     "explicit",
					WarningMessage:     "Array access required - use [index] syntax",
				}
			}
		}
		return TypeCompatibilityResult{Compatible: false, RequiresConversion: false}
	}

	sourceCategory := sourceType.Category
	targetCategory := targetType.Category

	// Boolean only with boolean
	if sourceCategory == TypeCategoryBoolean || targetCategory == TypeCategoryBoolean {
		return TypeCompatibilityResult{Compatible: sourceCategory == targetCategory, RequiresConversion: false}
	}

	// Integer to float (implicit, safe)
	if sourceCategory == TypeCategoryInteger && targetCategory == TypeCategoryFloat {
		return TypeCompatibilityResult{Compatible: true, RequiresConversion: true, ConversionType: "implicit"}
	}

	// Float to integer (lossy)
	if sourceCategory == TypeCategoryFloat && targetCategory == TypeCategoryInteger {
		return TypeCompatibilityResult{
			Compatible:         true,
			RequiresConversion: true,
			ConversionType:     "lossy",
			WarningMessage:     "Float to integer conversion may lose precision",
		}
	}

	// Integer type widening/narrowing
	if sourceCategory == TypeCategoryInteger && targetCategory == TypeCategoryInteger {
		return TypeCompatibilityResult{Compatible: true, RequiresConversion: true, ConversionType: "implicit"}
	}

	// Float type precision
	if sourceCategory == TypeCategoryFloat && targetCategory == TypeCategoryFloat {
		return TypeCompatibilityResult{Compatible: true, RequiresConversion: true, ConversionType: "implicit"}
	}

	// String only with string
	if sourceCategory == TypeCategoryString || targetCategory == TypeCategoryString {
		return TypeCompatibilityResult{Compatible: sourceCategory == targetCategory, RequiresConversion: false}
	}

	// Object compatibility
	if sourceCategory == TypeCategoryObject && targetCategory == TypeCategoryObject {
		return TypeCompatibilityResult{Compatible: true, RequiresConversion: false}
	}

	return TypeCompatibilityResult{Compatible: false, RequiresConversion: false}
}

// =============================================================================
// Parameter Field Source
// =============================================================================

// ParameterFieldSource defines how a single field gets its value
type ParameterFieldSource struct {
	Source      ParameterSourceType `json:"source"`                  // constant, step_result, dynamic, expression
	Value       interface{}         `json:"value,omitempty"`         // For constant
	StepID      string              `json:"step_id,omitempty"`       // For step_result
	ResultField string              `json:"result_field,omitempty"`  // For step_result (e.g., "pose.position.x", "poses[0].position")
	Expression  string              `json:"expression,omitempty"`    // For expression

	// Type information (for validation)
	SourceType *DataTypeInfo         `json:"source_type,omitempty"`
	TargetType *DataTypeInfo         `json:"target_type,omitempty"`
	Conversion *TypeConversionConfig `json:"conversion,omitempty"`
}

// StepResultSchema defines the expected result schema for a step
type StepResultSchema struct {
	Fields []ResultFieldDef `json:"fields,omitempty"`
}

// ResultFieldDef defines a single field in the result schema
type ResultFieldDef struct {
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Description string        `json:"description,omitempty"`
	TypeInfo    *DataTypeInfo `json:"type_info,omitempty"` // Parsed type information
}

// ActionParams defines how to resolve action parameters
type ActionParams struct {
	Source     string                 `json:"source"` // "waypoint", "inline", "dynamic", "mapped"
	WaypointID string                 `json:"waypoint_id,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Fields     []string               `json:"fields,omitempty"` // For dynamic: fields to request

	// NEW: Per-field source mapping (when Source="mapped")
	FieldSources map[string]ParameterFieldSource `json:"field_sources,omitempty"`
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
	ID      string `json:"id"`
	State   string `json:"state"`
	Label   string `json:"label,omitempty"`
	Color   string `json:"color,omitempty"`
	Outcome string `json:"outcome,omitempty"`
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
	if g.BehaviorTree.ID == "" {
		return fmt.Errorf("behavior_tree.id is required")
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

// =============================================================================
// Server Pattern Substitution
// =============================================================================

// SubstituteServerPatterns replaces pattern placeholders in action server names.
// Supported patterns:
//   - {namespace} : Replaced with the agent's ROS2 namespace
//
// Example: "{namespace}/navigate_to_pose" with namespace="/robot_001"
//
//	becomes "/robot_001/navigate_to_pose"
//
// If namespace is empty, "{namespace}" is removed and "//" is normalized to "/"
func (g *CanonicalGraph) SubstituteServerPatterns(namespace string) {
	for i := range g.Vertices {
		if g.Vertices[i].Step != nil && g.Vertices[i].Step.Action != nil {
			server := g.Vertices[i].Step.Action.Server
			if server != "" {
				// Substitute {namespace} pattern
				server = strings.ReplaceAll(server, "{namespace}", namespace)
				// Normalize double slashes (can happen when namespace is empty)
				for strings.Contains(server, "//") {
					server = strings.ReplaceAll(server, "//", "/")
				}
				g.Vertices[i].Step.Action.Server = server
			}
		}
	}
}

package db

import (
	"database/sql"
	"time"

	"gorm.io/datatypes"
)

// Agent represents a fleet agent that executes actions
type Agent struct {
	ID           string         `gorm:"primaryKey;size:50"`
	Name         string         `gorm:"size:100;not null"`
	Namespace    string         `gorm:"size:100"` // ROS2 namespace (optional)
	IPAddress    sql.NullString `gorm:"size:45"`
	Tags         datatypes.JSON `gorm:"type:jsonb"` // Grouping tags []string
	LastSeen     sql.NullTime
	CurrentState string    `gorm:"size:50;default:idle"`
	Status       string    `gorm:"size:20;default:offline"` // online, offline, warning
	CreatedAt    time.Time `gorm:"autoCreateTime"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`

	// Server-assigned ID support
	HardwareFingerprint sql.NullString `gorm:"size:64;index"` // Hardware fingerprint for ID recovery
	AssignedByServer    bool           `gorm:"default:false"` // True if ID was assigned by server

	// Enhanced state tracking
	CurrentStateCode string         `gorm:"size:100;default:idle"` // Current state code (e.g., "pick:executing")
	SemanticTags     datatypes.JSON `gorm:"type:jsonb"`            // Current semantic tags []string
	CurrentGraphID   sql.NullString `gorm:"size:50"`               // Currently executing graph ID

	// Relationships
	AgentBehaviorTrees []AgentBehaviorTree `gorm:"foreignKey:AgentID"`
	BehaviorTrees      []BehaviorTree      `gorm:"foreignKey:AgentID"`
	Capabilities       []AgentCapability   `gorm:"foreignKey:AgentID"`
	Tasks              []Task              `gorm:"foreignKey:AgentID"`
	Commands           []CommandQueue      `gorm:"foreignKey:AgentID"`
}

func (Agent) TableName() string {
	return "agents"
}

// AgentCapability represents an auto-discovered capability from ROS2 Action Server
// Capabilities are discovered per-agent from ROS2 Action Servers
type AgentCapability struct {
	ID              string `gorm:"primaryKey;size:100"` // agent_id + action_server hash
	AgentID         string `gorm:"size:50;not null;index"`
	CapabilityKind  string `gorm:"size:20;not null;default:action;index"` // action, service
	ActionType      string `gorm:"size:100;not null;index"`               // e.g., "nav2_msgs/action/NavigateToPose"
	ActionServer    string `gorm:"size:200;not null"`                     // e.g., "/navigate_to_pose"
	NodeName        string `gorm:"size:200"`                              // ROS2 node that provides this capability
	IsLifecycleNode bool   `gorm:"default:false"`                         // True if provider is lifecycle-managed

	// Auto-introspected schemas
	GoalSchema     datatypes.JSON `gorm:"type:jsonb"` // Goal message schema
	ResultSchema   datatypes.JSON `gorm:"type:jsonb"` // Result message schema
	FeedbackSchema datatypes.JSON `gorm:"type:jsonb"` // Feedback message schema

	// Inferred success criteria
	SuccessCriteria datatypes.JSON `gorm:"type:jsonb"` // Auto-inferred from result schema

	// User-editable metadata (for UI/documentation)
	Description    sql.NullString `gorm:"type:text"`     // Human-readable description
	Category       sql.NullString `gorm:"size:50;index"` // Category: navigation, manipulation, perception, etc.
	DefaultTimeout float64        `gorm:"default:30.0"`  // Default timeout in seconds
	SchemaVersion  int            `gorm:"default:1"`     // Schema version for compatibility tracking

	// Runtime status
	Status         string `gorm:"size:20;default:idle"` // idle, executing
	IsAvailable    bool   `gorm:"default:true"`
	LifecycleState string `gorm:"size:20;default:unknown"` // unknown, unconfigured, inactive, active, finalized
	LastUsedAt     sql.NullTime
	DiscoveredAt   time.Time    `gorm:"autoCreateTime"`
	UpdatedAt      time.Time    `gorm:"autoUpdateTime;index"` // Index for incremental sync
	DeletedAt      sql.NullTime `gorm:"index"`                // Soft delete for sync tracking

	// Relationships
	Agent *Agent `gorm:"foreignKey:AgentID"`
}

func (AgentCapability) TableName() string {
	return "agent_capabilities"
}

// Waypoint represents saved positions/poses
type Waypoint struct {
	ID           string         `gorm:"primaryKey;size:50"`
	Name         string         `gorm:"size:100;not null"`
	WaypointType string         `gorm:"size:50;not null"` // pose_2d, joint_state, pose_3d, gripper
	Data         datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedBy    sql.NullString `gorm:"size:20"` // "teach" or "manual"
	Description  sql.NullString `gorm:"type:text"`
	Tags         datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
}

func (Waypoint) TableName() string {
	return "waypoints"
}

// StateActionMapping maps action types to states during execution
type StateActionMapping struct {
	ActionType   string   `json:"action_type"`
	Server       string   `json:"server"`
	DuringState  string   `json:"during_state,omitempty"`
	DuringStates []string `json:"during_states,omitempty"`
}

// StateDefinition defines valid states and action mappings
type StateDefinition struct {
	ID                 string               `gorm:"primaryKey;size:50"`
	Name               string               `gorm:"size:100;not null"`
	Description        sql.NullString       `gorm:"type:text"`
	States             []string             `gorm:"-"`
	DefaultState       string               `gorm:"size:50;not null"`
	ActionMappings     []StateActionMapping `gorm:"-"`
	TeachableWaypoints []string             `gorm:"-"`
	Version            int                  `gorm:"default:1"`
	CreatedAt          time.Time            `gorm:"autoCreateTime"`
	UpdatedAt          time.Time            `gorm:"autoUpdateTime"`
}

func (StateDefinition) TableName() string {
	return "state_definitions"
}

// GraphState represents a state that can be reported during behavior tree execution
type GraphState struct {
	Code         string   `json:"code"`                    // Unique code: "pick:executing", "idle"
	Name         string   `json:"name"`                    // Display name: "Picking - Executing"
	Type         string   `json:"type"`                    // "system" | "auto" | "custom"
	StepID       string   `json:"step_id,omitempty"`       // Related step ID (for auto type)
	Phase        string   `json:"phase,omitempty"`         // "executing" | "success" | "failed"
	Color        string   `json:"color,omitempty"`         // UI color: "#3b82f6"
	Description  string   `json:"description,omitempty"`   // Optional description
	SemanticTags []string `json:"semantic_tags,omitempty"` // Cross-graph tags: ["ready_for_handoff"]
}

// SystemStates are always available for all agents
var SystemStates = []GraphState{
	{Code: "idle", Name: "Idle", Type: "system", Color: "#22c55e"},
	{Code: "executing", Name: "Executing", Type: "system", Color: "#3b82f6"},
	{Code: "error", Name: "Error", Type: "system", Color: "#ef4444"},
	{Code: "waiting_confirm", Name: "Waiting Confirmation", Type: "system", Color: "#eab308"},
	{Code: "paused", Name: "Paused", Type: "system", Color: "#6b7280"},
}

// BehaviorTree represents a workflow of steps (template or agent-specific)
type BehaviorTree struct {
	ID               string         `gorm:"primaryKey;size:50"`
	Name             string         `gorm:"size:100;not null"`
	Description      sql.NullString `gorm:"type:text"`
	AgentID          sql.NullString `gorm:"size:50;index"` // null = template
	EntryPoint       sql.NullString `gorm:"size:50"`
	Preconditions    datatypes.JSON `gorm:"type:jsonb"`
	Steps            datatypes.JSON `gorm:"type:jsonb;not null"`
	Version          int            `gorm:"default:1"`
	IsTemplate       bool           `gorm:"default:false"`
	TemplateCategory sql.NullString `gorm:"size:50"`
	CreatedAt        time.Time      `gorm:"autoCreateTime"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime"`

	// Capability-based: required action types for this graph
	RequiredActionTypes datatypes.JSON `gorm:"type:jsonb;default:'[]'"` // ["nav2_msgs/NavigateToPose", ...]

	// State management
	States             datatypes.JSON `gorm:"type:jsonb"`   // []GraphState - available states for this graph
	AutoGenerateStates bool           `gorm:"default:true"` // Auto-generate states from steps

	// Edit lock fields (for concurrent editing prevention)
	LockedBy      sql.NullString `gorm:"size:100"` // Display name of user who holds the lock
	LockedAt      sql.NullTime   // When the lock was acquired
	LockExpiresAt sql.NullTime   // When the lock expires (5 min timeout)
	LockSessionID sql.NullString `gorm:"size:100;index"` // Session ID for lock ownership verification

	// Relationships
	Agent              *Agent              `gorm:"foreignKey:AgentID"`
	Tasks              []Task              `gorm:"foreignKey:BehaviorTreeID"`
	AgentBehaviorTrees []AgentBehaviorTree `gorm:"foreignKey:BehaviorTreeID"`
}

// TableName returns "action_graphs" to keep the same DB table (avoid migration)
func (BehaviorTree) TableName() string {
	return "action_graphs"
}

// ExtractActionTypesFromSteps extracts unique action types from behavior tree steps
func ExtractActionTypesFromSteps(steps []BehaviorTreeStep) []string {
	actionTypeSet := make(map[string]bool)
	for _, step := range steps {
		if step.Action != nil && step.Action.Type != "" {
			actionTypeSet[step.Action.Type] = true
		}
	}
	actionTypes := make([]string, 0, len(actionTypeSet))
	for at := range actionTypeSet {
		actionTypes = append(actionTypes, at)
	}
	return actionTypes
}

// CompatibleAgentInfo summarizes capability matching for an agent.
type CompatibleAgentInfo struct {
	Agent               Agent
	MissingCapabilities []string
	HasAllCapabilities  bool
}

// ActionTypeWithCount summarizes how many agents support an action type.
type ActionTypeWithCount struct {
	ActionType string
	AgentCount int
}

// TemplateCompatibilityInfo summarizes template compatibility for an agent.
type TemplateCompatibilityInfo struct {
	Template            BehaviorTree
	RequiredActionTypes []string
	MissingCapabilities []string
	IsFullyCompatible   bool
	AlreadyAssigned     bool
}

// Task represents a running or completed behavior tree execution
type Task struct {
	ID               string         `gorm:"primaryKey;size:50"`
	BehaviorTreeID   sql.NullString `gorm:"size:50;column:action_graph_id"` // Keep DB column name for migration
	AgentID          sql.NullString `gorm:"size:50"`
	Status           string         `gorm:"size:20;not null;default:pending"` // pending, running, completed, failed, cancelled, paused, waiting_precondition
	CurrentStepID    sql.NullString `gorm:"size:50"`
	CurrentStepIndex int            `gorm:"default:0"`
	StepResults      datatypes.JSON `gorm:"type:jsonb"`
	RetryCount       datatypes.JSON `gorm:"type:jsonb"` // {step_id: count}
	ErrorMessage     sql.NullString `gorm:"type:text"`
	CreatedAt        time.Time      `gorm:"autoCreateTime"`
	StartedAt        sql.NullTime
	CompletedAt      sql.NullTime

	// Precondition waiting state
	WaitingForPreconditionSince sql.NullTime   `gorm:"column:waiting_for_precondition_since"`
	BlockingConditions          datatypes.JSON `gorm:"type:jsonb"`  // []BlockingConditionInfo
	PreconditionTimeoutSec      int            `gorm:"default:300"` // Default 5 minutes

	// Relationships
	BehaviorTree *BehaviorTree `gorm:"foreignKey:BehaviorTreeID"`
	Agent        *Agent        `gorm:"foreignKey:AgentID"`
}

// BlockingConditionInfo describes why a precondition is blocking
type BlockingConditionInfo struct {
	ConditionID     string `json:"condition_id"`
	Description     string `json:"description"`
	TargetAgentID   string `json:"target_agent_id,omitempty"`
	TargetAgentName string `json:"target_agent_name,omitempty"`
	RequiredState   string `json:"required_state"`
	CurrentState    string `json:"current_state,omitempty"`
	Reason          string `json:"reason"` // state_mismatch, agent_offline, state_too_old
}

func (Task) TableName() string {
	return "tasks"
}

// CommandQueue represents pending commands to agents
type CommandQueue struct {
	ID          string         `gorm:"primaryKey;size:50"`
	AgentID     sql.NullString `gorm:"size:50"`
	CommandType string         `gorm:"size:50;not null"` // EXECUTE_STEP, CANCEL, UPDATE_CONFIG
	Payload     datatypes.JSON `gorm:"type:jsonb"`
	Status      string         `gorm:"size:20;default:pending"` // pending, sent, completed, failed
	Result      datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	ProcessedAt sql.NullTime

	// Relationships
	Agent *Agent `gorm:"foreignKey:AgentID"`
}

func (CommandQueue) TableName() string {
	return "command_queue"
}

// AgentBehaviorTree tracks which behavior trees are deployed to which agents
type AgentBehaviorTree struct {
	ID             string `gorm:"primaryKey;size:50"`
	AgentID        string `gorm:"size:50;not null"`
	BehaviorTreeID string `gorm:"size:50;not null;column:action_graph_id"` // Keep DB column name

	// Version tracking
	ServerVersion   int `gorm:"not null"`                // Current version on server
	DeployedVersion int `gorm:"column:deployed_version"` // Version deployed to agent (0 = never)

	// Deployment status: pending, deploying, deployed, failed, outdated
	DeploymentStatus string         `gorm:"size:20;default:pending"`
	DeploymentError  sql.NullString `gorm:"type:text"`
	DeployedAt       sql.NullTime

	// Customization
	CustomSteps datatypes.JSON `gorm:"type:jsonb"`

	// Settings
	Enabled  bool `gorm:"default:true"`
	Priority int  `gorm:"default:0"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`

	// Relationships
	Agent          *Agent                      `gorm:"foreignKey:AgentID"`
	BehaviorTree   *BehaviorTree               `gorm:"foreignKey:BehaviorTreeID"`
	DeploymentLogs []BehaviorTreeDeploymentLog `gorm:"foreignKey:AgentBehaviorTreeID"`
}

// TableName returns "agent_action_graphs" to keep the same DB table (avoid migration)
func (AgentBehaviorTree) TableName() string {
	return "agent_action_graphs"
}

// BehaviorTreeDeploymentLog is an audit log for deployments
type BehaviorTreeDeploymentLog struct {
	ID                  string         `gorm:"primaryKey;size:50"`
	AgentBehaviorTreeID string         `gorm:"size:50;not null;column:agent_action_graph_id"` // Keep DB column name
	Action              string         `gorm:"size:20;not null"`                              // deploy, undeploy, update, retry
	Version             int            `gorm:"not null"`
	Status              string         `gorm:"size:20;not null"` // success, failed, timeout
	ErrorMessage        sql.NullString `gorm:"type:text"`
	InitiatedAt         time.Time      `gorm:"autoCreateTime"`
	CompletedAt         sql.NullTime

	// Relationships
	AgentBehaviorTree *AgentBehaviorTree `gorm:"foreignKey:AgentBehaviorTreeID"`
}

// TableName returns "action_graph_deployment_logs" to keep the same DB table (avoid migration)
func (BehaviorTreeDeploymentLog) TableName() string {
	return "action_graph_deployment_logs"
}

// ============================================================
// Parsed Types for Steps (not stored in DB directly)
// ============================================================

// BehaviorTreeStep represents a step in a behavior tree
type BehaviorTreeStep struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	JobName      string `json:"job_name,omitempty"`      // User-defined job name for this step
	Type         string `json:"type,omitempty"`          // terminal, fallback
	TerminalType string `json:"terminal_type,omitempty"` // success, failure
	Alert        bool   `json:"alert,omitempty"`
	Message      string `json:"message,omitempty"`

	AutoGenerateStates bool `json:"auto_generate_states,omitempty"` // Whether to auto-generate states from job name

	PreStates          []string      `json:"pre_states,omitempty"`
	DuringStates       []string      `json:"during_states,omitempty"`
	DuringStateTargets []StateTarget `json:"during_state_targets,omitempty"`
	SuccessStates      []string      `json:"success_states,omitempty"`
	FailureStates      []string      `json:"failure_states,omitempty"`

	StartConditions []StartCondition `json:"start_conditions,omitempty"`
	EndStates       []EndState       `json:"end_states,omitempty"`

	Action     *StepAction     `json:"action,omitempty"`
	WaitFor    *WaitFor        `json:"wait_for,omitempty"`
	Transition *StepTransition `json:"transition,omitempty"`
}

type StepAction struct {
	Type         string            `json:"type"`
	Server       string            `json:"server"`
	Params       *ActionParams     `json:"params,omitempty"`
	TimeoutSec   float64           `json:"timeout_sec,omitempty"`
	ResultSchema *StepResultSchema `json:"result_schema,omitempty"` // Expected result schema (for other steps to reference)
}

// ParameterFieldSource defines how a single parameter field gets its value
type ParameterFieldSource struct {
	Source      string      `json:"source"`                 // constant, step_result, dynamic, expression
	Value       interface{} `json:"value,omitempty"`        // For constant
	StepID      string      `json:"step_id,omitempty"`      // For step_result
	ResultField string      `json:"result_field,omitempty"` // For step_result (e.g., "pose.position.x")
	Expression  string      `json:"expression,omitempty"`   // For expression
}

// ResultFieldDef defines a single field in the result schema
type ResultFieldDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// StepResultSchema defines the expected result schema for a step
type StepResultSchema struct {
	Fields []ResultFieldDef `json:"fields,omitempty"`
}

type ActionParams struct {
	Source     string                 `json:"source,omitempty"` // waypoint, inline, dynamic, mapped
	WaypointID string                 `json:"waypoint_id,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Fields     []string               `json:"fields,omitempty"`

	// Per-field source mapping (when Source="mapped")
	FieldSources map[string]ParameterFieldSource `json:"field_sources,omitempty"`
}

type WaitFor struct {
	Type       string  `json:"type"` // manual_confirm
	Message    string  `json:"message,omitempty"`
	TimeoutSec float64 `json:"timeout_sec,omitempty"`
}

type StepTransition struct {
	OnSuccess  interface{}         `json:"on_success,omitempty"` // string or object
	OnFailure  interface{}         `json:"on_failure,omitempty"` // string or TransitionOnFailure
	OnConfirm  string              `json:"on_confirm,omitempty"`
	OnCancel   string              `json:"on_cancel,omitempty"`
	OnTimeout  string              `json:"on_timeout,omitempty"`
	OnOutcomes []OutcomeTransition `json:"on_outcomes,omitempty"`
}

type TransitionOnFailure struct {
	Retry    int    `json:"retry,omitempty"`
	Fallback string `json:"fallback,omitempty"`
	Next     string `json:"next,omitempty"`
}

type OutcomeTransition struct {
	Outcome string `json:"outcome"`
	Next    string `json:"next,omitempty"`
	State   string `json:"state,omitempty"`
}

type EndState struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Label   string `json:"label,omitempty"`
	Color   string `json:"color,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}

// StateTarget defines which agents receive a state during execution.
type StateTarget struct {
	State      string `json:"state"`
	TargetType string `json:"target_type,omitempty"` // self, all, agent
	AgentID    string `json:"agent_id,omitempty"`
}

// StartCondition represents a structured start condition (AND/OR list).
type StartCondition struct {
	ID string `json:"id"`

	Operator   string `json:"operator,omitempty"`    // and, or
	Quantifier string `json:"quantifier,omitempty"`  // self, all, any, none, specific
	TargetType string `json:"target_type,omitempty"` // self, agent, all
	AgentID    string `json:"agent_id,omitempty"`    // For 'specific' quantifier

	State         string   `json:"state,omitempty"`
	StateOperator string   `json:"state_operator,omitempty"` // ==, !=, in, not_in
	AllowedStates []string `json:"allowed_states,omitempty"`

	MaxStalenessSec float64 `json:"max_staleness_sec,omitempty"`
	RequireOnline   bool    `json:"require_online,omitempty"`

	Message string `json:"message,omitempty"`
}

// Precondition represents a behavior tree precondition
type Precondition struct {
	Type      string `json:"type"`      // agent_state, zone_free, etc.
	Condition string `json:"condition"` // Expression to evaluate
	Message   string `json:"message"`   // Error message if failed
}

// ============================================================
// Enhanced Precondition Types for Cross-Agent State Checking
// ============================================================

// EnhancedPrecondition represents a structured precondition with multiple types
type EnhancedPrecondition struct {
	Type string `json:"type"` // self_state, agent_state, semantic_tag, any_agent_state

	// For self_state
	StateCode string `json:"state_code,omitempty"`
	Operator  string `json:"operator,omitempty"` // ==, !=

	// For agent_state
	TargetAgentID string `json:"target_agent_id,omitempty"`
	SemanticTag   string `json:"semantic_tag,omitempty"`

	// For any_agent_state
	Filter         *PreconditionFilter `json:"filter,omitempty"`
	CountCondition string              `json:"count_condition,omitempty"` // ">= 1", "== 0", etc.

	// Common
	Message string `json:"message,omitempty"`
}

// PreconditionFilter for any_agent_state type
type PreconditionFilter struct {
	GraphID       string   `json:"graph_id,omitempty"`
	Capability    string   `json:"capability,omitempty"`
	Tags          []string `json:"tags,omitempty"`           // Required semantic tags
	OnlineOnly    bool     `json:"online_only,omitempty"`    // Only check online agents
	ExecutingOnly bool     `json:"executing_only,omitempty"` // Only check executing agents
	IncludeSelf   bool     `json:"include_self,omitempty"`   // Include self in any_agent_state
}

// ============================================================
// State Generation Helpers
// ============================================================

// GenerateStatesFromSteps creates auto-generated states from behavior tree steps
func GenerateStatesFromSteps(steps []BehaviorTreeStep, existingStates []GraphState) []GraphState {
	states := make([]GraphState, 0)

	// 1. Add system states
	states = append(states, SystemStates...)

	// 2. Keep existing custom states
	for _, s := range existingStates {
		if s.Type == "custom" {
			states = append(states, s)
		}
	}

	// 3. Generate auto states for each step
	phases := []struct {
		phase string
		name  string
		color string
	}{
		{"executing", "Executing", "#3b82f6"},
		{"success", "Success", "#22c55e"},
		{"failed", "Failed", "#ef4444"},
	}

	for _, step := range steps {
		// Skip terminal steps
		if step.Type == "terminal" {
			continue
		}

		stepName := step.Name
		if stepName == "" {
			stepName = step.ID
		}

		for _, p := range phases {
			states = append(states, GraphState{
				Code:   step.ID + ":" + p.phase,
				Name:   stepName + " - " + p.name,
				Type:   "auto",
				StepID: step.ID,
				Phase:  p.phase,
				Color:  p.color,
			})
		}
	}

	return states
}

// GetSemanticTagsForState returns semantic tags for a given state code
func GetSemanticTagsForState(states []GraphState, stateCode string) []string {
	for _, s := range states {
		if s.Code == stateCode {
			return s.SemanticTags
		}
	}
	return nil
}

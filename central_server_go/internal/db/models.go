package db

import (
	"database/sql"
	"time"

	"gorm.io/datatypes"
)

// Agent represents a fleet agent that manages one or more robots
type Agent struct {
	ID        string         `gorm:"primaryKey;size:50"`
	Name      string         `gorm:"size:100;not null"`
	IPAddress sql.NullString `gorm:"size:45"`
	LastSeen  sql.NullTime
	Status    string    `gorm:"size:20;default:offline"` // online, offline, warning
	CreatedAt time.Time `gorm:"autoCreateTime"`

	// Relationships
	Robots            []Robot            `gorm:"foreignKey:AgentID"`
	AgentActionGraphs []AgentActionGraph `gorm:"foreignKey:AgentID"`
	ActionGraphs      []ActionGraph      `gorm:"foreignKey:AgentID"`
	Capabilities      []AgentCapability  `gorm:"foreignKey:AgentID"`
}

func (Agent) TableName() string {
	return "agents"
}

// Robot represents an individual robot
type Robot struct {
	ID            string         `gorm:"primaryKey;size:50"`
	Name          string         `gorm:"size:100;not null"`
	Namespace     string         `gorm:"size:100"` // ROS2 namespace
	AgentID       sql.NullString `gorm:"size:50"`
	IPAddress     sql.NullString `gorm:"size:45"`
	Tags          datatypes.JSON `gorm:"type:jsonb"` // Grouping tags []string
	LastSeen      sql.NullTime
	CurrentState  string         `gorm:"size:50;default:idle"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`

	// Relationships
	Agent        *Agent            `gorm:"foreignKey:AgentID"`
	Tasks        []Task            `gorm:"foreignKey:RobotID"`
	Commands     []CommandQueue    `gorm:"foreignKey:RobotID"`
	Capabilities []RobotCapability `gorm:"foreignKey:RobotID"`
}

func (Robot) TableName() string {
	return "robots"
}

// RobotCapability represents an auto-discovered capability from ROS2 Action Server
type RobotCapability struct {
	ID           string         `gorm:"primaryKey;size:100"` // robot_id + action_type hash
	RobotID      string         `gorm:"size:50;not null;index"`
	ActionType   string         `gorm:"size:100;not null"` // e.g., "nav2_msgs/action/NavigateToPose"
	ActionServer string         `gorm:"size:200;not null"` // e.g., "/robot_001/navigate_to_pose"

	// Auto-introspected schemas
	GoalSchema     datatypes.JSON `gorm:"type:jsonb"` // Goal message schema
	ResultSchema   datatypes.JSON `gorm:"type:jsonb"` // Result message schema
	FeedbackSchema datatypes.JSON `gorm:"type:jsonb"` // Feedback message schema

	// Inferred success criteria
	SuccessCriteria datatypes.JSON `gorm:"type:jsonb"` // Auto-inferred from result schema

	// Runtime status
	Status       string       `gorm:"size:20;default:idle"` // idle, executing
	IsAvailable  bool         `gorm:"default:true"`
	LastUsedAt   sql.NullTime
	DiscoveredAt time.Time    `gorm:"autoCreateTime"`
	UpdatedAt    time.Time    `gorm:"autoUpdateTime"`

	// Relationships
	Robot *Robot `gorm:"foreignKey:RobotID"`
}

func (RobotCapability) TableName() string {
	return "robot_capabilities"
}

// AgentCapability represents an auto-discovered capability from ROS2 Action Server
// This is the primary capability model - capabilities are discovered per-agent, not per-robot
type AgentCapability struct {
	ID           string         `gorm:"primaryKey;size:100"` // agent_id + action_server hash
	AgentID      string         `gorm:"size:50;not null;index"`
	ActionType   string         `gorm:"size:100;not null"` // e.g., "nav2_msgs/action/NavigateToPose"
	ActionServer string         `gorm:"size:200;not null"` // e.g., "/navigate_to_pose"

	// Auto-introspected schemas
	GoalSchema     datatypes.JSON `gorm:"type:jsonb"` // Goal message schema
	ResultSchema   datatypes.JSON `gorm:"type:jsonb"` // Result message schema
	FeedbackSchema datatypes.JSON `gorm:"type:jsonb"` // Feedback message schema

	// Inferred success criteria
	SuccessCriteria datatypes.JSON `gorm:"type:jsonb"` // Auto-inferred from result schema

	// Runtime status
	Status       string       `gorm:"size:20;default:idle"` // idle, executing
	IsAvailable  bool         `gorm:"default:true"`
	LastUsedAt   sql.NullTime
	DiscoveredAt time.Time    `gorm:"autoCreateTime"`
	UpdatedAt    time.Time    `gorm:"autoUpdateTime"`

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
	ID                string                 `gorm:"primaryKey;size:50"`
	Name              string                 `gorm:"size:100;not null"`
	Description       sql.NullString         `gorm:"type:text"`
	States            []string               `gorm:"-"`
	DefaultState      string                 `gorm:"size:50;not null"`
	ActionMappings    []StateActionMapping   `gorm:"-"`
	TeachableWaypoints []string              `gorm:"-"`
	Version           int                    `gorm:"default:1"`
	CreatedAt         time.Time              `gorm:"autoCreateTime"`
	UpdatedAt         time.Time              `gorm:"autoUpdateTime"`
}

func (StateDefinition) TableName() string {
	return "state_definitions"
}

// ActionGraph represents a workflow of steps (template or agent-specific)
type ActionGraph struct {
	ID               string         `gorm:"primaryKey;size:50"`
	Name             string         `gorm:"size:100;not null"`
	Description      sql.NullString `gorm:"type:text"`
	AgentID          sql.NullString `gorm:"size:50;index"` // null = template
	Preconditions    datatypes.JSON `gorm:"type:jsonb"`
	Steps            datatypes.JSON `gorm:"type:jsonb;not null"`
	Version          int            `gorm:"default:1"`
	IsTemplate       bool           `gorm:"default:false"`
	TemplateCategory sql.NullString `gorm:"size:50"`
	CreatedAt        time.Time      `gorm:"autoCreateTime"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime"`

	// Capability-based: required action types for this graph
	RequiredActionTypes datatypes.JSON `gorm:"type:jsonb;default:'[]'"` // ["nav2_msgs/NavigateToPose", ...]

	// Relationships
	Agent             *Agent             `gorm:"foreignKey:AgentID"`
	Tasks             []Task             `gorm:"foreignKey:ActionGraphID"`
	AgentActionGraphs []AgentActionGraph `gorm:"foreignKey:ActionGraphID"`
}

func (ActionGraph) TableName() string {
	return "action_graphs"
}

// ExtractActionTypesFromSteps extracts unique action types from action graph steps
func ExtractActionTypesFromSteps(steps []ActionGraphStep) []string {
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
	Template            ActionGraph
	RequiredActionTypes []string
	MissingCapabilities []string
	IsFullyCompatible   bool
	AlreadyAssigned     bool
}

// Task represents a running or completed action graph execution
type Task struct {
	ID               string         `gorm:"primaryKey;size:50"`
	ActionGraphID    sql.NullString `gorm:"size:50"`
	RobotID          sql.NullString `gorm:"size:50"`
	Status           string         `gorm:"size:20;not null;default:pending"` // pending, running, completed, failed, cancelled, paused
	CurrentStepID    sql.NullString `gorm:"size:50"`
	CurrentStepIndex int            `gorm:"default:0"`
	StepResults      datatypes.JSON `gorm:"type:jsonb"`
	RetryCount       datatypes.JSON `gorm:"type:jsonb"` // {step_id: count}
	ErrorMessage     sql.NullString `gorm:"type:text"`
	CreatedAt        time.Time      `gorm:"autoCreateTime"`
	StartedAt        sql.NullTime
	CompletedAt      sql.NullTime

	// Relationships
	ActionGraph *ActionGraph `gorm:"foreignKey:ActionGraphID"`
	Robot       *Robot       `gorm:"foreignKey:RobotID"`
}

func (Task) TableName() string {
	return "tasks"
}

// CommandQueue represents pending commands to robots
type CommandQueue struct {
	ID          string         `gorm:"primaryKey;size:50"`
	RobotID     sql.NullString `gorm:"size:50"`
	CommandType string         `gorm:"size:50;not null"` // EXECUTE_STEP, CANCEL, UPDATE_CONFIG
	Payload     datatypes.JSON `gorm:"type:jsonb"`
	Status      string         `gorm:"size:20;default:pending"` // pending, sent, completed, failed
	Result      datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	ProcessedAt sql.NullTime

	// Relationships
	Robot *Robot `gorm:"foreignKey:RobotID"`
}

func (CommandQueue) TableName() string {
	return "command_queue"
}

// AgentActionGraph tracks which action graphs are deployed to which agents
type AgentActionGraph struct {
	ID            string `gorm:"primaryKey;size:50"`
	AgentID       string `gorm:"size:50;not null"`
	ActionGraphID string `gorm:"size:50;not null"`

	// Version tracking
	ServerVersion   int `gorm:"not null"`        // Current version on server
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
	Agent          *Agent                       `gorm:"foreignKey:AgentID"`
	ActionGraph    *ActionGraph                 `gorm:"foreignKey:ActionGraphID"`
	DeploymentLogs []ActionGraphDeploymentLog `gorm:"foreignKey:AgentActionGraphID"`
}

func (AgentActionGraph) TableName() string {
	return "agent_action_graphs"
}

// ActionGraphDeploymentLog is an audit log for deployments
type ActionGraphDeploymentLog struct {
	ID                 string    `gorm:"primaryKey;size:50"`
	AgentActionGraphID string    `gorm:"size:50;not null"`
	Action             string    `gorm:"size:20;not null"` // deploy, undeploy, update, retry
	Version            int       `gorm:"not null"`
	Status             string    `gorm:"size:20;not null"` // success, failed, timeout
	ErrorMessage       sql.NullString `gorm:"type:text"`
	InitiatedAt        time.Time `gorm:"autoCreateTime"`
	CompletedAt        sql.NullTime

	// Relationships
	AgentActionGraph *AgentActionGraph `gorm:"foreignKey:AgentActionGraphID"`
}

func (ActionGraphDeploymentLog) TableName() string {
	return "action_graph_deployment_logs"
}

// ============================================================
// Parsed Types for Steps (not stored in DB directly)
// ============================================================

// ActionGraphStep represents a step in an action graph
type ActionGraphStep struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name,omitempty"`
	Type         string                   `json:"type,omitempty"`         // terminal, fallback
	TerminalType string                   `json:"terminal_type,omitempty"` // success, failure
	Alert        bool                     `json:"alert,omitempty"`
	Message      string                   `json:"message,omitempty"`

	PreStates     []string `json:"pre_states,omitempty"`
	DuringStates  []string `json:"during_states,omitempty"`
	DuringStateTargets []StateTarget `json:"during_state_targets,omitempty"`
	SuccessStates []string `json:"success_states,omitempty"`
	FailureStates []string `json:"failure_states,omitempty"`

	StartConditions []StartCondition `json:"start_conditions,omitempty"`
	EndStates       []EndState       `json:"end_states,omitempty"`

	Action     *StepAction     `json:"action,omitempty"`
	WaitFor    *WaitFor        `json:"wait_for,omitempty"`
	Transition *StepTransition `json:"transition,omitempty"`
}

type StepAction struct {
	Type       string       `json:"type"`
	Server     string       `json:"server"`
	Params     *ActionParams `json:"params,omitempty"`
	TimeoutSec float64      `json:"timeout_sec,omitempty"`
}

type ActionParams struct {
	Source     string                 `json:"source,omitempty"` // waypoint, inline, dynamic
	WaypointID string                 `json:"waypoint_id,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Fields     []string               `json:"fields,omitempty"`
}

type WaitFor struct {
	Type       string  `json:"type"`                  // manual_confirm
	Message    string  `json:"message,omitempty"`
	TimeoutSec float64 `json:"timeout_sec,omitempty"`
}

type StepTransition struct {
	OnSuccess interface{} `json:"on_success,omitempty"` // string or object
	OnFailure interface{} `json:"on_failure,omitempty"` // string or TransitionOnFailure
	OnConfirm string      `json:"on_confirm,omitempty"`
	OnCancel  string      `json:"on_cancel,omitempty"`
	OnTimeout string      `json:"on_timeout,omitempty"`
	OnOutcomes []OutcomeTransition `json:"on_outcomes,omitempty"`
}

type TransitionOnFailure struct {
	Retry    int    `json:"retry,omitempty"`
	Fallback string `json:"fallback,omitempty"`
	Next     string `json:"next,omitempty"`
}

type OutcomeTransition struct {
	Outcome   string `json:"outcome"`
	Next      string `json:"next,omitempty"`
	Condition string `json:"condition,omitempty"`
	State     string `json:"state,omitempty"`
}

type EndState struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Label     string `json:"label,omitempty"`
	Color     string `json:"color,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Condition string `json:"condition,omitempty"`
}

// StateTarget defines which robots receive a state during execution.
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
	TargetType string `json:"target_type,omitempty"` // self, robot, agent, all
	RobotID    string `json:"robot_id,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`

	State         string   `json:"state,omitempty"`
	StateOperator string   `json:"state_operator,omitempty"` // ==, !=, in, not_in
	AllowedStates []string `json:"allowed_states,omitempty"`

	MaxStalenessSec float64 `json:"max_staleness_sec,omitempty"`
	RequireOnline   bool    `json:"require_online,omitempty"`

	Message string `json:"message,omitempty"`
}

// Precondition represents an action graph precondition
type Precondition struct {
	Type      string `json:"type"`      // robot_state, zone_free, etc.
	Condition string `json:"condition"` // Expression to evaluate
	Message   string `json:"message"`   // Error message if failed
}

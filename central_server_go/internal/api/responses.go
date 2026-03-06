package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// ============================================================
// Response Helpers
// ============================================================

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// ============================================================
// Robot Response Models
// ============================================================

type RobotResponse struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Namespace    string     `json:"namespace,omitempty"`
	AgentID      string     `json:"agent_id,omitempty"`
	IPAddress    string     `json:"ip_address,omitempty"`
	Tags         []string   `json:"tags,omitempty"`
	LastSeen     *time.Time `json:"last_seen,omitempty"`
	CurrentState string     `json:"current_state"`
	IsOnline     bool       `json:"is_online"`
	StalenessSec float64    `json:"staleness_sec"`
	CreatedAt    time.Time  `json:"created_at"`
}

type RobotDetailResponse struct {
	RobotResponse
	CurrentTask  map[string]interface{} `json:"current_task,omitempty"`
	Capabilities []CapabilityResponse   `json:"capabilities,omitempty"`
}

type RobotConnectRequest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AgentID   string `json:"agent_id,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
}

// ============================================================
// Behavior Tree Response Models
// ============================================================

type BehaviorTreeListResponse struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description,omitempty"`
	AgentID          string    `json:"agent_id,omitempty"`
	AgentName        string    `json:"agent_name,omitempty"`
	EntryPoint       string    `json:"entry_point,omitempty"`
	StepCount        int       `json:"step_count"`
	StateCount       int       `json:"state_count"`
	Version          int       `json:"version"`
	IsTemplate       bool      `json:"is_template"`
	DeploymentStatus string    `json:"deployment_status,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// GraphStateResponse represents a state in the API response
type GraphStateResponse struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	StepID       string   `json:"step_id,omitempty"`
	Phase        string   `json:"phase,omitempty"`
	Color        string   `json:"color,omitempty"`
	Description  string   `json:"description,omitempty"`
	SemanticTags []string `json:"semantic_tags,omitempty"`
}

// PlanningStateVarResponse represents a PDDL planning state variable in API responses
type PlanningStateVarResponse struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	InitialValue string `json:"initial_value,omitempty"`
	Description  string `json:"description,omitempty"`
}

type BehaviorTreeResponse struct {
	ID                 string                   `json:"id"`
	Name               string                   `json:"name"`
	Description        string                   `json:"description,omitempty"`
	AgentID            string                   `json:"agent_id,omitempty"`
	AgentName          string                   `json:"agent_name,omitempty"`
	EntryPoint         string                   `json:"entry_point,omitempty"`
	Preconditions      []map[string]interface{} `json:"preconditions,omitempty"`
	Steps              []map[string]interface{} `json:"steps"`
	States             []GraphStateResponse     `json:"states,omitempty"`
	PlanningStates     []PlanningStateVarResponse `json:"planning_states,omitempty"`
	AutoGenerateStates bool                     `json:"auto_generate_states"`
	Version            int                      `json:"version"`
	IsTemplate         bool                     `json:"is_template"`
	DeploymentStatus   string                   `json:"deployment_status,omitempty"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
}

type BehaviorTreeCreateRequest struct {
	ID                 string                   `json:"id"`
	Name               string                   `json:"name"`
	Description        string                   `json:"description,omitempty"`
	AgentID            string                   `json:"agent_id,omitempty"`
	EntryPoint         string                   `json:"entry_point,omitempty"`
	Preconditions      []map[string]interface{} `json:"preconditions,omitempty"`
	Steps              []map[string]interface{} `json:"steps"`
	States             []GraphStateResponse     `json:"states,omitempty"`
	PlanningStates     []PlanningStateVarResponse `json:"planning_states,omitempty"`
	AutoGenerateStates *bool                    `json:"auto_generate_states,omitempty"` // Pointer to detect if set
}

type BehaviorTreeUpdateRequest struct {
	Name               string                   `json:"name,omitempty"`
	Description        string                   `json:"description,omitempty"`
	EntryPoint         string                   `json:"entry_point,omitempty"`
	Preconditions      []map[string]interface{} `json:"preconditions,omitempty"`
	Steps              []map[string]interface{} `json:"steps,omitempty"`
	States             []GraphStateResponse     `json:"states,omitempty"`
	PlanningStates     []PlanningStateVarResponse `json:"planning_states,omitempty"`
	AutoGenerateStates *bool                    `json:"auto_generate_states,omitempty"` // Pointer to detect if set
}

type BehaviorTreeExecuteRequest struct {
	AgentID string                 `json:"agent_id"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// ============================================================
// Multi-Agent Execution Request/Response Models
// ============================================================

// MultiAgentExecuteRequest represents a request to execute a behavior tree on multiple agents simultaneously
type MultiAgentExecuteRequest struct {
	AgentIDs    []string                          `json:"agent_ids"`
	Params      map[string]interface{}            `json:"params,omitempty"`       // Common params for all agents
	AgentParams map[string]map[string]interface{} `json:"agent_params,omitempty"` // Per-agent params
	SyncMode    string                            `json:"sync_mode,omitempty"`    // "barrier" (default) or "best_effort"
	TimeoutSec  int                               `json:"timeout_sec,omitempty"`  // Timeout for barrier sync
}

// MultiAgentTaskInfo represents info about a single agent's task in a multi-agent execution
type MultiAgentTaskInfo struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name,omitempty"`
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
}

// MultiAgentExecuteResponse represents the response for a successful multi-agent execution
type MultiAgentExecuteResponse struct {
	ExecutionGroupID string               `json:"execution_group_id"`
	Tasks            []MultiAgentTaskInfo `json:"tasks"`
	StartedAt        time.Time            `json:"started_at"`
	SyncMode         string               `json:"sync_mode"`
	Message          string               `json:"message"`
}

// MultiAgentFailedAgent represents an agent that failed validation in multi-agent execution
type MultiAgentFailedAgent struct {
	AgentID string `json:"agent_id"`
	Reason  string `json:"reason"`
}

// MultiAgentExecuteErrorResponse represents an error response for multi-agent execution validation failure
type MultiAgentExecuteErrorResponse struct {
	Error        string                  `json:"error"`
	Message      string                  `json:"message"`
	FailedAgents []MultiAgentFailedAgent `json:"failed_agents"`
	PassedAgents []string                `json:"passed_agents,omitempty"`
}

// ============================================================
// Agent Behavior Tree Response Models
// ============================================================

type AgentResponse struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Namespace        string     `json:"namespace,omitempty"`
	IPAddress        string     `json:"ip_address,omitempty"`
	LastSeen         *time.Time `json:"last_seen,omitempty"`
	Status           string     `json:"status"`
	CurrentState     string     `json:"current_state,omitempty"`
	CurrentStateCode string     `json:"current_state_code,omitempty"` // Enhanced state code
	SemanticTags     []string   `json:"semantic_tags,omitempty"`      // Current semantic tags
	CurrentGraphID   string     `json:"current_graph_id,omitempty"`   // Currently executing graph
	RobotCount       int        `json:"robot_count"`
	HasCapabilityTemplate bool  `json:"has_capability_template"`
	CapabilityTemplateSavedAt *time.Time `json:"capability_template_saved_at,omitempty"`
	CapabilityTemplateCapabilityCount int `json:"capability_template_capability_count,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	Robots           []string   `json:"robots,omitempty"` // In 1:1 model, contains single agent ID
}

type AgentBehaviorTreeResponse struct {
	ID               string     `json:"id"`
	AgentID          string     `json:"agent_id"`
	BehaviorTreeID   string     `json:"behavior_tree_id"`
	BehaviorTreeName string     `json:"behavior_tree_name,omitempty"`
	ServerVersion    int        `json:"server_version"`
	DeployedVersion  int        `json:"deployed_version"`
	DeploymentStatus string     `json:"deployment_status"`
	DeploymentError  string     `json:"deployment_error,omitempty"`
	DeployedAt       *time.Time `json:"deployed_at,omitempty"`
	Enabled          bool       `json:"enabled"`
	Priority         int        `json:"priority"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type AssignBehaviorTreeRequest struct {
	BehaviorTreeID string `json:"behavior_tree_id"`
	Enabled        bool   `json:"enabled"`
	Priority       int    `json:"priority"`
}

type DeploymentLogResponse struct {
	ID                  string     `json:"id"`
	AgentBehaviorTreeID string     `json:"agent_behavior_tree_id"`
	Action              string     `json:"action"`
	Version             int        `json:"version"`
	Status              string     `json:"status"`
	ErrorMessage        string     `json:"error_message,omitempty"`
	InitiatedAt         time.Time  `json:"initiated_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
}

// ============================================================
// Task Response Models
// ============================================================

type TaskResponse struct {
	ID               string                   `json:"id"`
	BehaviorTreeID   string                   `json:"behavior_tree_id,omitempty"`
	BehaviorTreeName string                   `json:"behavior_tree_name,omitempty"`
	AgentID          string                   `json:"agent_id,omitempty"`
	AgentName        string                   `json:"agent_name,omitempty"`
	Status           string                   `json:"status"`
	CurrentStepID    string                   `json:"current_step_id,omitempty"`
	CurrentStepIndex int                      `json:"current_step_index"`
	StepResults      []map[string]interface{} `json:"step_results,omitempty"`
	ErrorMessage     string                   `json:"error_message,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	StartedAt        *time.Time               `json:"started_at,omitempty"`
	CompletedAt      *time.Time               `json:"completed_at,omitempty"`

	// Precondition waiting status
	IsWaitingForPrecondition    bool                            `json:"is_waiting_for_precondition,omitempty"`
	WaitingForPreconditionSince *time.Time                      `json:"waiting_for_precondition_since,omitempty"`
	BlockingConditions          []BlockingConditionInfoResponse `json:"blocking_conditions,omitempty"`
	PreconditionTimeoutSec      int                             `json:"precondition_timeout_sec,omitempty"`
}

// BlockingConditionInfoResponse represents blocking condition info for API response
type BlockingConditionInfoResponse struct {
	ConditionID     string `json:"condition_id"`
	Description     string `json:"description"`
	TargetAgentID   string `json:"target_agent_id,omitempty"`
	TargetAgentName string `json:"target_agent_name,omitempty"`
	RequiredState   string `json:"required_state"`
	CurrentState    string `json:"current_state,omitempty"`
	Reason          string `json:"reason"`
}

type TaskControlRequest struct {
	Reason string `json:"reason,omitempty"`
}

// ============================================================
// Waypoint Response Models
// ============================================================

type WaypointResponse struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	WaypointType string                 `json:"waypoint_type"`
	Data         map[string]interface{} `json:"data"`
	CreatedBy    string                 `json:"created_by,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Tags         []string               `json:"tags,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type WaypointCreateRequest struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	WaypointType string                 `json:"waypoint_type"`
	Data         map[string]interface{} `json:"data"`
	CreatedBy    string                 `json:"created_by,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Tags         []string               `json:"tags,omitempty"`
}

type WaypointUpdateRequest struct {
	Name        string                 `json:"name,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty"`
	Description string                 `json:"description,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
}

// ============================================================
// Fleet State Response Models
// ============================================================

type FleetStateResponse struct {
	Timestamp int64                            `json:"timestamp"`
	Robots    map[string]*RobotStateSnapshot   `json:"robots"`
	Agents    map[string]*AgentStateSnapshot   `json:"agents"`
	Zones     map[string]*ZoneReservationState `json:"zones,omitempty"`
}

type RobotStateSnapshot struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	AgentID        string   `json:"agent_id,omitempty"`
	CurrentState   string   `json:"current_state"`
	StateCode      string   `json:"state_code,omitempty"`       // Enhanced state code (e.g., "pick:executing")
	CurrentGraphID string   `json:"current_graph_id,omitempty"` // Currently executing graph ID
	ExecutionPhase string   `json:"execution_phase,omitempty"`  // idle, offline, starting, executing, waiting_for_precondition
	SemanticTags   []string `json:"semantic_tags,omitempty"`    // State semantic tags
	IsOnline       bool     `json:"is_online"`
	IsExecuting    bool     `json:"is_executing"`
	CurrentTaskID  string   `json:"current_task_id,omitempty"`
	CurrentStepID  string   `json:"current_step_id,omitempty"`
	StalenessSec   float64  `json:"staleness_sec"`

	// Precondition waiting status
	IsWaitingForPrecondition    bool                            `json:"is_waiting_for_precondition,omitempty"`
	WaitingForPreconditionSince string                          `json:"waiting_for_precondition_since,omitempty"` // ISO timestamp
	BlockingConditions          []BlockingConditionInfoResponse `json:"blocking_conditions,omitempty"`
}

type AgentStateSnapshot struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	IsOnline     bool    `json:"is_online"`
	StalenessSec float64 `json:"staleness_sec"`
}

type ZoneReservationState struct {
	ZoneID     string `json:"zone_id"`
	AgentID    string `json:"agent_id"`
	ReservedAt int64  `json:"reserved_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

type ValidatePreconditionsRequest struct {
	AgentID       string                   `json:"agent_id"`
	Preconditions []map[string]interface{} `json:"preconditions"`
}

type ValidatePreconditionsResponse struct {
	Valid        bool                   `json:"valid"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	Details      map[string]interface{} `json:"details,omitempty"`
}

// ============================================================
// Command Response Models
// ============================================================

type CommandPollResponse struct {
	Commands []CommandResponse `json:"commands"`
}

type CommandResponse struct {
	ID          string                 `json:"id"`
	CommandType string                 `json:"command_type"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

type CommandResultRequest struct {
	Status string                 `json:"status"`
	Result map[string]interface{} `json:"result,omitempty"`
	Error  string                 `json:"error,omitempty"`
}

// ============================================================
// Capability Response Models (Zero-Config Architecture)
// ============================================================

// CapabilityResponse represents a single robot capability
type CapabilityResponse struct {
	CapabilityKind  string                 `json:"capability_kind,omitempty"` // action, service
	ActionType      string                 `json:"action_type"`
	ActionServer    string                 `json:"action_server"`
	NodeName        string                 `json:"node_name,omitempty"`
	IsLifecycleNode bool                   `json:"is_lifecycle_node,omitempty"`
	GoalSchema      map[string]interface{} `json:"goal_schema,omitempty"`
	ResultSchema    map[string]interface{} `json:"result_schema,omitempty"`
	FeedbackSchema  map[string]interface{} `json:"feedback_schema,omitempty"`
	SuccessCriteria map[string]interface{} `json:"success_criteria,omitempty"`
	Status          string                 `json:"status"`
	IsAvailable     bool                   `json:"is_available"`
	LifecycleState  string                 `json:"lifecycle_state"` // unknown, unconfigured, inactive, active, finalized
	DiscoveredAt    time.Time              `json:"discovered_at"`
}

// AgentCapabilitiesListResponse represents all capabilities for an agent
type AgentCapabilitiesListResponse struct {
	AgentID      string               `json:"agent_id"`
	AgentName    string               `json:"agent_name"`
	Namespace    string               `json:"namespace"`
	Capabilities []CapabilityResponse `json:"capabilities"`
	LastUpdated  time.Time            `json:"last_updated"`
}

// CapabilityRegisterRequest represents a request to register capabilities
type CapabilityRegisterRequest struct {
	AgentID      string                   `json:"agent_id"`
	Capabilities []CapabilityRegisterItem `json:"capabilities"`
	Timestamp    string                   `json:"timestamp,omitempty"`
}

// CapabilityRegisterItem represents a single capability to register
type CapabilityRegisterItem struct {
	CapabilityKind  string                 `json:"capability_kind,omitempty"` // action, service
	ActionType      string                 `json:"action_type"`
	ActionServer    string                 `json:"action_server"`
	NodeName        string                 `json:"node_name,omitempty"`
	IsLifecycleNode *bool                  `json:"is_lifecycle_node,omitempty"`
	GoalSchema      map[string]interface{} `json:"goal_schema,omitempty"`
	ResultSchema    map[string]interface{} `json:"result_schema,omitempty"`
	FeedbackSchema  map[string]interface{} `json:"feedback_schema,omitempty"`
	SuccessCriteria map[string]interface{} `json:"success_criteria,omitempty"`
	IsAvailable     *bool                  `json:"is_available,omitempty"`    // Optional: defaults to true if not specified
	LifecycleState  string                 `json:"lifecycle_state,omitempty"` // unknown, unconfigured, inactive, active, finalized
}

// CapabilityStatusUpdateRequest represents a request to update capability status
type CapabilityStatusUpdateRequest struct {
	AgentID   string                      `json:"agent_id"`
	Status    map[string]CapabilityStatus `json:"status"` // action_type -> status
	Timestamp string                      `json:"timestamp,omitempty"`
}

// CapabilityStatus represents the runtime status of a capability
type CapabilityStatus struct {
	Available      bool   `json:"available"`
	Status         string `json:"status"`                    // idle, executing
	LifecycleState string `json:"lifecycle_state,omitempty"` // unknown, unconfigured, inactive, active, finalized
}

// AllCapabilitiesResponse represents capabilities aggregated across all agents
type AllCapabilitiesResponse struct {
	ActionTypes    []ActionTypeInfo    `json:"action_types"`
	ActionServers  []ActionServerInfo  `json:"action_servers"` // Individual action servers (not grouped)
	ServiceServers []ServiceServerInfo `json:"service_servers"`
	TotalAgents    int                 `json:"total_agents"`
}

// ActionTypeInfo represents info about a specific action type across agents
type ActionTypeInfo struct {
	ActionType     string   `json:"action_type"`
	AgentIDs       []string `json:"agent_ids"`
	AvailableCount int      `json:"available_count"`
	TotalCount     int      `json:"total_count"`
}

// ActionServerInfo represents an individual action server (not grouped by type)
type ActionServerInfo struct {
	ActionType      string `json:"action_type"`   // e.g., "test_msgs/TestAction"
	ActionServer    string `json:"action_server"` // e.g., "/test_A_action"
	AgentID         string `json:"agent_id"`
	AgentName       string `json:"agent_name,omitempty"`
	NodeName        string `json:"node_name,omitempty"`
	IsLifecycleNode bool   `json:"is_lifecycle_node"`
	IsAvailable     bool   `json:"is_available"`
	LifecycleState  string `json:"lifecycle_state"` // unknown, unconfigured, inactive, active, finalized
	Status          string `json:"status"`
}

// ServiceServerInfo represents an individual ROS2 service provider
type ServiceServerInfo struct {
	ServiceType     string `json:"service_type"` // e.g., "std_srvs/srv/Trigger"
	ServiceName     string `json:"service_name"` // e.g., "/reset_pose"
	AgentID         string `json:"agent_id"`
	AgentName       string `json:"agent_name,omitempty"`
	NodeName        string `json:"node_name,omitempty"`
	IsLifecycleNode bool   `json:"is_lifecycle_node"`
	IsAvailable     bool   `json:"is_available"`
	LifecycleState  string `json:"lifecycle_state"` // unknown, unconfigured, inactive, active, finalized
	Status          string `json:"status"`
}

// ============================================================
// Capability Detail & Incremental Sync Models
// ============================================================

// CapabilityDetailResponse represents a single capability with full details
type CapabilityDetailResponse struct {
	ID              string                 `json:"id"`
	AgentID         string                 `json:"agent_id"`
	AgentName       string                 `json:"agent_name,omitempty"`
	CapabilityKind  string                 `json:"capability_kind,omitempty"` // action, service
	ActionType      string                 `json:"action_type"`
	ActionServer    string                 `json:"action_server"`
	NodeName        string                 `json:"node_name,omitempty"`
	IsLifecycleNode bool                   `json:"is_lifecycle_node,omitempty"`
	GoalSchema      map[string]interface{} `json:"goal_schema,omitempty"`
	ResultSchema    map[string]interface{} `json:"result_schema,omitempty"`
	FeedbackSchema  map[string]interface{} `json:"feedback_schema,omitempty"`
	SuccessCriteria map[string]interface{} `json:"success_criteria,omitempty"`
	Description     string                 `json:"description,omitempty"`
	Category        string                 `json:"category,omitempty"`
	DefaultTimeout  float64                `json:"default_timeout"`
	SchemaVersion   int                    `json:"schema_version"`
	Status          string                 `json:"status"`
	IsAvailable     bool                   `json:"is_available"`
	LifecycleState  string                 `json:"lifecycle_state"` // unknown, unconfigured, inactive, active, finalized
	LastUsedAt      *time.Time             `json:"last_used_at,omitempty"`
	DiscoveredAt    time.Time              `json:"discovered_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	DeletedAt       *time.Time             `json:"deleted_at,omitempty"` // Non-nil if soft-deleted
}

// CapabilityChangeInfo represents a capability change for incremental sync
type CapabilityChangeInfo struct {
	ChangeType string                   `json:"change_type"` // "updated" or "deleted"
	ChangedAt  time.Time                `json:"changed_at"`
	Capability CapabilityDetailResponse `json:"capability"`
}

// CapabilitiesChangedResponse represents the response for incremental sync
type CapabilitiesChangedResponse struct {
	Since      time.Time              `json:"since"`       // The requested since timestamp
	Changes    []CapabilityChangeInfo `json:"changes"`     // Changed capabilities
	TotalCount int                    `json:"total_count"` // Number of changes
	ServerTime time.Time              `json:"server_time"` // Current server time for next sync
}

// ============================================================
// Updated Robot Request Models (Zero-Config)
// ============================================================

// RobotRegisterRequest represents a request to register a robot (capability-based)
type RobotRegisterRequest struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	AgentID      string                   `json:"agent_id"`
	Namespace    string                   `json:"namespace"`
	Tags         []string                 `json:"tags,omitempty"`
	Capabilities []CapabilityRegisterItem `json:"capabilities,omitempty"`
	IPAddress    string                   `json:"ip_address,omitempty"`
}

// RobotUpdateRequest represents a request to update a robot
type RobotUpdateRequest struct {
	Name      string   `json:"name,omitempty"`
	Namespace string   `json:"namespace,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

// ============================================================
// Agent Capability Aggregation Response Models
// ============================================================

// AgentCapabilitiesResponse represents aggregated capabilities for an agent
type AgentCapabilitiesResponse struct {
	AgentID          string                `json:"agent_id"`
	AgentName        string                `json:"agent_name"`
	Status           string                `json:"status"`
	Capabilities     []AgentCapabilityInfo `json:"capabilities"`
	TotalActionTypes int                   `json:"total_action_types"`
}

// AgentCapabilityInfo represents aggregated capability info for an agent
type AgentCapabilityInfo struct {
	ActionType     string                 `json:"action_type"`
	RobotCount     int                    `json:"robot_count"`
	ActionServers  []string               `json:"action_servers"`
	GoalSchema     map[string]interface{} `json:"goal_schema,omitempty"`
	ResultSchema   map[string]interface{} `json:"result_schema,omitempty"`
	FeedbackSchema map[string]interface{} `json:"feedback_schema,omitempty"`
}

// ActionTypeStats represents action type statistics across agents
type ActionTypeStats struct {
	ActionType string `json:"action_type"`
	AgentCount int    `json:"agent_count"`
}

// CompatibleAgentResponse represents an agent with template compatibility info
type CompatibleAgentResponse struct {
	AgentID             string   `json:"agent_id"`
	AgentName           string   `json:"agent_name"`
	Status              string   `json:"status"`
	HasAllCapabilities  bool     `json:"has_all_capabilities"`
	MissingCapabilities []string `json:"missing_capabilities,omitempty"`
}

// ============================================================
// Agent Compatible Templates Response Models
// ============================================================

// TemplateCompatibilityResponse represents a template with compatibility info for an agent
type TemplateCompatibilityResponse struct {
	TemplateID          string   `json:"template_id"`
	TemplateName        string   `json:"template_name"`
	Description         string   `json:"description,omitempty"`
	RequiredActionTypes []string `json:"required_action_types"`
	IsFullyCompatible   bool     `json:"is_fully_compatible"`
	MissingCapabilities []string `json:"missing_capabilities,omitempty"`
	AlreadyAssigned     bool     `json:"already_assigned"`
}

// AgentCompatibleTemplatesResponse represents all templates with compatibility info for an agent
type AgentCompatibleTemplatesResponse struct {
	AgentID          string                          `json:"agent_id"`
	AgentName        string                          `json:"agent_name"`
	AgentActionTypes []string                        `json:"agent_action_types"`
	Templates        []TemplateCompatibilityResponse `json:"templates"`
}

// ============================================================
// Agent Connection Status Response Models (Heartbeat Monitoring)
// ============================================================

// AgentConnectionStatusResponse represents real-time connection status of an agent
type AgentConnectionStatusResponse struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	IPAddress       string     `json:"ip_address,omitempty"`
	Status          string     `json:"status"` // "online" or "offline"
	IsOnline        bool       `json:"is_online"`
	ConnectedAt     *time.Time `json:"connected_at,omitempty"`
	LastHeartbeat   *time.Time `json:"last_heartbeat,omitempty"`
	HeartbeatAgeMs  int64      `json:"heartbeat_age_ms"` // Milliseconds since last heartbeat
	HeartbeatHealth string     `json:"heartbeat_health"` // "healthy", "warning", "critical"
	LastPing        *time.Time `json:"last_ping,omitempty"`
	PingLatencyMs   *int64     `json:"ping_latency_ms,omitempty"`
	PingLatencyUs   *int64     `json:"ping_latency_us,omitempty"`
}

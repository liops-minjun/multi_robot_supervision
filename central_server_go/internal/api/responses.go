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
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Namespace     string                 `json:"namespace,omitempty"`
	AgentID       string                 `json:"agent_id,omitempty"`
	IPAddress     string                 `json:"ip_address,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	LastSeen      *time.Time             `json:"last_seen,omitempty"`
	CurrentState  string                 `json:"current_state"`
	IsOnline      bool                   `json:"is_online"`
	StalenessSec  float64                `json:"staleness_sec"`
	CreatedAt     time.Time              `json:"created_at"`
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
// Action Graph Response Models
// ============================================================

type ActionGraphListResponse struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description,omitempty"`
	AgentID          string    `json:"agent_id,omitempty"`
	AgentName        string    `json:"agent_name,omitempty"`
	StepCount        int       `json:"step_count"`
	Version          int       `json:"version"`
	IsTemplate       bool      `json:"is_template"`
	DeploymentStatus string    `json:"deployment_status,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ActionGraphResponse struct {
	ID               string                   `json:"id"`
	Name             string                   `json:"name"`
	Description      string                   `json:"description,omitempty"`
	AgentID          string                   `json:"agent_id,omitempty"`
	AgentName        string                   `json:"agent_name,omitempty"`
	Preconditions    []map[string]interface{} `json:"preconditions,omitempty"`
	Steps            []map[string]interface{} `json:"steps"`
	Version          int                      `json:"version"`
	IsTemplate       bool                     `json:"is_template"`
	DeploymentStatus string                   `json:"deployment_status,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
}

type ActionGraphCreateRequest struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Description   string                   `json:"description,omitempty"`
	AgentID       string                   `json:"agent_id,omitempty"`
	Preconditions []map[string]interface{} `json:"preconditions,omitempty"`
	Steps         []map[string]interface{} `json:"steps"`
}

type ActionGraphUpdateRequest struct {
	Name          string                   `json:"name,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Preconditions []map[string]interface{} `json:"preconditions,omitempty"`
	Steps         []map[string]interface{} `json:"steps,omitempty"`
}

type ActionGraphExecuteRequest struct {
	RobotID string                 `json:"robot_id"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// ============================================================
// Agent Action Graph Response Models
// ============================================================

type AgentResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	IPAddress  string     `json:"ip_address,omitempty"`
	LastSeen   *time.Time `json:"last_seen,omitempty"`
	Status     string     `json:"status"`
	RobotCount int        `json:"robot_count"`
	CreatedAt  time.Time  `json:"created_at"`
	Robots     []string   `json:"robots,omitempty"`
}

type AgentActionGraphResponse struct {
	ID               string     `json:"id"`
	AgentID          string     `json:"agent_id"`
	ActionGraphID    string     `json:"action_graph_id"`
	ActionGraphName  string     `json:"action_graph_name,omitempty"`
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

type AssignActionGraphRequest struct {
	ActionGraphID string `json:"action_graph_id"`
	Enabled       bool   `json:"enabled"`
	Priority      int    `json:"priority"`
}

type DeploymentLogResponse struct {
	ID                 string     `json:"id"`
	AgentActionGraphID string     `json:"agent_action_graph_id"`
	Action             string     `json:"action"`
	Version            int        `json:"version"`
	Status             string     `json:"status"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	InitiatedAt        time.Time  `json:"initiated_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

// ============================================================
// Task Response Models
// ============================================================

type TaskResponse struct {
	ID               string                   `json:"id"`
	ActionGraphID    string                   `json:"action_graph_id,omitempty"`
	ActionGraphName  string                   `json:"action_graph_name,omitempty"`
	RobotID          string                   `json:"robot_id,omitempty"`
	RobotName        string                   `json:"robot_name,omitempty"`
	Status           string                   `json:"status"`
	CurrentStepID    string                   `json:"current_step_id,omitempty"`
	CurrentStepIndex int                      `json:"current_step_index"`
	StepResults      []map[string]interface{} `json:"step_results,omitempty"`
	ErrorMessage     string                   `json:"error_message,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	StartedAt        *time.Time               `json:"started_at,omitempty"`
	CompletedAt      *time.Time               `json:"completed_at,omitempty"`
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
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	AgentID       string  `json:"agent_id,omitempty"`
	CurrentState  string  `json:"current_state"`
	IsOnline      bool    `json:"is_online"`
	IsExecuting   bool    `json:"is_executing"`
	CurrentTaskID string  `json:"current_task_id,omitempty"`
	CurrentStepID string  `json:"current_step_id,omitempty"`
	StalenessSec  float64 `json:"staleness_sec"`
}

type AgentStateSnapshot struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	IsOnline     bool     `json:"is_online"`
	RobotIDs     []string `json:"robot_ids"`
	StalenessSec float64  `json:"staleness_sec"`
}

type ZoneReservationState struct {
	ZoneID     string `json:"zone_id"`
	RobotID    string `json:"robot_id"`
	ReservedAt int64  `json:"reserved_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

type ValidatePreconditionsRequest struct {
	RobotID       string                   `json:"robot_id"`
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
	ActionType      string                 `json:"action_type"`
	ActionServer    string                 `json:"action_server"`
	GoalSchema      map[string]interface{} `json:"goal_schema,omitempty"`
	ResultSchema    map[string]interface{} `json:"result_schema,omitempty"`
	FeedbackSchema  map[string]interface{} `json:"feedback_schema,omitempty"`
	SuccessCriteria map[string]interface{} `json:"success_criteria,omitempty"`
	Status          string                 `json:"status"`
	IsAvailable     bool                   `json:"is_available"`
	DiscoveredAt    time.Time              `json:"discovered_at"`
}

// RobotCapabilitiesResponse represents all capabilities for a robot
type RobotCapabilitiesResponse struct {
	RobotID      string               `json:"robot_id"`
	RobotName    string               `json:"robot_name"`
	Namespace    string               `json:"namespace"`
	Capabilities []CapabilityResponse `json:"capabilities"`
	LastUpdated  time.Time            `json:"last_updated"`
}

// CapabilityRegisterRequest represents a request to register capabilities
type CapabilityRegisterRequest struct {
	RobotID      string                   `json:"robot_id"`
	Capabilities []CapabilityRegisterItem `json:"capabilities"`
	Timestamp    string                   `json:"timestamp,omitempty"`
}

// CapabilityRegisterItem represents a single capability to register
type CapabilityRegisterItem struct {
	ActionType      string                 `json:"action_type"`
	ActionServer    string                 `json:"action_server"`
	GoalSchema      map[string]interface{} `json:"goal_schema,omitempty"`
	ResultSchema    map[string]interface{} `json:"result_schema,omitempty"`
	FeedbackSchema  map[string]interface{} `json:"feedback_schema,omitempty"`
	SuccessCriteria map[string]interface{} `json:"success_criteria,omitempty"`
}

// CapabilityStatusUpdateRequest represents a request to update capability status
type CapabilityStatusUpdateRequest struct {
	RobotID   string                      `json:"robot_id"`
	Status    map[string]CapabilityStatus `json:"status"` // action_type -> status
	Timestamp string                      `json:"timestamp,omitempty"`
}

// CapabilityStatus represents the runtime status of a capability
type CapabilityStatus struct {
	Available bool   `json:"available"`
	Status    string `json:"status"` // idle, executing
}

// AllCapabilitiesResponse represents capabilities aggregated across all robots
type AllCapabilitiesResponse struct {
	ActionTypes   []ActionTypeInfo   `json:"action_types"`
	ActionServers []ActionServerInfo `json:"action_servers"` // Individual action servers (not grouped)
	TotalRobots   int                `json:"total_robots"`
}

// ActionTypeInfo represents info about a specific action type across robots
type ActionTypeInfo struct {
	ActionType     string   `json:"action_type"`
	RobotIDs       []string `json:"robot_ids"`
	AvailableCount int      `json:"available_count"`
	TotalCount     int      `json:"total_count"`
}

// ActionServerInfo represents an individual action server (not grouped by type)
type ActionServerInfo struct {
	ActionType   string `json:"action_type"`   // e.g., "test_msgs/TestAction"
	ActionServer string `json:"action_server"` // e.g., "/test_A_action"
	AgentID      string `json:"agent_id"`
	AgentName    string `json:"agent_name,omitempty"`
	IsAvailable  bool   `json:"is_available"`
	Status       string `json:"status"`
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
	RobotIDs        []string   `json:"robot_ids"`
}

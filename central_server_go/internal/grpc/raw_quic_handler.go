package grpc

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"central_server_go/internal/db"
	"central_server_go/internal/state"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/encoding/protowire"
)

// ============================================================
// Raw QUIC Handler for Agent Communication
// ============================================================

// RawQUICHandler handles raw protobuf messages over QUIC
// without gRPC framing, for C++ agents using MsQuic
type RawQUICHandler struct {
	listener     *quic.Listener
	stateManager *state.GlobalStateManager
	repo         *db.Repository
	wsHub        WebSocketBroadcaster // Interface to broadcast to frontend

	connections map[string]*agentConnection
	connMu      sync.RWMutex

	// Pending command tracking for request-response pattern
	pendingCommands map[string]*PendingCommand
	pendingMu       sync.RWMutex

	// Callbacks for action results/feedback
	resultCallbacks map[string]CommandCallback
	callbackMu      sync.RWMutex

	// Pending deploy responses keyed by correlation_id
	deployWaiters map[string]chan *DeployResult
	deployMu      sync.RWMutex

	// Pending config update responses keyed by correlation_id
	configWaiters map[string]chan *ConfigUpdateResult
	configMu      sync.RWMutex

	// Graph execution state overrides (execution_id -> override state)
	graphOverrides  map[string]*graphOverrideState
	graphOverrideMu sync.Mutex

	// Ping interval for latency tracking
	pingInterval time.Duration

	// Task completion callback (for agent-driven execution)
	taskCompleteCallback   TaskCompleteCallback
	taskObserveCallback    TaskObserveCallback
	resourceChangeCallback func()

	ctx    context.Context
	cancel context.CancelFunc
}

// TaskCompleteCallback is called when agent reports task completion
type TaskCompleteCallback func(taskID string, status string, errorMsg string)

// TaskObserveCallback is called whenever the server receives the agent's
// current execution view via heartbeat or task-state update.
type TaskObserveCallback func(agentID string, isExecuting bool, currentTaskID string)

// Note: PendingCommand is defined in handlers.go

// WebSocketBroadcaster interface for broadcasting to frontend
// This abstracts the WebSocketHub from api package to avoid circular imports
type WebSocketBroadcaster interface {
	BroadcastAgentUpdate(agentID string, status string)
	BroadcastCapabilityUpdate(agentID string, capabilities interface{})
	BroadcastTaskStateUpdate(update interface{})
}

// TaskStateUpdateBroadcast is the struct sent to WebSocket clients for task state updates
type TaskStateUpdateBroadcast struct {
	TaskID         string               `json:"task_id"`
	StepID         string               `json:"step_id"`
	State          string               `json:"state"`
	Progress       float32              `json:"progress"`
	BlockingReason string               `json:"blocking_reason,omitempty"`
	Variables      map[string]string    `json:"variables,omitempty"`
	StepResult     *StepResultBroadcast `json:"step_result,omitempty"`
}

// StepResultBroadcast is the step result info sent to WebSocket clients
type StepResultBroadcast struct {
	StepID     string `json:"step_id"`
	Status     int32  `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// agentConnection tracks an agent's QUIC connection with bidirectional support
type agentConnection struct {
	agentID    string
	quicConn   quic.Connection
	streams    map[uint64]quic.Stream
	streamMu   sync.Mutex
	lastSeen   time.Time
	registered bool

	// Bidirectional communication
	commandStream quic.Stream           // Primary stream for Server→Agent commands
	sendChan      chan *OutboundCommand // Queue for outgoing commands
	sendDone      chan struct{}         // Signal to stop sender goroutine
	// In 1:1 model, agent_id = robot_id, so no separate robotIDs field needed

	// Debug counters
	telemetryLogCounter int // For rate-limited telemetry debug logging
}

// OutboundCommand represents a command to send to agent
type OutboundCommand struct {
	Data      []byte
	ResponseC chan *AgentMsg // Optional: for request-response pattern
	Timeout   time.Duration
}

type graphOverrideState struct {
	StepID   string
	AgentIDs []string
}

// CommandCallback is called when action result/feedback is received
type CommandCallback func(result *ActionResult, feedback *ActionFeedbackMsg, err error)

// NewRawQUICHandler creates a new raw QUIC handler
func NewRawQUICHandler(
	stateManager *state.GlobalStateManager,
	repo *db.Repository,
	wsHub WebSocketBroadcaster,
) *RawQUICHandler {
	ctx, cancel := context.WithCancel(context.Background())
	return &RawQUICHandler{
		stateManager:    stateManager,
		repo:            repo,
		wsHub:           wsHub,
		connections:     make(map[string]*agentConnection),
		pendingCommands: make(map[string]*PendingCommand),
		resultCallbacks: make(map[string]CommandCallback),
		deployWaiters:   make(map[string]chan *DeployResult),
		configWaiters:   make(map[string]chan *ConfigUpdateResult),
		graphOverrides:  make(map[string]*graphOverrideState),
		pingInterval:    time.Second,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// SetTaskCompleteCallback sets the callback for agent-driven task completion
func (h *RawQUICHandler) SetTaskCompleteCallback(cb TaskCompleteCallback) {
	h.taskCompleteCallback = cb
}

// SetTaskObserveCallback wires heartbeat-driven task dispatch decisions.
func (h *RawQUICHandler) SetTaskObserveCallback(cb TaskObserveCallback) {
	h.taskObserveCallback = cb
}

func (h *RawQUICHandler) SetResourceChangeCallback(cb func()) {
	h.resourceChangeCallback = cb
}

// Start starts listening for raw QUIC connections
func (h *RawQUICHandler) Start(addr string, tlsConfig *tls.Config) error {
	// Configure TLS for raw QUIC (different ALPN from gRPC)
	rawTLSConfig := tlsConfig.Clone()
	rawTLSConfig.NextProtos = []string{"robot-agent-raw", "h3"}

	// QUIC configuration
	quicConfig := &quic.Config{
		MaxIdleTimeout:        30 * time.Second,
		KeepAlivePeriod:       10 * time.Second,
		Allow0RTT:             true,
		MaxIncomingStreams:    1000,
		MaxIncomingUniStreams: 100,
	}

	listener, err := quic.ListenAddr(addr, rawTLSConfig, quicConfig)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	h.listener = listener

	log.Printf("[RawQUIC] Listening on %s (ALPN: robot-agent-raw)", addr)

	// Start accepting connections
	go h.acceptLoop()
	go h.pingLoop()

	return nil
}

// Stop stops the handler
func (h *RawQUICHandler) Stop() {
	h.cancel()
	if h.listener != nil {
		h.listener.Close()
	}

	// Close all connections
	h.connMu.Lock()
	for _, conn := range h.connections {
		conn.quicConn.CloseWithError(0, "server shutdown")
	}
	h.connections = make(map[string]*agentConnection)
	h.connMu.Unlock()
}

func (h *RawQUICHandler) acceptLoop() {
	log.Printf("[RawQUIC] acceptLoop started, waiting for connections...")

	// Periodic heartbeat to prove the loop is running
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.ctx.Done():
				return
			case <-ticker.C:
				log.Printf("[RawQUIC] acceptLoop heartbeat - still waiting for connections")
			}
		}
	}()

	for {
		select {
		case <-h.ctx.Done():
			log.Printf("[RawQUIC] acceptLoop context cancelled, exiting")
			return
		default:
			log.Printf("[RawQUIC] Calling Accept()...")
			conn, err := h.listener.Accept(h.ctx)
			if err != nil {
				if h.ctx.Err() != nil {
					log.Printf("[RawQUIC] Accept cancelled by context")
					return
				}
				log.Printf("[RawQUIC] Accept error: %v", err)
				continue
			}

			log.Printf("[RawQUIC] *** NEW CONNECTION from %s (ALPN: %v) ***",
				conn.RemoteAddr(), conn.ConnectionState().TLS.NegotiatedProtocol)
			go h.handleConnection(conn)
		}
	}
}

func (h *RawQUICHandler) pingLoop() {
	if h.pingInterval <= 0 {
		h.pingInterval = time.Second
	}

	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.connMu.RLock()
			agentIDs := make([]string, 0, len(h.connections))
			for agentID, conn := range h.connections {
				if conn != nil && conn.registered {
					agentIDs = append(agentIDs, agentID)
				}
			}
			h.connMu.RUnlock()

			for _, agentID := range agentIDs {
				pingID := fmt.Sprintf("ping-%s-%d", agentID, time.Now().UnixNano())
				if err := h.SendPing(agentID, pingID); err != nil {
					log.Printf("[RawQUIC] Ping send failed for %s: %v", agentID, err)
				}
			}
		}
	}
}

func (h *RawQUICHandler) handleConnection(conn quic.Connection) {
	log.Printf("[RawQUIC] handleConnection started for %s", conn.RemoteAddr())

	agentConn := &agentConnection{
		quicConn: conn,
		streams:  make(map[uint64]quic.Stream),
		lastSeen: time.Now(),
	}

	// Accept streams from this connection
	for {
		select {
		case <-h.ctx.Done():
			log.Printf("[RawQUIC] handleConnection: context done")
			return
		case <-conn.Context().Done():
			log.Printf("[RawQUIC] handleConnection: connection context done")
			h.handleDisconnect(agentConn)
			return
		default:
			stream, err := conn.AcceptStream(h.ctx)
			if err != nil {
				if h.ctx.Err() != nil || conn.Context().Err() != nil {
					log.Printf("[RawQUIC] handleConnection: AcceptStream error (context done): %v", err)
					h.handleDisconnect(agentConn)
					return
				}
				log.Printf("[RawQUIC] handleConnection: AcceptStream error (temporary): %v", err)
				continue
			}

			log.Printf("[RawQUIC] *** STREAM ACCEPTED: ID=%d ***", stream.StreamID())

			agentConn.streamMu.Lock()
			agentConn.streams[uint64(stream.StreamID())] = stream
			agentConn.streamMu.Unlock()

			go h.handleStream(agentConn, stream)
		}
	}
}

// RegisterAgentReq represents a parsed RegisterAgentRequest (1:1 model)
type RegisterAgentReq struct {
	AgentID             string
	Name                string
	Namespace           string // ROS namespace
	ClientVersion       string
	HardwareFingerprint string // For server-assigned ID recovery
}

// AgentMsg represents a parsed AgentMessage with all payload types
type AgentMsg struct {
	AgentID     string
	TimestampMs int64

	// Payload types (oneof - only one will be set)
	Heartbeat              *AgentHeartbeatMsg         // field 10
	ActionResult           *ActionResultMsg           // field 11
	ActionFeedback         *ActionFeedbackMsg         // field 12
	StatusUpdate           *AgentStatusUpdateMsg      // field 13
	DeployResponse         *DeployGraphResponseMsg    // field 14
	GraphStatus            *GraphExecutionStatusMsg   // field 15
	TaskStateUpdate        *TaskStateUpdateMsg        // field 20 (agent-driven)
	Pong                   *PongResponseMsg           // field 16
	ConfigAck              *ConfigUpdateAckMsg        // field 17
	CapabilityRegistration *CapabilityRegistrationMsg // field 18
	TaskLog                *TaskLogMsg                // field 19
}

// TaskStateUpdateMsg represents agent-driven task state update
//
//	message TaskStateUpdate {
//	  string task_id = 1;
//	  string current_step_id = 2;
//	  TaskState state = 3;
//	  float progress = 4;
//	  string blocking_reason = 5;
//	  map<string, string> variables = 6;
//	  StepResultInfo step_result = 7;
//	  int64 timestamp_ms = 8;
//	}
type TaskStateUpdateMsg struct {
	TaskID         string
	CurrentStepID  string
	State          int32 // TaskState enum
	Progress       float32
	BlockingReason string
	Variables      map[string]string
	StepResult     *StepResultInfoMsg
	TimestampMs    int64
	ResourceEvents []ResourceEventMsg
}

type ResourceEventMsg struct {
	ResourceID string
	StepID     string
	Kind       int32
}

// StepResultInfoMsg represents step completion info
type StepResultInfoMsg struct {
	StepID     string
	Status     int32 // ActionStatus enum
	ResultJSON []byte
	Error      string
	DurationMs int64
}

// TaskLogMsg represents task execution log from agent
type TaskLogMsg struct {
	AgentID     string
	TaskID      string
	StepID      string
	CommandID   string
	Level       int32 // TaskLogLevel enum
	Message     string
	TimestampMs int64
	Component   string
	Metadata    map[string]string
}

// AgentHeartbeatMsg represents heartbeat from agent (1:1 model)
type AgentHeartbeatMsg struct {
	AgentID             string
	TimestampMs         int64
	State               string // State name string (e.g., "idle", "navigate_during")
	IsExecuting         bool
	CurrentAction       string
	CurrentTaskID       string
	CurrentStepID       string
	ActionProgress      float32
	NetworkLatencyMs    uint32
	HasNetworkLatency   bool
	NetworkLatencyUs    uint32
	HasNetworkLatencyUs bool
	Telemetry           *TelemetryPayloadMsg // Optional telemetry data
	ResourceEvents      []ResourceEventMsg
}

// TelemetryPayloadMsg represents telemetry data from agent
type TelemetryPayloadMsg struct {
	RobotID       string
	JointState    *JointStateDataMsg
	Transforms    []*TransformDataMsg
	Odometry      *OdometryDataMsg
	CollectedAtNs int64
}

// JointStateDataMsg represents joint state telemetry
type JointStateDataMsg struct {
	Name        []string
	Position    []float64
	Velocity    []float64
	Effort      []float64
	TimestampNs int64
	TopicName   string // ROS2 topic name for visualization
}

// TransformDataMsg represents transform telemetry
type TransformDataMsg struct {
	FrameID      string
	ChildFrameID string
	Translation  *Vector3Msg
	Rotation     *QuaternionMsg
	TimestampNs  int64
}

// OdometryDataMsg represents odometry telemetry
type OdometryDataMsg struct {
	FrameID      string
	ChildFrameID string
	Pose         *PoseMsg
	Twist        *TwistMsg
	TimestampNs  int64
	TopicName    string // ROS2 topic name for visualization
}

// Vector3Msg represents 3D vector
type Vector3Msg struct {
	X float64
	Y float64
	Z float64
}

// QuaternionMsg represents quaternion rotation
type QuaternionMsg struct {
	X float64
	Y float64
	Z float64
	W float64
}

// PoseMsg represents pose (position + orientation)
type PoseMsg struct {
	Position    *Vector3Msg
	Orientation *QuaternionMsg
}

// TwistMsg represents twist (linear + angular velocity)
type TwistMsg struct {
	Linear  *Vector3Msg
	Angular *Vector3Msg
}

// ActionResultMsg represents action completion result
type ActionResultMsg struct {
	CommandID     string
	AgentID       string
	TaskID        string
	StepID        string
	Status        int32 // ActionStatus enum
	Result        []byte
	Error         string
	StartedAtMs   int64
	CompletedAtMs int64
}

// ActionFeedbackMsg represents action progress feedback
type ActionFeedbackMsg struct {
	CommandID   string
	AgentID     string
	TaskID      string
	StepID      string
	Progress    float32
	Feedback    []byte
	TimestampMs int64
}

// AgentStatusUpdateMsg represents agent status change (1:1 model)
type AgentStatusUpdateMsg struct {
	State    int32 // AgentState enum
	IsOnline bool
	Message  string
}

// DeployGraphResponseMsg represents graph deployment response
type DeployGraphResponseMsg struct {
	CorrelationID   string
	Success         bool
	GraphID         string
	DeployedVersion int32
	Error           string
	Checksum        string
}

// GraphExecutionStatusMsg represents graph execution progress
type GraphExecutionStatusMsg struct {
	ExecutionID      string
	GraphID          string
	AgentID          string
	State            int32 // GraphExecutionState enum
	CurrentVertexID  string
	CurrentStepIndex int32
	Progress         float32
	StartedAtMs      int64
	Error            string
	UpdatedAtMs      int64
	CompletedAtMs    int64
	FailedVertexID   string
}

// PongResponseMsg represents ping response for latency measurement
type PongResponseMsg struct {
	PingID            string
	ServerTimestampMs int64
	AgentTimestampMs  int64
}

// ConfigUpdateAckMsg represents config update acknowledgement
type ConfigUpdateAckMsg struct {
	AgentID       string
	StateDefID    string
	Version       int32
	Success       bool
	Error         string
	CorrelationID string
}

// CapabilityRegistrationMsg represents capability registration from agent (1:1 model)
type CapabilityRegistrationMsg struct {
	AgentID      string
	Capabilities []ActionCapabilityMsg
}

// ActionCapabilityMsg represents a single capability
type ActionCapabilityMsg struct {
	ActionType      string
	ActionServer    string
	Package         string
	ActionName      string
	CapabilityKind  string // action, service
	NodeName        string // ROS2 node name that provides this capability
	IsLifecycleNode bool   // True if provider is lifecycle-managed
	LifecycleState  string // unknown, unconfigured, inactive, active, finalized
	GoalSchema      string
	ResultSchema    string
	FeedbackSchema  string
	SuccessCriteria *SuccessCriteriaMsg
	IsAvailable     bool // true if action server is currently running
}

// SuccessCriteriaMsg represents auto-inferred success criteria
type SuccessCriteriaMsg struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// parseRegisterAgentRequest manually parses protobuf RegisterAgentRequest (1:1 model)
// Proto fields: agent_id=1, name=2, namespace=3, client_version=4
func parseRegisterAgentRequest(data []byte) (*RegisterAgentReq, error) {
	req := &RegisterAgentReq{}
	sawRegisterFields := false

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // agent_id (string)
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("invalid agent_id wire type")
			}
			v, n := protowire.ConsumeString(data)
			if n < 0 {
				return nil, fmt.Errorf("invalid agent_id")
			}
			req.AgentID = v
			data = data[n:]
		case 2: // name (string)
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("invalid name wire type")
			}
			v, n := protowire.ConsumeString(data)
			if n < 0 {
				return nil, fmt.Errorf("invalid name")
			}
			req.Name = v
			data = data[n:]
			sawRegisterFields = true
		case 3: // namespace (string) - ROS namespace
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("invalid namespace wire type")
			}
			v, n := protowire.ConsumeString(data)
			if n < 0 {
				return nil, fmt.Errorf("invalid namespace")
			}
			req.Namespace = v
			data = data[n:]
			sawRegisterFields = true
		case 4: // client_version (string)
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("invalid client_version wire type")
			}
			v, n := protowire.ConsumeString(data)
			if n < 0 {
				return nil, fmt.Errorf("invalid client_version")
			}
			req.ClientVersion = v
			data = data[n:]
			sawRegisterFields = true
		case 5: // hardware_fingerprint (string)
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("invalid hardware_fingerprint wire type")
			}
			v, n := protowire.ConsumeString(data)
			if n < 0 {
				return nil, fmt.Errorf("invalid hardware_fingerprint")
			}
			req.HardwareFingerprint = v
			data = data[n:]
			sawRegisterFields = true
		default:
			if fieldNum >= 10 {
				return nil, fmt.Errorf("unexpected payload field: %d", fieldNum)
			}
			// Skip unknown field
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	// Valid if has agent_id OR has fingerprint (for server-assigned ID)
	if !sawRegisterFields {
		return nil, fmt.Errorf("not a register request")
	}

	return req, nil
}

// parseAgentMessage parses an AgentMessage protobuf with all payload types
// AgentMessage: agent_id=1, timestamp_ms=2, oneof payload (10-18)
func parseAgentMessage(data []byte) (*AgentMsg, error) {
	msg := &AgentMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // agent_id (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid agent_id")
				}
				msg.AgentID = v
				data = data[n:]
			}
		case 2: // timestamp_ms (int64)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid timestamp")
				}
				msg.TimestampMs = int64(v)
				data = data[n:]
			}
		case 10: // heartbeat (AgentHeartbeat)
			if wireType == protowire.BytesType {
				hbData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid heartbeat")
				}
				hb, err := parseAgentHeartbeat(hbData)
				if err == nil {
					msg.Heartbeat = hb
				}
				data = data[n:]
			}
		case 11: // action_result (ActionResult)
			if wireType == protowire.BytesType {
				resultData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid action_result")
				}
				result, err := parseActionResult(resultData)
				if err == nil {
					msg.ActionResult = result
				}
				data = data[n:]
			}
		case 12: // action_feedback (ActionFeedback)
			if wireType == protowire.BytesType {
				fbData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid action_feedback")
				}
				fb, err := parseActionFeedback(fbData)
				if err == nil {
					msg.ActionFeedback = fb
				}
				data = data[n:]
			}
		case 13: // status_update (AgentStatusUpdate)
			if wireType == protowire.BytesType {
				statusData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid status_update")
				}
				status, err := parseAgentStatusUpdate(statusData)
				if err == nil {
					msg.StatusUpdate = status
				}
				data = data[n:]
			}
		case 14: // deploy_response (DeployGraphResponse)
			if wireType == protowire.BytesType {
				respData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid deploy_response")
				}
				resp, err := parseDeployGraphResponse(respData)
				if err == nil {
					msg.DeployResponse = resp
				}
				data = data[n:]
			}
		case 15: // graph_status (GraphExecutionStatus)
			if wireType == protowire.BytesType {
				statusData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid graph_status")
				}
				status, err := parseGraphExecutionStatus(statusData)
				if err == nil {
					msg.GraphStatus = status
				}
				data = data[n:]
			}
		case 16: // pong (PongResponse)
			if wireType == protowire.BytesType {
				pongData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid pong")
				}
				pong, err := parsePongResponse(pongData)
				if err == nil {
					msg.Pong = pong
				}
				data = data[n:]
			}
		case 17: // config_ack (ConfigUpdateAck)
			if wireType == protowire.BytesType {
				ackData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid config_ack")
				}
				ack, err := parseConfigUpdateAck(ackData)
				if err == nil {
					msg.ConfigAck = ack
				}
				data = data[n:]
			}
		case 18: // capability_registration (CapabilityRegistration)
			if wireType == protowire.BytesType {
				capData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid capability_registration")
				}
				capReg, err := parseCapabilityRegistration(capData)
				if err == nil {
					msg.CapabilityRegistration = capReg
				}
				data = data[n:]
			}
		case 19: // task_log (TaskLog)
			if wireType == protowire.BytesType {
				logData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid task_log")
				}
				taskLog, err := parseTaskLog(logData)
				if err == nil {
					msg.TaskLog = taskLog
				}
				data = data[n:]
			}
		case 20: // task_state (TaskStateUpdate) - agent-driven execution
			if wireType == protowire.BytesType {
				updateData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid task_state")
				}
				update, err := parseTaskStateUpdate(updateData)
				if err == nil {
					msg.TaskStateUpdate = update
				}
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field %d", fieldNum)
			}
			data = data[n:]
		}
	}

	return msg, nil
}

// parseCapabilityRegistration parses CapabilityRegistration protobuf
// agent_id=1, capabilities=2 (repeated ActionCapability)
func parseCapabilityRegistration(data []byte) (*CapabilityRegistrationMsg, error) {
	reg := &CapabilityRegistrationMsg{}
	log.Printf("[DEBUG] parseCapabilityRegistration: data length=%d, hex=%x", len(data), data[:min(len(data), 64)])

	fieldCount := 0
	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag at position %d", fieldCount)
		}
		data = data[n:]
		fieldCount++

		log.Printf("[DEBUG] Field %d: fieldNum=%d, wireType=%d, remaining=%d bytes",
			fieldCount, fieldNum, wireType, len(data))

		switch fieldNum {
		case 1: // agent_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid agent_id")
				}
				reg.AgentID = v
				data = data[n:]
				log.Printf("[DEBUG] Parsed agent_id: %s", v)
			} else {
				// Skip unexpected wireType
				n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
				if n < 0 {
					return nil, fmt.Errorf("invalid field value for agent_id")
				}
				data = data[n:]
			}
		case 2: // capabilities (repeated)
			if wireType == protowire.BytesType {
				capData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid capability")
				}
				log.Printf("[DEBUG] Capability data: %d bytes", len(capData))
				cap, err := parseActionCapability(capData)
				if err == nil {
					reg.Capabilities = append(reg.Capabilities, *cap)
					log.Printf("[DEBUG] Parsed capability #%d: %s at %s",
						len(reg.Capabilities), cap.ActionType, cap.ActionServer)
				} else {
					log.Printf("[DEBUG] Failed to parse capability: %v", err)
				}
				data = data[n:]
			} else {
				// Skip unexpected wireType
				n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
				if n < 0 {
					return nil, fmt.Errorf("invalid field value for capability")
				}
				data = data[n:]
				log.Printf("[DEBUG] Skipped capability with wireType %d", wireType)
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field %d", fieldNum)
			}
			data = data[n:]
			log.Printf("[DEBUG] Skipped unknown field %d", fieldNum)
		}
	}

	log.Printf("[DEBUG] parseCapabilityRegistration complete: agent_id=%s, capabilities=%d",
		reg.AgentID, len(reg.Capabilities))
	return reg, nil
}

// parseTaskLog parses TaskLog protobuf
// TaskLog: agent_id=1, task_id=2, step_id=3, command_id=4, level=5, message=6, timestamp_ms=7, component=8, metadata=9
func parseTaskLog(data []byte) (*TaskLogMsg, error) {
	taskLog := &TaskLogMsg{
		Metadata: make(map[string]string),
	}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // agent_id (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid agent_id")
				}
				taskLog.AgentID = v
				data = data[n:]
			}
		case 2: // task_id (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid task_id")
				}
				taskLog.TaskID = v
				data = data[n:]
			}
		case 3: // step_id (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid step_id")
				}
				taskLog.StepID = v
				data = data[n:]
			}
		case 4: // command_id (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid command_id")
				}
				taskLog.CommandID = v
				data = data[n:]
			}
		case 5: // level (TaskLogLevel enum - varint)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid level")
				}
				taskLog.Level = int32(v)
				data = data[n:]
			}
		case 6: // message (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid message")
				}
				taskLog.Message = v
				data = data[n:]
			}
		case 7: // timestamp_ms (int64)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid timestamp_ms")
				}
				taskLog.TimestampMs = int64(v)
				data = data[n:]
			}
		case 8: // component (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid component")
				}
				taskLog.Component = v
				data = data[n:]
			}
		case 9: // metadata (map<string, string>)
			if wireType == protowire.BytesType {
				mapData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid metadata")
				}
				// Parse map entry: key=1, value=2
				var key, value string
				for len(mapData) > 0 {
					fn, wt, m := protowire.ConsumeTag(mapData)
					if m < 0 {
						break
					}
					mapData = mapData[m:]
					if fn == 1 && wt == protowire.BytesType {
						k, m := protowire.ConsumeString(mapData)
						if m >= 0 {
							key = k
							mapData = mapData[m:]
						}
					} else if fn == 2 && wt == protowire.BytesType {
						v, m := protowire.ConsumeString(mapData)
						if m >= 0 {
							value = v
							mapData = mapData[m:]
						}
					} else {
						m := protowire.ConsumeFieldValue(fn, wt, mapData)
						if m < 0 {
							break
						}
						mapData = mapData[m:]
					}
				}
				if key != "" {
					taskLog.Metadata[key] = value
				}
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field %d", fieldNum)
			}
			data = data[n:]
		}
	}

	return taskLog, nil
}

// parseTaskStateUpdate parses TaskStateUpdate protobuf (agent-driven execution)
//
//	message TaskStateUpdate {
//	  string task_id = 1;
//	  string current_step_id = 2;
//	  TaskState state = 3;
//	  float progress = 4;
//	  string blocking_reason = 5;
//	  map<string, string> variables = 6;
//	  StepResultInfo step_result = 7;
//	  int64 timestamp_ms = 8;
//	}
func parseTaskStateUpdate(data []byte) (*TaskStateUpdateMsg, error) {
	update := &TaskStateUpdateMsg{
		Variables: make(map[string]string),
	}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // task_id (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid task_id")
				}
				update.TaskID = v
				data = data[n:]
			}
		case 2: // current_step_id (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid current_step_id")
				}
				update.CurrentStepID = v
				data = data[n:]
			}
		case 3: // state (TaskState enum - varint)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid state")
				}
				update.State = int32(v)
				data = data[n:]
			}
		case 4: // progress (float)
			if wireType == protowire.Fixed32Type {
				v, n := protowire.ConsumeFixed32(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid progress")
				}
				update.Progress = float32FromBits(v)
				data = data[n:]
			}
		case 5: // blocking_reason (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid blocking_reason")
				}
				update.BlockingReason = v
				data = data[n:]
			}
		case 6: // variables (map<string, string>)
			if wireType == protowire.BytesType {
				mapData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid variables")
				}
				// Parse map entry: key=1, value=2
				var key, value string
				for len(mapData) > 0 {
					fn, wt, m := protowire.ConsumeTag(mapData)
					if m < 0 {
						break
					}
					mapData = mapData[m:]
					if fn == 1 && wt == protowire.BytesType {
						k, m := protowire.ConsumeString(mapData)
						if m >= 0 {
							key = k
							mapData = mapData[m:]
						}
					} else if fn == 2 && wt == protowire.BytesType {
						v, m := protowire.ConsumeString(mapData)
						if m >= 0 {
							value = v
							mapData = mapData[m:]
						}
					} else {
						m := protowire.ConsumeFieldValue(fn, wt, mapData)
						if m < 0 {
							break
						}
						mapData = mapData[m:]
					}
				}
				if key != "" {
					update.Variables[key] = value
				}
				data = data[n:]
			}
		case 7: // step_result (StepResultInfo message)
			if wireType == protowire.BytesType {
				resultData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid step_result")
				}
				result, err := parseStepResultInfo(resultData)
				if err == nil {
					update.StepResult = result
				}
				data = data[n:]
			}
		case 8: // timestamp_ms (int64)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid timestamp_ms")
				}
				update.TimestampMs = int64(v)
				data = data[n:]
			}
		case 9: // resource_events (ResourceEvent message)
			if wireType == protowire.BytesType {
				eventData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid resource_events")
				}
				event, err := parseResourceEvent(eventData)
				if err == nil && strings.TrimSpace(event.ResourceID) != "" {
					update.ResourceEvents = append(update.ResourceEvents, *event)
				}
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field %d", fieldNum)
			}
			data = data[n:]
		}
	}

	return update, nil
}

func parseResourceEvent(data []byte) (*ResourceEventMsg, error) {
	event := &ResourceEventMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // resource_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid resource_id")
				}
				event.ResourceID = v
				data = data[n:]
			}
		case 2: // step_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid step_id")
				}
				event.StepID = v
				data = data[n:]
			}
		case 3: // kind
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid kind")
				}
				event.Kind = int32(v)
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid resource event field %d", fieldNum)
			}
			data = data[n:]
		}
	}

	return event, nil
}

// parseStepResultInfo parses StepResultInfo protobuf
//
//	message StepResultInfo {
//	  string step_id = 1;
//	  ActionStatus status = 2;
//	  bytes result_json = 3;
//	  string error = 4;
//	  int64 duration_ms = 5;
//	}
func parseStepResultInfo(data []byte) (*StepResultInfoMsg, error) {
	info := &StepResultInfoMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // step_id (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid step_id")
				}
				info.StepID = v
				data = data[n:]
			}
		case 2: // status (ActionStatus enum - varint)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid status")
				}
				info.Status = int32(v)
				data = data[n:]
			}
		case 3: // result_json (bytes)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid result_json")
				}
				info.ResultJSON = v
				data = data[n:]
			}
		case 4: // error (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid error")
				}
				info.Error = v
				data = data[n:]
			}
		case 5: // duration_ms (int64)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid duration_ms")
				}
				info.DurationMs = int64(v)
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field %d", fieldNum)
			}
			data = data[n:]
		}
	}

	return info, nil
}

func lifecycleStateFromProto(value uint64) string {
	switch value {
	case 1:
		return "unconfigured"
	case 2:
		return "inactive"
	case 3:
		return "active"
	case 4:
		return "finalized"
	default:
		return "unknown"
	}
}

// parseActionCapability parses ActionCapability protobuf
func parseActionCapability(data []byte) (*ActionCapabilityMsg, error) {
	cap := &ActionCapabilityMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // action_type
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid action_type")
				}
				cap.ActionType = v
				data = data[n:]
			}
		case 2: // action_server
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid action_server")
				}
				cap.ActionServer = v
				data = data[n:]
			}
		case 3: // package
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid package")
				}
				cap.Package = v
				data = data[n:]
			}
		case 4: // action_name
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid action_name")
				}
				cap.ActionName = v
				data = data[n:]
			}
		case 10: // capability_kind (string, new) OR lifecycle_state (enum, legacy)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid capability_kind")
				}
				cap.CapabilityKind = v
				data = data[n:]
			} else if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid lifecycle_state")
				}
				cap.LifecycleState = lifecycleStateFromProto(v)
				data = data[n:]
			}
		case 11: // node_name
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid node_name")
				}
				cap.NodeName = v
				data = data[n:]
			}
		case 12: // is_lifecycle_node
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid is_lifecycle_node")
				}
				cap.IsLifecycleNode = v != 0
				data = data[n:]
			}
		case 13: // lifecycle_state (enum, new)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid lifecycle_state")
				}
				cap.LifecycleState = lifecycleStateFromProto(v)
				data = data[n:]
			}
		case 5: // goal_schema
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid goal_schema")
				}
				cap.GoalSchema = v
				data = data[n:]
			}
		case 6: // result_schema
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid result_schema")
				}
				cap.ResultSchema = v
				data = data[n:]
			}
		case 7: // feedback_schema
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid feedback_schema")
				}
				cap.FeedbackSchema = v
				data = data[n:]
			}
		case 8: // success_criteria (nested message)
			if wireType == protowire.BytesType {
				criteriaData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid success_criteria")
				}
				criteria, err := parseSuccessCriteria(criteriaData)
				if err == nil && criteria != nil {
					cap.SuccessCriteria = criteria
				}
				data = data[n:]
			}
		case 9: // is_available (bool)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid is_available")
				}
				cap.IsAvailable = v != 0
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	if cap.CapabilityKind == "" {
		if strings.Contains(strings.ToLower(cap.ActionType), "/srv/") {
			cap.CapabilityKind = "service"
		} else {
			cap.CapabilityKind = "action"
		}
	}
	if cap.LifecycleState == "" {
		cap.LifecycleState = "unknown"
	}
	if cap.IsLifecycleNode == false && cap.LifecycleState != "unknown" {
		cap.IsLifecycleNode = true
	}

	return cap, nil
}

// parseSuccessCriteria parses SuccessCriteria protobuf message
func parseSuccessCriteria(data []byte) (*SuccessCriteriaMsg, error) {
	criteria := &SuccessCriteriaMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // field
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid field")
				}
				criteria.Field = v
				data = data[n:]
			}
		case 2: // operator
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid operator")
				}
				criteria.Operator = v
				data = data[n:]
			}
		case 3: // value
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid value")
				}
				criteria.Value = v
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	// Return nil if criteria is empty
	if criteria.Field == "" && criteria.Operator == "" && criteria.Value == "" {
		return nil, nil
	}

	return criteria, nil
}

func (h *RawQUICHandler) handleStream(agentConn *agentConnection, stream quic.Stream) {
	defer stream.Close()
	log.Printf("[RawQUIC] handleStream started for stream %d", stream.StreamID())

	msgCount := 0
	for {
		// Read message with length prefix (4 bytes, big-endian)
		lenBuf := make([]byte, 4)
		_, err := io.ReadFull(stream, lenBuf)
		if err != nil {
			if err != io.EOF {
				log.Printf("[RawQUIC] Stream read error: %v", err)
			}
			log.Printf("[RawQUIC] Stream %d closed after %d messages", stream.StreamID(), msgCount)
			return
		}

		msgLen := binary.BigEndian.Uint32(lenBuf)
		msgCount++
		log.Printf("[RawQUIC] Stream %d: Message #%d, length=%d bytes", stream.StreamID(), msgCount, msgLen)

		if msgLen > 16*1024*1024 { // 16MB max
			log.Printf("[RawQUIC] Message too large: %d bytes", msgLen)
			return
		}

		msgBuf := make([]byte, msgLen)
		_, err = io.ReadFull(stream, msgBuf)
		if err != nil {
			log.Printf("[RawQUIC] Failed to read message body: %v", err)
			return
		}

		log.Printf("[RawQUIC] Received %d bytes, first 32 hex: %x", len(msgBuf), msgBuf[:min(len(msgBuf), 32)])

		// Try to parse as RegisterAgentRequest first
		regReq, err := parseRegisterAgentRequest(msgBuf)
		// Accept registration if: has AgentID OR has HardwareFingerprint (for server-assigned ID)
		if err == nil && (regReq.AgentID != "" || regReq.HardwareFingerprint != "") {
			log.Printf("[RawQUIC] Parsed as RegisterAgentRequest: agentID=%q, fingerprint=%q",
				regReq.AgentID, regReq.HardwareFingerprint)
			h.handleRegisterAgent(agentConn, stream, regReq)
			// Continue reading - C++ agent may reuse this stream for capability registration
			continue
		}
		// Not a RegisterAgentRequest, try AgentMessage
		agentID := ""
		if regReq != nil {
			agentID = regReq.AgentID
		}
		log.Printf("[RawQUIC] Not RegisterAgentRequest (err=%v, agentID=%q)", err, agentID)

		// Try to parse as AgentMessage (for capability registration, etc.)
		agentMsg, err := parseAgentMessage(msgBuf)
		hasCapReg := agentMsg != nil && agentMsg.CapabilityRegistration != nil
		msgAgentID := ""
		if agentMsg != nil {
			msgAgentID = agentMsg.AgentID
		}
		log.Printf("[RawQUIC] parseAgentMessage result: err=%v, agentID=%q, hasCapReg=%v",
			err, msgAgentID, hasCapReg)

		if err == nil && agentMsg != nil && agentMsg.AgentID != "" {
			// Route message to appropriate handler based on payload type
			handled := false

			if agentMsg.Heartbeat != nil {
				log.Printf("[RawQUIC] Processing heartbeat from %s (state=%s, executing=%v)",
					agentMsg.AgentID, agentMsg.Heartbeat.State, agentMsg.Heartbeat.IsExecuting)
				h.handleHeartbeat(agentConn, agentMsg.Heartbeat)
				handled = true
			} else {
				log.Printf("[RawQUIC] AgentMessage has no heartbeat payload (agent=%s)", agentMsg.AgentID)
			}

			if agentMsg.ActionResult != nil {
				h.handleActionResult(agentConn, agentMsg.ActionResult)
				handled = true
			}

			if agentMsg.ActionFeedback != nil {
				h.handleActionFeedback(agentConn, agentMsg.ActionFeedback)
				handled = true
			}

			if agentMsg.StatusUpdate != nil {
				h.handleStatusUpdate(agentConn, agentMsg.StatusUpdate)
				handled = true
			}

			if agentMsg.DeployResponse != nil {
				log.Printf("[RawQUIC] Received DeployGraphResponse: success=%v, graph=%s",
					agentMsg.DeployResponse.Success, agentMsg.DeployResponse.GraphID)
				h.handleDeployResponse(agentMsg.DeployResponse)
				handled = true
			}

			if agentMsg.GraphStatus != nil {
				log.Printf("[RawQUIC] Received GraphExecutionStatus: exec=%s, state=%d",
					agentMsg.GraphStatus.ExecutionID, agentMsg.GraphStatus.State)
				h.handleGraphStatus(agentConn, agentMsg.GraphStatus)
				handled = true
			}

			if agentMsg.Pong != nil {
				h.handlePong(agentConn, agentMsg.Pong)
				handled = true
			}

			if agentMsg.ConfigAck != nil {
				log.Printf("[RawQUIC] Received ConfigUpdateAck: robot=%s, success=%v",
					agentMsg.ConfigAck.AgentID, agentMsg.ConfigAck.Success)
				h.handleConfigUpdateAck(agentMsg.ConfigAck)
				handled = true
			}

			if agentMsg.CapabilityRegistration != nil {
				// Use agentConn.agentID (server-assigned) instead of agentMsg.AgentID (client's config)
				effectiveID := agentConn.agentID
				if effectiveID == "" {
					effectiveID = agentMsg.AgentID // Fallback if connection not registered yet
				}
				log.Printf("[RawQUIC] Found CapabilityRegistration, handling for %s (msg had %s)...",
					effectiveID, agentMsg.AgentID)
				h.handleCapabilityRegistration(agentConn, effectiveID, agentMsg.CapabilityRegistration)
				handled = true
			}

			if agentMsg.TaskLog != nil {
				h.handleTaskLog(agentConn, agentMsg.TaskLog)
				handled = true
			}

			if agentMsg.TaskStateUpdate != nil {
				log.Printf("[RawQUIC] Received TaskStateUpdate: task=%s, step=%s, state=%d",
					agentMsg.TaskStateUpdate.TaskID, agentMsg.TaskStateUpdate.CurrentStepID,
					agentMsg.TaskStateUpdate.State)
				h.handleTaskStateUpdate(agentConn, agentMsg.TaskStateUpdate)
				handled = true
			}

			if handled {
				continue
			}

			log.Printf("[RawQUIC] AgentMessage with no recognized payload")
		}

		log.Printf("[RawQUIC] Unknown message format or parse error: %v", err)
	}
}

func (h *RawQUICHandler) handleRegisterAgent(
	agentConn *agentConnection,
	stream quic.Stream,
	req *RegisterAgentReq,
) {
	// Server is the sole authority for Agent ID assignment
	// Agent's provided ID is only used as a hint for logging
	var effectiveAgentID string
	idWasAssigned := true // Server always assigns

	// Step 1: Try to recover existing ID from hardware fingerprint
	if req.HardwareFingerprint != "" {
		log.Printf("[RawQUIC] Looking up agent by hardware fingerprint: %s", req.HardwareFingerprint)
		existingAgent, err := h.repo.GetAgentByHardwareFingerprint(req.HardwareFingerprint)
		if err != nil {
			log.Printf("[RawQUIC] Warning: fingerprint lookup failed: %v (will generate new ID)", err)
		} else if existingAgent != nil {
			// Found existing agent with same fingerprint - reuse the ID
			effectiveAgentID = existingAgent.ID
			log.Printf("[RawQUIC] Recovered agent ID %s from hardware fingerprint (agent suggested: %s)",
				effectiveAgentID, req.AgentID)
		} else {
			log.Printf("[RawQUIC] No existing agent found with fingerprint %s", req.HardwareFingerprint)
		}
	} else {
		log.Printf("[RawQUIC] Warning: no hardware fingerprint provided - cannot recover existing ID")
	}

	// Step 2: If no existing ID found, generate new UUID
	if effectiveAgentID == "" {
		effectiveAgentID = uuid.New().String()
		log.Printf("[RawQUIC] Generated new agent ID: %s (agent suggested: %s)",
			effectiveAgentID, req.AgentID)
	}

	// Step 3: Determine effective agent name
	// Server generates name if agent provides empty or generic placeholder name.
	effectiveName := req.Name
	if isAutoAssignedAgentName(effectiveName) {
		// Check if existing agent has a name
		existingAgent, _ := h.repo.GetAgent(effectiveAgentID)
		if existingAgent != nil {
			if seq, ok := extractAutoAssignedSequence(existingAgent.Name); ok {
				effectiveName = formatTaskManagerName(seq)
				log.Printf("[RawQUIC] Normalized existing auto-assigned name to: %s", effectiveName)
			} else if !isAutoAssignedAgentName(existingAgent.Name) {
				// Reuse existing custom name
				effectiveName = existingAgent.Name
				log.Printf("[RawQUIC] Reusing existing agent name: %s", effectiveName)
			}
		}

		if isGenericPlaceholderName(effectiveName) {
			// Generate new sequential name
			nextNum, err := h.repo.GetNextAgentNumber()
			if err != nil {
				log.Printf("[RawQUIC] Warning: failed to get next agent number: %v, using fallback", err)
				nextNum = 1
			}
			effectiveName = formatTaskManagerName(nextNum)
			log.Printf("[RawQUIC] Generated new agent name: %s", effectiveName)
		}
	}

	log.Printf("[RawQUIC] Agent registration: %s (%s) assigned=%v", effectiveAgentID, effectiveName, idWasAssigned)

	// Store agent ID in connection
	agentConn.agentID = effectiveAgentID
	agentConn.registered = true
	agentConn.lastSeen = time.Now()

	// Track connection
	h.connMu.Lock()
	if existing, ok := h.connections[effectiveAgentID]; ok && existing != agentConn {
		existing.quicConn.CloseWithError(0, "replaced by new connection")
	}
	h.connections[effectiveAgentID] = agentConn
	h.connMu.Unlock()

	// Register robot in state manager (agent_id = robot_id in 1:1 model)
	h.stateManager.RegisterRobot(
		effectiveAgentID,
		effectiveName,
		effectiveAgentID,
		"idle",
	)

	// In 1:1 model, agent_id = robot_id, so no separate robotIDs needed
	h.stateManager.RegisterAgent(effectiveAgentID, effectiveName, req.Namespace)

	// Update database - Agent
	remoteAddr := agentConn.quicConn.RemoteAddr().String()
	agent, _ := h.repo.GetAgent(effectiveAgentID)
	if agent == nil {
		// Create new agent
		agent = &db.Agent{
			ID:               effectiveAgentID,
			Name:             effectiveName,
			Namespace:        req.Namespace,
			Status:           "online",
			IPAddress:        sql.NullString{String: remoteAddr, Valid: true},
			AssignedByServer: idWasAssigned,
			CreatedAt:        time.Now(),
			LastSeen:         sql.NullTime{Time: time.Now(), Valid: true},
		}
		// Always store hardware fingerprint for future ID recovery
		agent.HardwareFingerprint = sql.NullString{
			String: req.HardwareFingerprint,
			Valid:  req.HardwareFingerprint != "",
		}
		h.repo.CreateAgent(agent)
		log.Printf("[RawQUIC] Created new agent %s with IP %s (fingerprint: %s)",
			effectiveAgentID, remoteAddr, req.HardwareFingerprint)
	} else {
		// Update existing agent - always update hardware fingerprint to ensure recovery works
		agent.Status = "online"
		agent.IPAddress = sql.NullString{String: remoteAddr, Valid: true}
		agent.LastSeen = sql.NullTime{Time: time.Now(), Valid: true}
		if req.Namespace != "" {
			agent.Namespace = req.Namespace
		}
		if req.HardwareFingerprint != "" {
			agent.HardwareFingerprint = sql.NullString{String: req.HardwareFingerprint, Valid: true}
		}
		// Update name if existing name is a generic placeholder.
		if isAutoAssignedAgentName(agent.Name) {
			agent.Name = effectiveName
		}
		h.repo.CreateOrUpdateAgent(agent)
		log.Printf("[RawQUIC] Updated existing agent %s (%s) with IP %s", effectiveAgentID, agent.Name, remoteAddr)
	}

	// Broadcast to frontend via WebSocket
	if h.wsHub != nil {
		h.wsHub.BroadcastAgentUpdate(effectiveAgentID, "online")
	}

	// Send response (length-prefixed protobuf RegisterAgentResponse)
	h.sendRegisterResponse(stream, true, "", effectiveAgentID, idWasAssigned)

	log.Printf("[RawQUIC] Agent %s registered (1:1 model, assigned=%v)", effectiveAgentID, idWasAssigned)
}

func isAutoAssignedAgentName(name string) bool {
	if isGenericPlaceholderName(name) {
		return true
	}
	_, ok := extractAutoAssignedSequence(name)
	return ok
}

func isGenericPlaceholderName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "", "fleet agent", "robot agent", "agent", "task manager":
		return true
	default:
		return false
	}
}

func extractAutoAssignedSequence(name string) (int, bool) {
	trimmed := strings.TrimSpace(name)
	for _, prefix := range []string{"Task Manager-", "Agent-"} {
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		seqText := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if seqText == "" {
			return 0, false
		}
		seq, err := strconv.Atoi(seqText)
		if err != nil {
			return 0, false
		}
		return seq, true
	}
	return 0, false
}

func formatTaskManagerName(sequence int) string {
	if sequence < 1 {
		sequence = 1
	}
	return fmt.Sprintf("Task Manager-%03d", sequence)
}

// sendRegisterResponse sends a RegisterAgentResponse protobuf
func (h *RawQUICHandler) sendRegisterResponse(stream quic.Stream, success bool, errorMsg string, assignedAgentID string, idWasAssigned bool) error {
	// Build RegisterAgentResponse protobuf manually
	// message RegisterAgentResponse {
	//   bool success = 1;
	//   string error = 2;
	//   AgentConfig config = 3;
	//   int64 server_time_ms = 4;
	//   string assigned_agent_id = 5;
	//   bool id_was_assigned = 6;
	// }

	var data []byte

	// Field 1: success (bool)
	data = protowire.AppendTag(data, 1, protowire.VarintType)
	if success {
		data = protowire.AppendVarint(data, 1)
	} else {
		data = protowire.AppendVarint(data, 0)
	}

	// Field 2: error (string) - only if not empty
	if errorMsg != "" {
		data = protowire.AppendTag(data, 2, protowire.BytesType)
		data = protowire.AppendString(data, errorMsg)
	}

	// Field 4: server_time_ms (int64)
	data = protowire.AppendTag(data, 4, protowire.VarintType)
	data = protowire.AppendVarint(data, uint64(time.Now().UnixMilli()))

	// Field 5: assigned_agent_id (string) - always send if not empty
	if assignedAgentID != "" {
		data = protowire.AppendTag(data, 5, protowire.BytesType)
		data = protowire.AppendString(data, assignedAgentID)
	}

	// Field 6: id_was_assigned (bool)
	data = protowire.AppendTag(data, 6, protowire.VarintType)
	if idWasAssigned {
		data = protowire.AppendVarint(data, 1)
	} else {
		data = protowire.AppendVarint(data, 0)
	}

	// Write length prefix
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	if _, err := stream.Write(lenBuf); err != nil {
		return fmt.Errorf("failed to write length: %w", err)
	}

	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

func (h *RawQUICHandler) handleDisconnect(agentConn *agentConnection) {
	if agentConn.agentID == "" {
		return
	}

	log.Printf("[RawQUIC] Agent disconnected: %s", agentConn.agentID)

	// Remove from connections
	h.connMu.Lock()
	if current, ok := h.connections[agentConn.agentID]; !ok || current != agentConn {
		h.connMu.Unlock()
		return
	}
	delete(h.connections, agentConn.agentID)
	h.connMu.Unlock()

	// Update state manager (mark agent as offline by unregistering)
	h.stateManager.UnregisterAgent(agentConn.agentID)

	// Update database
	h.repo.UpdateAgentStatus(agentConn.agentID, "offline", "")

	// Broadcast to frontend
	if h.wsHub != nil {
		h.wsHub.BroadcastAgentUpdate(agentConn.agentID, "offline")
	}
}

// GetConnectedAgents returns list of connected agent IDs
func (h *RawQUICHandler) GetConnectedAgents() []string {
	h.connMu.RLock()
	defer h.connMu.RUnlock()

	agents := make([]string, 0, len(h.connections))
	for agentID := range h.connections {
		agents = append(agents, agentID)
	}
	return agents
}

// IsAgentConnected checks if an agent is connected
func (h *RawQUICHandler) IsAgentConnected(agentID string) bool {
	h.connMu.RLock()
	defer h.connMu.RUnlock()

	conn, exists := h.connections[agentID]
	return exists && conn.registered
}

// GetAgentRemoteAddr returns the remote address of an agent
func (h *RawQUICHandler) GetAgentRemoteAddr(agentID string) (net.Addr, bool) {
	h.connMu.RLock()
	defer h.connMu.RUnlock()

	conn, exists := h.connections[agentID]
	if !exists {
		return nil, false
	}
	return conn.quicConn.RemoteAddr(), true
}

// SetWebSocketHub sets the WebSocket broadcaster after initialization
// This is needed to break circular dependency between api.Server and RawQUICHandler
func (h *RawQUICHandler) SetWebSocketHub(hub WebSocketBroadcaster) {
	h.wsHub = hub
}

// DeployResult represents the result of a graph deployment
type DeployResult struct {
	Success         bool
	Error           string
	Checksum        string
	CorrelationID   string
	GraphID         string
	DeployedVersion int32
}

// ConfigUpdateResult represents the result of a state definition update
type ConfigUpdateResult struct {
	Success       bool
	Error         string
	AgentID       string
	StateDefID    string
	Version       int32
	CorrelationID string
}

// DeployCanonicalGraph deploys a behavior tree to an agent via QUIC
func (h *RawQUICHandler) DeployCanonicalGraph(ctx context.Context, agentID string, graphJSON []byte) (*DeployResult, error) {
	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		return nil, fmt.Errorf("agent %s not connected", agentID)
	}

	// Build ServerMessage with DeployGraphRequest
	// ServerMessage: message_id=1, timestamp_ms=3, deploy_graph=12
	correlationID := fmt.Sprintf("deploy-%s-%d", agentID, time.Now().UnixNano())
	serverMsg := h.buildDeployGraphMessage(correlationID, graphJSON)

	respCh := make(chan *DeployResult, 1)
	h.deployMu.Lock()
	h.deployWaiters[correlationID] = respCh
	h.deployMu.Unlock()
	defer func() {
		h.deployMu.Lock()
		delete(h.deployWaiters, correlationID)
		h.deployMu.Unlock()
	}()

	if err := h.sendToAgent(conn, serverMsg); err != nil {
		return nil, err
	}

	timeout := 30 * time.Second
	select {
	case result := <-respCh:
		if result == nil {
			return nil, fmt.Errorf("deploy response missing")
		}
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("deploy response timeout after %s", timeout)
	}
}

// SendConfigUpdate sends a state definition update to an agent via QUIC.
// In 1:1 model, targetAgentID is the same as agentID (kept for API compatibility).
func (h *RawQUICHandler) SendConfigUpdate(ctx context.Context, agentID, targetAgentID, stateDefID string, version int32, stateDefJSON []byte) (*ConfigUpdateResult, error) {
	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		return nil, fmt.Errorf("agent %s not connected", agentID)
	}

	correlationID := fmt.Sprintf("config-%s-%d", agentID, time.Now().UnixNano())
	msgData := h.buildConfigUpdateMessage(correlationID, targetAgentID, stateDefID, version, stateDefJSON)

	respCh := make(chan *ConfigUpdateResult, 1)
	h.configMu.Lock()
	h.configWaiters[correlationID] = respCh
	h.configMu.Unlock()
	defer func() {
		h.configMu.Lock()
		delete(h.configWaiters, correlationID)
		h.configMu.Unlock()
	}()

	if err := h.sendToAgent(conn, msgData); err != nil {
		return nil, err
	}

	timeout := 20 * time.Second
	select {
	case result := <-respCh:
		if result == nil {
			return nil, fmt.Errorf("config update response missing")
		}
		if !result.Success {
			return result, fmt.Errorf("config update failed: %s", result.Error)
		}
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("config update response timeout after %s", timeout)
	}
}

// SendExecuteGraph sends an ExecuteGraphRequest to an agent via QUIC.
// In 1:1 model, targetAgentID is the same as agentID (kept for API compatibility).
func (h *RawQUICHandler) SendExecuteGraph(ctx context.Context, agentID, executionID, graphID, targetAgentID string, params map[string]interface{}) error {
	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		return fmt.Errorf("agent %s not connected", agentID)
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal execute params: %w", err)
	}

	correlationID := fmt.Sprintf("exec-%s-%d", agentID, time.Now().UnixNano())
	msgData := h.buildExecuteGraphMessage(correlationID, executionID, graphID, targetAgentID, paramsJSON)
	return h.sendToAgent(conn, msgData)
}

// buildDeployGraphMessage builds a ServerMessage with DeployGraphRequest
// Encodes complete protobuf structure (vertices, edges, entry_point) for agent consumption
func (h *RawQUICHandler) buildDeployGraphMessage(correlationID string, graphJSON []byte) []byte {
	// Parse canonical JSON
	var canonical struct {
		BehaviorTree struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Version int    `json:"version"`
		} `json:"behavior_tree"`
		Vertices []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Step *struct {
				StepType string `json:"step_type"`
				Action   *struct {
					Type     string                 `json:"type"`
					Server   string                 `json:"server"`
					TimeoutS float64                `json:"timeout_sec"`
					Params   map[string]interface{} `json:"params"`
				} `json:"action"`
				States *struct {
					During  []string `json:"during"`
					Success []string `json:"success"`
					Failure []string `json:"failure"`
				} `json:"states"`
			} `json:"step"`
			Terminal *struct {
				TerminalType string `json:"terminal_type"`
			} `json:"terminal"`
		} `json:"vertices"`
		Edges []struct {
			From      string `json:"from"`
			To        string `json:"to"`
			Type      string `json:"type"`
			Condition string `json:"condition"`
		} `json:"edges"`
		EntryPoint string `json:"entry_point"`
		Checksum   string `json:"checksum"`
	}

	if err := json.Unmarshal(graphJSON, &canonical); err != nil {
		log.Printf("[RawQUIC] Failed to parse graph JSON: %v", err)
		return nil
	}

	// Build BehaviorTree protobuf
	// message BehaviorTree {
	//   string schema_version = 1;
	//   BehaviorTreeMetadata metadata = 2;
	//   repeated Vertex vertices = 3;
	//   repeated Edge edges = 4;
	//   string entry_point = 5;
	//   string checksum = 6;
	// }
	var graphMsg []byte

	// Field 1: schema_version
	graphMsg = protowire.AppendTag(graphMsg, 1, protowire.BytesType)
	graphMsg = protowire.AppendString(graphMsg, "1.0.0")

	// Field 2: metadata
	var metadata []byte
	if canonical.BehaviorTree.ID != "" {
		metadata = protowire.AppendTag(metadata, 1, protowire.BytesType)
		metadata = protowire.AppendString(metadata, canonical.BehaviorTree.ID)
	}
	if canonical.BehaviorTree.Name != "" {
		metadata = protowire.AppendTag(metadata, 2, protowire.BytesType)
		metadata = protowire.AppendString(metadata, canonical.BehaviorTree.Name)
	}
	if canonical.BehaviorTree.Version > 0 {
		metadata = protowire.AppendTag(metadata, 3, protowire.VarintType)
		metadata = protowire.AppendVarint(metadata, uint64(canonical.BehaviorTree.Version))
	}
	if len(metadata) > 0 {
		graphMsg = protowire.AppendTag(graphMsg, 2, protowire.BytesType)
		graphMsg = protowire.AppendBytes(graphMsg, metadata)
	}

	// Field 3: vertices (repeated)
	for _, v := range canonical.Vertices {
		vertexMsg := h.buildVertexMessage(v.ID, v.Type, v.Step, v.Terminal)
		graphMsg = protowire.AppendTag(graphMsg, 3, protowire.BytesType)
		graphMsg = protowire.AppendBytes(graphMsg, vertexMsg)
	}

	// Field 4: edges (repeated)
	for _, e := range canonical.Edges {
		edgeMsg := h.buildEdgeMessage(e.From, e.To, e.Type, e.Condition)
		graphMsg = protowire.AppendTag(graphMsg, 4, protowire.BytesType)
		graphMsg = protowire.AppendBytes(graphMsg, edgeMsg)
	}

	// Field 5: entry_point
	if canonical.EntryPoint != "" {
		graphMsg = protowire.AppendTag(graphMsg, 5, protowire.BytesType)
		graphMsg = protowire.AppendString(graphMsg, canonical.EntryPoint)
	}

	// Field 6: checksum
	if canonical.Checksum != "" {
		graphMsg = protowire.AppendTag(graphMsg, 6, protowire.BytesType)
		graphMsg = protowire.AppendString(graphMsg, canonical.Checksum)
	}

	// Build DeployGraphRequest
	var deployReq []byte
	deployReq = protowire.AppendTag(deployReq, 1, protowire.BytesType)
	deployReq = protowire.AppendString(deployReq, correlationID)
	deployReq = protowire.AppendTag(deployReq, 2, protowire.BytesType)
	deployReq = protowire.AppendBytes(deployReq, graphMsg)
	deployReq = protowire.AppendTag(deployReq, 3, protowire.VarintType)
	deployReq = protowire.AppendVarint(deployReq, 1) // force = true

	log.Printf("[RawQUIC] Built deploy message: %d vertices, %d edges, entry=%s",
		len(canonical.Vertices), len(canonical.Edges), canonical.EntryPoint)

	return h.buildServerMessage(correlationID, 12, deployReq)
}

// buildVertexMessage builds a Vertex protobuf message
func (h *RawQUICHandler) buildVertexMessage(id, vType string, step interface{}, terminal interface{}) []byte {
	var msg []byte

	// Field 1: id
	msg = protowire.AppendTag(msg, 1, protowire.BytesType)
	msg = protowire.AppendString(msg, id)

	// Field 2: type (enum VertexType)
	vertexType := uint64(1) // VERTEX_TYPE_STEP
	if vType == "terminal" {
		vertexType = 2 // VERTEX_TYPE_TERMINAL
	}
	msg = protowire.AppendTag(msg, 2, protowire.VarintType)
	msg = protowire.AppendVarint(msg, vertexType)

	// Field 3: step (StepVertex) or Field 4: terminal (TerminalVertex)
	if vType == "terminal" && terminal != nil {
		termData, ok := terminal.(*struct {
			TerminalType string `json:"terminal_type"`
		})
		if ok && termData != nil {
			var termMsg []byte
			termType := uint64(1) // TERMINAL_TYPE_SUCCESS
			if termData.TerminalType == "failure" {
				termType = 2
			}
			termMsg = protowire.AppendTag(termMsg, 1, protowire.VarintType)
			termMsg = protowire.AppendVarint(termMsg, termType)
			msg = protowire.AppendTag(msg, 4, protowire.BytesType)
			msg = protowire.AppendBytes(msg, termMsg)
		}
	} else if step != nil {
		stepData, ok := step.(*struct {
			StepType string `json:"step_type"`
			Action   *struct {
				Type     string                 `json:"type"`
				Server   string                 `json:"server"`
				TimeoutS float64                `json:"timeout_sec"`
				Params   map[string]interface{} `json:"params"`
			} `json:"action"`
			States *struct {
				During  []string `json:"during"`
				Success []string `json:"success"`
				Failure []string `json:"failure"`
			} `json:"states"`
		})
		if ok && stepData != nil {
			stepMsg := h.buildStepVertexMessage(stepData)
			msg = protowire.AppendTag(msg, 3, protowire.BytesType)
			msg = protowire.AppendBytes(msg, stepMsg)
		}
	}

	return msg
}

// buildStepVertexMessage builds a StepVertex protobuf message
func (h *RawQUICHandler) buildStepVertexMessage(step *struct {
	StepType string `json:"step_type"`
	Action   *struct {
		Type     string                 `json:"type"`
		Server   string                 `json:"server"`
		TimeoutS float64                `json:"timeout_sec"`
		Params   map[string]interface{} `json:"params"`
	} `json:"action"`
	States *struct {
		During  []string `json:"during"`
		Success []string `json:"success"`
		Failure []string `json:"failure"`
	} `json:"states"`
}) []byte {
	var msg []byte

	// Field 1: step_type (enum StepType)
	stepType := uint64(1) // STEP_TYPE_ACTION
	if step.StepType == "wait" {
		stepType = 2
	} else if step.StepType == "condition" {
		stepType = 3
	}
	msg = protowire.AppendTag(msg, 1, protowire.VarintType)
	msg = protowire.AppendVarint(msg, stepType)

	// Field 2: action (ActionStep)
	if step.Action != nil {
		var actionMsg []byte
		if step.Action.Type != "" {
			actionMsg = protowire.AppendTag(actionMsg, 1, protowire.BytesType)
			actionMsg = protowire.AppendString(actionMsg, step.Action.Type)
		}
		if step.Action.Server != "" {
			actionMsg = protowire.AppendTag(actionMsg, 2, protowire.BytesType)
			actionMsg = protowire.AppendString(actionMsg, step.Action.Server)
		}
		if step.Action.TimeoutS > 0 {
			actionMsg = protowire.AppendTag(actionMsg, 3, protowire.Fixed32Type)
			actionMsg = protowire.AppendFixed32(actionMsg, math.Float32bits(float32(step.Action.TimeoutS)))
		}
		if len(step.Action.Params) > 0 {
			paramsJSON, _ := json.Marshal(step.Action.Params)
			actionMsg = protowire.AppendTag(actionMsg, 4, protowire.BytesType)
			actionMsg = protowire.AppendBytes(actionMsg, paramsJSON)
		}
		if len(actionMsg) > 0 {
			msg = protowire.AppendTag(msg, 2, protowire.BytesType)
			msg = protowire.AppendBytes(msg, actionMsg)
		}
	}

	// Fields 15-17: states (during_states, success_states, failure_states)
	if step.States != nil {
		for _, s := range step.States.During {
			msg = protowire.AppendTag(msg, 15, protowire.BytesType)
			msg = protowire.AppendString(msg, s)
		}
		for _, s := range step.States.Success {
			msg = protowire.AppendTag(msg, 16, protowire.BytesType)
			msg = protowire.AppendString(msg, s)
		}
		for _, s := range step.States.Failure {
			msg = protowire.AppendTag(msg, 17, protowire.BytesType)
			msg = protowire.AppendString(msg, s)
		}
	}

	return msg
}

// buildEdgeMessage builds an Edge protobuf message
func (h *RawQUICHandler) buildEdgeMessage(from, to, edgeType, condition string) []byte {
	var msg []byte

	// Field 1: from_vertex
	msg = protowire.AppendTag(msg, 1, protowire.BytesType)
	msg = protowire.AppendString(msg, from)

	// Field 2: to_vertex
	msg = protowire.AppendTag(msg, 2, protowire.BytesType)
	msg = protowire.AppendString(msg, to)

	// Field 3: type (enum EdgeType)
	eType := uint64(4) // EDGE_TYPE_CONDITIONAL (default)
	switch edgeType {
	case "on_success":
		eType = 1
	case "on_failure":
		eType = 2
	case "on_timeout":
		eType = 3
	case "conditional":
		eType = 4
	}
	msg = protowire.AppendTag(msg, 3, protowire.VarintType)
	msg = protowire.AppendVarint(msg, eType)

	// Field 4: condition (optional)
	if condition != "" {
		msg = protowire.AppendTag(msg, 4, protowire.BytesType)
		msg = protowire.AppendString(msg, condition)
	}

	return msg
}

// buildConfigUpdateMessage builds a ServerMessage with ConfigUpdate.
func (h *RawQUICHandler) buildConfigUpdateMessage(correlationID, agentID, stateDefID string, version int32, stateDefJSON []byte) []byte {
	var update []byte

	// Field 1: agent_id
	update = protowire.AppendTag(update, 1, protowire.BytesType)
	update = protowire.AppendString(update, agentID)

	// Field 2: state_def_id
	if stateDefID != "" {
		update = protowire.AppendTag(update, 2, protowire.BytesType)
		update = protowire.AppendString(update, stateDefID)
	}

	// Field 3: state_definition
	if len(stateDefJSON) > 0 {
		update = protowire.AppendTag(update, 3, protowire.BytesType)
		update = protowire.AppendBytes(update, stateDefJSON)
	}

	// Field 4: version
	if version > 0 {
		update = protowire.AppendTag(update, 4, protowire.VarintType)
		update = protowire.AppendVarint(update, uint64(version))
	}

	// Field 5: correlation_id
	update = protowire.AppendTag(update, 5, protowire.BytesType)
	update = protowire.AppendString(update, correlationID)

	return h.buildServerMessage(correlationID, 15, update) // field 15 = config_update
}

// buildExecuteGraphMessage builds a ServerMessage with ExecuteGraphRequest.
func (h *RawQUICHandler) buildExecuteGraphMessage(correlationID, executionID, graphID, agentID string, params []byte) []byte {
	var execReq []byte

	// Field 1: correlation_id
	execReq = protowire.AppendTag(execReq, 1, protowire.BytesType)
	execReq = protowire.AppendString(execReq, correlationID)

	// Field 2: execution_id
	execReq = protowire.AppendTag(execReq, 2, protowire.BytesType)
	execReq = protowire.AppendString(execReq, executionID)

	// Field 3: graph_id
	execReq = protowire.AppendTag(execReq, 3, protowire.BytesType)
	execReq = protowire.AppendString(execReq, graphID)

	// Field 4: agent_id
	execReq = protowire.AppendTag(execReq, 4, protowire.BytesType)
	execReq = protowire.AppendString(execReq, agentID)

	// Field 5: params
	if len(params) > 0 {
		execReq = protowire.AppendTag(execReq, 5, protowire.BytesType)
		execReq = protowire.AppendBytes(execReq, params)
	}

	// Build ServerMessage wrapper
	return h.buildServerMessage(executionID, 13, execReq) // field 13 = execute_graph
}

// parseDeployResponse parses AgentMessage with DeployGraphResponse
func (h *RawQUICHandler) parseDeployResponse(data []byte) (*DeployResult, error) {
	// Parse AgentMessage to find DeployGraphResponse (field 14)
	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		if fieldNum == 14 && wireType == protowire.BytesType {
			// DeployGraphResponse
			respData, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return nil, fmt.Errorf("invalid deploy_response")
			}
			return h.parseDeployGraphResponse(respData)
		}

		// Skip other fields
		n = protowire.ConsumeFieldValue(fieldNum, wireType, data)
		if n < 0 {
			return nil, fmt.Errorf("invalid field")
		}
		data = data[n:]
	}

	return nil, fmt.Errorf("no deploy_response in message")
}

// parseDeployGraphResponse parses DeployGraphResponse protobuf
func (h *RawQUICHandler) parseDeployGraphResponse(data []byte) (*DeployResult, error) {
	result := &DeployResult{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // correlation_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid correlation_id")
				}
				result.CorrelationID = v
				data = data[n:]
			}
		case 2: // success (bool)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid success")
				}
				result.Success = v != 0
				data = data[n:]
			}
		case 3: // error (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid error")
				}
				result.Error = v
				data = data[n:]
			}
		case 4: // graph_id (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid graph_id")
				}
				result.GraphID = v
				data = data[n:]
			}
		case 5: // deployed_version (int32)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid deployed_version")
				}
				result.DeployedVersion = int32(v)
				data = data[n:]
			}
		case 6: // checksum (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid checksum")
				}
				result.Checksum = v
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return result, nil
}

// handleCapabilityRegistration processes capability registration from agent
// NOTE: Capabilities are now stored per-agent (not per-robot) for simplicity
func (h *RawQUICHandler) handleCapabilityRegistration(
	agentConn *agentConnection,
	agentID string,
	reg *CapabilityRegistrationMsg,
) {
	log.Printf("[RawQUIC] Capability registration from agent %s: %d capabilities",
		agentID, len(reg.Capabilities))

	// Keep the last known capability snapshot when transient discovery returns empty.
	// This allows editing with cached capabilities even while RTM is offline/unstable.
	if len(reg.Capabilities) == 0 {
		existingCaps, err := h.repo.GetAgentCapabilities(agentID)
		if err != nil {
			log.Printf("[RawQUIC] Failed to read existing capabilities for %s: %v", agentID, err)
			return
		}
		if len(existingCaps) > 0 {
			log.Printf("[RawQUIC] Empty capability registration from %s ignored; preserving %d cached capabilities",
				agentID, len(existingCaps))
			return
		}
		log.Printf("[RawQUIC] Empty capability registration from %s (no cached snapshot)", agentID)
		return
	}

	now := time.Now().UTC()

	// Convert to db.AgentCapability (agent-based, not robot-based)
	dbCaps := make([]db.AgentCapability, 0, len(reg.Capabilities))
	for _, cap := range reg.Capabilities {
		capabilityKind := strings.ToLower(strings.TrimSpace(cap.CapabilityKind))
		if capabilityKind == "" {
			if strings.Contains(strings.ToLower(cap.ActionType), "/srv/") {
				capabilityKind = "service"
			} else {
				capabilityKind = "action"
			}
		}
		lifecycleState := strings.ToLower(strings.TrimSpace(cap.LifecycleState))
		if lifecycleState == "" {
			lifecycleState = "unknown"
		}
		isLifecycleNode := cap.IsLifecycleNode || lifecycleState != "unknown"

		// Generate ID using agent_id + action_server (unique per server per agent)
		id := fmt.Sprintf("%s_%s_%s_%s", agentID, capabilityKind, cap.ActionType, cap.ActionServer)

		// Determine status based on availability
		status := "idle"
		if !cap.IsAvailable {
			status = "offline"
		}

		dbCap := db.AgentCapability{
			ID:              id,
			AgentID:         agentID,
			CapabilityKind:  capabilityKind,
			ActionType:      cap.ActionType,
			ActionServer:    cap.ActionServer,
			NodeName:        cap.NodeName,
			IsLifecycleNode: isLifecycleNode,
			IsAvailable:     cap.IsAvailable, // Use the actual availability from the agent
			Status:          status,
			LifecycleState:  lifecycleState,
			DiscoveredAt:    now,
			UpdatedAt:       now,
		}

		// Store schemas as JSON (they come as JSON strings from C++)
		if cap.GoalSchema != "" {
			dbCap.GoalSchema = []byte(cap.GoalSchema)
		}
		if cap.ResultSchema != "" {
			dbCap.ResultSchema = []byte(cap.ResultSchema)
		}
		if cap.FeedbackSchema != "" {
			dbCap.FeedbackSchema = []byte(cap.FeedbackSchema)
		}

		// Store success criteria as JSON
		if cap.SuccessCriteria != nil {
			criteriaJSON, err := json.Marshal(cap.SuccessCriteria)
			if err == nil {
				dbCap.SuccessCriteria = criteriaJSON
			}
		}

		dbCaps = append(dbCaps, dbCap)

		log.Printf("[RawQUIC]   - [%s] %s at %s (node=%s, lifecycle_node=%v, lifecycle_state=%s, available=%v, criteria=%v)",
			capabilityKind, cap.ActionType, cap.ActionServer, cap.NodeName, isLifecycleNode, lifecycleState, cap.IsAvailable, cap.SuccessCriteria != nil)
	}

	// Sync to database (using agent-based capabilities)
	if err := h.repo.SyncAgentCapabilities(agentID, dbCaps); err != nil {
		log.Printf("[RawQUIC] Failed to sync capabilities for agent %s: %v", agentID, err)
		return
	}

	log.Printf("[RawQUIC] Successfully registered %d capabilities for agent %s",
		len(dbCaps), agentID)

	// Broadcast capability update to WebSocket clients
	if h.wsHub != nil {
		wsCaps := make([]map[string]interface{}, 0, len(reg.Capabilities))
		for _, cap := range reg.Capabilities {
			capabilityKind := strings.ToLower(strings.TrimSpace(cap.CapabilityKind))
			if capabilityKind == "" {
				if strings.Contains(strings.ToLower(cap.ActionType), "/srv/") {
					capabilityKind = "service"
				} else {
					capabilityKind = "action"
				}
			}
			lifecycleState := strings.ToLower(strings.TrimSpace(cap.LifecycleState))
			if lifecycleState == "" {
				lifecycleState = "unknown"
			}
			isLifecycleNode := cap.IsLifecycleNode || lifecycleState != "unknown"
			wsCaps = append(wsCaps, map[string]interface{}{
				"capability_kind":   capabilityKind,
				"action_type":       cap.ActionType,
				"action_server":     cap.ActionServer,
				"package":           cap.Package,
				"action_name":       cap.ActionName,
				"node_name":         cap.NodeName,
				"is_lifecycle_node": isLifecycleNode,
				"lifecycle_state":   lifecycleState,
				"is_available":      cap.IsAvailable,
				"status":            statusFromAvailability(cap.IsAvailable),
			})
		}
		// Broadcast with agent_id instead of robot_id
		h.wsHub.BroadcastCapabilityUpdate(agentID, wsCaps)
		log.Printf("[RawQUIC] Broadcasted capability update for agent %s to WebSocket clients", agentID)
	}
}

// statusFromAvailability returns the status string based on availability
func statusFromAvailability(available bool) string {
	if available {
		return "idle"
	}
	return "offline"
}

// ============================================================
// Message Parsers for All AgentMessage Payload Types
// ============================================================

// parseAgentHeartbeat parses AgentHeartbeat protobuf (1:1 model)
// AgentHeartbeat: agent_id=1, state=2, is_executing=3, current_action=4
func parseAgentHeartbeat(data []byte) (*AgentHeartbeatMsg, error) {
	hb := &AgentHeartbeatMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // agent_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid agent_id")
				}
				hb.AgentID = v
				data = data[n:]
			}
		case 2: // state (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid state")
				}
				hb.State = v
				data = data[n:]
			} else if wireType == protowire.VarintType {
				// Legacy: convert int to string for backwards compatibility
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid state")
				}
				hb.State = robotStateToString(int32(v))
				data = data[n:]
			}
		case 3: // is_executing (bool)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid is_executing")
				}
				hb.IsExecuting = v != 0
				data = data[n:]
			}
		case 4: // current_action (string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid current_action")
				}
				hb.CurrentAction = v
				data = data[n:]
			}
		case 5: // network_latency_ms (reserved in proto, but may be used)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid network_latency_ms")
				}
				hb.NetworkLatencyMs = uint32(v)
				hb.HasNetworkLatency = true
				data = data[n:]
			}
		case 6: // network_latency_us
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid network_latency_us")
				}
				hb.NetworkLatencyUs = uint32(v)
				hb.HasNetworkLatencyUs = true
				data = data[n:]
			}
		case 10: // current_task_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid current_task_id")
				}
				hb.CurrentTaskID = v
				data = data[n:]
			}
		case 11: // current_step_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid current_step_id")
				}
				hb.CurrentStepID = v
				data = data[n:]
			}
		case 12: // telemetry (TelemetryPayload message)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid telemetry")
				}
				telemetry, err := parseTelemetryPayload(msgData)
				if err != nil {
					log.Printf("[RawQUIC] Failed to parse telemetry payload: %v", err)
					// Don't fail the whole heartbeat, just skip telemetry
				} else {
					hb.Telemetry = telemetry
				}
				data = data[n:]
			}
		case 13: // resource_events (ResourceEvent message)
			if wireType == protowire.BytesType {
				eventData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid resource_events")
				}
				event, err := parseResourceEvent(eventData)
				if err == nil && strings.TrimSpace(event.ResourceID) != "" {
					hb.ResourceEvents = append(hb.ResourceEvents, *event)
				}
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return hb, nil
}

// parseTelemetryPayload parses TelemetryPayload protobuf
func parseTelemetryPayload(data []byte) (*TelemetryPayloadMsg, error) {
	tp := &TelemetryPayloadMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // robot_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid robot_id")
				}
				tp.RobotID = v
				data = data[n:]
			}
		case 2: // joint_state
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid joint_state")
				}
				js, err := parseJointStateData(msgData)
				if err == nil {
					tp.JointState = js
				}
				data = data[n:]
			}
		case 3: // transforms (repeated)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid transforms")
				}
				tf, err := parseTransformData(msgData)
				if err == nil {
					tp.Transforms = append(tp.Transforms, tf)
				}
				data = data[n:]
			}
		case 4: // odometry
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid odometry")
				}
				odom, err := parseOdometryData(msgData)
				if err == nil {
					tp.Odometry = odom
				}
				data = data[n:]
			}
		case 5: // collected_at_ns
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid collected_at_ns")
				}
				tp.CollectedAtNs = int64(v)
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return tp, nil
}

// parseJointStateData parses JointStateData protobuf
func parseJointStateData(data []byte) (*JointStateDataMsg, error) {
	js := &JointStateDataMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // name (repeated string)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid name")
				}
				js.Name = append(js.Name, v)
				data = data[n:]
			}
		case 2: // position (repeated double, packed)
			if wireType == protowire.BytesType {
				packed, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid position")
				}
				js.Position = parsePackedDoubles(packed)
				data = data[n:]
			} else if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid position")
				}
				js.Position = append(js.Position, float64FromBits(v))
				data = data[n:]
			}
		case 3: // velocity (repeated double, packed)
			if wireType == protowire.BytesType {
				packed, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid velocity")
				}
				js.Velocity = parsePackedDoubles(packed)
				data = data[n:]
			} else if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid velocity")
				}
				js.Velocity = append(js.Velocity, float64FromBits(v))
				data = data[n:]
			}
		case 4: // effort (repeated double, packed)
			if wireType == protowire.BytesType {
				packed, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid effort")
				}
				js.Effort = parsePackedDoubles(packed)
				data = data[n:]
			} else if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid effort")
				}
				js.Effort = append(js.Effort, float64FromBits(v))
				data = data[n:]
			}
		case 5: // timestamp_ns
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid timestamp_ns")
				}
				js.TimestampNs = int64(v)
				data = data[n:]
			}
		case 6: // topic_name
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid topic_name")
				}
				js.TopicName = v
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return js, nil
}

// parseTransformData parses TransformData protobuf
func parseTransformData(data []byte) (*TransformDataMsg, error) {
	tf := &TransformDataMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // frame_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid frame_id")
				}
				tf.FrameID = v
				data = data[n:]
			}
		case 2: // child_frame_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid child_frame_id")
				}
				tf.ChildFrameID = v
				data = data[n:]
			}
		case 3: // translation (Vector3)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid translation")
				}
				vec, err := parseVector3(msgData)
				if err == nil {
					tf.Translation = vec
				}
				data = data[n:]
			}
		case 4: // rotation (Quaternion)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid rotation")
				}
				quat, err := parseQuaternion(msgData)
				if err == nil {
					tf.Rotation = quat
				}
				data = data[n:]
			}
		case 5: // timestamp_ns
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid timestamp_ns")
				}
				tf.TimestampNs = int64(v)
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return tf, nil
}

// parseOdometryData parses OdometryData protobuf
func parseOdometryData(data []byte) (*OdometryDataMsg, error) {
	odom := &OdometryDataMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // frame_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid frame_id")
				}
				odom.FrameID = v
				data = data[n:]
			}
		case 2: // child_frame_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid child_frame_id")
				}
				odom.ChildFrameID = v
				data = data[n:]
			}
		case 3: // pose (Pose)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid pose")
				}
				pose, err := parsePose(msgData)
				if err == nil {
					odom.Pose = pose
				}
				data = data[n:]
			}
		case 4: // twist (Twist)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid twist")
				}
				twist, err := parseTwist(msgData)
				if err == nil {
					odom.Twist = twist
				}
				data = data[n:]
			}
		case 5: // timestamp_ns
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid timestamp_ns")
				}
				odom.TimestampNs = int64(v)
				data = data[n:]
			}
		case 6: // topic_name
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid topic_name")
				}
				odom.TopicName = v
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return odom, nil
}

// parseVector3 parses Vector3 protobuf
func parseVector3(data []byte) (*Vector3Msg, error) {
	vec := &Vector3Msg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // x
			if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid x")
				}
				vec.X = float64FromBits(v)
				data = data[n:]
			}
		case 2: // y
			if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid y")
				}
				vec.Y = float64FromBits(v)
				data = data[n:]
			}
		case 3: // z
			if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid z")
				}
				vec.Z = float64FromBits(v)
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return vec, nil
}

// parseQuaternion parses Quaternion protobuf
func parseQuaternion(data []byte) (*QuaternionMsg, error) {
	quat := &QuaternionMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // x
			if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid x")
				}
				quat.X = float64FromBits(v)
				data = data[n:]
			}
		case 2: // y
			if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid y")
				}
				quat.Y = float64FromBits(v)
				data = data[n:]
			}
		case 3: // z
			if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid z")
				}
				quat.Z = float64FromBits(v)
				data = data[n:]
			}
		case 4: // w
			if wireType == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid w")
				}
				quat.W = float64FromBits(v)
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return quat, nil
}

// parsePose parses Pose protobuf
func parsePose(data []byte) (*PoseMsg, error) {
	pose := &PoseMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // position (Vector3)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid position")
				}
				vec, err := parseVector3(msgData)
				if err == nil {
					pose.Position = vec
				}
				data = data[n:]
			}
		case 2: // orientation (Quaternion)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid orientation")
				}
				quat, err := parseQuaternion(msgData)
				if err == nil {
					pose.Orientation = quat
				}
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return pose, nil
}

// parseTwist parses Twist protobuf
func parseTwist(data []byte) (*TwistMsg, error) {
	twist := &TwistMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // linear (Vector3)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid linear")
				}
				vec, err := parseVector3(msgData)
				if err == nil {
					twist.Linear = vec
				}
				data = data[n:]
			}
		case 2: // angular (Vector3)
			if wireType == protowire.BytesType {
				msgData, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid angular")
				}
				vec, err := parseVector3(msgData)
				if err == nil {
					twist.Angular = vec
				}
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return twist, nil
}

// parsePackedDoubles parses packed repeated double field
func parsePackedDoubles(data []byte) []float64 {
	var doubles []float64
	for len(data) >= 8 {
		v := binary.LittleEndian.Uint64(data[:8])
		doubles = append(doubles, float64FromBits(v))
		data = data[8:]
	}
	return doubles
}

// parseActionResult parses ActionResult protobuf
func parseActionResult(data []byte) (*ActionResultMsg, error) {
	r := &ActionResultMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // command_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid command_id")
				}
				r.CommandID = v
				data = data[n:]
			}
		case 2: // agent_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid agent_id")
				}
				r.AgentID = v
				data = data[n:]
			}
		case 3: // task_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid task_id")
				}
				r.TaskID = v
				data = data[n:]
			}
		case 4: // step_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid step_id")
				}
				r.StepID = v
				data = data[n:]
			}
		case 5: // status (ActionStatus enum)
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid status")
				}
				r.Status = int32(v)
				data = data[n:]
			}
		case 6: // result (bytes)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid result")
				}
				r.Result = v
				data = data[n:]
			}
		case 7: // error
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid error")
				}
				r.Error = v
				data = data[n:]
			}
		case 8: // started_at_ms
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid started_at_ms")
				}
				r.StartedAtMs = int64(v)
				data = data[n:]
			}
		case 9: // completed_at_ms
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid completed_at_ms")
				}
				r.CompletedAtMs = int64(v)
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return r, nil
}

// parseActionFeedback parses ActionFeedback protobuf
func parseActionFeedback(data []byte) (*ActionFeedbackMsg, error) {
	fb := &ActionFeedbackMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // command_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid command_id")
				}
				fb.CommandID = v
				data = data[n:]
			}
		case 2: // agent_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid agent_id")
				}
				fb.AgentID = v
				data = data[n:]
			}
		case 3: // task_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid task_id")
				}
				fb.TaskID = v
				data = data[n:]
			}
		case 4: // step_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid step_id")
				}
				fb.StepID = v
				data = data[n:]
			}
		case 5: // progress
			if wireType == protowire.Fixed32Type {
				v, n := protowire.ConsumeFixed32(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid progress")
				}
				fb.Progress = float32FromBits(v)
				data = data[n:]
			}
		case 6: // feedback (bytes)
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeBytes(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid feedback")
				}
				fb.Feedback = v
				data = data[n:]
			}
		case 7: // timestamp_ms
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid timestamp_ms")
				}
				fb.TimestampMs = int64(v)
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return fb, nil
}

// parseAgentStatusUpdate parses AgentStatusUpdate protobuf
func parseAgentStatusUpdate(data []byte) (*AgentStatusUpdateMsg, error) {
	s := &AgentStatusUpdateMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // state
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid state")
				}
				s.State = int32(v)
				data = data[n:]
			}
		case 2: // is_online
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid is_online")
				}
				s.IsOnline = v != 0
				data = data[n:]
			}
		case 3: // message
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid message")
				}
				s.Message = v
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return s, nil
}

// parseDeployGraphResponse parses DeployGraphResponse protobuf (standalone)
func parseDeployGraphResponse(data []byte) (*DeployGraphResponseMsg, error) {
	r := &DeployGraphResponseMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // correlation_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid correlation_id")
				}
				r.CorrelationID = v
				data = data[n:]
			}
		case 2: // success
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid success")
				}
				r.Success = v != 0
				data = data[n:]
			}
		case 3: // error
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid error")
				}
				r.Error = v
				data = data[n:]
			}
		case 4: // graph_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid graph_id")
				}
				r.GraphID = v
				data = data[n:]
			}
		case 5: // deployed_version
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid deployed_version")
				}
				r.DeployedVersion = int32(v)
				data = data[n:]
			}
		case 6: // checksum
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid checksum")
				}
				r.Checksum = v
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return r, nil
}

// parseGraphExecutionStatus parses GraphExecutionStatus protobuf
func parseGraphExecutionStatus(data []byte) (*GraphExecutionStatusMsg, error) {
	s := &GraphExecutionStatusMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // execution_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid execution_id")
				}
				s.ExecutionID = v
				data = data[n:]
			}
		case 2: // graph_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid graph_id")
				}
				s.GraphID = v
				data = data[n:]
			}
		case 3: // agent_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid agent_id")
				}
				s.AgentID = v
				data = data[n:]
			}
		case 4: // state
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid state")
				}
				s.State = int32(v)
				data = data[n:]
			}
		case 5: // current_vertex_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid current_vertex_id")
				}
				s.CurrentVertexID = v
				data = data[n:]
			}
		case 6: // current_step_index
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid current_step_index")
				}
				s.CurrentStepIndex = int32(v)
				data = data[n:]
			}
		case 7: // progress
			if wireType == protowire.Fixed32Type {
				v, n := protowire.ConsumeFixed32(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid progress")
				}
				s.Progress = float32FromBits(v)
				data = data[n:]
			}
		case 8: // started_at_ms
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid started_at_ms")
				}
				s.StartedAtMs = int64(v)
				data = data[n:]
			}
		case 9: // updated_at_ms
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid updated_at_ms")
				}
				s.UpdatedAtMs = int64(v)
				data = data[n:]
			}
		case 10: // completed_at_ms
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid completed_at_ms")
				}
				s.CompletedAtMs = int64(v)
				data = data[n:]
			}
		case 11: // error
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid error")
				}
				s.Error = v
				data = data[n:]
			}
		case 12: // failed_vertex_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid failed_vertex_id")
				}
				s.FailedVertexID = v
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return s, nil
}

// parsePongResponse parses PongResponse protobuf
func parsePongResponse(data []byte) (*PongResponseMsg, error) {
	p := &PongResponseMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // ping_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid ping_id")
				}
				p.PingID = v
				data = data[n:]
			}
		case 2: // server_timestamp_ms
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid server_timestamp_ms")
				}
				p.ServerTimestampMs = int64(v)
				data = data[n:]
			}
		case 3: // agent_timestamp_ms
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid agent_timestamp_ms")
				}
				p.AgentTimestampMs = int64(v)
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return p, nil
}

// parseConfigUpdateAck parses ConfigUpdateAck protobuf
func parseConfigUpdateAck(data []byte) (*ConfigUpdateAckMsg, error) {
	a := &ConfigUpdateAckMsg{}

	for len(data) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		switch fieldNum {
		case 1: // agent_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid agent_id")
				}
				a.AgentID = v
				data = data[n:]
			}
		case 2: // state_def_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid state_def_id")
				}
				a.StateDefID = v
				data = data[n:]
			}
		case 3: // version
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid version")
				}
				a.Version = int32(v)
				data = data[n:]
			}
		case 4: // success
			if wireType == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid success")
				}
				a.Success = v != 0
				data = data[n:]
			}
		case 5: // error
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid error")
				}
				a.Error = v
				data = data[n:]
			}
		case 6: // correlation_id
			if wireType == protowire.BytesType {
				v, n := protowire.ConsumeString(data)
				if n < 0 {
					return nil, fmt.Errorf("invalid correlation_id")
				}
				a.CorrelationID = v
				data = data[n:]
			}
		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data)
			if n < 0 {
				return nil, fmt.Errorf("invalid field")
			}
			data = data[n:]
		}
	}

	return a, nil
}

// Helper functions for float parsing
func float32FromBits(bits uint32) float32 {
	return *(*float32)(unsafe.Pointer(&bits))
}

func float64FromBits(bits uint64) float64 {
	return *(*float64)(unsafe.Pointer(&bits))
}

// ============================================================
// Message Handlers - Process Incoming Messages
// ============================================================

// handleHeartbeat processes heartbeat message and updates state manager
func (h *RawQUICHandler) handleHeartbeat(agentConn *agentConnection, hb *AgentHeartbeatMsg) {
	// Update agent heartbeat timestamp
	if err := h.stateManager.UpdateAgentHeartbeat(agentConn.agentID); err != nil {
		log.Printf("[RawQUIC] Failed to update agent heartbeat for %s: %v", agentConn.agentID, err)
	}
	if err := h.repo.UpdateAgentLastSeen(agentConn.agentID); err != nil {
		log.Printf("[RawQUIC] Failed to update agent last_seen for %s: %v", agentConn.agentID, err)
	}

	// IMPORTANT:
	// Network latency is measured exclusively via server-initiated ping/pong
	// (see handlePong). Heartbeat-reported latency fields are intentionally
	// ignored to avoid drift/sawtooth artifacts from agent-side calculations.

	// Update agent state (1:1 model: agent_id = robot_id)
	// State is now sent as a string directly from the agent
	stateName := hb.State
	if stateName == "" {
		stateName = "idle" // Default if empty
	}

	// Update state (in 1:1 model, agent_id is used as robot_id)
	if err := h.stateManager.UpdateRobotState(agentConn.agentID, stateName); err != nil {
		log.Printf("[RawQUIC] Failed to update agent state for %s: %v", agentConn.agentID, err)
	}

	// Update execution state atomically - this prevents TOCTOU race condition
	// The TryUpdateRobotExecutionFromHeartbeat method checks and updates inside a single lock
	// This ensures server-managed tasks are not overwritten by heartbeat data
	updated, err := h.stateManager.TryUpdateRobotExecutionFromHeartbeat(
		agentConn.agentID,
		hb.IsExecuting,
		hb.CurrentTaskID,
		hb.CurrentStepID,
	)
	if err != nil {
		log.Printf("[RawQUIC] Failed to update agent execution for %s: %v", agentConn.agentID, err)
	} else if !updated {
		// Server is managing execution - heartbeat was intentionally skipped
		// Only log at debug level to avoid log spam
		if hb.IsExecuting || hb.CurrentTaskID != "" {
			log.Printf("[RawQUIC] Skipping heartbeat execution update for %s: server is managing task (heartbeat task=%s is_executing=%v)",
				agentConn.agentID, hb.CurrentTaskID, hb.IsExecuting)
		}
	}

	// Update last seen
	agentConn.lastSeen = time.Now()

	if h.taskObserveCallback != nil {
		h.taskObserveCallback(agentConn.agentID, hb.IsExecuting, hb.CurrentTaskID)
	}

	if len(hb.ResourceEvents) > 0 {
		if changed := h.applyResourceEvents(agentConn.agentID, hb.CurrentTaskID, hb.CurrentStepID, hb.ResourceEvents); changed && h.resourceChangeCallback != nil {
			h.resourceChangeCallback()
		}
	}

	// Update telemetry if present in heartbeat
	if hb.Telemetry != nil {
		robotID := hb.Telemetry.RobotID
		if robotID == "" {
			// In 1:1 model, use agent_id as robot_id
			robotID = agentConn.agentID
		}
		telemetry := convertTelemetryPayload(hb.Telemetry)

		// Debug log telemetry data (every 10 heartbeats)
		agentConn.telemetryLogCounter++
		if agentConn.telemetryLogCounter >= 10 {
			agentConn.telemetryLogCounter = 0
			hasJoint := telemetry.JointState != nil
			hasOdom := telemetry.Odometry != nil
			numTf := 0
			if telemetry.Transforms != nil {
				numTf = len(telemetry.Transforms)
			}
			log.Printf("[RawQUIC] Telemetry for %s: joint_state=%v odometry=%v transforms=%d",
				robotID, hasJoint, hasOdom, numTf)
		}

		if err := h.stateManager.UpdateRobotTelemetry(robotID, telemetry); err != nil {
			log.Printf("[RawQUIC] Failed to update robot telemetry for %s: %v", robotID, err)
		}
	}
}

// convertTelemetryPayload converts TelemetryPayloadMsg to state.RobotTelemetry
func convertTelemetryPayload(tp *TelemetryPayloadMsg) *state.RobotTelemetry {
	telemetry := &state.RobotTelemetry{}

	// Convert JointState
	if tp.JointState != nil {
		telemetry.JointState = &state.JointStateData{
			Name:        tp.JointState.Name,
			Position:    tp.JointState.Position,
			Velocity:    tp.JointState.Velocity,
			Effort:      tp.JointState.Effort,
			TopicName:   tp.JointState.TopicName,
			TimestampNs: tp.JointState.TimestampNs,
		}
	}

	// Convert Odometry
	if tp.Odometry != nil {
		telemetry.Odometry = &state.OdometryData{
			FrameID:      tp.Odometry.FrameID,
			ChildFrameID: tp.Odometry.ChildFrameID,
			TopicName:    tp.Odometry.TopicName,
			TimestampNs:  tp.Odometry.TimestampNs,
		}
		if tp.Odometry.Pose != nil {
			if tp.Odometry.Pose.Position != nil {
				telemetry.Odometry.Pose.Position = state.Vector3{
					X: tp.Odometry.Pose.Position.X,
					Y: tp.Odometry.Pose.Position.Y,
					Z: tp.Odometry.Pose.Position.Z,
				}
			}
			if tp.Odometry.Pose.Orientation != nil {
				telemetry.Odometry.Pose.Orientation = state.Quaternion{
					X: tp.Odometry.Pose.Orientation.X,
					Y: tp.Odometry.Pose.Orientation.Y,
					Z: tp.Odometry.Pose.Orientation.Z,
					W: tp.Odometry.Pose.Orientation.W,
				}
			}
		}
		if tp.Odometry.Twist != nil {
			if tp.Odometry.Twist.Linear != nil {
				telemetry.Odometry.Twist.Linear = state.Vector3{
					X: tp.Odometry.Twist.Linear.X,
					Y: tp.Odometry.Twist.Linear.Y,
					Z: tp.Odometry.Twist.Linear.Z,
				}
			}
			if tp.Odometry.Twist.Angular != nil {
				telemetry.Odometry.Twist.Angular = state.Vector3{
					X: tp.Odometry.Twist.Angular.X,
					Y: tp.Odometry.Twist.Angular.Y,
					Z: tp.Odometry.Twist.Angular.Z,
				}
			}
		}
	}

	// Convert Transforms
	for _, tf := range tp.Transforms {
		transformData := state.TransformData{
			FrameID:      tf.FrameID,
			ChildFrameID: tf.ChildFrameID,
			TimestampNs:  tf.TimestampNs,
		}
		if tf.Translation != nil {
			transformData.Translation = state.Vector3{
				X: tf.Translation.X,
				Y: tf.Translation.Y,
				Z: tf.Translation.Z,
			}
		}
		if tf.Rotation != nil {
			transformData.Rotation = state.Quaternion{
				X: tf.Rotation.X,
				Y: tf.Rotation.Y,
				Z: tf.Rotation.Z,
				W: tf.Rotation.W,
			}
		}
		telemetry.Transforms = append(telemetry.Transforms, transformData)
	}

	return telemetry
}

func (h *RawQUICHandler) applyResourceEvents(agentID, runtimeTaskID, fallbackStepID string, events []ResourceEventMsg) bool {
	runtimeTaskID = strings.TrimSpace(runtimeTaskID)
	if runtimeTaskID == "" || len(events) == 0 {
		return false
	}

	changed := false
	for _, event := range events {
		resourceID := strings.TrimSpace(event.ResourceID)
		if resourceID == "" {
			continue
		}

		acquired := false
		switch event.Kind {
		case 1:
			acquired = true
		case 2:
			acquired = false
		default:
			continue
		}

		stepID := strings.TrimSpace(event.StepID)
		if stepID == "" {
			stepID = fallbackStepID
		}

		if err := h.stateManager.HandleTaskResourceEvent(runtimeTaskID, agentID, stepID, resourceID, acquired); err != nil {
			log.Printf("[RawQUIC] Failed to apply resource event task=%s agent=%s resource=%s acquired=%v: %v",
				runtimeTaskID, agentID, resourceID, acquired, err)
			continue
		}
		changed = true
	}

	return changed
}

// handleActionResult processes action result and notifies pending commands
func (h *RawQUICHandler) handleActionResult(agentConn *agentConnection, resultMsg *ActionResultMsg) {
	log.Printf("[RawQUIC] Action result: command=%s agent=%s status=%d",
		resultMsg.CommandID, resultMsg.AgentID, resultMsg.Status)

	// Convert ActionResultMsg to ActionResult (from handlers.go)
	result := &ActionResult{
		CommandID:     resultMsg.CommandID,
		AgentID:       resultMsg.AgentID,
		TaskID:        resultMsg.TaskID,
		StepID:        resultMsg.StepID,
		Status:        ActionStatus(resultMsg.Status),
		Error:         resultMsg.Error,
		StartedAtMs:   resultMsg.StartedAtMs,
		CompletedAtMs: resultMsg.CompletedAtMs,
	}

	// Parse JSON result if present
	if len(resultMsg.Result) > 0 {
		var resultData map[string]interface{}
		if err := json.Unmarshal(resultMsg.Result, &resultData); err == nil {
			result.Result = resultData
		}
	}

	// Find and notify pending command
	h.pendingMu.Lock()
	pending, exists := h.pendingCommands[resultMsg.CommandID]
	if exists {
		delete(h.pendingCommands, resultMsg.CommandID)
	}
	h.pendingMu.Unlock()

	if exists && pending.ResultChan != nil {
		select {
		case pending.ResultChan <- result:
		default:
			log.Printf("[RawQUIC] Result channel full for command %s", resultMsg.CommandID)
		}
	}

	// Call registered callback
	h.callbackMu.RLock()
	callback, hasCallback := h.resultCallbacks[resultMsg.CommandID]
	h.callbackMu.RUnlock()

	if hasCallback {
		go callback(result, nil, nil)
		// Cleanup callback
		h.callbackMu.Lock()
		delete(h.resultCallbacks, resultMsg.CommandID)
		h.callbackMu.Unlock()
	}

	// NOTE: Do NOT call CompleteExecution() here.
	// Individual action completion does not mean graph execution is complete.
	// Graph completion is handled by handleGraphStatus() when the agent sends
	// GraphExecutionStatusMsg with completed/failed/cancelled status.
}

// handleActionFeedback processes action progress feedback
func (h *RawQUICHandler) handleActionFeedback(agentConn *agentConnection, fb *ActionFeedbackMsg) {
	// Call registered callback
	h.callbackMu.RLock()
	callback, hasCallback := h.resultCallbacks[fb.CommandID]
	h.callbackMu.RUnlock()

	if hasCallback {
		go callback(nil, fb, nil)
	}

	// Update progress in state if needed
	// h.stateManager.UpdateActionProgress(fb.AgentID, fb.Progress)
}

// handleTaskLog processes task execution log from agent
func (h *RawQUICHandler) handleTaskLog(agentConn *agentConnection, taskLog *TaskLogMsg) {
	if taskLog == nil {
		return
	}

	// Create log entry for TaskLogManager
	entry := state.TaskLogEntry{
		AgentID:     taskLog.AgentID,
		TaskID:      taskLog.TaskID,
		StepID:      taskLog.StepID,
		CommandID:   taskLog.CommandID,
		Level:       state.TaskLogLevel(taskLog.Level),
		Message:     taskLog.Message,
		TimestampMs: taskLog.TimestampMs,
		Component:   taskLog.Component,
		Metadata:    taskLog.Metadata,
	}

	// Add to task log manager
	h.stateManager.TaskLogManager().AddLog(entry)

	// Log at appropriate level for debugging
	levelStr := "INFO"
	switch state.TaskLogLevel(taskLog.Level) {
	case state.TaskLogDebug:
		levelStr = "DEBUG"
	case state.TaskLogInfo:
		levelStr = "INFO"
	case state.TaskLogWarn:
		levelStr = "WARN"
	case state.TaskLogError:
		levelStr = "ERROR"
	}

	log.Printf("[TaskLog] [%s] [%s] [%s/%s] %s: %s",
		levelStr, taskLog.AgentID, taskLog.TaskID, taskLog.StepID,
		taskLog.Component, taskLog.Message)
}

// handleTaskStateUpdate processes agent-driven task state updates
// This is the primary state update mechanism for agent-driven execution
func (h *RawQUICHandler) handleTaskStateUpdate(agentConn *agentConnection, update *TaskStateUpdateMsg) {
	if update == nil || update.TaskID == "" {
		return
	}

	log.Printf("[RawQUIC] TaskStateUpdate: task=%s, step=%s, state=%d, progress=%.2f",
		update.TaskID, update.CurrentStepID, update.State, update.Progress)

	// Convert TaskState enum to status string
	taskStatus := taskStateToString(update.State)

	// Update task state in database
	// stepIndex is not tracked for agent-driven execution, use 0
	if err := h.repo.UpdateTaskStatus(update.TaskID, taskStatus, update.CurrentStepID, 0, ""); err != nil {
		log.Printf("[RawQUIC] Failed to update task state: %v", err)
	}

	// Update in-memory state manager (so WebSocket broadcasts reflect current step)
	isExecuting := taskStatus == "running" || taskStatus == "executing"
	if err := h.stateManager.UpdateRobotExecution(agentConn.agentID, isExecuting, update.TaskID, update.CurrentStepID); err != nil {
		log.Printf("[RawQUIC] Failed to update state manager: %v", err)
	}
	if h.taskObserveCallback != nil {
		h.taskObserveCallback(agentConn.agentID, isExecuting, update.TaskID)
	}

	resourceChanged := false
	if len(update.ResourceEvents) > 0 {
		resourceChanged = h.applyResourceEvents(agentConn.agentID, update.TaskID, update.CurrentStepID, update.ResourceEvents)
	}

	// If task is terminal (completed/failed/cancelled), call CompleteExecution to fully clear state
	if taskStatus == "completed" || taskStatus == "failed" || taskStatus == "cancelled" {
		if released := h.stateManager.ReleaseTaskResources(update.TaskID, agentConn.agentID); len(released) > 0 {
			resourceChanged = true
		}
		h.stateManager.UnregisterTaskRuntime(update.TaskID)
		h.stateManager.CompleteExecution(agentConn.agentID, nil)
		log.Printf("[RawQUIC] Task %s terminal state: %s - cleared execution state for agent %s",
			update.TaskID, taskStatus, agentConn.agentID)

		// Notify scheduler of task completion (for agent-driven execution)
		if h.taskCompleteCallback != nil {
			errorMsg := ""
			if update.StepResult != nil {
				errorMsg = update.StepResult.Error
			}
			h.taskCompleteCallback(update.TaskID, taskStatus, errorMsg)
		}
	}

	if resourceChanged && h.resourceChangeCallback != nil {
		h.resourceChangeCallback()
	}

	// If step result is present, log it
	if update.StepResult != nil {
		log.Printf("[RawQUIC] Step result: step=%s, status=%d, duration=%dms",
			update.StepResult.StepID, update.StepResult.Status, update.StepResult.DurationMs)

		// Create action result from step result and process it
		actionResult := &ActionResultMsg{
			TaskID:        update.TaskID,
			StepID:        update.StepResult.StepID,
			Status:        update.StepResult.Status,
			Result:        update.StepResult.ResultJSON,
			Error:         update.StepResult.Error,
			CompletedAtMs: update.TimestampMs,
		}
		h.handleActionResult(agentConn, actionResult)
	}

	// If blocking, log the reason
	if update.BlockingReason != "" {
		log.Printf("[RawQUIC] Task %s blocked: %s", update.TaskID, update.BlockingReason)
	}

	// Log variables for debugging
	if len(update.Variables) > 0 {
		log.Printf("[RawQUIC] Task %s variables: %v", update.TaskID, update.Variables)
	}

	// Broadcast to WebSocket clients
	h.broadcastTaskUpdate(update)
}

// taskStateToString converts TaskState enum to status string
func taskStateToString(state int32) string {
	switch state {
	case 1: // TASK_STATE_PENDING
		return "pending"
	case 2: // TASK_STATE_RUNNING
		return "running"
	case 3: // TASK_STATE_WAITING_PRECONDITION
		return "waiting"
	case 4: // TASK_STATE_EXECUTING_ACTION
		return "executing"
	case 5: // TASK_STATE_COMPLETED
		return "completed"
	case 6: // TASK_STATE_FAILED
		return "failed"
	case 7: // TASK_STATE_CANCELLED
		return "cancelled"
	default:
		return "unknown"
	}
}

// broadcastTaskUpdate sends task update to WebSocket clients
func (h *RawQUICHandler) broadcastTaskUpdate(update *TaskStateUpdateMsg) {
	if h.wsHub == nil {
		return
	}

	broadcast := TaskStateUpdateBroadcast{
		TaskID:         update.TaskID,
		StepID:         update.CurrentStepID,
		State:          taskStateToString(update.State),
		Progress:       update.Progress,
		BlockingReason: update.BlockingReason,
		Variables:      update.Variables,
	}

	if update.StepResult != nil {
		broadcast.StepResult = &StepResultBroadcast{
			StepID:     update.StepResult.StepID,
			Status:     update.StepResult.Status,
			DurationMs: update.StepResult.DurationMs,
			Error:      update.StepResult.Error,
		}
	}

	h.wsHub.BroadcastTaskStateUpdate(broadcast)
}

// handleStatusUpdate processes agent status update
func (h *RawQUICHandler) handleStatusUpdate(agentConn *agentConnection, status *AgentStatusUpdateMsg) {
	log.Printf("[RawQUIC] Agent status update: %s - state=%d, online=%v, msg=%s",
		agentConn.agentID, status.State, status.IsOnline, status.Message)

	// Update agent online status (1:1 model: agent_id = robot_id)
	if err := h.stateManager.SetRobotOnline(agentConn.agentID, status.IsOnline); err != nil {
		log.Printf("[RawQUIC] Failed to set agent online status: %v", err)
	}
}

// handleGraphStatus processes graph execution status updates.
func (h *RawQUICHandler) handleGraphStatus(agentConn *agentConnection, status *GraphExecutionStatusMsg) {
	if status == nil {
		return
	}

	log.Printf("[RawQUIC] handleGraphStatus: ExecutionID=%s GraphID=%s AgentID=%s State=%d StepID=%s",
		status.ExecutionID, status.GraphID, status.AgentID, status.State, status.CurrentVertexID)

	taskID := status.ExecutionID
	if taskID == "" {
		return
	}

	taskStatus := "running"
	switch status.State {
	case 2: // completed
		taskStatus = "completed"
	case 3: // failed
		taskStatus = "failed"
	case 4: // cancelled
		taskStatus = "cancelled"
	case 5: // paused
		taskStatus = "paused"
	default:
		taskStatus = "running"
	}

	stepID := status.CurrentVertexID
	stepIndex := int(status.CurrentStepIndex)
	errMsg := status.Error

	if err := h.repo.UpdateTaskStatus(taskID, taskStatus, stepID, stepIndex, errMsg); err != nil {
		log.Printf("[RawQUIC] Failed to update task status: %v", err)
	}

	if status.AgentID != "" {
		isExecuting := taskStatus == "running" || taskStatus == "paused"
		if err := h.stateManager.UpdateRobotExecution(status.AgentID, isExecuting, taskID, stepID); err != nil {
			log.Printf("[RawQUIC] Failed to update agent execution: %v", err)
		}
	}

	if taskStatus == "completed" || taskStatus == "failed" || taskStatus == "cancelled" {
		h.clearGraphStateOverrides(taskID)
	} else if status.GraphID != "" && stepID != "" && status.AgentID != "" {
		h.updateGraphStateOverrides(taskID, status.GraphID, status.AgentID, stepID)
	}

	if taskStatus == "completed" || taskStatus == "failed" || taskStatus == "cancelled" {
		if status.AgentID != "" {
			h.stateManager.CompleteExecution(status.AgentID, nil)
		}
		task, err := h.repo.GetTask(taskID)
		if err != nil {
			log.Printf("[RawQUIC] Failed to load task: %v", err)
			return
		}
		if task != nil {
			task.Status = taskStatus
			if errMsg != "" {
				task.ErrorMessage = sql.NullString{String: errMsg, Valid: true}
			}
			if status.CompletedAtMs > 0 {
				task.CompletedAt = sql.NullTime{Time: time.UnixMilli(status.CompletedAtMs), Valid: true}
			} else {
				task.CompletedAt = sql.NullTime{Time: time.Now(), Valid: true}
			}
			if err := h.repo.UpdateTask(task); err != nil {
				log.Printf("[RawQUIC] Failed to update task completion: %v", err)
			}
		}
	}
}

func (h *RawQUICHandler) updateGraphStateOverrides(executionID, graphID, agentID, stepID string) {
	if executionID == "" || graphID == "" || stepID == "" {
		return
	}

	h.graphOverrideMu.Lock()
	prev := h.graphOverrides[executionID]
	if prev != nil && prev.StepID == stepID {
		h.graphOverrideMu.Unlock()
		return
	}
	h.graphOverrides[executionID] = &graphOverrideState{StepID: stepID}
	h.graphOverrideMu.Unlock()

	if prev != nil && len(prev.AgentIDs) > 0 {
		h.clearDuringStateTargets(executionID, prev.AgentIDs)
	}

	steps, err := h.repo.GetBehaviorTreeSteps(graphID)
	if err != nil {
		log.Printf("[RawQUIC] Failed to load graph steps for %s: %v", graphID, err)
		return
	}

	var step *db.BehaviorTreeStep
	for i := range steps {
		if steps[i].ID == stepID {
			step = &steps[i]
			break
		}
	}
	if step == nil {
		log.Printf("[RawQUIC] updateGraphStateOverrides: step %s not found in graph %s", stepID, graphID)
		return
	}

	log.Printf("[RawQUIC] updateGraphStateOverrides: step=%s DuringStates=%v DuringStateTargets=%+v",
		stepID, step.DuringStates, step.DuringStateTargets)

	applied := h.applyDuringStateTargets(
		agentID,
		executionID,
		step.DuringStateTargets,
		step.DuringStates,
	)

	if len(applied) == 0 {
		return
	}

	h.graphOverrideMu.Lock()
	if current, ok := h.graphOverrides[executionID]; ok && current.StepID == stepID {
		current.AgentIDs = applied
	}
	h.graphOverrideMu.Unlock()
}

func (h *RawQUICHandler) clearGraphStateOverrides(executionID string) {
	if executionID == "" {
		return
	}

	h.graphOverrideMu.Lock()
	state := h.graphOverrides[executionID]
	delete(h.graphOverrides, executionID)
	h.graphOverrideMu.Unlock()

	if state != nil && len(state.AgentIDs) > 0 {
		h.clearDuringStateTargets(executionID, state.AgentIDs)
	}
}

func (h *RawQUICHandler) applyDuringStateTargets(
	executingAgentID string,
	sourceID string,
	targets []db.StateTarget,
	fallbackStates []string,
) []string {
	overrides := h.resolveDuringStateOverrides(executingAgentID, targets, fallbackStates)
	if len(overrides) == 0 {
		return nil
	}

	applied := make([]string, 0, len(overrides))
	for agentID, state := range overrides {
		if err := h.stateManager.SetRobotStateOverride(agentID, sourceID, state); err == nil {
			applied = append(applied, agentID)
		}
	}
	if len(applied) == 0 {
		return nil
	}
	return applied
}

func (h *RawQUICHandler) clearDuringStateTargets(sourceID string, agentIDs []string) {
	for _, agentID := range agentIDs {
		_ = h.stateManager.ClearRobotStateOverride(agentID, sourceID)
	}
}

func normalizeStateTargetsForOverrides(targets []db.StateTarget, fallbackStates []string) []db.StateTarget {
	if len(targets) > 0 {
		return targets
	}
	for _, state := range fallbackStates {
		if state == "" {
			continue
		}
		return []db.StateTarget{{
			State:      state,
			TargetType: "self",
		}}
	}
	return nil
}

func orderStateTargetsForOverrides(targets []db.StateTarget) []db.StateTarget {
	if len(targets) == 0 {
		return nil
	}
	ordered := make([]db.StateTarget, 0, len(targets))
	appendMatches := func(match func(string) bool) {
		for _, target := range targets {
			targetType := strings.ToLower(target.TargetType)
			if targetType == "" {
				targetType = "self"
			}
			if match(targetType) {
				ordered = append(ordered, target)
			}
		}
	}

	appendMatches(func(tt string) bool { return tt == "self" })
	appendMatches(func(tt string) bool { return tt == "agent" || tt == "specific" })
	appendMatches(func(tt string) bool { return tt == "all" })
	appendMatches(func(tt string) bool {
		return tt != "self" && tt != "agent" && tt != "specific" && tt != "all"
	})

	return ordered
}

func (h *RawQUICHandler) resolveDuringStateOverrides(
	executingAgentID string,
	targets []db.StateTarget,
	fallbackStates []string,
) map[string]string {
	effectiveTargets := normalizeStateTargetsForOverrides(targets, fallbackStates)
	if len(effectiveTargets) == 0 {
		return nil
	}
	orderedTargets := orderStateTargetsForOverrides(effectiveTargets)
	overrides := make(map[string]string)

	for _, target := range orderedTargets {
		if target.State == "" {
			continue
		}
		targetType := strings.ToLower(target.TargetType)
		if targetType == "" {
			targetType = "self"
		}
		agentIDs := h.stateManager.ResolveTargetAgents(executingAgentID, targetType, target.AgentID)
		for _, agentID := range agentIDs {
			if _, exists := overrides[agentID]; exists {
				continue
			}
			overrides[agentID] = target.State
		}
	}

	if len(overrides) == 0 {
		return nil
	}
	return overrides
}

// handleDeployResponse resolves pending deploy waiters by correlation_id.
func (h *RawQUICHandler) handleDeployResponse(resp *DeployGraphResponseMsg) {
	if resp == nil || resp.CorrelationID == "" {
		log.Printf("[RawQUIC] DeployResponse: nil or empty correlation_id")
		return
	}

	log.Printf("[RawQUIC] DeployResponse received: graph=%s version=%d success=%v correlation=%s",
		resp.GraphID, resp.DeployedVersion, resp.Success, resp.CorrelationID)

	result := &DeployResult{
		Success:         resp.Success,
		Error:           resp.Error,
		Checksum:        resp.Checksum,
		CorrelationID:   resp.CorrelationID,
		GraphID:         resp.GraphID,
		DeployedVersion: resp.DeployedVersion,
	}

	h.deployMu.RLock()
	ch, exists := h.deployWaiters[resp.CorrelationID]
	h.deployMu.RUnlock()
	if !exists {
		log.Printf("[RawQUIC] DeployResponse: no waiter for correlation_id=%s (late response or timeout?)",
			resp.CorrelationID)
		return
	}

	select {
	case ch <- result:
		log.Printf("[RawQUIC] DeployResponse: delivered to waiter correlation_id=%s", resp.CorrelationID)
	default:
		log.Printf("[RawQUIC] DeployResponse: channel full, dropped correlation_id=%s", resp.CorrelationID)
	}
}

// handleConfigUpdateAck resolves pending config update waiters by correlation_id.
func (h *RawQUICHandler) handleConfigUpdateAck(ack *ConfigUpdateAckMsg) {
	if ack == nil || ack.CorrelationID == "" {
		return
	}

	result := &ConfigUpdateResult{
		Success:       ack.Success,
		Error:         ack.Error,
		AgentID:       ack.AgentID,
		StateDefID:    ack.StateDefID,
		Version:       ack.Version,
		CorrelationID: ack.CorrelationID,
	}

	h.configMu.RLock()
	ch, exists := h.configWaiters[ack.CorrelationID]
	h.configMu.RUnlock()
	if !exists {
		return
	}

	select {
	case ch <- result:
	default:
	}
}

// handlePong processes pong response for latency measurement
func (h *RawQUICHandler) handlePong(agentConn *agentConnection, pong *PongResponseMsg) {
	latencyMs := time.Now().UnixMilli() - pong.ServerTimestampMs
	if latencyMs < 0 {
		latencyMs = 0
	}
	latency := time.Duration(latencyMs) * time.Millisecond
	if err := h.stateManager.UpdateAgentPing(agentConn.agentID, latency); err != nil {
		log.Printf("[RawQUIC] Failed to update agent ping for %s: %v", agentConn.agentID, err)
	}
	log.Printf("[RawQUIC] Pong from %s: ping_id=%s, latency=%dms",
		agentConn.agentID, pong.PingID, latencyMs)
}

// robotStateToString converts RobotState enum to string
func robotStateToString(state int32) string {
	switch state {
	case 0:
		return "unknown"
	case 1:
		return "idle"
	case 2:
		return "executing"
	case 3:
		return "error"
	case 4:
		return "charging"
	case 5:
		return "manual"
	case 6:
		return "emergency"
	default:
		return "unknown"
	}
}

// ============================================================
// ServerMessage Builders - Outbound Messages to Agent
// ============================================================

// ExecuteCommandReq represents a command execution request
type ExecuteCommandReq struct {
	CommandID       string
	AgentID         string
	TaskID          string
	StepID          string
	ActionType      string
	ActionServer    string
	Params          []byte // JSON-encoded
	TimeoutSec      float32
	DeadlineMs      int64
	StartConditions []db.StartCondition
	DuringStates    []string
	SuccessStates   []string
	FailureStates   []string
}

// SendCommand sends an ExecuteCommand to an agent
func (h *RawQUICHandler) SendCommand(agentID string, cmd *ExecuteCommandReq) error {
	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		return fmt.Errorf("agent %s not connected", agentID)
	}

	// Build ServerMessage with ExecuteCommand
	msgData := h.buildExecuteCommandMessage(cmd)

	// Send via QUIC stream
	return h.sendToAgent(conn, msgData)
}

// SendCancelCommand sends a CancelCommand to an agent
// In 1:1 model, targetAgentID is the same as agentID (kept for API compatibility).
func (h *RawQUICHandler) SendCancelCommand(agentID, commandID, targetAgentID, taskID, reason string) error {
	log.Printf("[QUIC] SendCancelCommand: agent=%s, task=%s, reason=%s", agentID, taskID, reason)

	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		log.Printf("[QUIC] SendCancelCommand failed: agent %s not connected", agentID)
		return fmt.Errorf("agent %s not connected", agentID)
	}

	msgData := h.buildCancelCommandMessage(commandID, targetAgentID, taskID, reason)
	log.Printf("[QUIC] SendCancelCommand: sending %d bytes to agent %s", len(msgData), agentID)
	err := h.sendToAgent(conn, msgData)
	if err != nil {
		log.Printf("[QUIC] SendCancelCommand failed to send: %v", err)
		return err
	}
	log.Printf("[QUIC] SendCancelCommand: sent successfully to agent %s", agentID)
	return nil
}

// SendPing sends a ping request to an agent
func (h *RawQUICHandler) SendPing(agentID, pingID string) error {
	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		return fmt.Errorf("agent %s not connected", agentID)
	}

	msgData := h.buildPingMessage(pingID)
	return h.sendToAgent(conn, msgData)
}

// SendCommandAndWait sends a command and waits for result
func (h *RawQUICHandler) SendCommandAndWait(ctx context.Context, agentID string, cmd *ExecuteCommandReq, timeout time.Duration) (*ActionResult, error) {
	// Create pending command (using PendingCommand from handlers.go)
	resultChan := make(chan *ActionResult, 1)
	pending := &PendingCommand{
		CommandID:  cmd.CommandID,
		AgentID:    cmd.AgentID,
		TaskID:     cmd.TaskID,
		StepID:     cmd.StepID,
		SentAt:     time.Now(),
		TimeoutAt:  time.Now().Add(timeout),
		ResultChan: resultChan,
	}

	h.pendingMu.Lock()
	h.pendingCommands[cmd.CommandID] = pending
	h.pendingMu.Unlock()

	// Send command
	if err := h.SendCommand(agentID, cmd); err != nil {
		h.pendingMu.Lock()
		delete(h.pendingCommands, cmd.CommandID)
		h.pendingMu.Unlock()
		return nil, err
	}

	// Wait for result
	select {
	case result := <-resultChan:
		return result, nil
	case <-time.After(timeout):
		h.pendingMu.Lock()
		delete(h.pendingCommands, cmd.CommandID)
		h.pendingMu.Unlock()
		return nil, fmt.Errorf("command %s timed out", cmd.CommandID)
	case <-ctx.Done():
		h.pendingMu.Lock()
		delete(h.pendingCommands, cmd.CommandID)
		h.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// RegisterResultCallback registers a callback for action results
func (h *RawQUICHandler) RegisterResultCallback(commandID string, callback CommandCallback) {
	h.callbackMu.Lock()
	defer h.callbackMu.Unlock()
	h.resultCallbacks[commandID] = callback
}

// sendToAgent sends raw data to an agent via QUIC
func (h *RawQUICHandler) sendToAgent(conn *agentConnection, data []byte) error {
	// Open a new stream for this message
	stream, err := conn.quicConn.OpenStreamSync(h.ctx)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Write length-prefixed message
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	if _, err := stream.Write(lenBuf); err != nil {
		return fmt.Errorf("failed to write length: %w", err)
	}

	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

// buildExecuteCommandMessage builds a ServerMessage with ExecuteCommand
func (h *RawQUICHandler) buildExecuteCommandMessage(cmd *ExecuteCommandReq) []byte {
	// Build ExecuteCommand protobuf
	var execCmd []byte

	// Field 1: command_id
	execCmd = protowire.AppendTag(execCmd, 1, protowire.BytesType)
	execCmd = protowire.AppendString(execCmd, cmd.CommandID)

	// Field 2: agent_id
	execCmd = protowire.AppendTag(execCmd, 2, protowire.BytesType)
	execCmd = protowire.AppendString(execCmd, cmd.AgentID)

	// Field 3: task_id
	if cmd.TaskID != "" {
		execCmd = protowire.AppendTag(execCmd, 3, protowire.BytesType)
		execCmd = protowire.AppendString(execCmd, cmd.TaskID)
	}

	// Field 4: step_id
	if cmd.StepID != "" {
		execCmd = protowire.AppendTag(execCmd, 4, protowire.BytesType)
		execCmd = protowire.AppendString(execCmd, cmd.StepID)
	}

	// Field 5: action_type
	execCmd = protowire.AppendTag(execCmd, 5, protowire.BytesType)
	execCmd = protowire.AppendString(execCmd, cmd.ActionType)

	// Field 6: action_server
	execCmd = protowire.AppendTag(execCmd, 6, protowire.BytesType)
	execCmd = protowire.AppendString(execCmd, cmd.ActionServer)

	// Field 7: params (bytes)
	if len(cmd.Params) > 0 {
		execCmd = protowire.AppendTag(execCmd, 7, protowire.BytesType)
		execCmd = protowire.AppendBytes(execCmd, cmd.Params)
	}

	// Field 8: timeout_sec (float)
	if cmd.TimeoutSec > 0 {
		execCmd = protowire.AppendTag(execCmd, 8, protowire.Fixed32Type)
		execCmd = protowire.AppendFixed32(execCmd, float32ToBits(cmd.TimeoutSec))
	}

	// Field 9: deadline_ms
	if cmd.DeadlineMs > 0 {
		execCmd = protowire.AppendTag(execCmd, 9, protowire.VarintType)
		execCmd = protowire.AppendVarint(execCmd, uint64(cmd.DeadlineMs))
	}

	// Field 10: start_conditions (repeated message)
	for _, cond := range cmd.StartConditions {
		var condMsg []byte
		if cond.ID != "" {
			condMsg = protowire.AppendTag(condMsg, 1, protowire.BytesType)
			condMsg = protowire.AppendString(condMsg, cond.ID)
		}
		if cond.Operator != "" {
			condMsg = protowire.AppendTag(condMsg, 2, protowire.BytesType)
			condMsg = protowire.AppendString(condMsg, cond.Operator)
		}
		if cond.Quantifier != "" {
			condMsg = protowire.AppendTag(condMsg, 3, protowire.BytesType)
			condMsg = protowire.AppendString(condMsg, cond.Quantifier)
		}
		if cond.TargetType != "" {
			condMsg = protowire.AppendTag(condMsg, 4, protowire.BytesType)
			condMsg = protowire.AppendString(condMsg, cond.TargetType)
		}
		if cond.AgentID != "" {
			condMsg = protowire.AppendTag(condMsg, 6, protowire.BytesType)
			condMsg = protowire.AppendString(condMsg, cond.AgentID)
		}
		if cond.State != "" {
			condMsg = protowire.AppendTag(condMsg, 7, protowire.BytesType)
			condMsg = protowire.AppendString(condMsg, cond.State)
		}
		if cond.StateOperator != "" {
			condMsg = protowire.AppendTag(condMsg, 8, protowire.BytesType)
			condMsg = protowire.AppendString(condMsg, cond.StateOperator)
		}
		for _, state := range cond.AllowedStates {
			if state == "" {
				continue
			}
			condMsg = protowire.AppendTag(condMsg, 9, protowire.BytesType)
			condMsg = protowire.AppendString(condMsg, state)
		}
		if cond.MaxStalenessSec > 0 {
			condMsg = protowire.AppendTag(condMsg, 10, protowire.Fixed32Type)
			condMsg = protowire.AppendFixed32(condMsg, float32ToBits(float32(cond.MaxStalenessSec)))
		}
		if cond.RequireOnline {
			condMsg = protowire.AppendTag(condMsg, 11, protowire.VarintType)
			condMsg = protowire.AppendVarint(condMsg, 1)
		}
		if cond.Message != "" {
			condMsg = protowire.AppendTag(condMsg, 12, protowire.BytesType)
			condMsg = protowire.AppendString(condMsg, cond.Message)
		}
		execCmd = protowire.AppendTag(execCmd, 10, protowire.BytesType)
		execCmd = protowire.AppendBytes(execCmd, condMsg)
	}

	// Field 11: during_states
	for _, state := range cmd.DuringStates {
		if state == "" {
			continue
		}
		execCmd = protowire.AppendTag(execCmd, 11, protowire.BytesType)
		execCmd = protowire.AppendString(execCmd, state)
	}

	// Field 12: success_states
	for _, state := range cmd.SuccessStates {
		if state == "" {
			continue
		}
		execCmd = protowire.AppendTag(execCmd, 12, protowire.BytesType)
		execCmd = protowire.AppendString(execCmd, state)
	}

	// Field 13: failure_states
	for _, state := range cmd.FailureStates {
		if state == "" {
			continue
		}
		execCmd = protowire.AppendTag(execCmd, 13, protowire.BytesType)
		execCmd = protowire.AppendString(execCmd, state)
	}

	// Build ServerMessage wrapper
	return h.buildServerMessage(cmd.CommandID, 10, execCmd) // field 10 = execute
}

// buildCancelCommandMessage builds a ServerMessage with CancelCommand
func (h *RawQUICHandler) buildCancelCommandMessage(commandID, agentID, taskID, reason string) []byte {
	var cancelCmd []byte

	// Field 1: command_id
	cancelCmd = protowire.AppendTag(cancelCmd, 1, protowire.BytesType)
	cancelCmd = protowire.AppendString(cancelCmd, commandID)

	// Field 2: agent_id
	cancelCmd = protowire.AppendTag(cancelCmd, 2, protowire.BytesType)
	cancelCmd = protowire.AppendString(cancelCmd, agentID)

	if taskID != "" {
		cancelCmd = protowire.AppendTag(cancelCmd, 3, protowire.BytesType)
		cancelCmd = protowire.AppendString(cancelCmd, taskID)
	}

	if reason != "" {
		cancelCmd = protowire.AppendTag(cancelCmd, 4, protowire.BytesType)
		cancelCmd = protowire.AppendString(cancelCmd, reason)
	}

	return h.buildServerMessage(commandID, 11, cancelCmd) // field 11 = cancel
}

// buildPingMessage builds a ServerMessage with PingRequest
func (h *RawQUICHandler) buildPingMessage(pingID string) []byte {
	var ping []byte

	ping = protowire.AppendTag(ping, 1, protowire.BytesType)
	ping = protowire.AppendString(ping, pingID)

	ping = protowire.AppendTag(ping, 2, protowire.VarintType)
	ping = protowire.AppendVarint(ping, uint64(time.Now().UnixMilli()))

	return h.buildServerMessage(pingID, 16, ping) // field 16 = ping
}

// buildServerMessage builds the ServerMessage wrapper
func (h *RawQUICHandler) buildServerMessage(messageID string, payloadField protowire.Number, payloadData []byte) []byte {
	var msg []byte

	// Field 1: message_id
	msg = protowire.AppendTag(msg, 1, protowire.BytesType)
	msg = protowire.AppendString(msg, messageID)

	// Field 3: timestamp_ms
	msg = protowire.AppendTag(msg, 3, protowire.VarintType)
	msg = protowire.AppendVarint(msg, uint64(time.Now().UnixMilli()))

	// Payload field (10-16)
	msg = protowire.AppendTag(msg, payloadField, protowire.BytesType)
	msg = protowire.AppendBytes(msg, payloadData)

	return msg
}

// ============================================================
// Agent-Driven Task Execution
// ============================================================

// SendStartTask sends a StartTaskCommand to an agent for agent-driven execution
func (h *RawQUICHandler) SendStartTask(agentID, taskID, graphID, robotID string, params map[string]string) error {
	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		return fmt.Errorf("agent %s not connected", agentID)
	}

	msgData := h.buildStartTaskMessage(taskID, graphID, robotID, params)
	return h.sendToAgent(conn, msgData)
}

// buildStartTaskMessage builds a ServerMessage with StartTaskCommand
// StartTaskCommand is field 18 in ServerMessage oneof
//
//	message StartTaskCommand {
//	  string task_id = 1;
//	  string graph_id = 2;
//	  string robot_id = 3;
//	  map<string, string> params = 4;
//	}
func (h *RawQUICHandler) buildStartTaskMessage(taskID, graphID, robotID string, params map[string]string) []byte {
	var cmd []byte

	// Field 1: task_id
	cmd = protowire.AppendTag(cmd, 1, protowire.BytesType)
	cmd = protowire.AppendString(cmd, taskID)

	// Field 2: graph_id
	cmd = protowire.AppendTag(cmd, 2, protowire.BytesType)
	cmd = protowire.AppendString(cmd, graphID)

	// Field 3: robot_id
	cmd = protowire.AppendTag(cmd, 3, protowire.BytesType)
	cmd = protowire.AppendString(cmd, robotID)

	// Field 4: params (map<string, string>)
	// In protobuf, map<K,V> is encoded as repeated message { K key = 1; V value = 2; }
	for key, value := range params {
		var entry []byte
		entry = protowire.AppendTag(entry, 1, protowire.BytesType)
		entry = protowire.AppendString(entry, key)
		entry = protowire.AppendTag(entry, 2, protowire.BytesType)
		entry = protowire.AppendString(entry, value)

		cmd = protowire.AppendTag(cmd, 4, protowire.BytesType)
		cmd = protowire.AppendBytes(cmd, entry)
	}

	// Build ServerMessage wrapper with field 18 (start_task)
	correlationID := fmt.Sprintf("task-%s", taskID)
	return h.buildServerMessage(correlationID, 18, cmd)
}

// Helper for float32 to bits conversion
func float32ToBits(f float32) uint32 {
	return *(*uint32)(unsafe.Pointer(&f))
}

// GetAgentForRobot finds which agent manages a robot
// In 1:1 model, agent_id = robot_id, so this just checks if the agent exists
func (h *RawQUICHandler) GetAgentForRobot(agentID string) (string, bool) {
	h.connMu.RLock()
	defer h.connMu.RUnlock()

	// In 1:1 model, robot_id = agent_id
	if _, exists := h.connections[agentID]; exists {
		return agentID, true
	}
	return "", false
}

// ============================================================
// Graph Update Notifications (real-time sync)
// ============================================================

// SendGraphUpdateNotification sends a graph update notification to an agent
// Used for real-time sync when graphs are modified, deleted, or unassigned
// action: "MODIFIED" (with graphJSON), "DELETED", "UNASSIGNED"
func (h *RawQUICHandler) SendGraphUpdateNotification(agentID, graphID string, version int, action string, graphJSON []byte) error {
	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		log.Printf("[RawQUIC] SendGraphUpdateNotification: agent %s not connected, skipping", agentID)
		return nil // Not an error - agent may be offline
	}

	// Build GraphUpdateNotification message
	// message GraphUpdateNotification {
	//   string command_id = 1;
	//   string agent_id = 2;
	//   string action_graph_id = 3;
	//   GraphUpdateAction action = 4;
	//   int32 new_version = 5;
	//   bytes graph_json = 6;
	// }
	var notification []byte

	commandID := fmt.Sprintf("graph-update-%s-%d", graphID, time.Now().UnixNano())

	// Field 1: command_id
	notification = protowire.AppendTag(notification, 1, protowire.BytesType)
	notification = protowire.AppendString(notification, commandID)

	// Field 2: agent_id
	notification = protowire.AppendTag(notification, 2, protowire.BytesType)
	notification = protowire.AppendString(notification, agentID)

	// Field 3: action_graph_id
	notification = protowire.AppendTag(notification, 3, protowire.BytesType)
	notification = protowire.AppendString(notification, graphID)

	// Field 4: action (enum GraphUpdateAction)
	var actionEnum uint64 = 0 // UPDATE_UNKNOWN
	switch action {
	case "MODIFIED":
		actionEnum = 1
	case "DELETED":
		actionEnum = 2
	case "UNASSIGNED":
		actionEnum = 3
	}
	notification = protowire.AppendTag(notification, 4, protowire.VarintType)
	notification = protowire.AppendVarint(notification, actionEnum)

	// Field 5: new_version
	if version > 0 {
		notification = protowire.AppendTag(notification, 5, protowire.VarintType)
		notification = protowire.AppendVarint(notification, uint64(version))
	}

	// Field 6: graph_json (only for MODIFIED action)
	if action == "MODIFIED" && len(graphJSON) > 0 {
		notification = protowire.AppendTag(notification, 6, protowire.BytesType)
		notification = protowire.AppendBytes(notification, graphJSON)
	}

	// Build ServerMessage wrapper with field 18 (graph_update)
	msgData := h.buildServerMessage(commandID, 18, notification)

	if err := h.sendToAgent(conn, msgData); err != nil {
		log.Printf("[RawQUIC] SendGraphUpdateNotification failed for agent %s: %v", agentID, err)
		return err
	}

	log.Printf("[RawQUIC] Sent graph update notification to agent %s: graph=%s, action=%s, version=%d",
		agentID, graphID, action, version)
	return nil
}

// SendDeleteGraphCommand sends a delete graph command to an agent
// Used when a graph is unassigned or deleted
func (h *RawQUICHandler) SendDeleteGraphCommand(agentID, graphID, reason string) error {
	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		log.Printf("[RawQUIC] SendDeleteGraphCommand: agent %s not connected, skipping", agentID)
		return nil
	}

	// Build DeleteGraphCommand message
	// message DeleteGraphCommand {
	//   string command_id = 1;
	//   string agent_id = 2;
	//   string action_graph_id = 3;
	//   string reason = 4;
	// }
	var cmd []byte

	commandID := fmt.Sprintf("delete-graph-%s-%d", graphID, time.Now().UnixNano())

	// Field 1: command_id
	cmd = protowire.AppendTag(cmd, 1, protowire.BytesType)
	cmd = protowire.AppendString(cmd, commandID)

	// Field 2: agent_id
	cmd = protowire.AppendTag(cmd, 2, protowire.BytesType)
	cmd = protowire.AppendString(cmd, agentID)

	// Field 3: action_graph_id
	cmd = protowire.AppendTag(cmd, 3, protowire.BytesType)
	cmd = protowire.AppendString(cmd, graphID)

	// Field 4: reason
	if reason != "" {
		cmd = protowire.AppendTag(cmd, 4, protowire.BytesType)
		cmd = protowire.AppendString(cmd, reason)
	}

	// Build ServerMessage wrapper with field 17 (delete_graph)
	msgData := h.buildServerMessage(commandID, 17, cmd)

	if err := h.sendToAgent(conn, msgData); err != nil {
		log.Printf("[RawQUIC] SendDeleteGraphCommand failed for agent %s: %v", agentID, err)
		return err
	}

	log.Printf("[RawQUIC] Sent delete graph command to agent %s: graph=%s, reason=%s",
		agentID, graphID, reason)
	return nil
}

// ============================================================
// Fleet State Broadcasting (for cross-agent coordination)
// ============================================================

// StartFleetStateBroadcast starts periodic fleet state broadcasts to all agents
func (h *RawQUICHandler) StartFleetStateBroadcast(interval time.Duration) {
	log.Printf("[RawQUIC] Starting fleet state broadcast (interval: %v)", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			log.Println("[RawQUIC] Fleet state broadcast stopped")
			return
		case <-ticker.C:
			h.broadcastFleetState()
		}
	}
}

// broadcastFleetState sends fleet state to all connected agents
func (h *RawQUICHandler) broadcastFleetState() {
	snapshot := h.stateManager.GetSnapshot()
	now := time.Now()

	// Build FleetStateBroadcast message
	// message FleetStateBroadcast {
	//   int64 timestamp_ms = 1;
	//   repeated AgentStateSnapshot agents = 2;
	// }
	var fleetState []byte

	// Field 1: timestamp_ms
	fleetState = protowire.AppendTag(fleetState, 1, protowire.VarintType)
	fleetState = protowire.AppendVarint(fleetState, uint64(now.UnixMilli()))

	// Field 2: agents (repeated)
	for _, robot := range snapshot.Robots {
		agentEntry := h.buildAgentStateSnapshot(robot, now)
		fleetState = protowire.AppendTag(fleetState, 2, protowire.BytesType)
		fleetState = protowire.AppendBytes(fleetState, agentEntry)
	}

	// Wrap in ServerMessage with fleet_state = 17
	msgID := fmt.Sprintf("fleet-state-%d", now.UnixNano())
	serverMsg := h.buildServerMessage(msgID, 17, fleetState) // fleet_state field = 17

	// Broadcast to all connected agents
	h.connMu.RLock()
	conns := make([]*agentConnection, 0, len(h.connections))
	for _, conn := range h.connections {
		if conn.registered {
			conns = append(conns, conn)
		}
	}
	h.connMu.RUnlock()

	for _, conn := range conns {
		if err := h.sendToAgent(conn, serverMsg); err != nil {
			log.Printf("[RawQUIC] Failed to broadcast fleet state to %s: %v", conn.agentID, err)
		}
	}
}

// buildAgentStateSnapshot builds protobuf for AgentStateSnapshot
func (h *RawQUICHandler) buildAgentStateSnapshot(robot *state.RobotState, now time.Time) []byte {
	// message AgentStateSnapshot {
	//   string agent_id = 1;
	//   string agent_name = 2;
	//   string state = 5;
	//   bool is_online = 6;
	//   bool is_executing = 7;
	//   float staleness_sec = 8;
	// }
	var msg []byte

	// Field 1: agent_id
	msg = protowire.AppendTag(msg, 1, protowire.BytesType)
	msg = protowire.AppendString(msg, robot.ID)

	// Field 2: agent_name
	msg = protowire.AppendTag(msg, 2, protowire.BytesType)
	msg = protowire.AppendString(msg, robot.Name)

	// Field 5: state
	msg = protowire.AppendTag(msg, 5, protowire.BytesType)
	msg = protowire.AppendString(msg, robot.CurrentState)

	// Field 6: is_online
	msg = protowire.AppendTag(msg, 6, protowire.VarintType)
	if robot.IsOnline {
		msg = protowire.AppendVarint(msg, 1)
	} else {
		msg = protowire.AppendVarint(msg, 0)
	}

	// Field 7: is_executing
	msg = protowire.AppendTag(msg, 7, protowire.VarintType)
	if robot.IsExecuting {
		msg = protowire.AppendVarint(msg, 1)
	} else {
		msg = protowire.AppendVarint(msg, 0)
	}

	// Field 8: staleness_sec (float)
	stalenessSec := float32(now.Sub(robot.LastSeen).Seconds())
	msg = protowire.AppendTag(msg, 8, protowire.Fixed32Type)
	msg = protowire.AppendFixed32(msg, float32ToBits(stalenessSec))

	return msg
}

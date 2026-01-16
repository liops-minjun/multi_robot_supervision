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
	"net"
	"strings"
	"sync"
	"sync/atomic"
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
	graphOverrides map[string]*graphOverrideState
	graphOverrideMu sync.Mutex

	// Ping interval for latency tracking
	pingInterval time.Duration

	ctx    context.Context
	cancel context.CancelFunc
}

// Note: PendingCommand is defined in handlers.go

// WebSocketBroadcaster interface for broadcasting to frontend
// This abstracts the WebSocketHub from api package to avoid circular imports
type WebSocketBroadcaster interface {
	BroadcastAgentUpdate(agentID string, status string)
	BroadcastCapabilityUpdate(agentID string, capabilities interface{})
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
	commandStream   quic.Stream           // Primary stream for Server→Agent commands
	sendChan        chan *OutboundCommand // Queue for outgoing commands
	sendDone        chan struct{}         // Signal to stop sender goroutine
	useHeartbeatRtt atomic.Bool           // Prefer latency from heartbeat stats
	// In 1:1 model, agent_id = robot_id, so no separate robotIDs field needed
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

// Start starts listening for raw QUIC connections
func (h *RawQUICHandler) Start(addr string, tlsConfig *tls.Config) error {
	// Configure TLS for raw QUIC (different ALPN from gRPC)
	rawTLSConfig := tlsConfig.Clone()
	rawTLSConfig.NextProtos = []string{"fleet-agent-raw", "h3"}

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

	log.Printf("[RawQUIC] Listening on %s (ALPN: fleet-agent-raw)", addr)

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
				if conn != nil && conn.registered && !conn.useHeartbeatRtt.Load() {
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
	Pong                   *PongResponseMsg           // field 16
	ConfigAck              *ConfigUpdateAckMsg        // field 17
	CapabilityRegistration *CapabilityRegistrationMsg // field 18
	TaskLog                *TaskLogMsg                // field 19
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
		if err == nil && regReq.AgentID != "" {
			log.Printf("[RawQUIC] Parsed as RegisterAgentRequest: %s", regReq.AgentID)
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
				h.handleHeartbeat(agentConn, agentMsg.Heartbeat)
				handled = true
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
				log.Printf("[RawQUIC] Found CapabilityRegistration, handling...")
				h.handleCapabilityRegistration(agentConn, agentMsg.AgentID, agentMsg.CapabilityRegistration)
				handled = true
			}

			if agentMsg.TaskLog != nil {
				h.handleTaskLog(agentConn, agentMsg.TaskLog)
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
	// Server-assigned ID logic
	effectiveAgentID := req.AgentID
	idWasAssigned := false

	if req.AgentID == "" {
		// Agent requesting server-assigned ID
		// First, try to recover ID from hardware fingerprint
		if req.HardwareFingerprint != "" {
			existingAgent, err := h.repo.GetAgentByHardwareFingerprint(req.HardwareFingerprint)
			if err == nil && existingAgent != nil {
				// Found existing agent with same fingerprint
				effectiveAgentID = existingAgent.ID
				idWasAssigned = true
				log.Printf("[RawQUIC] Recovered agent ID %s from hardware fingerprint", effectiveAgentID)
			}
		}

		// If no existing ID found, generate new UUID
		if effectiveAgentID == "" {
			effectiveAgentID = uuid.New().String()
			idWasAssigned = true
			log.Printf("[RawQUIC] Generated new agent ID: %s", effectiveAgentID)
		}
	}

	log.Printf("[RawQUIC] Agent registration: %s (%s) assigned=%v", effectiveAgentID, req.Name, idWasAssigned)

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
		req.Name,
		effectiveAgentID,
		"idle",
	)

	// In 1:1 model, agent_id = robot_id, so no separate robotIDs needed
	h.stateManager.RegisterAgent(effectiveAgentID, req.Name, req.Namespace)

	// Update database - Agent
	agent, _ := h.repo.GetAgent(effectiveAgentID)
	if agent == nil {
		// Create new agent
		agent = &db.Agent{
			ID:               effectiveAgentID,
			Name:             req.Name,
			Namespace:        req.Namespace,
			Status:           "online",
			AssignedByServer: idWasAssigned,
			CreatedAt:        time.Now(),
			LastSeen:         sql.NullTime{Time: time.Now(), Valid: true},
		}
		if req.HardwareFingerprint != "" {
			agent.HardwareFingerprint = sql.NullString{String: req.HardwareFingerprint, Valid: true}
		}
		h.repo.CreateAgent(agent)
	} else {
		// Update existing agent
		h.repo.UpdateAgentStatus(effectiveAgentID, "online", agentConn.quicConn.RemoteAddr().String())
		// Update hardware fingerprint if provided and not yet set
		if req.HardwareFingerprint != "" && !agent.HardwareFingerprint.Valid {
			agent.HardwareFingerprint = sql.NullString{String: req.HardwareFingerprint, Valid: true}
			h.repo.CreateOrUpdateAgent(agent)
		}
	}

	// Broadcast to frontend via WebSocket
	if h.wsHub != nil {
		h.wsHub.BroadcastAgentUpdate(effectiveAgentID, "online")
	}

	// Send response (length-prefixed protobuf RegisterAgentResponse)
	h.sendRegisterResponse(stream, true, "", effectiveAgentID, idWasAssigned)

	log.Printf("[RawQUIC] Agent %s registered (1:1 model, assigned=%v)", effectiveAgentID, idWasAssigned)
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

// DeployCanonicalGraph deploys an action graph to an agent via QUIC
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
func (h *RawQUICHandler) buildDeployGraphMessage(correlationID string, graphJSON []byte) []byte {
	// Build DeployGraphRequest
	// message DeployGraphRequest {
	//   string correlation_id = 1;
	//   ActionGraph graph = 2;
	//   bool force = 3;
	// }
	var deployReq []byte

	// Field 1: correlation_id
	deployReq = protowire.AppendTag(deployReq, 1, protowire.BytesType)
	deployReq = protowire.AppendString(deployReq, correlationID)

	// Field 2: graph - embed canonical JSON in ActionGraph.graph_json (field 20)
	var graphMsg []byte
	graphMsg = protowire.AppendTag(graphMsg, 20, protowire.BytesType) // graph_json field
	graphMsg = protowire.AppendString(graphMsg, string(graphJSON))

	deployReq = protowire.AppendTag(deployReq, 2, protowire.BytesType)
	deployReq = protowire.AppendBytes(deployReq, graphMsg)

	// Field 3: force = true
	deployReq = protowire.AppendTag(deployReq, 3, protowire.VarintType)
	deployReq = protowire.AppendVarint(deployReq, 1)

	// Build ServerMessage wrapper (timestamp_ms field = 3)
	return h.buildServerMessage(correlationID, 12, deployReq)
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

	// Convert to db.AgentCapability (agent-based, not robot-based)
	dbCaps := make([]db.AgentCapability, 0, len(reg.Capabilities))
	for _, cap := range reg.Capabilities {
		// Generate ID using agent_id + action_server (unique per server per agent)
		id := fmt.Sprintf("%s_%s", agentID, cap.ActionServer)

		// Determine status based on availability
		status := "idle"
		if !cap.IsAvailable {
			status = "offline"
		}

		dbCap := db.AgentCapability{
			ID:           id,
			AgentID:      agentID,
			ActionType:   cap.ActionType,
			ActionServer: cap.ActionServer,
			IsAvailable:  cap.IsAvailable, // Use the actual availability from the agent
			Status:       status,
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

		log.Printf("[RawQUIC]   - %s at %s (available: %v, criteria: %v)",
			cap.ActionType, cap.ActionServer, cap.IsAvailable, cap.SuccessCriteria != nil)
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
			wsCaps = append(wsCaps, map[string]interface{}{
				"action_type":   cap.ActionType,
				"action_server": cap.ActionServer,
				"package":       cap.Package,
				"action_name":   cap.ActionName,
				"is_available":  cap.IsAvailable,
				"status":        statusFromAvailability(cap.IsAvailable),
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
	if hb.HasNetworkLatencyUs {
		latency := time.Duration(hb.NetworkLatencyUs) * time.Microsecond
		if err := h.stateManager.UpdateAgentPing(agentConn.agentID, latency); err != nil {
			log.Printf("[RawQUIC] Failed to update agent network latency for %s: %v", agentConn.agentID, err)
		}
		agentConn.useHeartbeatRtt.Store(true)
	} else if hb.HasNetworkLatency {
		latency := time.Duration(hb.NetworkLatencyMs) * time.Millisecond
		if err := h.stateManager.UpdateAgentPing(agentConn.agentID, latency); err != nil {
			log.Printf("[RawQUIC] Failed to update agent network latency for %s: %v", agentConn.agentID, err)
		}
		agentConn.useHeartbeatRtt.Store(true)
	}

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

	// Update execution state, but only if server is not actively managing a task
	// This prevents agent heartbeat from overwriting server-controlled execution state
	robotState, robotExists := h.stateManager.GetRobotState(agentConn.agentID)
	if robotExists && robotState.IsExecuting && robotState.CurrentTaskID != "" {
		// Server is managing this task - don't let heartbeat override execution state
		// Only allow heartbeat to update if it's reporting the same task
		if hb.CurrentTaskID != robotState.CurrentTaskID {
			// Different task or no task from heartbeat - skip execution update to preserve server state
			log.Printf("[RawQUIC] Skipping heartbeat execution update: server task=%s running (heartbeat task=%s)",
				robotState.CurrentTaskID, hb.CurrentTaskID)
		} else {
			// Same task - allow update
			if err := h.stateManager.UpdateRobotExecution(agentConn.agentID, hb.IsExecuting, hb.CurrentTaskID, hb.CurrentStepID); err != nil {
				log.Printf("[RawQUIC] Failed to update agent execution for %s: %v", agentConn.agentID, err)
			}
		}
	} else {
		// No server-managed task running
		// If agent reports is_executing=false, don't accept stale task_id/step_id
		// This prevents stale heartbeat values from restoring cleared state
		if !hb.IsExecuting && hb.CurrentTaskID != "" {
			// Agent is not executing but reports a task_id - this is stale data, ignore it
			// Just update is_executing to false with empty task/step
			if err := h.stateManager.UpdateRobotExecution(agentConn.agentID, false, "", ""); err != nil {
				log.Printf("[RawQUIC] Failed to update agent execution for %s: %v", agentConn.agentID, err)
			}
		} else {
			// Use heartbeat values as-is
			if err := h.stateManager.UpdateRobotExecution(agentConn.agentID, hb.IsExecuting, hb.CurrentTaskID, hb.CurrentStepID); err != nil {
				log.Printf("[RawQUIC] Failed to update agent execution for %s: %v", agentConn.agentID, err)
			}
		}
	}

	// Update last seen
	agentConn.lastSeen = time.Now()
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

	steps, err := h.repo.GetActionGraphSteps(graphID)
	if err != nil {
		log.Printf("[RawQUIC] Failed to load graph steps for %s: %v", graphID, err)
		return
	}

	var step *db.ActionGraphStep
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
		return
	}

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
		return
	}

	select {
	case ch <- result:
	default:
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
	if agentConn.useHeartbeatRtt.Load() {
		return
	}
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
	h.connMu.RLock()
	conn, exists := h.connections[agentID]
	h.connMu.RUnlock()

	if !exists || !conn.registered {
		return fmt.Errorf("agent %s not connected", agentID)
	}

	msgData := h.buildCancelCommandMessage(commandID, targetAgentID, taskID, reason)
	return h.sendToAgent(conn, msgData)
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

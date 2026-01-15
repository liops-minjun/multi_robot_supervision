package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/state"

	"github.com/google/uuid"
)

// FleetControlHandler implements the FleetControl gRPC service
type FleetControlHandler struct {
	stateManager *state.GlobalStateManager
	repo         *db.Repository
	server       *Server

	// Command handlers
	commandHandlers map[string]CommandHandler
	handlerMu       sync.RWMutex

	// Pending command tracking
	pendingCommands map[string]*PendingCommand
	pendingMu       sync.RWMutex

	// Fleet state broadcast control
	broadcastStop chan struct{}
	broadcastMu   sync.Mutex
}

// CommandHandler is a callback for handling specific command types
type CommandHandler func(ctx context.Context, agentID string, result *ActionResult) error

// PendingCommand tracks an outstanding command
type PendingCommand struct {
	CommandID   string
	AgentID     string
	TaskID      string
	StepID      string
	SentAt      time.Time
	TimeoutAt   time.Time
	ResultChan  chan *ActionResult
}

// ActionResult represents the result of an action execution
type ActionResult struct {
	CommandID     string
	AgentID       string
	TaskID        string
	StepID        string
	Status        ActionStatus
	Result        map[string]interface{}
	Error         string
	StartedAtMs   int64
	CompletedAtMs int64
}

// ActionStatus represents the status of an action
type ActionStatus int32

const (
	ActionStatusUnknown   ActionStatus = 0
	ActionStatusSucceeded ActionStatus = 1
	ActionStatusFailed    ActionStatus = 2
	ActionStatusCancelled ActionStatus = 3
	ActionStatusTimeout   ActionStatus = 4
	ActionStatusRejected  ActionStatus = 5
)

// ExecuteCommand represents a command to execute on an agent
type ExecuteCommand struct {
	CommandID    string
	AgentID      string
	TaskID       string
	StepID       string
	ActionType   string
	ActionServer string
	Params       map[string]interface{}
	TimeoutSec   float32
	DeadlineMs   int64
	DuringStates  []string
	SuccessStates []string
	FailureStates []string
}

// NewFleetControlHandler creates a new handler
func NewFleetControlHandler(stateManager *state.GlobalStateManager, repo *db.Repository, server *Server) *FleetControlHandler {
	return &FleetControlHandler{
		stateManager:    stateManager,
		repo:            repo,
		server:          server,
		commandHandlers: make(map[string]CommandHandler),
		pendingCommands: make(map[string]*PendingCommand),
	}
}

// ============================================================
// Agent Registration
// ============================================================

// RegisterAgentRequest represents an agent registration request (1:1 model)
type RegisterAgentRequest struct {
	AgentID       string
	Name          string
	Namespace     string // ROS namespace
	ClientVersion string
}

// RegisterAgentResponse represents a registration response
type RegisterAgentResponse struct {
	Success      bool
	Error        string
	Config       *AgentConfigMsg
	ServerTimeMs int64
}

// AgentConfigMsg represents agent configuration to send to agent
type AgentConfigMsg struct {
	AgentID           string
	StateDefinition   []byte
	ActionDefinitions []byte
}

// RegisterAgent handles agent registration (1:1 model: agent_id = robot_id)
func (h *FleetControlHandler) RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*RegisterAgentResponse, error) {
	log.Printf("Agent registration request: %s (%s)", req.AgentID, req.Name)

	// Get or create agent in database
	agent, err := h.repo.GetAgent(req.AgentID)
	if err != nil {
		return &RegisterAgentResponse{
			Success: false,
			Error:   fmt.Sprintf("database error: %v", err),
		}, nil
	}

	// Register robot in state manager (agent_id = robot_id in 1:1 model)
	h.stateManager.RegisterRobot(
		req.AgentID,
		req.Name,
		req.AgentID,
		"idle",
	)

	// Register agent in state manager (1:1 model: agent_id = robot_id)
	h.stateManager.RegisterAgent(req.AgentID, req.Name, req.Namespace)

	// Update agent status in database
	if agent != nil {
		if err := h.repo.UpdateAgentStatus(req.AgentID, "online", ""); err != nil {
			log.Printf("Failed to update agent status: %v", err)
		}
	}

	log.Printf("Agent %s registered (1:1 model)", req.AgentID)

	return &RegisterAgentResponse{
		Success:      true,
		ServerTimeMs: time.Now().UnixMilli(),
	}, nil
}

// ============================================================
// Command Stream Handling
// ============================================================

// AgentMessage represents a message from an agent
type AgentMessage struct {
	AgentID     string
	TimestampMs int64
	Heartbeat   *AgentHeartbeat
	Result      *ActionResult
	Status      *AgentStatusUpdate
}

// AgentHeartbeat represents a heartbeat from an agent (1:1 model)
type AgentHeartbeat struct {
	AgentID       string
	State         string
	IsExecuting   bool
	CurrentAction string
}

// AgentStatusUpdate represents an agent status update
type AgentStatusUpdate struct {
	State     string
	IsOnline  bool
	Message   string
}

// ServerMessage represents a message to an agent
type ServerMessage struct {
	MessageID   string
	Sequence    int64
	TimestampMs int64
	Execute     *ExecuteCommand
	Cancel      *CancelCommand
	Ack         *ServerAck
	Config      *ConfigUpdate
}

// CancelCommand represents a command cancellation
type CancelCommand struct {
	CommandID string
	AgentID   string
	TaskID    string
	Reason    string
}

// ServerAck represents an acknowledgment
type ServerAck struct {
	AckedMessageID string
	Success        bool
	Error          string
}

// ConfigUpdate represents a configuration update
type ConfigUpdate struct {
	AgentID         string
	StateDefinition []byte
	Version         int32
}

// HandleAgentMessage processes a message from an agent
func (h *FleetControlHandler) HandleAgentMessage(agentID string, msg *AgentMessage) (*ServerMessage, error) {
	// Update agent heartbeat
	if err := h.stateManager.UpdateAgentHeartbeat(agentID); err != nil {
		log.Printf("Failed to update agent heartbeat: %v", err)
	}

	// Process heartbeat
	if msg.Heartbeat != nil {
		h.processHeartbeat(agentID, msg.Heartbeat)
	}

	// Process action result
	if msg.Result != nil {
		h.processActionResult(msg.Result)
	}

	// Process status update
	if msg.Status != nil {
		h.processStatusUpdate(agentID, msg.Status)
	}

	// Return acknowledgment
	return &ServerMessage{
		MessageID:   uuid.New().String(),
		TimestampMs: time.Now().UnixMilli(),
		Ack: &ServerAck{
			Success: true,
		},
	}, nil
}

// processHeartbeat updates agent state from heartbeat (1:1 model: agent_id = robot_id)
func (h *FleetControlHandler) processHeartbeat(agentID string, hb *AgentHeartbeat) {
	// Update robot state (agent_id = robot_id in 1:1 model)
	// NOTE: Execution state (taskID, stepID, graphID) is managed by the scheduler.
	// Heartbeat only updates state and lastSeen, NOT execution state.
	if hb.State != "" {
		if err := h.stateManager.UpdateRobotState(agentID, hb.State); err != nil {
			log.Printf("Failed to update robot state: %v", err)
		}
	}
}

// processActionResult handles action completion
func (h *FleetControlHandler) processActionResult(result *ActionResult) {
	log.Printf("Action result: command=%s agent=%s status=%d",
		result.CommandID, result.AgentID, result.Status)

	// Find pending command
	h.pendingMu.Lock()
	pending, exists := h.pendingCommands[result.CommandID]
	if exists {
		delete(h.pendingCommands, result.CommandID)
	}
	h.pendingMu.Unlock()

	if exists && pending.ResultChan != nil {
		select {
		case pending.ResultChan <- result:
		default:
			log.Printf("Result channel full for command %s", result.CommandID)
		}
	}

	// Call registered handler
	h.handlerMu.RLock()
	handler, hasHandler := h.commandHandlers[result.CommandID]
	h.handlerMu.RUnlock()

	if hasHandler {
		go func() {
			if err := handler(context.Background(), result.AgentID, result); err != nil {
				log.Printf("Command handler error: %v", err)
			}
		}()
	}

	// NOTE: Execution state completion is handled by the scheduler.
	// Do NOT call CompleteExecution here - it would clear CurrentGraphID prematurely
	// when a multi-step task moves to the next step.
}

// processStatusUpdate handles agent status changes (1:1 model)
func (h *FleetControlHandler) processStatusUpdate(agentID string, status *AgentStatusUpdate) {
	log.Printf("Agent status update: %s - %s", agentID, status.Message)

	// Update agent online status (agent_id = robot_id in 1:1 model)
	if err := h.stateManager.SetRobotOnline(agentID, status.IsOnline); err != nil {
		log.Printf("Failed to set agent online: %v", err)
	}
}

// ============================================================
// Command Execution
// ============================================================

// SendCommand sends a command to an agent (1:1 model: agent_id = robot_id)
func (h *FleetControlHandler) SendCommand(ctx context.Context, cmd *ExecuteCommand) error {
	// In 1:1 model, agent_id = robot_id, so verify agent exists
	robotState, exists := h.stateManager.GetRobotState(cmd.AgentID)
	if !exists {
		return fmt.Errorf("agent %s not found", cmd.AgentID)
	}

	// Create pending command
	pending := &PendingCommand{
		CommandID:  cmd.CommandID,
		AgentID:    cmd.AgentID,
		TaskID:     cmd.TaskID,
		StepID:     cmd.StepID,
		SentAt:     time.Now(),
		TimeoutAt:  time.Now().Add(time.Duration(cmd.TimeoutSec) * time.Second),
		ResultChan: make(chan *ActionResult, 1),
	}

	h.pendingMu.Lock()
	h.pendingCommands[cmd.CommandID] = pending
	h.pendingMu.Unlock()

	// Marshal params
	paramsJSON, err := json.Marshal(cmd.Params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}

	// Create server message
	msg := &ServerMessage{
		MessageID:   uuid.New().String(),
		TimestampMs: time.Now().UnixMilli(),
		Execute: &ExecuteCommand{
			CommandID:    cmd.CommandID,
			AgentID:      cmd.AgentID,
			TaskID:       cmd.TaskID,
			StepID:       cmd.StepID,
			ActionType:   cmd.ActionType,
			ActionServer: cmd.ActionServer,
			TimeoutSec:   cmd.TimeoutSec,
			DeadlineMs:   cmd.DeadlineMs,
			DuringStates:  cmd.DuringStates,
			SuccessStates: cmd.SuccessStates,
			FailureStates: cmd.FailureStates,
		},
	}

	// Store params as map (for serialization to protobuf)
	msg.Execute.Params = make(map[string]interface{})
	json.Unmarshal(paramsJSON, &msg.Execute.Params)

	// Send to agent (agent_id = robot_id in 1:1 model)
	return h.server.SendToAgent(robotState.AgentID, msg)
}

// SendCommandAndWait sends a command and waits for result
func (h *FleetControlHandler) SendCommandAndWait(ctx context.Context, cmd *ExecuteCommand) (*ActionResult, error) {
	if err := h.SendCommand(ctx, cmd); err != nil {
		return nil, err
	}

	// Get pending command to access result channel
	h.pendingMu.RLock()
	pending, exists := h.pendingCommands[cmd.CommandID]
	h.pendingMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("command %s not tracked", cmd.CommandID)
	}

	// Wait for result with timeout
	timeout := time.Duration(cmd.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	select {
	case result := <-pending.ResultChan:
		return result, nil
	case <-time.After(timeout):
		// Cleanup
		h.pendingMu.Lock()
		delete(h.pendingCommands, cmd.CommandID)
		h.pendingMu.Unlock()
		return nil, fmt.Errorf("command %s timed out", cmd.CommandID)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CancelCommand cancels a running command (1:1 model: agent_id = robot_id)
func (h *FleetControlHandler) CancelCommand(ctx context.Context, commandID, agentID, taskID, reason string) error {
	robotState, exists := h.stateManager.GetRobotState(agentID)
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	msg := &ServerMessage{
		MessageID:   uuid.New().String(),
		TimestampMs: time.Now().UnixMilli(),
		Cancel: &CancelCommand{
			CommandID: commandID,
			AgentID:   agentID,
			TaskID:    taskID,
			Reason:    reason,
		},
	}

	return h.server.SendToAgent(robotState.AgentID, msg)
}

// RegisterCommandHandler registers a callback for command completion
func (h *FleetControlHandler) RegisterCommandHandler(commandID string, handler CommandHandler) {
	h.handlerMu.Lock()
	defer h.handlerMu.Unlock()
	h.commandHandlers[commandID] = handler
}

// UnregisterCommandHandler removes a command handler
func (h *FleetControlHandler) UnregisterCommandHandler(commandID string) {
	h.handlerMu.Lock()
	defer h.handlerMu.Unlock()
	delete(h.commandHandlers, commandID)
}

// ============================================================
// Task Execution
// ============================================================

// ExecuteTaskRequest represents a task execution request
type ExecuteTaskRequest struct {
	ActionGraphID string
	AgentID       string
	Params        map[string]interface{}
	Requester     string
}

// ExecuteTaskResponse represents a task execution response
type ExecuteTaskResponse struct {
	Success bool
	TaskID  string
	Error   string
}

// ExecuteTask starts a new task (1:1 model: agent_id = robot_id)
func (h *FleetControlHandler) ExecuteTask(ctx context.Context, req *ExecuteTaskRequest) (*ExecuteTaskResponse, error) {
	// This will be implemented by the scheduler
	// For now, just validate
	_, exists := h.stateManager.GetRobotState(req.AgentID)
	if !exists {
		return &ExecuteTaskResponse{
			Success: false,
			Error:   fmt.Sprintf("agent %s not found", req.AgentID),
		}, nil
	}

	taskID := uuid.New().String()

	return &ExecuteTaskResponse{
		Success: true,
		TaskID:  taskID,
	}, nil
}

// CancelTask cancels a running task
func (h *FleetControlHandler) CancelTask(ctx context.Context, taskID, reason string) error {
	// This will be implemented by the scheduler
	return nil
}

// ============================================================
// Fleet State
// ============================================================

// FleetStateRequest represents a fleet state request
type FleetStateRequest struct {
	AgentIDs     []string
	IncludeZones bool
}

// FleetStateResponse represents fleet state (1:1 model)
type FleetStateResponse struct {
	TimestampMs int64
	Agents      map[string]*AgentStateSnapshotMsg
	Zones       []*ZoneReservationInfo
}

// AgentStateSnapshotMsg represents an agent's state (1:1 model: agent_id = robot_id)
type AgentStateSnapshotMsg struct {
	AgentID       string
	AgentName     string
	State         string
	IsOnline      bool
	IsExecuting   bool
	StalenessSec  float32
	CurrentTaskID string
	CurrentStepID string
	Namespace     string
}

// ZoneReservationInfo represents a zone reservation
type ZoneReservationInfo struct {
	ZoneID       string
	AgentID      string
	ReservedAtMs int64
	ExpiresAtMs  int64
}

// GetFleetState returns the current fleet state (1:1 model)
func (h *FleetControlHandler) GetFleetState(ctx context.Context, req *FleetStateRequest) (*FleetStateResponse, error) {
	snapshot := h.stateManager.GetSnapshot()

	response := &FleetStateResponse{
		TimestampMs: snapshot.Timestamp.UnixMilli(),
		Agents:      make(map[string]*AgentStateSnapshotMsg),
		Zones:       make([]*ZoneReservationInfo, 0),
	}

	// Filter agents if specific IDs requested
	agentFilter := make(map[string]bool)
	if len(req.AgentIDs) > 0 {
		for _, id := range req.AgentIDs {
			agentFilter[id] = true
		}
	}

	// In 1:1 model, robots map contains agent state (agent_id = robot_id)
	for id, robot := range snapshot.Robots {
		if len(agentFilter) > 0 && !agentFilter[id] {
			continue
		}

		response.Agents[id] = &AgentStateSnapshotMsg{
			AgentID:       robot.ID,
			AgentName:     robot.Name,
			State:         robot.CurrentState,
			IsOnline:      robot.IsOnline,
			IsExecuting:   robot.IsExecuting,
			StalenessSec:  float32(time.Since(robot.LastSeen).Seconds()),
			CurrentTaskID: robot.CurrentTaskID,
			CurrentStepID: robot.CurrentStepID,
		}
	}

	// Include zones if requested
	if req.IncludeZones {
		for _, zone := range snapshot.Zones {
			response.Zones = append(response.Zones, &ZoneReservationInfo{
				ZoneID:       zone.ZoneID,
				AgentID:      zone.AgentID,
				ReservedAtMs: zone.ReservedAt.UnixMilli(),
				ExpiresAtMs:  zone.ExpiresAt.UnixMilli(),
			})
		}
	}

	return response, nil
}

// ============================================================
// Fleet State Broadcasting (for cross-agent coordination)
// ============================================================

// FleetStateUpdateMsg represents a fleet state broadcast message
type FleetStateUpdateMsg struct {
	TimestampMs int64
	Agents      []*AgentStateEntry
}

// AgentStateEntry represents an agent state in broadcast
type AgentStateEntry struct {
	AgentID        string
	State          string
	StateCode      string
	SemanticTags   []string
	CurrentGraphID string
	IsOnline       bool
	IsExecuting    bool
	StalenessSec   float32
}

// StartFleetStateBroadcast starts periodic fleet state broadcasts to agents
func (h *FleetControlHandler) StartFleetStateBroadcast(interval time.Duration) {
	h.broadcastMu.Lock()
	if h.broadcastStop != nil {
		h.broadcastMu.Unlock()
		return // Already running
	}
	h.broadcastStop = make(chan struct{})
	h.broadcastMu.Unlock()

	log.Printf("[FleetState] Starting broadcast loop (interval: %v)", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.broadcastStop:
			log.Println("[FleetState] Broadcast loop stopped")
			return
		case <-ticker.C:
			h.broadcastFleetState()
		}
	}
}

// StopFleetStateBroadcast stops the fleet state broadcast loop
func (h *FleetControlHandler) StopFleetStateBroadcast() {
	h.broadcastMu.Lock()
	defer h.broadcastMu.Unlock()

	if h.broadcastStop != nil {
		close(h.broadcastStop)
		h.broadcastStop = nil
	}
}

// broadcastFleetState sends fleet state to all connected agents
func (h *FleetControlHandler) broadcastFleetState() {
	snapshot := h.stateManager.GetSnapshot()

	agents := make([]*AgentStateEntry, 0, len(snapshot.Robots))
	for _, robot := range snapshot.Robots {
		entry := &AgentStateEntry{
			AgentID:        robot.ID,
			State:          robot.CurrentState,
			StateCode:      robot.CurrentStateCode,
			SemanticTags:   robot.SemanticTags,
			CurrentGraphID: robot.CurrentGraphID,
			IsOnline:       robot.IsOnline,
			IsExecuting:    robot.IsExecuting,
			StalenessSec:   float32(time.Since(robot.LastSeen).Seconds()),
		}
		agents = append(agents, entry)
	}

	msg := &FleetStateUpdateMsg{
		TimestampMs: time.Now().UnixMilli(),
		Agents:      agents,
	}

	// Broadcast to all connected agents
	h.server.BroadcastToAgents(msg)
}

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
}

// CommandHandler is a callback for handling specific command types
type CommandHandler func(ctx context.Context, robotID string, result *ActionResult) error

// PendingCommand tracks an outstanding command
type PendingCommand struct {
	CommandID   string
	RobotID     string
	TaskID      string
	StepID      string
	SentAt      time.Time
	TimeoutAt   time.Time
	ResultChan  chan *ActionResult
}

// ActionResult represents the result of an action execution
type ActionResult struct {
	CommandID     string
	RobotID       string
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

// ExecuteCommand represents a command to execute on a robot
type ExecuteCommand struct {
	CommandID    string
	RobotID      string
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

// RegisterAgentRequest represents an agent registration request
type RegisterAgentRequest struct {
	AgentID       string
	Name          string
	Robots        []RobotInfo
	ClientVersion string
}

// RobotInfo represents robot information from registration
type RobotInfo struct {
	RobotID      string
	ROSNamespace string
	Name         string
}

// RegisterAgentResponse represents a registration response
type RegisterAgentResponse struct {
	Success      bool
	Error        string
	RobotConfigs []RobotConfig
	ServerTimeMs int64
}

// RobotConfig represents robot configuration to send to agent
type RobotConfig struct {
	RobotID           string
	StateDefinition   []byte
	ActionDefinitions []byte
}

// RegisterAgent handles agent registration
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

	// Collect robot IDs
	robotIDs := make([]string, len(req.Robots))
	for i, r := range req.Robots {
		robotIDs[i] = r.RobotID

		// Register robot in state manager
		h.stateManager.RegisterRobot(
			r.RobotID,
			r.Name,
			req.AgentID,
			"idle",
		)
	}

	// Register agent in state manager
	h.stateManager.RegisterAgent(req.AgentID, req.Name, "", robotIDs)

	// Update agent status in database
	if agent != nil {
		if err := h.repo.UpdateAgentStatus(req.AgentID, "online", ""); err != nil {
			log.Printf("Failed to update agent status: %v", err)
		}
	}

	// Robot configs (capabilities are auto-discovered)
	robotConfigs := make([]RobotConfig, 0)

	log.Printf("Agent %s registered with %d robots", req.AgentID, len(req.Robots))

	return &RegisterAgentResponse{
		Success:      true,
		RobotConfigs: robotConfigs,
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

// AgentHeartbeat represents a heartbeat from an agent
type AgentHeartbeat struct {
	Robots map[string]*RobotHeartbeat
}

// RobotHeartbeat represents robot heartbeat status
type RobotHeartbeat struct {
	RobotID       string
	State         string
	IsExecuting   bool
	CurrentAction string
}

// AgentStatusUpdate represents an agent status update
type AgentStatusUpdate struct {
	State        string
	OnlineRobots []string
	Message      string
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
	RobotID   string
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
	RobotID         string
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

// processHeartbeat updates robot states from heartbeat
func (h *FleetControlHandler) processHeartbeat(agentID string, hb *AgentHeartbeat) {
	for robotID, rb := range hb.Robots {
		// Update robot state
		if rb.State != "" {
			if err := h.stateManager.UpdateRobotState(robotID, rb.State); err != nil {
				log.Printf("Failed to update robot state: %v", err)
			}
		}

		// Update execution state
		if err := h.stateManager.UpdateRobotExecution(robotID, rb.IsExecuting, "", rb.CurrentAction); err != nil {
			log.Printf("Failed to update robot execution state: %v", err)
		}
	}
}

// processActionResult handles action completion
func (h *FleetControlHandler) processActionResult(result *ActionResult) {
	log.Printf("Action result: command=%s robot=%s status=%d",
		result.CommandID, result.RobotID, result.Status)

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
			if err := handler(context.Background(), result.RobotID, result); err != nil {
				log.Printf("Command handler error: %v", err)
			}
		}()
	}

	// Update execution state
	if result.Status == ActionStatusSucceeded || result.Status == ActionStatusFailed || result.Status == ActionStatusCancelled {
		h.stateManager.CompleteExecution(result.RobotID, nil)
	}
}

// processStatusUpdate handles agent status changes
func (h *FleetControlHandler) processStatusUpdate(agentID string, status *AgentStatusUpdate) {
	log.Printf("Agent status update: %s - %s", agentID, status.Message)

	// Update online robots
	for _, robotID := range status.OnlineRobots {
		if err := h.stateManager.SetRobotOnline(robotID, true); err != nil {
			log.Printf("Failed to set robot online: %v", err)
		}
	}
}

// ============================================================
// Command Execution
// ============================================================

// SendCommand sends a command to a robot via its agent
func (h *FleetControlHandler) SendCommand(ctx context.Context, cmd *ExecuteCommand) error {
	// Get robot to find agent
	robotState, exists := h.stateManager.GetRobotState(cmd.RobotID)
	if !exists {
		return fmt.Errorf("robot %s not found", cmd.RobotID)
	}

	// Create pending command
	pending := &PendingCommand{
		CommandID:  cmd.CommandID,
		RobotID:    cmd.RobotID,
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
			RobotID:      cmd.RobotID,
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

	// Send to agent
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

// CancelCommand cancels a running command
func (h *FleetControlHandler) CancelCommand(ctx context.Context, commandID, robotID, taskID, reason string) error {
	robotState, exists := h.stateManager.GetRobotState(robotID)
	if !exists {
		return fmt.Errorf("robot %s not found", robotID)
	}

	msg := &ServerMessage{
		MessageID:   uuid.New().String(),
		TimestampMs: time.Now().UnixMilli(),
		Cancel: &CancelCommand{
			CommandID: commandID,
			RobotID:   robotID,
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
	RobotID       string
	Params        map[string]interface{}
	Requester     string
}

// ExecuteTaskResponse represents a task execution response
type ExecuteTaskResponse struct {
	Success bool
	TaskID  string
	Error   string
}

// ExecuteTask starts a new task
func (h *FleetControlHandler) ExecuteTask(ctx context.Context, req *ExecuteTaskRequest) (*ExecuteTaskResponse, error) {
	// This will be implemented by the scheduler
	// For now, just validate
	_, exists := h.stateManager.GetRobotState(req.RobotID)
	if !exists {
		return &ExecuteTaskResponse{
			Success: false,
			Error:   fmt.Sprintf("robot %s not found", req.RobotID),
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
	RobotIDs     []string
	IncludeZones bool
}

// FleetStateResponse represents fleet state
type FleetStateResponse struct {
	TimestampMs int64
	Robots      map[string]*RobotStateSnapshot
	Zones       []*ZoneReservationInfo
}

// RobotStateSnapshot represents a robot's state
type RobotStateSnapshot struct {
	RobotID       string
	RobotName     string
	AgentID       string
	State         string
	IsOnline      bool
	IsExecuting   bool
	StalenessSec  float32
	CurrentTaskID string
	CurrentStepID string
}

// ZoneReservationInfo represents a zone reservation
type ZoneReservationInfo struct {
	ZoneID       string
	RobotID      string
	ReservedAtMs int64
	ExpiresAtMs  int64
}

// GetFleetState returns the current fleet state
func (h *FleetControlHandler) GetFleetState(ctx context.Context, req *FleetStateRequest) (*FleetStateResponse, error) {
	snapshot := h.stateManager.GetSnapshot()

	response := &FleetStateResponse{
		TimestampMs: snapshot.Timestamp.UnixMilli(),
		Robots:      make(map[string]*RobotStateSnapshot),
		Zones:       make([]*ZoneReservationInfo, 0),
	}

	// Filter robots if specific IDs requested
	robotFilter := make(map[string]bool)
	if len(req.RobotIDs) > 0 {
		for _, id := range req.RobotIDs {
			robotFilter[id] = true
		}
	}

	for id, robot := range snapshot.Robots {
		if len(robotFilter) > 0 && !robotFilter[id] {
			continue
		}

		response.Robots[id] = &RobotStateSnapshot{
			RobotID:       robot.ID,
			RobotName:     robot.Name,
			AgentID:       robot.AgentID,
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
				RobotID:      zone.RobotID,
				ReservedAtMs: zone.ReservedAt.UnixMilli(),
				ExpiresAtMs:  zone.ExpiresAt.UnixMilli(),
			})
		}
	}

	return response, nil
}

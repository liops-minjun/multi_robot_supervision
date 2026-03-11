package executor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/graph"
	fleetgrpc "central_server_go/internal/grpc"
	"central_server_go/internal/state"

	"github.com/google/uuid"
)

// Scheduler manages task execution across the fleet
// This is the CENTRAL point for all task orchestration
// All precondition checks and zone reservations happen here atomically
type Scheduler struct {
	stateManager *state.GlobalStateManager
	repo         *db.Repository
	handler      *fleetgrpc.FleetControlHandler
	quicHandler  *fleetgrpc.RawQUICHandler

	// Active tasks
	tasks   map[string]*RunningTask
	tasksMu sync.RWMutex

	// Task completion channels (for agent-driven execution)
	taskComplete   map[string]chan TaskCompletionResult
	taskCompleteMu sync.RWMutex

	// Task queue per robot
	taskQueues   map[string][]string // agentID -> taskIDs
	taskQueuesMu sync.RWMutex

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (s *Scheduler) ensureGraphDeployed(ctx context.Context, agentID string, dbGraph *db.BehaviorTree) error {
	if dbGraph == nil {
		return fmt.Errorf("behavior tree is nil")
	}

	assignment, err := s.repo.GetAgentBehaviorTree(agentID, dbGraph.ID)
	if err != nil {
		return fmt.Errorf("load agent behavior tree assignment: %w", err)
	}

	if assignment != nil &&
		assignment.DeployedVersion == dbGraph.Version &&
		assignment.DeploymentStatus == "deployed" &&
		assignment.DeployedAt.Valid &&
		!dbGraph.UpdatedAt.After(assignment.DeployedAt.Time) {
		return nil
	}

	if s.quicHandler == nil {
		return fmt.Errorf("raw QUIC handler is not configured")
	}

	reason := "missing assignment"
	if assignment != nil {
		switch {
		case assignment.DeploymentStatus != "deployed":
			reason = "assignment status=" + assignment.DeploymentStatus
		case assignment.DeployedVersion != dbGraph.Version:
			reason = fmt.Sprintf("deployed_version=%d server_version=%d", assignment.DeployedVersion, dbGraph.Version)
		case !assignment.DeployedAt.Valid:
			reason = "missing deployed_at"
		case dbGraph.UpdatedAt.After(assignment.DeployedAt.Time):
			reason = fmt.Sprintf("graph updated_at=%s is newer than deployed_at=%s", dbGraph.UpdatedAt.Format(time.RFC3339), assignment.DeployedAt.Time.Format(time.RFC3339))
		default:
			reason = "stale deployment metadata"
		}
	}
	log.Printf("[Scheduler] Ensuring latest graph %s for agent %s (%s)", dbGraph.ID, agentID, reason)

	canonicalGraph, err := graph.FromDBModel(dbGraph)
	if err != nil {
		return fmt.Errorf("convert graph to canonical: %w", err)
	}
	if err := canonicalGraph.Validate(); err != nil {
		return fmt.Errorf("graph validation failed before deploy: %w", err)
	}
	if canonicalGraph.HasCycle() {
		return fmt.Errorf("cannot deploy graph with cycles")
	}

	canonicalGraph.BehaviorTree.AgentID = agentID
	canonicalGraph.SubstituteServerPatterns("")

	graphJSON, err := json.Marshal(canonicalGraph)
	if err != nil {
		return fmt.Errorf("serialize canonical graph: %w", err)
	}

	now := time.Now()
	if assignment == nil {
		assignment = &db.AgentBehaviorTree{
			ID:               uuid.New().String(),
			AgentID:          agentID,
			BehaviorTreeID:   dbGraph.ID,
			ServerVersion:    dbGraph.Version,
			DeployedVersion:  0,
			DeploymentStatus: "deploying",
			Enabled:          true,
			Priority:         0,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := s.repo.CreateAgentBehaviorTree(assignment); err != nil {
			return fmt.Errorf("create agent behavior tree assignment: %w", err)
		}
	} else {
		assignment.ServerVersion = dbGraph.Version
		assignment.DeploymentStatus = "deploying"
		assignment.DeploymentError = sql.NullString{}
		assignment.UpdatedAt = now
		if err := s.repo.UpdateAgentBehaviorTree(assignment); err != nil {
			return fmt.Errorf("mark graph deployment in progress: %w", err)
		}
	}

	deployCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	result, err := s.quicHandler.DeployCanonicalGraph(deployCtx, agentID, graphJSON)
	if err != nil {
		assignment.DeploymentStatus = "failed"
		assignment.DeploymentError = sql.NullString{String: err.Error(), Valid: true}
		assignment.UpdatedAt = time.Now()
		_ = s.repo.UpdateAgentBehaviorTree(assignment)
		return fmt.Errorf("deploy latest graph to agent: %w", err)
	}
	if !result.Success {
		deployErr := strings.TrimSpace(result.Error)
		if deployErr == "" {
			deployErr = "unknown deploy error"
		}
		assignment.DeploymentStatus = "failed"
		assignment.DeploymentError = sql.NullString{String: deployErr, Valid: true}
		assignment.UpdatedAt = time.Now()
		_ = s.repo.UpdateAgentBehaviorTree(assignment)
		return fmt.Errorf("deploy latest graph to agent failed: %s", deployErr)
	}

	assignment.ServerVersion = dbGraph.Version
	assignment.DeployedVersion = dbGraph.Version
	assignment.DeploymentStatus = "deployed"
	assignment.DeploymentError = sql.NullString{}
	assignment.DeployedAt = sql.NullTime{Time: time.Now(), Valid: true}
	assignment.UpdatedAt = time.Now()
	if err := s.repo.UpdateAgentBehaviorTree(assignment); err != nil {
		return fmt.Errorf("update deployed graph version: %w", err)
	}

	s.stateManager.GraphCache().SetDeployed(agentID, dbGraph.ID, canonicalGraph)
	log.Printf("[Scheduler] Auto-deployed latest graph %s v%d to agent %s before execution", dbGraph.ID, dbGraph.Version, agentID)
	return nil
}

// TaskCompletionResult represents the result of a completed task
type TaskCompletionResult struct {
	TaskID string
	Status TaskStatus
	Error  string
}

// CapabilityValidationResult contains the result of capability validation
type CapabilityValidationResult struct {
	Valid               bool     `json:"valid"`
	MissingCapabilities []string `json:"missing_capabilities,omitempty"`
	UnavailableServers  []string `json:"unavailable_servers,omitempty"`
	Message             string   `json:"message,omitempty"`
}

const (
	startConditionPollInterval = 250 * time.Millisecond
	logRetentionInterval       = 24 * time.Hour
	logRetentionWindow         = 30 * 24 * time.Hour
	taskDispatchLease          = 5 * time.Second

	taskParamPlanProblemID     = "__fleet_plan_problem_id"
	taskParamPlanExecutionID   = "__fleet_plan_execution_id"
	taskParamLogicalTaskID     = "__fleet_logical_task_id"
	taskParamLogicalTaskName   = "__fleet_logical_task_name"
	taskParamTaskDistributorID = "__fleet_task_distributor_id"
	taskParamResourceBindings  = "__fleet_resource_bindings"
)

// RunningTask represents an actively executing task
type RunningTask struct {
	ID                 string
	BehaviorTreeID     string
	RuntimeGraphID     string
	AgentID            string
	TaskDistributorID  string
	PlanProblemID      string
	PlanExecutionID    string
	LogicalTaskID      string
	LogicalTaskName    string
	RequiredResources  []string
	Bindings           map[string]string
	Params             map[string]string
	Steps              []db.BehaviorTreeStep
	StepIndex          map[string]int // Step ID -> index for O(1) lookup
	CurrentStep        int
	EntryStepID        string
	Status             TaskStatus
	RetryCount         map[string]int
	ReservedZones      []string
	Preconditions      []state.Precondition
	StartedAt          time.Time
	ResultChan         chan *StepResult
	CancelFunc         context.CancelFunc
	DispatchSent       bool
	DispatchLeaseUntil time.Time
	DispatchAttempts   int

	// Step results storage for field_sources resolution
	// Maps step_id -> result data (e.g., {"success": true, "distance": 5.2})
	StepResults map[string]map[string]interface{}

	// Pause/Resume support
	pauseMu   sync.Mutex
	pauseCond *sync.Cond
	isPaused  bool
}

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
	TaskPaused    TaskStatus = "paused"
)

// StepResult represents the result of a step execution
type StepResult struct {
	StepID string
	Status fleetgrpc.ActionStatus
	Result map[string]interface{}
	Error  string
}

// NewScheduler creates a new scheduler
func NewScheduler(stateManager *state.GlobalStateManager, repo *db.Repository, handler *fleetgrpc.FleetControlHandler, quicHandler *fleetgrpc.RawQUICHandler) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		stateManager: stateManager,
		repo:         repo,
		handler:      handler,
		quicHandler:  quicHandler,
		tasks:        make(map[string]*RunningTask),
		taskComplete: make(map[string]chan TaskCompletionResult),
		taskQueues:   make(map[string][]string),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start starts the scheduler background workers
func (s *Scheduler) Start() {
	s.wg.Add(3)

	// Timeout monitor
	go func() {
		defer s.wg.Done()
		s.runTimeoutMonitor()
	}()

	// Zone cleanup
	go func() {
		defer s.wg.Done()
		s.runZoneCleanup()
	}()

	// Log retention cleanup
	go func() {
		defer s.wg.Done()
		s.runLogRetentionCleanup()
	}()

	log.Println("Scheduler started")
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.cancel()
	s.wg.Wait()
	log.Println("Scheduler stopped")
}

// NotifyTaskComplete is called by RawQUICHandler when agent reports task completion
// This unblocks the waiting runTask goroutine
func (s *Scheduler) NotifyTaskComplete(taskID string, status TaskStatus, errorMsg string) {
	s.taskCompleteMu.RLock()
	ch, exists := s.taskComplete[taskID]
	s.taskCompleteMu.RUnlock()

	if exists {
		select {
		case ch <- TaskCompletionResult{TaskID: taskID, Status: status, Error: errorMsg}:
			log.Printf("[Scheduler] Notified task completion: %s status=%s", taskID, status)
		default:
			log.Printf("[Scheduler] Task completion channel full or closed: %s", taskID)
		}
	}
}

// ObserveAgentExecution is called from QUIC heartbeat/task-state updates.
// It acknowledges dispatched tasks when the agent reports them and only
// hands out the next queued task when the agent is confirmed idle.
func (s *Scheduler) ObserveAgentExecution(agentID string, isExecuting bool, currentTaskID string) {
	s.acknowledgeQueuedTask(agentID, currentTaskID)

	if isExecuting || strings.TrimSpace(currentTaskID) != "" {
		return
	}
	if robot, exists := s.stateManager.GetRobotState(agentID); exists && robot.IsExecuting && robot.CurrentTaskID != "" {
		return
	}

	if err := s.dispatchNextQueuedTask(agentID); err != nil {
		log.Printf("[Scheduler] Failed to dispatch queued task for %s: %v", agentID, err)
	}
}

func (s *Scheduler) DispatchIdleAgents() {
	s.tasksMu.RLock()
	agentIDs := make([]string, 0, len(s.taskQueues))
	for agentID := range s.taskQueues {
		agentIDs = append(agentIDs, agentID)
	}
	s.tasksMu.RUnlock()

	for _, agentID := range agentIDs {
		robot, exists := s.stateManager.GetRobotState(agentID)
		if !exists {
			continue
		}
		if robot.IsExecuting || strings.TrimSpace(robot.CurrentTaskID) != "" {
			continue
		}
		if err := s.dispatchNextQueuedTask(agentID); err != nil {
			log.Printf("[Scheduler] Failed to dispatch queued task for idle agent %s: %v", agentID, err)
		}
	}
}

func (s *Scheduler) acknowledgeQueuedTask(agentID, taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}

	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	if s.removeQueuedTaskLocked(agentID, taskID) {
		if task, exists := s.tasks[taskID]; exists {
			task.DispatchLeaseUntil = time.Time{}
		}
		log.Printf("[Scheduler] Agent %s acknowledged queued task %s", agentID, taskID)
	}
}

func (s *Scheduler) removeQueuedTask(agentID, taskID string) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	s.removeQueuedTaskLocked(agentID, taskID)
}

func (s *Scheduler) removeQueuedTaskLocked(agentID, taskID string) bool {
	queue := s.taskQueues[agentID]
	for i, queuedID := range queue {
		if queuedID != taskID {
			continue
		}
		s.taskQueues[agentID] = append(queue[:i], queue[i+1:]...)
		if len(s.taskQueues[agentID]) == 0 {
			delete(s.taskQueues, agentID)
		}
		return true
	}
	return false
}

func (s *Scheduler) dispatchNextQueuedTask(agentID string) error {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	queue := s.taskQueues[agentID]
	now := time.Now()

	for len(queue) > 0 {
		taskID := queue[0]
		task, exists := s.tasks[taskID]
		if !exists {
			queue = queue[1:]
			continue
		}

		if task.Status == TaskCompleted || task.Status == TaskFailed || task.Status == TaskCancelled {
			queue = queue[1:]
			continue
		}

		if !task.DispatchLeaseUntil.IsZero() && now.Before(task.DispatchLeaseUntil) {
			s.taskQueues[agentID] = queue
			return nil
		}

		if !task.DispatchLeaseUntil.IsZero() && !now.Before(task.DispatchLeaseUntil) {
			log.Printf("[Scheduler] Dispatch lease expired for task %s on agent %s, retrying", task.ID, agentID)
			s.stateManager.CompleteExecution(task.AgentID, task.ReservedZones)
			s.stateManager.ReleaseTaskResources(task.ID, task.AgentID)
			s.stateManager.UnregisterTaskRuntime(task.ID)
			task.Bindings = make(map[string]string)
			task.DispatchLeaseUntil = time.Time{}
			task.Status = TaskPending
		}

		if err := s.dispatchQueuedTaskLocked(task, now); err != nil {
			return err
		}

		s.taskQueues[agentID] = queue
		return nil
	}

	delete(s.taskQueues, agentID)
	return nil
}

func (s *Scheduler) reserveTaskResourcesForDispatch(task *RunningTask) (bool, error) {
	runtimeCtx := state.TaskRuntimeContext{
		PlanID:          task.PlanProblemID,
		PlanExecutionID: task.PlanExecutionID,
		LogicalTaskID:   task.LogicalTaskID,
		LogicalTaskName: task.LogicalTaskName,
		BehaviorTreeID:  task.BehaviorTreeID,
		AgentID:         task.AgentID,
		Bindings:        make(map[string]string),
		HeldResources:   make(map[string]struct{}),
	}

	if len(task.RequiredResources) == 0 {
		task.Bindings = make(map[string]string)
		s.stateManager.RegisterTaskRuntime(task.ID, runtimeCtx)
		return true, nil
	}

	catalog, err := loadRuntimeResourceCatalog(s.repo, task.TaskDistributorID)
	if err != nil {
		return false, err
	}

	bindings, err := resolveRuntimeBindings(task.RequiredResources, s.stateManager.GetAllResourceHolds(), nil, catalog)
	if err != nil {
		if strings.Contains(err.Error(), "no free instance") {
			return false, nil
		}
		return false, err
	}

	runtimeCtx.Bindings = bindings
	s.stateManager.RegisterTaskRuntime(task.ID, runtimeCtx)

	logicalTaskID := task.LogicalTaskID
	if logicalTaskID == "" {
		logicalTaskID = task.ID
	}

	for _, resourceID := range bindings {
		ok, _ := s.stateManager.TryAcquirePlanResource(task.PlanProblemID, resourceID, task.AgentID, task.ID, logicalTaskID, task.EntryStepID)
		if !ok {
			s.stateManager.ReleaseTaskResources(task.ID, task.AgentID)
			s.stateManager.UnregisterTaskRuntime(task.ID)
			task.Bindings = make(map[string]string)
			return false, nil
		}
	}

	task.Bindings = bindings
	return true, nil
}

func (s *Scheduler) dispatchQueuedTaskLocked(task *RunningTask, now time.Time) error {
	if s.quicHandler == nil {
		return fmt.Errorf("raw QUIC handler is not configured")
	}

	if ready, err := s.reserveTaskResourcesForDispatch(task); err != nil {
		return err
	} else if !ready {
		return nil
	}

	success, errMsg := s.stateManager.TryStartExecution(
		task.AgentID,
		task.ID,
		task.EntryStepID,
		task.BehaviorTreeID,
		task.ReservedZones,
		task.Preconditions,
	)
	if !success {
		s.stateManager.ReleaseTaskResources(task.ID, task.AgentID)
		s.stateManager.UnregisterTaskRuntime(task.ID)
		task.Bindings = make(map[string]string)
		log.Printf("[Scheduler] Task %s is still waiting to start on %s: %s", task.ID, task.AgentID, errMsg)
		return nil
	}

	task.DispatchLeaseUntil = now.Add(taskDispatchLease)
	task.DispatchAttempts++
	task.StartedAt = now

	dbTask := &db.Task{
		ID:               task.ID,
		BehaviorTreeID:   sql.NullString{String: task.BehaviorTreeID, Valid: true},
		AgentID:          sql.NullString{String: task.AgentID, Valid: true},
		Status:           string(TaskPending),
		CurrentStepID:    sql.NullString{String: task.EntryStepID, Valid: true},
		CurrentStepIndex: task.CurrentStep,
		StartedAt:        sql.NullTime{Time: now, Valid: true},
	}
	if err := s.repo.UpdateTask(dbTask); err != nil {
		log.Printf("[Scheduler] Failed to update task %s before dispatch: %v", task.ID, err)
	}

	sendParams := make(map[string]string, len(task.Params)+1)
	for key, value := range task.Params {
		sendParams[key] = value
	}
	if len(task.Bindings) > 0 {
		bindingJSON, err := json.Marshal(task.Bindings)
		if err != nil {
			s.stateManager.ReleaseTaskResources(task.ID, task.AgentID)
			s.stateManager.UnregisterTaskRuntime(task.ID)
			s.stateManager.CompleteExecution(task.AgentID, task.ReservedZones)
			task.DispatchLeaseUntil = time.Time{}
			task.DispatchSent = false
			task.Bindings = make(map[string]string)
			return fmt.Errorf("marshal resource bindings for %s: %w", task.ID, err)
		}
		sendParams[taskParamResourceBindings] = string(bindingJSON)
	}

	graphID := task.RuntimeGraphID
	if strings.TrimSpace(graphID) == "" {
		graphID = task.BehaviorTreeID
	}

	if err := s.quicHandler.SendStartTask(task.AgentID, task.ID, graphID, task.AgentID, sendParams); err != nil {
		s.stateManager.ReleaseTaskResources(task.ID, task.AgentID)
		s.stateManager.UnregisterTaskRuntime(task.ID)
		s.stateManager.CompleteExecution(task.AgentID, task.ReservedZones)
		task.DispatchLeaseUntil = time.Time{}
		task.DispatchSent = false
		task.Bindings = make(map[string]string)
		return fmt.Errorf("send start_task failed for %s: %w", task.ID, err)
	}

	task.DispatchSent = true
	log.Printf("[Scheduler] Dispatched queued task %s to agent %s (attempt=%d)", task.ID, task.AgentID, task.DispatchAttempts)
	return nil
}

// ============================================================
// Capability Validation (Safety Check)
// ============================================================

// ValidateCapabilities checks if all required action servers in the graph are available on the agent
// This is a critical safety check before executing any behavior tree
func (s *Scheduler) ValidateCapabilities(behaviorTreeID, agentID string) (*CapabilityValidationResult, error) {
	// Get behavior tree
	dbGraph, err := s.repo.GetBehaviorTree(behaviorTreeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get behavior tree: %w", err)
	}
	if dbGraph == nil {
		return nil, fmt.Errorf("behavior tree %s not found", behaviorTreeID)
	}

	// Parse steps from graph
	var steps []db.BehaviorTreeStep
	if err := json.Unmarshal(dbGraph.Steps, &steps); err != nil {
		return nil, fmt.Errorf("failed to parse steps: %w", err)
	}

	// Extract required action types and servers from steps
	requiredCapabilities := make(map[string]string) // action_type -> action_server
	for _, step := range steps {
		if step.Action != nil && step.Action.Type != "" {
			requiredCapabilities[step.Action.Type] = step.Action.Server
		}
	}

	// If no actions required, validation passes
	if len(requiredCapabilities) == 0 {
		return &CapabilityValidationResult{
			Valid:   true,
			Message: "No action capabilities required",
		}, nil
	}

	// Get agent's capabilities
	agentCaps, err := s.repo.GetAgentCapabilities(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent capabilities: %w", err)
	}

	// Build capability map for quick lookup
	capMap := make(map[string]*db.AgentCapability)    // action_type -> capability
	serverMap := make(map[string]*db.AgentCapability) // action_server -> capability
	for i := range agentCaps {
		cap := &agentCaps[i]
		capMap[cap.ActionType] = cap
		serverMap[cap.ActionServer] = cap
	}

	// Check each required capability
	var missingCaps []string
	var unavailableServers []string

	for actionType, actionServer := range requiredCapabilities {
		// First check by action server (more specific)
		if actionServer != "" {
			if cap, exists := serverMap[actionServer]; exists {
				if !cap.IsAvailable {
					unavailableServers = append(unavailableServers, actionServer)
				}
				continue
			}
		}

		// Check by action type
		if cap, exists := capMap[actionType]; exists {
			if !cap.IsAvailable {
				unavailableServers = append(unavailableServers, actionType)
			}
			continue
		}

		// Capability not found
		if actionServer != "" {
			missingCaps = append(missingCaps, fmt.Sprintf("%s (%s)", actionType, actionServer))
		} else {
			missingCaps = append(missingCaps, actionType)
		}
	}

	// Build result
	result := &CapabilityValidationResult{
		Valid:               len(missingCaps) == 0 && len(unavailableServers) == 0,
		MissingCapabilities: missingCaps,
		UnavailableServers:  unavailableServers,
	}

	if !result.Valid {
		var messages []string
		if len(missingCaps) > 0 {
			messages = append(messages, fmt.Sprintf("missing capabilities: %v", missingCaps))
		}
		if len(unavailableServers) > 0 {
			messages = append(messages, fmt.Sprintf("unavailable servers: %v", unavailableServers))
		}
		result.Message = strings.Join(messages, "; ")
	} else {
		result.Message = "All required capabilities are available"
	}

	return result, nil
}

// ============================================================
// Task Execution
// ============================================================

func stringifyTaskParams(params map[string]interface{}) (map[string]string, error) {
	if len(params) == 0 {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(params))
	for key, value := range params {
		switch typed := value.(type) {
		case nil:
			result[key] = ""
		case string:
			result[key] = typed
		default:
			raw, err := json.Marshal(typed)
			if err != nil {
				return nil, fmt.Errorf("marshal param %q: %w", key, err)
			}
			result[key] = string(raw)
		}
	}
	return result, nil
}

func takeTaskMeta(params map[string]string, key string) string {
	if params == nil {
		return ""
	}
	value := strings.TrimSpace(params[key])
	delete(params, key)
	return value
}

// StartTask starts a new task for an agent
// This is the main entry point for task execution.
// The task is queued first and only dispatched when the agent's next
// heartbeat confirms it is idle.
func (s *Scheduler) StartTask(ctx context.Context, actionGraphID, agentID string, params map[string]interface{}) (string, error) {
	// Generate task ID
	taskID := uuid.New().String()

	// Get agent state (in 1:1 model, agent ID is also robot ID)
	_, exists := s.stateManager.GetRobotState(agentID)
	if !exists {
		return "", fmt.Errorf("agent %s not found in state manager", agentID)
	}

	// SAFETY CHECK: Validate that all required capabilities are available
	capResult, err := s.ValidateCapabilities(actionGraphID, agentID)
	if err != nil {
		return "", fmt.Errorf("capability validation failed: %w", err)
	}
	if !capResult.Valid {
		return "", fmt.Errorf("cannot start task: %s", capResult.Message)
	}
	log.Printf("Capability validation passed for graph=%s agent=%s", actionGraphID, agentID)

	dbGraph, err := s.repo.GetBehaviorTree(actionGraphID)
	if err != nil {
		return "", fmt.Errorf("failed to get behavior tree: %w", err)
	}
	if dbGraph == nil {
		return "", fmt.Errorf("behavior tree %s not found", actionGraphID)
	}

	paramStrings, err := stringifyTaskParams(params)
	if err != nil {
		return "", err
	}
	planProblemID := takeTaskMeta(paramStrings, taskParamPlanProblemID)
	planExecutionID := takeTaskMeta(paramStrings, taskParamPlanExecutionID)
	logicalTaskID := takeTaskMeta(paramStrings, taskParamLogicalTaskID)
	logicalTaskName := takeTaskMeta(paramStrings, taskParamLogicalTaskName)
	taskDistributorID := takeTaskMeta(paramStrings, taskParamTaskDistributorID)
	if taskDistributorID == "" && dbGraph.TaskDistributorID.Valid {
		taskDistributorID = dbGraph.TaskDistributorID.String
	}

	runtimeGraphID, err := s.prepareExecutionGraph(ctx, agentID, taskID, dbGraph, paramStrings)
	if err != nil {
		return "", fmt.Errorf("failed to prepare execution graph: %w", err)
	}

	planningTask, err := db.DecodePlanningTaskSpec(dbGraph.PlanningTask)
	if err != nil {
		return "", fmt.Errorf("failed to decode task planning metadata: %w", err)
	}
	requiredResources := append([]string{}, planningTask.RequiredResources...)

	// Try to get behavior tree from cache first
	var steps []db.BehaviorTreeStep
	var preconditions []state.Precondition
	var graphVersion int
	var entryPoint string

	cached, cacheHit := s.stateManager.GraphCache().Get(agentID, actionGraphID)
	if cacheHit {
		// Cache hit - use cached graph
		graphVersion = cached.Version
		steps = s.canonicalToDBSteps(cached.Graph)
		preconditions = s.canonicalToPreconditions(cached.Graph)
		entryPoint = cached.Graph.EntryPoint
		log.Printf("Cache HIT for graph %s (agent=%s, version=%d)", actionGraphID, agentID, graphVersion)
	} else {
		// Cache miss - load from database
		log.Printf("Cache MISS for graph %s (agent=%s), loading from DB", actionGraphID, agentID)

		graphVersion = dbGraph.Version
		if dbGraph.EntryPoint.Valid {
			entryPoint = dbGraph.EntryPoint.String
		}

		// Parse steps from DB
		if err := json.Unmarshal(dbGraph.Steps, &steps); err != nil {
			return "", fmt.Errorf("failed to parse steps: %w", err)
		}

		// Parse preconditions from DB
		if dbGraph.Preconditions != nil {
			var dbPrecons []db.Precondition
			if err := json.Unmarshal(dbGraph.Preconditions, &dbPrecons); err != nil {
				return "", fmt.Errorf("failed to parse preconditions: %w", err)
			}
			for _, p := range dbPrecons {
				preconditions = append(preconditions, state.Precondition{
					Type:      p.Type,
					Condition: p.Condition,
					Message:   p.Message,
				})
			}
		}

		// Convert to canonical and cache for future use
		canonicalGraph, err := graph.FromDBModel(dbGraph)
		if err == nil {
			s.stateManager.GraphCache().Set(agentID, actionGraphID, canonicalGraph)
			if entryPoint == "" && canonicalGraph.EntryPoint != "" {
				entryPoint = canonicalGraph.EntryPoint
			}
		}
	}

	// Validate steps
	if len(steps) == 0 {
		return "", fmt.Errorf("behavior tree has no steps")
	}

	// Debug: Log parsed steps and their action servers
	for i, step := range steps {
		if step.Action != nil {
			log.Printf("[DEBUG] Parsed step[%d]: id=%s action_type='%s' action_server='%s'",
				i, step.ID, step.Action.Type, step.Action.Server)
		} else {
			log.Printf("[DEBUG] Parsed step[%d]: id=%s (no action)", i, step.ID)
		}
	}

	// NOTE: Agent mode delegation removed - all graphs execute server-side for consistency
	// This ensures predictable behavior and centralized state management

	// Build step index for O(1) lookup (used for entry point and step transitions)
	stepIndex := make(map[string]int, len(steps))
	for i, step := range steps {
		stepIndex[step.ID] = i
	}

	entryIndex := 0
	entryStepID := steps[0].ID
	if entryPoint != "" {
		if idx, found := stepIndex[entryPoint]; found {
			entryIndex = idx
			entryStepID = steps[idx].ID
		}
	}

	// Extract required zones from entry step
	requiredZones := s.extractRequiredZones(steps[entryIndex])

	// Save to database
	now := time.Now()
	dbTask := &db.Task{
		ID:               taskID,
		BehaviorTreeID:   sql.NullString{String: actionGraphID, Valid: true},
		AgentID:          sql.NullString{String: agentID, Valid: true},
		Status:           string(TaskPending),
		CurrentStepID:    sql.NullString{String: entryStepID, Valid: true},
		CurrentStepIndex: entryIndex,
		CreatedAt:        now,
	}
	if err := s.repo.CreateTask(dbTask); err != nil {
		log.Printf("Failed to save task to database: %v", err)
	}

	// Create task context
	taskCtx, taskCancel := context.WithCancel(s.ctx)

	// Create running task
	task := &RunningTask{
		ID:                taskID,
		BehaviorTreeID:    actionGraphID,
		RuntimeGraphID:    runtimeGraphID,
		AgentID:           agentID,
		TaskDistributorID: taskDistributorID,
		PlanProblemID:     planProblemID,
		PlanExecutionID:   planExecutionID,
		LogicalTaskID:     logicalTaskID,
		LogicalTaskName:   logicalTaskName,
		RequiredResources: requiredResources,
		Bindings:          make(map[string]string),
		Params:            paramStrings,
		Steps:             steps,
		StepIndex:         stepIndex,
		CurrentStep:       entryIndex,
		EntryStepID:       entryStepID,
		Status:            TaskPending,
		RetryCount:        make(map[string]int),
		ReservedZones:     requiredZones,
		Preconditions:     preconditions,
		StepResults:       make(map[string]map[string]interface{}),
		ResultChan:        make(chan *StepResult, 1),
		CancelFunc:        taskCancel,
	}
	// Initialize pause condition variable
	task.pauseCond = sync.NewCond(&task.pauseMu)

	// Store task
	s.tasksMu.Lock()
	s.tasks[taskID] = task
	s.taskQueues[agentID] = append(s.taskQueues[agentID], taskID)
	s.tasksMu.Unlock()

	// Start lifecycle watcher in background. Actual dispatch happens on heartbeat.
	go s.runTask(taskCtx, task)
	s.DispatchIdleAgents()

	log.Printf("Task queued: %s (graph=%s, agent=%s, entry=%s)", taskID, actionGraphID, agentID, entryStepID)

	// Debug: Log graph structure
	for i, step := range steps {
		trans := ""
		if step.Transition != nil {
			trans = fmt.Sprintf("on_success=%v on_failure=%v on_outcomes=%d",
				step.Transition.OnSuccess, step.Transition.OnFailure, len(step.Transition.OnOutcomes))
		}
		log.Printf("[GRAPH] Step[%d] id=%s type=%s transition={%s}", i, step.ID, step.Type, trans)
	}

	return taskID, nil
}

// runTask waits for a queued agent-driven task to complete after heartbeat-triggered dispatch.
func (s *Scheduler) runTask(ctx context.Context, task *RunningTask) {
	// Create completion channel
	completeChan := make(chan TaskCompletionResult, 1)
	s.taskCompleteMu.Lock()
	s.taskComplete[task.ID] = completeChan
	s.taskCompleteMu.Unlock()

	defer func() {
		// Cleanup completion channel
		s.taskCompleteMu.Lock()
		delete(s.taskComplete, task.ID)
		s.taskCompleteMu.Unlock()
		close(completeChan)

		// Cleanup state
		s.stateManager.CompleteExecution(task.AgentID, task.ReservedZones)
		s.stateManager.ReleaseTaskResources(task.ID, task.AgentID)
		s.stateManager.UnregisterTaskRuntime(task.ID)

		// Update database
		s.repo.UpdateTaskStatus(task.ID, string(task.Status), "", task.CurrentStep, "")

		// Remove from active tasks
		s.tasksMu.Lock()
		s.removeQueuedTaskLocked(task.AgentID, task.ID)
		delete(s.tasks, task.ID)
		s.tasksMu.Unlock()

		if task.RuntimeGraphID != "" && task.RuntimeGraphID != task.BehaviorTreeID {
			s.stateManager.GraphCache().InvalidateDeployed(task.AgentID, task.RuntimeGraphID)
		}

		s.DispatchIdleAgents()

		log.Printf("[Scheduler] Task completed: %s status=%s", task.ID, task.Status)
	}()

	// Wait for agent to complete the task
	select {
	case result := <-completeChan:
		log.Printf("[Scheduler] Task %s completed by agent: status=%s", task.ID, result.Status)
		task.Status = result.Status
		if result.Error != "" {
			log.Printf("[Scheduler] Task error: %s", result.Error)
		}

	case <-ctx.Done():
		log.Printf("[Scheduler] Task %s cancelled by context", task.ID)
		task.Status = TaskCancelled
	}
}

// executeStep executes a single step
func (s *Scheduler) executeStep(ctx context.Context, task *RunningTask, step *db.BehaviorTreeStep) *StepResult {
	log.Printf("Executing step: task=%s step=%s type=%s", task.ID, step.ID, step.Type)

	// Apply during states at step start
	s.applyStepDuringStates(task, step)
	defer s.clearStepDuringStates(task, step)

	// Handle terminal steps
	if step.Type == "terminal" {
		return &StepResult{
			StepID: step.ID,
			Status: fleetgrpc.ActionStatusSucceeded,
		}
	}

	// Wait for pre-states before evaluating start conditions
	if err := s.waitForPreStates(ctx, task.AgentID, step.PreStates); err != nil {
		return &StepResult{
			StepID: step.ID,
			Status: fleetgrpc.ActionStatusCancelled,
			Error:  err.Error(),
		}
	}

	// Wait for start conditions (server-mode only) with timeout and state tracking
	if err := s.waitForStartConditionsWithConfig(ctx, task.AgentID, step.StartConditions, PreconditionWaitConfig{
		TaskID:     task.ID,
		TimeoutSec: DefaultPreconditionTimeout,
	}); err != nil {
		return &StepResult{
			StepID: step.ID,
			Status: fleetgrpc.ActionStatusCancelled,
			Error:  err.Error(),
		}
	}

	// Handle wait_for steps
	if step.WaitFor != nil {
		return s.executeWaitFor(ctx, task, step)
	}

	// Handle action steps
	if step.Action != nil {
		return s.executeAction(ctx, task, step)
	}

	// No action defined
	return &StepResult{
		StepID: step.ID,
		Status: fleetgrpc.ActionStatusSucceeded,
	}
}

// PreconditionWaitConfig configures precondition waiting behavior
type PreconditionWaitConfig struct {
	TaskID     string
	TimeoutSec int // Default: 300 (5 minutes)
}

// DefaultPreconditionTimeout is the default timeout for precondition waiting (5 minutes)
const DefaultPreconditionTimeout = 300

func (s *Scheduler) waitForStartConditions(ctx context.Context, agentID string, conditions []db.StartCondition) error {
	return s.waitForStartConditionsWithConfig(ctx, agentID, conditions, PreconditionWaitConfig{})
}

func (s *Scheduler) waitForStartConditionsWithConfig(ctx context.Context, agentID string, conditions []db.StartCondition, config PreconditionWaitConfig) error {
	if len(conditions) == 0 {
		return nil
	}

	stateConditions := make([]state.StartCondition, 0, len(conditions))
	for _, c := range conditions {
		stateConditions = append(stateConditions, state.StartCondition{
			ID:              c.ID,
			Operator:        c.Operator,
			Quantifier:      state.StartConditionQuantifier(c.Quantifier),
			TargetType:      c.TargetType,
			AgentID:         c.AgentID,
			State:           c.State,
			StateOperator:   c.StateOperator,
			AllowedStates:   c.AllowedStates,
			MaxStalenessSec: c.MaxStalenessSec,
			RequireOnline:   c.RequireOnline,
			Message:         c.Message,
		})
	}

	// Set timeout
	timeoutSec := config.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = DefaultPreconditionTimeout
	}

	ticker := time.NewTicker(startConditionPollInterval)
	defer ticker.Stop()

	waitStartTime := time.Now()
	timeout := time.After(time.Duration(timeoutSec) * time.Second)
	var lastBlockingInfos []state.BlockingConditionInfo
	waitingReported := false

	for {
		passed, blockingInfos := s.stateManager.EvaluateStartConditionsWithBlockingInfo(agentID, stateConditions)
		if passed {
			// Clear waiting status if it was set
			if waitingReported && config.TaskID != "" {
				s.repo.UpdateTaskPreconditionStatus(config.TaskID, false, nil)
				s.stateManager.SetRobotWaitingForPrecondition(agentID, false, nil)
			}
			return nil
		}

		// Report waiting status (only when blocking info changes or first time)
		if !waitingReported || !blockingInfosEqual(lastBlockingInfos, blockingInfos) {
			lastBlockingInfos = blockingInfos
			waitingReported = true

			// Convert to db.BlockingConditionInfo
			dbBlockingInfos := make([]db.BlockingConditionInfo, len(blockingInfos))
			for i, info := range blockingInfos {
				dbBlockingInfos[i] = db.BlockingConditionInfo{
					ConditionID:     info.ConditionID,
					Description:     info.Description,
					TargetAgentID:   info.TargetAgentID,
					TargetAgentName: info.TargetAgentName,
					RequiredState:   info.RequiredState,
					CurrentState:    info.CurrentState,
					Reason:          info.Reason,
				}
			}

			// Update task in database
			if config.TaskID != "" {
				s.repo.UpdateTaskPreconditionStatus(config.TaskID, true, dbBlockingInfos)
			}

			// Update in-memory robot state for WebSocket broadcast
			s.stateManager.SetRobotWaitingForPrecondition(agentID, true, blockingInfos)

			log.Printf("Task %s waiting for precondition: %d blocking conditions for agent %s",
				config.TaskID, len(blockingInfos), agentID)
		}

		select {
		case <-ctx.Done():
			// Clear waiting status on cancel
			if waitingReported && config.TaskID != "" {
				s.repo.UpdateTaskPreconditionStatus(config.TaskID, false, nil)
				s.stateManager.SetRobotWaitingForPrecondition(agentID, false, nil)
			}
			return fmt.Errorf("start condition wait cancelled")

		case <-timeout:
			// Clear waiting status on timeout
			if config.TaskID != "" {
				s.repo.UpdateTaskPreconditionStatus(config.TaskID, false, nil)
				s.stateManager.SetRobotWaitingForPrecondition(agentID, false, nil)
			}
			elapsed := time.Since(waitStartTime)
			return fmt.Errorf("precondition wait timed out after %.1f seconds", elapsed.Seconds())

		case <-ticker.C:
		}
	}
}

// blockingInfosEqual checks if two blocking info slices are equal
func blockingInfosEqual(a, b []state.BlockingConditionInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ConditionID != b[i].ConditionID ||
			a[i].TargetAgentID != b[i].TargetAgentID ||
			a[i].CurrentState != b[i].CurrentState ||
			a[i].Reason != b[i].Reason {
			return false
		}
	}
	return true
}

func (s *Scheduler) waitForPreStates(ctx context.Context, agentID string, preStates []string) error {
	if len(preStates) == 0 {
		return nil
	}

	ticker := time.NewTicker(startConditionPollInterval)
	defer ticker.Stop()

	for {
		if robotState, ok := s.stateManager.GetRobotState(agentID); ok {
			for _, state := range preStates {
				if robotState.CurrentState == state {
					return nil
				}
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("pre-state wait cancelled")
		case <-ticker.C:
		}
	}
}

// executeAction executes an action step
func (s *Scheduler) executeAction(ctx context.Context, task *RunningTask, step *db.BehaviorTreeStep) *StepResult {
	action := step.Action

	// Get timeout
	timeout := action.TimeoutSec
	if timeout == 0 {
		timeout = 30.0
	}

	// Build params
	params := make(map[string]interface{})
	if action.Params != nil {
		switch action.Params.Source {
		case "waypoint":
			// Get waypoint data from DB
			if action.Params.WaypointID != "" {
				wp, err := s.repo.GetWaypoint(action.Params.WaypointID)
				if err != nil {
					return &StepResult{
						StepID: step.ID,
						Status: fleetgrpc.ActionStatusFailed,
						Error:  fmt.Sprintf("failed to get waypoint: %v", err),
					}
				}
				if wp != nil {
					json.Unmarshal(wp.Data, &params)
				}
			}

		case "mapped":
			// Process per-field source mappings
			if action.Params.FieldSources != nil {
				for fieldName, fieldSource := range action.Params.FieldSources {
					switch fieldSource.Source {
					case "constant":
						// Use the constant value directly
						params[fieldName] = fieldSource.Value

					case "step_result":
						// Resolve value from previous step's result stored in task.StepResults
						if fieldSource.StepID != "" {
							if stepResult, ok := task.StepResults[fieldSource.StepID]; ok {
								if fieldSource.ResultField != "" {
									// Get specific field from result
									if value, exists := stepResult[fieldSource.ResultField]; exists {
										params[fieldName] = value
										log.Printf("[DEBUG] Resolved %s.%s = %v for field %s",
											fieldSource.StepID, fieldSource.ResultField, value, fieldName)
									} else {
										log.Printf("[WARN] Result field %s not found in step %s result",
											fieldSource.ResultField, fieldSource.StepID)
									}
								} else {
									// Use entire result object
									params[fieldName] = stepResult
									log.Printf("[DEBUG] Resolved entire result of step %s for field %s",
										fieldSource.StepID, fieldName)
								}
							} else {
								log.Printf("[WARN] Step result not found for step %s (available: %v)",
									fieldSource.StepID, getMapKeys(task.StepResults))
							}
						}

					case "expression":
						// Use the expression directly (already in ${} format)
						if fieldSource.Expression != "" {
							params[fieldName] = fieldSource.Expression
						}

					case "dynamic":
						// Dynamic params are provided at execution time - skip for now
						// These will be merged from execution request params
					}
				}
			}
			// Also merge any inline data as base values
			if action.Params.Data != nil {
				for k, v := range action.Params.Data {
					if _, exists := params[k]; !exists {
						params[k] = v
					}
				}
			}

		default:
			// inline or other: use data directly
			if action.Params.Data != nil {
				params = action.Params.Data
			}
		}
	}

	// Create command
	commandID := uuid.New().String()

	overrideRobots := s.applyDuringStateTargets(
		task.AgentID,
		commandID,
		step.DuringStateTargets,
		step.DuringStates,
	)
	if len(overrideRobots) > 0 {
		defer s.clearDuringStateTargets(commandID, overrideRobots)
	}

	cmdDuringStates := s.selectSelfDuringState(task.AgentID, step.DuringStateTargets, step.DuringStates)

	// Marshal params to JSON for QUIC handler
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return &StepResult{
			StepID: step.ID,
			Status: fleetgrpc.ActionStatusFailed,
			Error:  fmt.Sprintf("failed to marshal params: %v", err),
		}
	}

	// Debug: Log action server value being sent
	log.Printf("[DEBUG] executeAction: step=%s action_type=%s action_server='%s'", step.ID, action.Type, action.Server)

	cmdReq := &fleetgrpc.ExecuteCommandReq{
		CommandID:     commandID,
		AgentID:       task.AgentID,
		TaskID:        task.ID,
		StepID:        step.ID,
		ActionType:    action.Type,
		ActionServer:  action.Server,
		Params:        paramsJSON,
		TimeoutSec:    float32(timeout),
		DeadlineMs:    time.Now().Add(time.Duration(timeout) * time.Second).UnixMilli(),
		DuringStates:  cmdDuringStates,
		SuccessStates: step.SuccessStates,
		FailureStates: step.FailureStates,
	}

	// Calculate timeout duration
	timeoutDuration := time.Duration(timeout) * time.Second
	if timeoutDuration == 0 {
		timeoutDuration = 30 * time.Second
	}

	// Send command via QUIC and wait for result
	result, err := s.quicHandler.SendCommandAndWait(ctx, task.AgentID, cmdReq, timeoutDuration)
	if err != nil {
		return &StepResult{
			StepID: step.ID,
			Status: fleetgrpc.ActionStatusTimeout,
			Error:  err.Error(),
		}
	}

	return &StepResult{
		StepID: step.ID,
		Status: result.Status,
		Result: result.Result,
		Error:  result.Error,
	}
}

// executeWaitFor executes a wait_for step
func (s *Scheduler) executeWaitFor(ctx context.Context, task *RunningTask, step *db.BehaviorTreeStep) *StepResult {
	// Currently no wait_for types implemented
	// Can be extended for future wait types (e.g., wait_for_state, wait_for_event)
	return &StepResult{
		StepID: step.ID,
		Status: fleetgrpc.ActionStatusSucceeded,
	}
}

// handleStepResult processes step result and determines next step
func (s *Scheduler) handleStepResult(task *RunningTask, step *db.BehaviorTreeStep, result *StepResult) string {
	log.Printf("[DEBUG] handleStepResult: task=%s step=%s stepType=%s result.Status=%v",
		task.ID, step.ID, step.Type, result.Status)

	if step.Type == "terminal" {
		log.Printf("[DEBUG] Step is terminal: terminalType=%s", step.TerminalType)
		if step.TerminalType == "success" {
			task.Status = TaskCompleted
		} else {
			task.Status = TaskFailed
		}
		return ""
	}

	transition := step.Transition
	if transition == nil {
		log.Printf("[DEBUG] No transition defined for step %s", step.ID)
		// No transition defined - move to next step
		if task.CurrentStep+1 < len(task.Steps) {
			return task.Steps[task.CurrentStep+1].ID
		}
		task.Status = TaskCompleted
		return ""
	}

	log.Printf("[DEBUG] Transition: OnSuccess=%v OnFailure=%v OnOutcomes=%d",
		transition.OnSuccess, transition.OnFailure, len(transition.OnOutcomes))

	if len(transition.OnOutcomes) > 0 {
		outcome := actionStatusToOutcome(result.Status)
		vars := buildOutcomeVariables(result, outcome)
		if next := s.resolveOutcomeTransition(transition.OnOutcomes, outcome, vars); next != "" {
			log.Printf("[DEBUG] OnOutcomes resolved to: %s", next)
			return next
		}
		log.Printf("[DEBUG] OnOutcomes did not match, falling through to status switch")
	}

	switch result.Status {
	case fleetgrpc.ActionStatusSucceeded:
		nextStep := s.resolveTransition(transition.OnSuccess)
		log.Printf("[DEBUG] Succeeded: resolveTransition returned '%s'", nextStep)
		return nextStep

	case fleetgrpc.ActionStatusFailed:
		// Handle retry logic
		failureTransition := s.parseFailureTransition(transition.OnFailure)
		if failureTransition != nil {
			// Check retry count
			retryCount := task.RetryCount[step.ID]
			if failureTransition.Retry > 0 && retryCount < failureTransition.Retry {
				task.RetryCount[step.ID]++
				log.Printf("Retrying step %s (attempt %d/%d)", step.ID, retryCount+1, failureTransition.Retry)
				return step.ID // Retry same step
			}

			// Check fallback
			if failureTransition.Fallback != "" {
				return failureTransition.Fallback
			}

			// Check next
			if failureTransition.Next != "" {
				return failureTransition.Next
			}
		}

		// No handler - use simple transition
		nextStep := s.resolveTransition(transition.OnFailure)
		if nextStep != "" {
			return nextStep
		}

		task.Status = TaskFailed
		return ""

	case fleetgrpc.ActionStatusCancelled:
		task.Status = TaskCancelled
		return ""

	case fleetgrpc.ActionStatusTimeout:
		if transition.OnTimeout != "" {
			return transition.OnTimeout
		}
		task.Status = TaskFailed
		return ""

	default:
		task.Status = TaskFailed
		return ""
	}
}

func (s *Scheduler) applyDuringStateTargets(
	executingAgentID string,
	sourceID string,
	targets []db.StateTarget,
	fallbackStates []string,
) []string {
	overrides := s.resolveDuringStateOverrides(executingAgentID, targets, fallbackStates)
	if len(overrides) == 0 {
		return nil
	}

	applied := make([]string, 0, len(overrides))
	for agentID, state := range overrides {
		if err := s.stateManager.SetRobotStateOverride(agentID, sourceID, state); err == nil {
			applied = append(applied, agentID)
		}
	}
	if len(applied) == 0 {
		return nil
	}
	return applied
}

func (s *Scheduler) clearDuringStateTargets(sourceID string, agentIDs []string) {
	for _, agentID := range agentIDs {
		_ = s.stateManager.ClearRobotStateOverride(agentID, sourceID)
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

func (s *Scheduler) resolveDuringStateOverrides(
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
		agentIDs := s.stateManager.ResolveTargetAgents(executingAgentID, targetType, target.AgentID)
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

func (s *Scheduler) selectSelfDuringState(
	executingAgentID string,
	targets []db.StateTarget,
	fallbackStates []string,
) []string {
	overrides := s.resolveDuringStateOverrides(executingAgentID, targets, fallbackStates)
	if len(overrides) == 0 {
		return nil
	}
	state, ok := overrides[executingAgentID]
	if !ok || state == "" {
		return nil
	}
	return []string{state}
}

// resolveTransition resolves a transition to a step ID
func (s *Scheduler) resolveTransition(transition interface{}) string {
	if transition == nil {
		return ""
	}

	// String transition
	if stepID, ok := transition.(string); ok {
		return stepID
	}

	// TransitionOnFailure struct (directly assigned)
	if tf, ok := transition.(db.TransitionOnFailure); ok {
		return tf.Next
	}

	// Object transition (from JSON unmarshaling)
	if obj, ok := transition.(map[string]interface{}); ok {
		if next, exists := obj["next"]; exists {
			if nextID, ok := next.(string); ok {
				return nextID
			}
		}
	}

	return ""
}

// parseFailureTransition parses failure transition config
func (s *Scheduler) parseFailureTransition(transition interface{}) *db.TransitionOnFailure {
	if transition == nil {
		return nil
	}

	// String - just a step ID
	if _, ok := transition.(string); ok {
		return nil
	}

	// TransitionOnFailure struct (directly assigned)
	if tf, ok := transition.(db.TransitionOnFailure); ok {
		return &tf
	}

	// Object - parse as TransitionOnFailure (from JSON unmarshaling)
	if obj, ok := transition.(map[string]interface{}); ok {
		result := &db.TransitionOnFailure{}

		if retry, exists := obj["retry"]; exists {
			if retryInt, ok := retry.(float64); ok {
				result.Retry = int(retryInt)
			}
		}
		if fallback, exists := obj["fallback"]; exists {
			if fallbackStr, ok := fallback.(string); ok {
				result.Fallback = fallbackStr
			}
		}
		if next, exists := obj["next"]; exists {
			if nextStr, ok := next.(string); ok {
				result.Next = nextStr
			}
		}

		return result
	}

	return nil
}

func (s *Scheduler) resolveOutcomeTransition(transitions []db.OutcomeTransition, outcome string, vars map[string]string) string {
	if len(transitions) == 0 {
		return ""
	}

	var defaultNext string
	for _, transition := range transitions {
		// No outcome specified - this is a default fallback
		if transition.Outcome == "" {
			if defaultNext == "" {
				defaultNext = transition.Next
			}
			continue
		}
		// Check if outcome matches
		if outcomeMatches(outcome, transition.Outcome) {
			return transition.Next
		}
	}

	return defaultNext
}

func actionStatusToOutcome(status fleetgrpc.ActionStatus) string {
	switch status {
	case fleetgrpc.ActionStatusSucceeded:
		return "success"
	case fleetgrpc.ActionStatusCancelled:
		return "cancelled"
	case fleetgrpc.ActionStatusTimeout:
		return "timeout"
	case fleetgrpc.ActionStatusRejected:
		return "rejected"
	case fleetgrpc.ActionStatusFailed:
		return "failed"
	default:
		return "failed"
	}
}

func buildOutcomeVariables(result *StepResult, outcome string) map[string]string {
	vars := map[string]string{
		"prev_step.outcome": outcome,
		"prev_step.success": strconv.FormatBool(result != nil && result.Status == fleetgrpc.ActionStatusSucceeded),
	}

	if result == nil || result.Result == nil {
		return vars
	}

	if raw, err := json.Marshal(result.Result); err == nil {
		vars["prev_step.result"] = string(raw)
	}

	for key, value := range result.Result {
		if key == "" {
			continue
		}
		if strVal := stringifyResultValue(value); strVal != "" {
			vars["prev_step."+key] = strVal
		}
	}

	return vars
}

func stringifyResultValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

// getMapKeys returns the keys of a map for debugging purposes
func getMapKeys(m map[string]map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func normalizeOutcomeValue(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "succeeded":
		return "success"
	case "failed", "failure", "error":
		return "failed"
	case "aborted", "abort":
		return "aborted"
	case "cancelled", "canceled", "cancel":
		return "cancelled"
	case "timeout", "timed_out":
		return "timeout"
	case "rejected":
		return "rejected"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func outcomeMatches(actual, expected string) bool {
	if strings.TrimSpace(expected) == "" {
		return true
	}
	actualNorm := normalizeOutcomeValue(actual)
	expectedNorm := normalizeOutcomeValue(expected)
	if actualNorm == expectedNorm {
		return true
	}
	if actualNorm == "failed" && expectedNorm == "aborted" {
		return true
	}
	if actualNorm == "aborted" && expectedNorm == "failed" {
		return true
	}
	return false
}

// extractRequiredZones extracts zone requirements from a step
func (s *Scheduler) extractRequiredZones(step db.BehaviorTreeStep) []string {
	// This would parse the step to determine required zones
	// For now, return empty
	return nil
}

// ============================================================
// Task Control
// ============================================================

// CancelTask cancels a running task
func (s *Scheduler) CancelTask(taskID, reason string) error {
	s.tasksMu.RLock()
	task, exists := s.tasks[taskID]
	s.tasksMu.RUnlock()

	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	log.Printf("[CancelTask] Cancelling task %s for agent %s: %s", taskID, task.AgentID, reason)

	// Cancel context
	task.CancelFunc()
	s.removeQueuedTask(task.AgentID, taskID)

	// If the task has never been dispatched, local cancellation is enough.
	if !task.DispatchSent {
		log.Printf("[CancelTask] Task %s was still queued; no cancel sent to agent", taskID)
		return nil
	}

	// Otherwise send cancel to agent via QUIC.
	log.Printf("[CancelTask] Sending cancel command to agent %s for task %s", task.AgentID, taskID)
	err := s.quicHandler.SendCancelCommand(task.AgentID, "", task.AgentID, taskID, reason)
	if err != nil {
		log.Printf("[CancelTask] Failed to send cancel command: %v", err)
		return fmt.Errorf("failed to send cancel to agent: %w", err)
	}
	log.Printf("[CancelTask] Cancel command sent successfully for task %s", taskID)

	return nil
}

// PauseTask pauses a running task
func (s *Scheduler) PauseTask(taskID string) error {
	s.tasksMu.Lock()
	task, exists := s.tasks[taskID]
	s.tasksMu.Unlock()

	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Set pause flag with task-specific lock
	task.pauseMu.Lock()
	task.isPaused = true
	task.Status = TaskPaused
	task.pauseMu.Unlock()

	log.Printf("Task %s paused", taskID)
	return nil
}

// ResumeTask resumes a paused task
func (s *Scheduler) ResumeTask(taskID string) error {
	s.tasksMu.Lock()
	task, exists := s.tasks[taskID]
	s.tasksMu.Unlock()

	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	task.pauseMu.Lock()
	if !task.isPaused {
		task.pauseMu.Unlock()
		return fmt.Errorf("task %s is not paused", taskID)
	}

	task.isPaused = false
	task.Status = TaskRunning
	task.pauseCond.Broadcast() // Wake up waiting goroutine
	task.pauseMu.Unlock()

	log.Printf("Task %s resumed", taskID)
	return nil
}

// waitWhilePaused blocks until the task is no longer paused or context is cancelled
// Returns true if should continue, false if context was cancelled
func (t *RunningTask) waitWhilePaused(ctx context.Context) bool {
	t.pauseMu.Lock()
	defer t.pauseMu.Unlock()

	for t.isPaused {
		// Check context before waiting
		select {
		case <-ctx.Done():
			return false
		default:
		}

		// Wait for resume signal with timeout to periodically check context
		// Using a goroutine to handle the condvar wait with context cancellation
		done := make(chan struct{})
		go func() {
			t.pauseCond.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Resumed, continue checking in case of spurious wakeup
		case <-ctx.Done():
			// Context cancelled while paused
			t.pauseCond.Broadcast() // Wake up the waiting goroutine
			return false
		}
	}
	return true
}

// GetTask returns a task by ID
func (s *Scheduler) GetTask(taskID string) (*RunningTask, bool) {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	task, exists := s.tasks[taskID]
	return task, exists
}

// GetTasksByRobot returns all tasks for a robot
func (s *Scheduler) GetTasksByRobot(agentID string) []*RunningTask {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	var result []*RunningTask
	for _, task := range s.tasks {
		if task.AgentID == agentID {
			result = append(result, task)
		}
	}
	return result
}

// ============================================================
// Background Workers
// ============================================================

// runTimeoutMonitor monitors for task timeouts
func (s *Scheduler) runTimeoutMonitor() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkTimeouts()
		}
	}
}

func (s *Scheduler) checkTimeouts() {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	// Check for stale tasks
	for _, task := range s.tasks {
		if task.Status == TaskRunning {
			// Check if task has been running too long without progress
			// This would need more sophisticated tracking
		}
	}
}

// runZoneCleanup periodically cleans up expired zone reservations
func (s *Scheduler) runZoneCleanup() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			count := s.stateManager.CleanupExpiredZones()
			if count > 0 {
				log.Printf("Cleaned up %d expired zone reservations", count)
			}
		}
	}
}

func (s *Scheduler) runLogRetentionCleanup() {
	ticker := time.NewTicker(logRetentionInterval)
	defer ticker.Stop()

	s.cleanupLogs()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.cleanupLogs()
		}
	}
}

func (s *Scheduler) cleanupLogs() {
	report, err := s.repo.CleanupOldData(logRetentionWindow)
	if err != nil {
		log.Printf("Log retention cleanup failed: %v", err)
		return
	}
	if report.Total() > 0 {
		log.Printf(
			"Log retention cleanup removed deployment_logs=%d tasks=%d commands=%d",
			report.DeploymentLogs,
			report.Tasks,
			report.Commands,
		)
	}
}

// ============================================================
// Cache Conversion Helpers
// ============================================================

// canonicalToDBSteps converts canonical graph vertices to DB step format
func (s *Scheduler) canonicalToDBSteps(g *graph.CanonicalGraph) []db.BehaviorTreeStep {
	steps := make([]db.BehaviorTreeStep, 0, len(g.Vertices))

	for _, v := range g.Vertices {
		step := db.BehaviorTreeStep{
			ID:   v.ID,
			Name: v.Name,
		}

		// Handle terminal vertices
		if v.Type == graph.VertexTypeTerminal {
			step.Type = "terminal"
			if v.Terminal != nil {
				step.TerminalType = string(v.Terminal.TerminalType)
				step.Alert = v.Terminal.Alert
				step.Message = v.Terminal.Message
			}
			steps = append(steps, step)
			continue
		}

		// Handle step vertices
		if v.Step != nil {
			// States
			if v.Step.States != nil {
				step.PreStates = v.Step.States.Pre
				step.DuringStates = v.Step.States.During
				step.SuccessStates = v.Step.States.Success
				step.FailureStates = v.Step.States.Failure
			}

			// Action
			if v.Step.Action != nil {
				step.Action = &db.StepAction{
					Type:       v.Step.Action.Type,
					Server:     v.Step.Action.Server,
					TimeoutSec: v.Step.Action.TimeoutSec,
				}
				if v.Step.Action.Params != nil {
					step.Action.Params = &db.ActionParams{
						Source:       v.Step.Action.Params.Source,
						WaypointID:   v.Step.Action.Params.WaypointID,
						Data:         v.Step.Action.Params.Data,
						Fields:       v.Step.Action.Params.Fields,
						FieldSources: graphToDBFieldSources(v.Step.Action.Params.FieldSources),
					}
				}
			}

			// Wait
			if v.Step.Wait != nil {
				step.WaitFor = &db.WaitFor{
					Type:       string(v.Step.Wait.Type),
					Message:    v.Step.Wait.Message,
					TimeoutSec: v.Step.Wait.TimeoutSec,
				}
			}
		}

		// Build transitions from edges
		step.Transition = s.buildTransitionsForVertex(g, v.ID)

		steps = append(steps, step)
	}

	return steps
}

// graphToDBFieldSources converts graph schema field sources to DB format for scheduler use
func graphToDBFieldSources(graphSources map[string]graph.ParameterFieldSource) map[string]db.ParameterFieldSource {
	if len(graphSources) == 0 {
		return nil
	}
	result := make(map[string]db.ParameterFieldSource, len(graphSources))
	for fieldName, graphSource := range graphSources {
		result[fieldName] = db.ParameterFieldSource{
			Source:      string(graphSource.Source),
			Value:       graphSource.Value,
			StepID:      graphSource.StepID,
			ResultField: graphSource.ResultField,
			Expression:  graphSource.Expression,
		}
	}
	return result
}

// buildTransitionsForVertex builds transitions from edges for a vertex
func (s *Scheduler) buildTransitionsForVertex(g *graph.CanonicalGraph, vertexID string) *db.StepTransition {
	edges := g.GetOutgoingEdges(vertexID)
	if len(edges) == 0 {
		return nil
	}

	transition := &db.StepTransition{}

	for _, e := range edges {
		switch e.Type {
		case graph.EdgeTypeOnSuccess:
			if e.Config != nil && e.Config.Condition != "" {
				// Conditional transition
				transition.OnSuccess = map[string]interface{}{
					"next":      e.To,
					"condition": e.Config.Condition,
				}
			} else {
				transition.OnSuccess = e.To
			}
		case graph.EdgeTypeOnFailure:
			if e.Config != nil && (e.Config.Retry > 0 || e.Config.Fallback != "") {
				transition.OnFailure = db.TransitionOnFailure{
					Retry:    e.Config.Retry,
					Fallback: e.Config.Fallback,
					Next:     e.To,
				}
			} else {
				transition.OnFailure = e.To
			}
		case graph.EdgeTypeOnTimeout:
			transition.OnTimeout = e.To
		case graph.EdgeTypeOnConfirm:
			transition.OnConfirm = e.To
		case graph.EdgeTypeOnCancel:
			transition.OnCancel = e.To
		}
	}

	return transition
}

// canonicalToPreconditions converts canonical graph to preconditions
func (s *Scheduler) canonicalToPreconditions(g *graph.CanonicalGraph) []state.Precondition {
	// For now, canonical graphs don't have preconditions at graph level
	// Preconditions are per-step in canonical format
	// This returns empty - preconditions would come from the entry vertex
	return nil
}

// ============================================================
// Multi-Agent Simultaneous Execution
// ============================================================

// ExecutionGroup represents a coordinated multi-agent execution
type ExecutionGroup struct {
	ID        string
	GraphID   string
	Tasks     map[string]*RunningTask // agentID -> task
	SyncMode  string                  // "barrier" or "best_effort"
	StartedAt time.Time
	Status    string // pending, running, completed, failed, partial
}

// MultiAgentTaskResult contains the result of starting multi-agent execution
type MultiAgentTaskResult struct {
	ExecutionGroupID string
	Tasks            []MultiAgentTaskInfo
	Success          bool
	FailedAgentID    string
	ErrorMessage     string
}

// MultiAgentTaskInfo contains info about a single task in the execution group
type MultiAgentTaskInfo struct {
	AgentID string
	TaskID  string
	Status  string
}

// StartMultiAgentTask starts synchronized execution for multiple agents
// All agents must pass validation before any start (atomic all-or-nothing)
// In barrier mode, all agents start execution at the same time
func (s *Scheduler) StartMultiAgentTask(
	ctx context.Context,
	actionGraphID string,
	agentIDs []string,
	commonParams map[string]interface{},
	agentParams map[string]map[string]interface{},
	syncMode string,
) (*MultiAgentTaskResult, error) {
	if len(agentIDs) == 0 {
		return nil, fmt.Errorf("at least one agent_id is required")
	}

	if syncMode == "" {
		syncMode = "barrier"
	}

	// Generate execution group ID
	executionGroupID := uuid.New().String()

	// Load and validate graph (same for all agents)
	var steps []db.BehaviorTreeStep
	var preconditions []state.Precondition
	var graphVersion int
	var entryPoint string

	// Try cache first with first agent ID
	cached, cacheHit := s.stateManager.GraphCache().Get(agentIDs[0], actionGraphID)
	if cacheHit {
		graphVersion = cached.Version
		steps = s.canonicalToDBSteps(cached.Graph)
		preconditions = s.canonicalToPreconditions(cached.Graph)
		entryPoint = cached.Graph.EntryPoint
	} else {
		dbGraph, err := s.repo.GetBehaviorTree(actionGraphID)
		if err != nil {
			return nil, fmt.Errorf("failed to get behavior tree: %w", err)
		}
		if dbGraph == nil {
			return nil, fmt.Errorf("behavior tree %s not found", actionGraphID)
		}

		graphVersion = dbGraph.Version
		if dbGraph.EntryPoint.Valid {
			entryPoint = dbGraph.EntryPoint.String
		}

		if err := json.Unmarshal(dbGraph.Steps, &steps); err != nil {
			return nil, fmt.Errorf("failed to parse steps: %w", err)
		}

		if dbGraph.Preconditions != nil {
			var dbPrecons []db.Precondition
			if err := json.Unmarshal(dbGraph.Preconditions, &dbPrecons); err != nil {
				return nil, fmt.Errorf("failed to parse preconditions: %w", err)
			}
			for _, p := range dbPrecons {
				preconditions = append(preconditions, state.Precondition{
					Type:      p.Type,
					Condition: p.Condition,
					Message:   p.Message,
				})
			}
		}

		// Convert to canonical and cache
		canonicalGraph, err := graph.FromDBModel(dbGraph)
		if err == nil {
			for _, agentID := range agentIDs {
				s.stateManager.GraphCache().Set(agentID, actionGraphID, canonicalGraph)
			}
			if entryPoint == "" && canonicalGraph.EntryPoint != "" {
				entryPoint = canonicalGraph.EntryPoint
			}
		}
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("behavior tree has no steps")
	}

	// Build step index
	stepIndex := make(map[string]int, len(steps))
	for i, step := range steps {
		stepIndex[step.ID] = i
	}

	entryIndex := 0
	entryStepID := steps[0].ID
	if entryPoint != "" {
		if idx, found := stepIndex[entryPoint]; found {
			entryIndex = idx
			entryStepID = steps[idx].ID
		}
	}

	// Prepare execution requests for all agents
	executions := make([]state.MultiExecutionRequest, len(agentIDs))
	taskIDs := make([]string, len(agentIDs))
	for i, agentID := range agentIDs {
		taskID := uuid.New().String()
		taskIDs[i] = taskID
		requiredZones := s.extractRequiredZones(steps[entryIndex])
		executions[i] = state.MultiExecutionRequest{
			AgentID:       agentID,
			TaskID:        taskID,
			StepID:        entryStepID,
			GraphID:       actionGraphID,
			RequiredZones: requiredZones,
		}
	}

	// Atomic: Validate all agents and reserve resources
	result := s.stateManager.TryStartMultiExecution(executions, preconditions)
	if !result.Success {
		return &MultiAgentTaskResult{
			Success:       false,
			FailedAgentID: result.FailedAgentID,
			ErrorMessage:  result.ErrorMessage,
		}, nil
	}

	// All validations passed - create tasks and start execution
	tasks := make(map[string]*RunningTask)
	taskInfos := make([]MultiAgentTaskInfo, len(agentIDs))
	var startBarrier sync.WaitGroup

	if syncMode == "barrier" {
		startBarrier.Add(len(agentIDs))
	}

	for i, agentID := range agentIDs {
		taskID := taskIDs[i]

		// Save to database
		dbTask := &db.Task{
			ID:               taskID,
			BehaviorTreeID:   sql.NullString{String: actionGraphID, Valid: true},
			AgentID:          sql.NullString{String: agentID, Valid: true},
			Status:           string(TaskRunning),
			CurrentStepID:    sql.NullString{String: entryStepID, Valid: true},
			CurrentStepIndex: entryIndex,
			StartedAt:        sql.NullTime{Time: time.Now(), Valid: true},
		}
		if err := s.repo.CreateTask(dbTask); err != nil {
			log.Printf("Failed to save task to database: %v", err)
		}

		// Create task context
		taskCtx, taskCancel := context.WithCancel(s.ctx)

		// Create running task
		task := &RunningTask{
			ID:             taskID,
			BehaviorTreeID: actionGraphID,
			AgentID:        agentID,
			Steps:          steps,
			StepIndex:      stepIndex,
			CurrentStep:    entryIndex,
			Status:         TaskRunning,
			RetryCount:     make(map[string]int),
			StepResults:    make(map[string]map[string]interface{}),
			ReservedZones:  executions[i].RequiredZones,
			StartedAt:      time.Now(),
			ResultChan:     make(chan *StepResult, 1),
			CancelFunc:     taskCancel,
		}
		// Initialize pause condition variable
		task.pauseCond = sync.NewCond(&task.pauseMu)

		tasks[agentID] = task
		taskInfos[i] = MultiAgentTaskInfo{
			AgentID: agentID,
			TaskID:  taskID,
			Status:  "running",
		}

		// Store task
		s.tasksMu.Lock()
		s.tasks[taskID] = task
		s.tasksMu.Unlock()

		// Start execution with optional barrier synchronization
		go s.runMultiAgentTask(taskCtx, task, syncMode, &startBarrier)
	}

	log.Printf("Multi-agent task started: group=%s graph=%s agents=%v (version=%d)",
		executionGroupID, actionGraphID, agentIDs, graphVersion)

	return &MultiAgentTaskResult{
		ExecutionGroupID: executionGroupID,
		Tasks:            taskInfos,
		Success:          true,
	}, nil
}

// runMultiAgentTask executes a task with optional barrier synchronization
func (s *Scheduler) runMultiAgentTask(ctx context.Context, task *RunningTask, syncMode string, barrier *sync.WaitGroup) {
	// If barrier mode, wait for all agents to be ready
	if syncMode == "barrier" {
		barrier.Done() // Signal this agent is ready
		barrier.Wait() // Wait for all agents to be ready
	}

	// Run the task (reuse existing runTask logic)
	s.runTask(ctx, task)
}

// applyStepDuringStates applies during_states for a step execution
func (s *Scheduler) applyStepDuringStates(task *RunningTask, step *db.BehaviorTreeStep) {
	if s.stateManager == nil {
		return
	}

	sourceID := fmt.Sprintf("task:%s:step:%s", task.ID, step.ID)

	// First try DuringStateTargets
	for _, target := range step.DuringStateTargets {
		if target.State == "" {
			continue
		}

		targetAgentID := task.AgentID
		switch target.TargetType {
		case "self", "":
			targetAgentID = task.AgentID
		case "agent", "specific":
			if target.AgentID != "" {
				targetAgentID = target.AgentID
			}
		case "all":
			// Apply to all robots - for now just apply to self
			targetAgentID = task.AgentID
		}

		if err := s.stateManager.SetRobotStateOverride(targetAgentID, sourceID, target.State); err != nil {
			log.Printf("[Scheduler] Failed to apply during state %s to %s: %v", target.State, targetAgentID, err)
		} else {
			log.Printf("[Scheduler] Applied during state %s to %s (step=%s)", target.State, targetAgentID, step.ID)
		}
	}

	// Fallback to DuringStates if no targets
	if len(step.DuringStateTargets) == 0 && len(step.DuringStates) > 0 {
		for _, state := range step.DuringStates {
			if state == "" {
				continue
			}
			if err := s.stateManager.SetRobotStateOverride(task.AgentID, sourceID, state); err != nil {
				log.Printf("[Scheduler] Failed to apply during state %s: %v", state, err)
			} else {
				log.Printf("[Scheduler] Applied during state %s (step=%s)", state, step.ID)
			}
			break // Only apply first state
		}
	}
}

// clearStepDuringStates clears during_states after step execution
func (s *Scheduler) clearStepDuringStates(task *RunningTask, step *db.BehaviorTreeStep) {
	if s.stateManager == nil {
		return
	}

	sourceID := fmt.Sprintf("task:%s:step:%s", task.ID, step.ID)

	// Clear from DuringStateTargets
	for _, target := range step.DuringStateTargets {
		targetAgentID := task.AgentID
		switch target.TargetType {
		case "agent", "specific":
			if target.AgentID != "" {
				targetAgentID = target.AgentID
			}
		}
		_ = s.stateManager.ClearRobotStateOverride(targetAgentID, sourceID)
	}

	// Also clear from self if using fallback DuringStates
	if len(step.DuringStateTargets) == 0 && len(step.DuringStates) > 0 {
		_ = s.stateManager.ClearRobotStateOverride(task.AgentID, sourceID)
	}
}

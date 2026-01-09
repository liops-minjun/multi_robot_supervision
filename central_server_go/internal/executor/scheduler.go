package executor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
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

	// Task queue per robot
	taskQueues   map[string][]string // robotID -> taskIDs
	taskQueuesMu sync.RWMutex

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

const (
	startConditionPollInterval = 250 * time.Millisecond
	logRetentionInterval       = 24 * time.Hour
	logRetentionWindow         = 30 * 24 * time.Hour
)

func isSelfOnlyGraph(steps []db.ActionGraphStep) bool {
	for _, step := range steps {
		for _, cond := range step.StartConditions {
			if cond.Quantifier != "" && strings.ToLower(cond.Quantifier) != "self" {
				return false
			}
			if cond.TargetType != "" && strings.ToLower(cond.TargetType) != "self" {
				return false
			}
			if cond.RobotID != "" || cond.AgentID != "" {
				return false
			}
		}
	}
	return true
}

// RunningTask represents an actively executing task
type RunningTask struct {
	ID            string
	ActionGraphID string
	RobotID       string
	AgentID       string
	Steps         []db.ActionGraphStep
	CurrentStep   int
	Status        TaskStatus
	RetryCount    map[string]int
	ReservedZones []string
	StartedAt     time.Time
	ResultChan    chan *StepResult
	CancelFunc    context.CancelFunc
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
	StepID  string
	Status  fleetgrpc.ActionStatus
	Result  map[string]interface{}
	Error   string
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

// ============================================================
// Task Execution
// ============================================================

// StartTask starts a new task for a robot
// This is the main entry point for task execution
func (s *Scheduler) StartTask(ctx context.Context, actionGraphID, robotID string, params map[string]interface{}) (string, error) {
	// Generate task ID
	taskID := uuid.New().String()

	// Get robot state first to determine agent
	robotState, exists := s.stateManager.GetRobotState(robotID)
	if !exists {
		return "", fmt.Errorf("robot %s not found", robotID)
	}

	// Try to get action graph from cache first
	var steps []db.ActionGraphStep
	var preconditions []state.Precondition
	var graphVersion int

	cached, cacheHit := s.stateManager.GraphCache().Get(robotState.AgentID, actionGraphID)
	if cacheHit {
		// Cache hit - use cached graph
		graphVersion = cached.Version
		steps = s.canonicalToDBSteps(cached.Graph)
		preconditions = s.canonicalToPreconditions(cached.Graph)
		log.Printf("Cache HIT for graph %s (agent=%s, version=%d)", actionGraphID, robotState.AgentID, graphVersion)
	} else {
		// Cache miss - load from database
		log.Printf("Cache MISS for graph %s (agent=%s), loading from DB", actionGraphID, robotState.AgentID)

		dbGraph, err := s.repo.GetActionGraph(actionGraphID)
		if err != nil {
			return "", fmt.Errorf("failed to get action graph: %w", err)
		}
		if dbGraph == nil {
			return "", fmt.Errorf("action graph %s not found", actionGraphID)
		}

		graphVersion = dbGraph.Version

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
			s.stateManager.GraphCache().Set(robotState.AgentID, actionGraphID, canonicalGraph)
		}
	}

	// Validate steps
	if len(steps) == 0 {
		return "", fmt.Errorf("action graph has no steps")
	}

	agentMode := isSelfOnlyGraph(steps)

	// Extract required zones from first step
	requiredZones := s.extractRequiredZones(steps[0])
	if agentMode {
		requiredZones = nil
	}

	// Atomic: Check preconditions + Reserve zones + Start execution
	success, errMsg := s.stateManager.TryStartExecution(
		robotID,
		taskID,
		steps[0].ID,
		requiredZones,
		preconditions,
	)
	if !success {
		return "", fmt.Errorf("cannot start task: %s", errMsg)
	}

	// Save to database
	dbTask := &db.Task{
		ID:               taskID,
		ActionGraphID:    sql.NullString{String: actionGraphID, Valid: true},
		RobotID:          sql.NullString{String: robotID, Valid: true},
		Status:           string(TaskRunning),
		CurrentStepID:    sql.NullString{String: steps[0].ID, Valid: true},
		CurrentStepIndex: 0,
		StartedAt:        sql.NullTime{Time: time.Now(), Valid: true},
	}
	if err := s.repo.CreateTask(dbTask); err != nil {
		log.Printf("Failed to save task to database: %v", err)
	}

	// Agent mode: delegate graph execution to the agent if possible.
	if agentMode && s.quicHandler != nil && robotState.AgentID != "" {
		if err := s.quicHandler.SendExecuteGraph(ctx, robotState.AgentID, taskID, actionGraphID, robotID, params); err == nil {
			log.Printf("Task delegated to agent: %s (graph=%s, robot=%s)", taskID, actionGraphID, robotID)
			return taskID, nil
		}
		log.Printf("ExecuteGraph failed, falling back to server mode for task %s", taskID)
	}

	// Create task context
	taskCtx, taskCancel := context.WithCancel(s.ctx)

	// Create running task
	task := &RunningTask{
		ID:            taskID,
		ActionGraphID: actionGraphID,
		RobotID:       robotID,
		AgentID:       robotState.AgentID,
		Steps:         steps,
		CurrentStep:   0,
		Status:        TaskRunning,
		RetryCount:    make(map[string]int),
		ReservedZones: requiredZones,
		StartedAt:     time.Now(),
		ResultChan:    make(chan *StepResult, 1),
		CancelFunc:    taskCancel,
	}

	// Store task
	s.tasksMu.Lock()
	s.tasks[taskID] = task
	s.tasksMu.Unlock()

	// Start step execution in background
	go s.runTask(taskCtx, task)

	log.Printf("Task started: %s (graph=%s, robot=%s)", taskID, actionGraphID, robotID)

	return taskID, nil
}

// runTask executes task steps sequentially
func (s *Scheduler) runTask(ctx context.Context, task *RunningTask) {
	defer func() {
		// Cleanup
		s.stateManager.CompleteExecution(task.RobotID, task.ReservedZones)

		// Update database
		s.repo.UpdateTaskStatus(task.ID, string(task.Status), "", task.CurrentStep, "")

		// Remove from active tasks
		s.tasksMu.Lock()
		delete(s.tasks, task.ID)
		s.tasksMu.Unlock()

		log.Printf("Task completed: %s status=%s", task.ID, task.Status)
	}()

	for task.CurrentStep < len(task.Steps) {
		select {
		case <-ctx.Done():
			task.Status = TaskCancelled
			return
		default:
		}

		step := task.Steps[task.CurrentStep]

		// Execute step
		result := s.executeStep(ctx, task, &step)

		// Handle result
		nextStep := s.handleStepResult(task, &step, result)

		if nextStep == "" {
			// Task completed or failed
			return
		}

		// Find next step
		found := false
		for i, s := range task.Steps {
			if s.ID == nextStep {
				task.CurrentStep = i
				found = true
				break
			}
		}

		if !found {
			log.Printf("Next step %s not found in task %s", nextStep, task.ID)
			task.Status = TaskFailed
			return
		}

		// Update database
		s.repo.UpdateTaskStatus(task.ID, string(task.Status), step.ID, task.CurrentStep, "")
	}

	task.Status = TaskCompleted
}

// executeStep executes a single step
func (s *Scheduler) executeStep(ctx context.Context, task *RunningTask, step *db.ActionGraphStep) *StepResult {
	log.Printf("Executing step: task=%s step=%s type=%s", task.ID, step.ID, step.Type)

	// Handle terminal steps
	if step.Type == "terminal" {
		return &StepResult{
			StepID: step.ID,
			Status: fleetgrpc.ActionStatusSucceeded,
		}
	}

	// Wait for pre-states before evaluating start conditions
	if err := s.waitForPreStates(ctx, task.RobotID, step.PreStates); err != nil {
		return &StepResult{
			StepID: step.ID,
			Status: fleetgrpc.ActionStatusCancelled,
			Error:  err.Error(),
		}
	}

	// Wait for start conditions (server-mode only)
	if err := s.waitForStartConditions(ctx, task.RobotID, step.StartConditions); err != nil {
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

func (s *Scheduler) waitForStartConditions(ctx context.Context, robotID string, conditions []db.StartCondition) error {
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
			RobotID:         c.RobotID,
			AgentID:         c.AgentID,
			State:           c.State,
			StateOperator:   c.StateOperator,
			AllowedStates:   c.AllowedStates,
			MaxStalenessSec: c.MaxStalenessSec,
			RequireOnline:   c.RequireOnline,
			Message:         c.Message,
		})
	}

	ticker := time.NewTicker(startConditionPollInterval)
	defer ticker.Stop()

	for {
		passed, errMsg := s.stateManager.EvaluateStartConditionList(robotID, stateConditions)
		if passed {
			return nil
		}

		select {
		case <-ctx.Done():
			if errMsg == "" {
				errMsg = "start condition wait cancelled"
			}
			return fmt.Errorf(errMsg)
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) waitForPreStates(ctx context.Context, robotID string, preStates []string) error {
	if len(preStates) == 0 {
		return nil
	}

	ticker := time.NewTicker(startConditionPollInterval)
	defer ticker.Stop()

	for {
		if robotState, ok := s.stateManager.GetRobotState(robotID); ok {
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
func (s *Scheduler) executeAction(ctx context.Context, task *RunningTask, step *db.ActionGraphStep) *StepResult {
	action := step.Action

	// Get timeout
	timeout := action.TimeoutSec
	if timeout == 0 {
		timeout = 30.0
	}

	// Build params
	params := make(map[string]interface{})
	if action.Params != nil {
		// Get waypoint data if needed
		if action.Params.Source == "waypoint" && action.Params.WaypointID != "" {
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
		} else if action.Params.Data != nil {
			params = action.Params.Data
		}
	}

	// Create command
	commandID := uuid.New().String()
	cmd := &fleetgrpc.ExecuteCommand{
		CommandID:    commandID,
		RobotID:      task.RobotID,
		TaskID:       task.ID,
		StepID:       step.ID,
		ActionType:   action.Type,
		ActionServer: action.Server,
		Params:       params,
		TimeoutSec:   float32(timeout),
		DeadlineMs:   time.Now().Add(time.Duration(timeout) * time.Second).UnixMilli(),
		DuringStates:  step.DuringStates,
		SuccessStates: step.SuccessStates,
		FailureStates: step.FailureStates,
	}

	// Send command and wait for result
	result, err := s.handler.SendCommandAndWait(ctx, cmd)
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
func (s *Scheduler) executeWaitFor(ctx context.Context, task *RunningTask, step *db.ActionGraphStep) *StepResult {
	waitFor := step.WaitFor

	switch waitFor.Type {
	case "manual_confirm":
		// For now, auto-confirm after a short delay
		// In production, this would wait for user input via websocket
		log.Printf("Waiting for manual confirmation: %s", waitFor.Message)

		timeout := waitFor.TimeoutSec
		if timeout == 0 {
			timeout = 300 // 5 minutes default
		}

		select {
		case <-time.After(time.Duration(timeout) * time.Second):
			return &StepResult{
				StepID: step.ID,
				Status: fleetgrpc.ActionStatusTimeout,
				Error:  "manual confirmation timeout",
			}
		case <-ctx.Done():
			return &StepResult{
				StepID: step.ID,
				Status: fleetgrpc.ActionStatusCancelled,
			}
		}

	default:
		return &StepResult{
			StepID: step.ID,
			Status: fleetgrpc.ActionStatusSucceeded,
		}
	}
}

// handleStepResult processes step result and determines next step
func (s *Scheduler) handleStepResult(task *RunningTask, step *db.ActionGraphStep, result *StepResult) string {
	if step.Type == "terminal" {
		if step.TerminalType == "success" {
			task.Status = TaskCompleted
		} else {
			task.Status = TaskFailed
		}
		return ""
	}

	transition := step.Transition
	if transition == nil {
		// No transition defined - move to next step
		if task.CurrentStep+1 < len(task.Steps) {
			return task.Steps[task.CurrentStep+1].ID
		}
		task.Status = TaskCompleted
		return ""
	}

	if len(transition.OnOutcomes) > 0 {
		outcome := actionStatusToOutcome(result.Status)
		vars := buildOutcomeVariables(result, outcome)
		if next := s.resolveOutcomeTransition(transition.OnOutcomes, outcome, vars); next != "" {
			return next
		}
	}

	switch result.Status {
	case fleetgrpc.ActionStatusSucceeded:
		return s.resolveTransition(transition.OnSuccess)

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

// resolveTransition resolves a transition to a step ID
func (s *Scheduler) resolveTransition(transition interface{}) string {
	if transition == nil {
		return ""
	}

	// String transition
	if stepID, ok := transition.(string); ok {
		return stepID
	}

	// Object transition (conditional)
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

	// Object - parse as TransitionOnFailure
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
		if !outcomeMatches(outcome, transition.Outcome) {
			continue
		}
		condition := strings.TrimSpace(transition.Condition)
		if condition == "" || strings.EqualFold(condition, "default") || strings.EqualFold(condition, "else") {
			if defaultNext == "" {
				defaultNext = transition.Next
			}
			continue
		}
		if evaluateTransitionCondition(condition, vars) {
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

var transitionConditionPattern = regexp.MustCompile(
	`^\s*\$([a-zA-Z0-9_]+)\.?([a-zA-Z0-9_]*)\s*(==|!=|<=|>=|<|>)\s*(.+)\s*$`,
)

func evaluateTransitionCondition(condition string, vars map[string]string) bool {
	trimmed := strings.TrimSpace(condition)
	if trimmed == "" {
		return true
	}
	if strings.EqualFold(trimmed, "true") {
		return true
	}
	if strings.EqualFold(trimmed, "false") {
		return false
	}

	matches := transitionConditionPattern.FindStringSubmatch(trimmed)
	if len(matches) != 5 {
		return false
	}

	varName := matches[1]
	field := matches[2]
	op := matches[3]
	expectedRaw := strings.TrimSpace(matches[4])
	expectedRaw = strings.Trim(expectedRaw, "\"'")

	key := varName
	if field != "" {
		key = varName + "." + field
	}
	actual, ok := vars[key]
	if !ok {
		return false
	}

	switch op {
	case "==":
		return actual == expectedRaw
	case "!=":
		return actual != expectedRaw
	case "<", "<=", ">", ">=":
		actualVal, err1 := strconv.ParseFloat(actual, 64)
		expectedVal, err2 := strconv.ParseFloat(expectedRaw, 64)
		if err1 != nil || err2 != nil {
			return false
		}
		switch op {
		case "<":
			return actualVal < expectedVal
		case "<=":
			return actualVal <= expectedVal
		case ">":
			return actualVal > expectedVal
		case ">=":
			return actualVal >= expectedVal
		}
		return false
	default:
		return false
	}
}

// extractRequiredZones extracts zone requirements from a step
func (s *Scheduler) extractRequiredZones(step db.ActionGraphStep) []string {
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

	log.Printf("Cancelling task %s: %s", taskID, reason)

	// Cancel context
	task.CancelFunc()

	// Send cancel to robot
	step := task.Steps[task.CurrentStep]
	if step.Action != nil {
		s.handler.CancelCommand(context.Background(), "", task.RobotID, taskID, reason)
	}

	return nil
}

// PauseTask pauses a running task
func (s *Scheduler) PauseTask(taskID string) error {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	task.Status = TaskPaused
	return nil
}

// ResumeTask resumes a paused task
func (s *Scheduler) ResumeTask(taskID string) error {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	if task.Status != TaskPaused {
		return fmt.Errorf("task %s is not paused", taskID)
	}

	task.Status = TaskRunning
	return nil
}

// GetTask returns a task by ID
func (s *Scheduler) GetTask(taskID string) (*RunningTask, bool) {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	task, exists := s.tasks[taskID]
	return task, exists
}

// GetTasksByRobot returns all tasks for a robot
func (s *Scheduler) GetTasksByRobot(robotID string) []*RunningTask {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	var result []*RunningTask
	for _, task := range s.tasks {
		if task.RobotID == robotID {
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
func (s *Scheduler) canonicalToDBSteps(g *graph.CanonicalGraph) []db.ActionGraphStep {
	steps := make([]db.ActionGraphStep, 0, len(g.Vertices))

	for _, v := range g.Vertices {
		step := db.ActionGraphStep{
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

			// Start conditions
			if len(v.Step.StartConditions) > 0 {
				step.StartConditions = graphToDBStartConditions(v.Step.StartConditions)
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
						Source:     v.Step.Action.Params.Source,
						WaypointID: v.Step.Action.Params.WaypointID,
						Data:       v.Step.Action.Params.Data,
						Fields:     v.Step.Action.Params.Fields,
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

func graphToDBStartConditions(conds []graph.StartCondition) []db.StartCondition {
	if len(conds) == 0 {
		return nil
	}
	out := make([]db.StartCondition, 0, len(conds))
	for _, c := range conds {
		out = append(out, db.StartCondition{
			ID:              c.ID,
			Operator:        c.Operator,
			Quantifier:      c.Quantifier,
			TargetType:      c.TargetType,
			RobotID:         c.RobotID,
			AgentID:         c.AgentID,
			State:           c.State,
			StateOperator:   c.StateOperator,
			AllowedStates:   c.AllowedStates,
			MaxStalenessSec: c.MaxStalenessSec,
			RequireOnline:   c.RequireOnline,
			Message:         c.Message,
		})
	}
	return out
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

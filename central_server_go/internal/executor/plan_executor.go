package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/pddl"
	"central_server_go/internal/state"

	"github.com/google/uuid"
)

// PlanExecutionStatus represents the status of a plan execution
type PlanExecutionStatus string

const (
	PlanExecPending   PlanExecutionStatus = "pending"
	PlanExecRunning   PlanExecutionStatus = "running"
	PlanExecCompleted PlanExecutionStatus = "completed"
	PlanExecFailed    PlanExecutionStatus = "failed"
	PlanExecCancelled PlanExecutionStatus = "cancelled"
)

// StepExecutionStatus tracks execution of a single task assignment within a plan.
type StepExecutionStatus struct {
	TaskID        string     `json:"task_id"`
	TaskName      string     `json:"task_name,omitempty"`
	BehaviorTreeID string     `json:"behavior_tree_id,omitempty"`
	RuntimeTaskID string     `json:"runtime_task_id,omitempty"`
	StepID        string     `json:"step_id"`
	StepName      string     `json:"step_name"`
	AgentID       string     `json:"agent_id"`
	AgentName     string     `json:"agent_name"`
	Order         int        `json:"order"`
	Status        TaskStatus `json:"status"`
	StartedAt     time.Time  `json:"started_at,omitempty"`
	EndedAt       time.Time  `json:"ended_at,omitempty"`
	Error         string     `json:"error,omitempty"`
}

// PlanExecution represents a running plan execution
type PlanExecution struct {
	ID             string                          `json:"id"`
	ProblemID      string                          `json:"problem_id"`
	BehaviorTreeID string                          `json:"behavior_tree_id"`
	BehaviorTreeIDs []string                        `json:"behavior_tree_ids,omitempty"`
	Status         PlanExecutionStatus             `json:"status"`
	CurrentOrder   int                             `json:"current_order"`
	TotalOrders    int                             `json:"total_orders"`
	StepStatuses   map[string]*StepExecutionStatus `json:"step_statuses"`
	StartedAt      time.Time                       `json:"started_at"`
	CompletedAt    time.Time                       `json:"completed_at,omitempty"`
	Error          string                          `json:"error,omitempty"`

	// Internal
	orderGroups [][]pddl.TaskAssignment
	cancelFunc  context.CancelFunc
	mu          sync.RWMutex
}

// PlanExecutor orchestrates PDDL plan execution
type PlanExecutor struct {
	scheduler    *Scheduler
	stateManager *state.GlobalStateManager
	repo         *db.Repository
	broadcastFn  func(interface{})

	executions map[string]*PlanExecution
	mu         sync.RWMutex
}

// NewPlanExecutor creates a new plan executor
func NewPlanExecutor(scheduler *Scheduler, stateManager *state.GlobalStateManager, repo *db.Repository, broadcastFn func(interface{})) *PlanExecutor {
	return &PlanExecutor{
		scheduler:    scheduler,
		stateManager: stateManager,
		repo:         repo,
		broadcastFn:  broadcastFn,
		executions:   make(map[string]*PlanExecution),
	}
}

// StartPlanExecution starts executing a solved plan
func (pe *PlanExecutor) StartPlanExecution(ctx context.Context, problemID string, behaviorTreeIDs []string, plan *pddl.Plan) (string, error) {
	if !plan.IsValid {
		return "", fmt.Errorf("plan is not valid: %s", plan.ErrorMessage)
	}
	if len(plan.Assignments) == 0 {
		return "", fmt.Errorf("plan has no assignments")
	}
	if len(behaviorTreeIDs) == 0 {
		for _, assignment := range plan.Assignments {
			if assignment.BehaviorTreeID != "" {
				behaviorTreeIDs = append(behaviorTreeIDs, assignment.BehaviorTreeID)
			}
		}
	}
	behaviorTreeIDs = uniqueStrings(behaviorTreeIDs)
	primaryBehaviorTreeID := ""
	if len(behaviorTreeIDs) > 0 {
		primaryBehaviorTreeID = behaviorTreeIDs[0]
	}

	// Group assignments by order
	orderGroups := make(map[int][]pddl.TaskAssignment)
	for _, a := range plan.Assignments {
		orderGroups[a.Order] = append(orderGroups[a.Order], a)
	}

	// Build ordered list of groups
	maxOrder := 0
	for _, a := range plan.Assignments {
		if a.Order > maxOrder {
			maxOrder = a.Order
		}
	}

	groups := make([][]pddl.TaskAssignment, maxOrder+1)
	for order, assignments := range orderGroups {
		groups[order] = assignments
	}

	// Build step statuses
	stepStatuses := make(map[string]*StepExecutionStatus, len(plan.Assignments))
	for _, a := range plan.Assignments {
		stepStatuses[a.TaskID] = &StepExecutionStatus{
			TaskID:         a.TaskID,
			TaskName:       a.TaskName,
			BehaviorTreeID: a.BehaviorTreeID,
			StepID:         a.StepID,
			StepName:       a.StepName,
			AgentID:        a.AgentID,
			AgentName:      a.AgentName,
			Order:          a.Order,
			Status:         TaskPending,
		}
	}

	execID := uuid.New().String()[:8]
	execCtx, cancel := context.WithCancel(ctx)

	execution := &PlanExecution{
		ID:              execID,
		ProblemID:       problemID,
		BehaviorTreeID:  primaryBehaviorTreeID,
		BehaviorTreeIDs: append([]string{}, behaviorTreeIDs...),
		Status:          PlanExecPending,
		CurrentOrder:    0,
		TotalOrders:     maxOrder + 1,
		StepStatuses:    stepStatuses,
		StartedAt:       time.Now(),
		orderGroups:     groups,
		cancelFunc:      cancel,
	}

	pe.mu.Lock()
	pe.executions[execID] = execution
	pe.mu.Unlock()

	// Start execution in background
	go pe.runPlanExecution(execCtx, execution)

	return execID, nil
}

// runPlanExecution executes order groups sequentially
func (pe *PlanExecutor) runPlanExecution(ctx context.Context, exec *PlanExecution) {
	exec.mu.Lock()
	exec.Status = PlanExecRunning
	exec.mu.Unlock()
	pe.broadcastPlanUpdate(exec)

	behaviorTreeIDs := exec.BehaviorTreeIDs
	if len(behaviorTreeIDs) == 0 && exec.BehaviorTreeID != "" {
		behaviorTreeIDs = []string{exec.BehaviorTreeID}
	}
	behaviorTrees := make(map[string]*db.BehaviorTree, len(behaviorTreeIDs))
	for _, behaviorTreeID := range behaviorTreeIDs {
		bt, err := pe.repo.GetBehaviorTree(behaviorTreeID)
		if err != nil {
			pe.failExecution(exec, fmt.Sprintf("failed to load behavior tree %q: %v", behaviorTreeID, err))
			return
		}
		if bt == nil {
			pe.failExecution(exec, fmt.Sprintf("behavior tree %q not found", behaviorTreeID))
			return
		}
		behaviorTrees[behaviorTreeID] = bt
	}
	primaryBT := behaviorTrees[exec.BehaviorTreeID]
	if primaryBT == nil {
		for _, bt := range behaviorTrees {
			primaryBT = bt
			break
		}
	}

	pp, err := pe.repo.GetPlanningProblem(exec.ProblemID)
	if err != nil {
		pe.failExecution(exec, fmt.Sprintf("failed to load planning problem: %v", err))
		return
	}
	if pp == nil {
		pe.failExecution(exec, "planning problem not found")
		return
	}

	initialPlanningState, err := pe.buildInitialPlanningState(pp, primaryBT)
	if err != nil {
		pe.failExecution(exec, fmt.Sprintf("failed to build planning state: %v", err))
		return
	}

	taskSpecs, err := pe.loadExecutionTaskSpecs(behaviorTrees)
	if err != nil {
		pe.failExecution(exec, fmt.Sprintf("failed to load task planning spec: %v", err))
		return
	}

	selectedDistributorID := ""
	if pp != nil && pp.TaskDistributorID.Valid && pp.TaskDistributorID.String != "" {
		selectedDistributorID = pp.TaskDistributorID.String
	} else if primaryBT != nil && primaryBT.TaskDistributorID.Valid && primaryBT.TaskDistributorID.String != "" {
		selectedDistributorID = primaryBT.TaskDistributorID.String
	}

	// Initialize planning state
	pe.stateManager.InitPlanningState(exec.ProblemID, initialPlanningState)
	defer pe.stateManager.ClearPlanningState(exec.ProblemID)
	defer pe.stateManager.ReleaseAllPlanResources(exec.ProblemID)

	for order := 0; order < exec.TotalOrders; order++ {
		select {
		case <-ctx.Done():
			pe.cancelExecution(exec)
			return
		default:
		}

		group := exec.orderGroups[order]
		if len(group) == 0 {
			continue
		}

		exec.mu.Lock()
		exec.CurrentOrder = order
		exec.mu.Unlock()
		pe.broadcastPlanUpdate(exec)

		// Dispatch all tasks in this group in parallel. Runtime resources are
		// now reconciled from agent-reported acquire/release events.
		var wg sync.WaitGroup
		results := make(chan stepDispatchResult, len(group))

		for _, assignment := range group {
			wg.Add(1)
			go func(a pddl.TaskAssignment) {
				defer wg.Done()
				result := pe.dispatchStep(ctx, exec, a, taskSpecs, selectedDistributorID)
				results <- result
			}(assignment)
		}

		// Wait for all dispatches to complete
		go func() {
			wg.Wait()
			close(results)
		}()

		// Collect results
		var groupError string
		for result := range results {
			if result.err != nil {
				groupError = fmt.Sprintf("task %s failed: %v", result.taskID, result.err)
			}
		}

		if groupError != "" {
			pe.failExecution(exec, groupError)
			return
		}
	}

	// All groups completed successfully
	exec.mu.Lock()
	exec.Status = PlanExecCompleted
	exec.CompletedAt = time.Now()
	exec.mu.Unlock()

	pe.repo.UpdatePlanningProblemStatus(exec.ProblemID, "completed", nil, "")
	pe.broadcastPlanUpdate(exec)

	log.Printf("[PlanExecutor] Plan execution %s completed successfully", exec.ID)
}

type stepDispatchResult struct {
	taskID string
	err    error
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

// dispatchStep dispatches a single task to an agent and waits for completion.
func (pe *PlanExecutor) dispatchStep(ctx context.Context, exec *PlanExecution, assignment pddl.TaskAssignment, taskSpecs map[string]db.PlanningTaskSpec, taskDistributorID string) stepDispatchResult {
	stepStatus := exec.StepStatuses[assignment.TaskID]
	behaviorTreeID := assignment.BehaviorTreeID
	if behaviorTreeID == "" {
		behaviorTreeID = exec.BehaviorTreeID
	}
	taskSpec, ok := taskSpecs[behaviorTreeID]
	if !ok {
		err := fmt.Errorf("missing task planning metadata for behavior tree %q", behaviorTreeID)
		exec.mu.Lock()
		stepStatus.Status = TaskFailed
		stepStatus.EndedAt = time.Now()
		stepStatus.Error = err.Error()
		exec.mu.Unlock()
		pe.broadcastPlanUpdate(exec)
		return stepDispatchResult{taskID: assignment.TaskID, err: err}
	}

	exec.mu.Lock()
	stepStatus.Status = TaskRunning
	stepStatus.StartedAt = time.Now()
	stepStatus.BehaviorTreeID = behaviorTreeID
	exec.mu.Unlock()
	pe.broadcastPlanUpdate(exec)

	// Use the scheduler to execute this step on the assigned agent
	params := map[string]interface{}{
		taskParamPlanProblemID:     exec.ProblemID,
		taskParamPlanExecutionID:   exec.ID,
		taskParamLogicalTaskID:     assignment.TaskID,
		taskParamLogicalTaskName:   assignment.TaskName,
		taskParamTaskDistributorID: taskDistributorID,
	}

	taskID, err := pe.scheduler.StartTask(ctx, behaviorTreeID, assignment.AgentID, params)
	if err != nil {
		exec.mu.Lock()
		stepStatus.Status = TaskFailed
		stepStatus.EndedAt = time.Now()
		stepStatus.Error = err.Error()
		exec.mu.Unlock()
		pe.broadcastPlanUpdate(exec)
		return stepDispatchResult{taskID: assignment.TaskID, err: err}
	}

	exec.mu.Lock()
	stepStatus.RuntimeTaskID = taskID
	exec.mu.Unlock()

	// Wait for task completion
	err = pe.waitForTask(ctx, taskID)

	exec.mu.Lock()
	stepStatus.EndedAt = time.Now()
	if err != nil {
		stepStatus.Status = TaskFailed
		stepStatus.Error = err.Error()
	} else {
		pe.applyPlanningEffects(exec.ProblemID, taskSpec)
		stepStatus.Status = TaskCompleted
	}
	exec.mu.Unlock()
	pe.broadcastPlanUpdate(exec)

	if err != nil {
		return stepDispatchResult{taskID: assignment.TaskID, err: err}
	}
	return stepDispatchResult{taskID: assignment.TaskID}
}

func (pe *PlanExecutor) buildInitialPlanningState(
	pp *db.PlanningProblem,
	bt *db.BehaviorTree) (map[string]string, error) {

	initial := make(map[string]string)
	selectedDistributorID := ""

	if pp != nil && pp.TaskDistributorID.Valid && pp.TaskDistributorID.String != "" {
		selectedDistributorID = pp.TaskDistributorID.String
	} else if bt != nil && bt.TaskDistributorID.Valid && bt.TaskDistributorID.String != "" {
		selectedDistributorID = bt.TaskDistributorID.String
	}

	if selectedDistributorID != "" {
		tdStates, err := pe.repo.ListTaskDistributorStates(selectedDistributorID)
		if err != nil {
			return nil, err
		}
		for _, stateVar := range tdStates {
			if stateVar.InitialValue != "" {
				initial[stateVar.Name] = stateVar.InitialValue
			}
		}
	} else if bt != nil && bt.PlanningStates != nil && len(bt.PlanningStates) > 0 {
		var planningStates []db.PlanningStateVar
		if err := json.Unmarshal(bt.PlanningStates, &planningStates); err != nil {
			return nil, err
		}
		for _, stateVar := range planningStates {
			if stateVar.InitialValue != "" {
				initial[stateVar.Name] = stateVar.InitialValue
			}
		}
	}

	if pp != nil && pp.InitialState != nil && len(pp.InitialState) > 0 {
		var overrides map[string]string
		if err := json.Unmarshal(pp.InitialState, &overrides); err != nil {
			return nil, err
		}
		for key, value := range overrides {
			initial[key] = value
		}
	}

	return initial, nil
}

func (pe *PlanExecutor) loadExecutionTaskSpecs(behaviorTrees map[string]*db.BehaviorTree) (map[string]db.PlanningTaskSpec, error) {
	taskSpecs := make(map[string]db.PlanningTaskSpec, len(behaviorTrees))
	for behaviorTreeID, bt := range behaviorTrees {
		if bt == nil {
			return nil, fmt.Errorf("behavior tree %q is nil", behaviorTreeID)
		}
		spec, err := db.DecodePlanningTaskSpec(bt.PlanningTask)
		if err != nil {
			return nil, err
		}
		if !spec.HasData() {
			return nil, fmt.Errorf("behavior tree %q does not define task planning metadata", bt.ID)
		}
		taskSpecs[behaviorTreeID] = spec
	}
	return taskSpecs, nil
}

func (pe *PlanExecutor) applyPlanningEffects(planID string, taskSpec db.PlanningTaskSpec) {
	if len(taskSpec.ResultStates) == 0 {
		return
	}

	effects := make(map[string]string, len(taskSpec.ResultStates))
	for _, effect := range taskSpec.ResultStates {
		if strings.TrimSpace(effect.Variable) == "" {
			continue
		}
		effects[effect.Variable] = effect.Value
	}
	if len(effects) > 0 {
		pe.stateManager.UpdatePlanningState(planID, effects)
	}
}

func (pe *PlanExecutor) updateResourceOccupancyState(planID, resourceID string, occupied bool) {
	current := pe.stateManager.GetPlanningState(planID)
	if len(current) == 0 {
		return
	}

	value := "false"
	if occupied {
		value = "true"
	}

	for _, candidate := range occupancyStateCandidates(resourceID) {
		if _, exists := current[candidate]; exists {
			pe.stateManager.UpdatePlanningState(planID, map[string]string{candidate: value})
			return
		}
	}
}

func occupancyStateCandidates(resourceID string) []string {
	trimmed := strings.TrimSpace(resourceID)
	if trimmed == "" {
		return nil
	}

	return []string{
		trimmed + " 점유",
		trimmed + " Occupied",
		trimmed + " occupied",
		trimmed + "_occupied",
		trimmed + ".occupied",
	}
}

type executionResourceCatalog struct {
	typeByID         map[string]db.TaskDistributorResource
	typeNameToID     map[string]string
	instanceByID     map[string]db.TaskDistributorResource
	instanceNameToID map[string]string
	instanceToType   map[string]string
	typeInstances    map[string][]db.TaskDistributorResource
}

func (pe *PlanExecutor) loadExecutionResourceCatalog(pp *db.PlanningProblem, bt *db.BehaviorTree) (executionResourceCatalog, error) {
	selectedDistributorID := ""
	if pp != nil && pp.TaskDistributorID.Valid && pp.TaskDistributorID.String != "" {
		selectedDistributorID = pp.TaskDistributorID.String
	} else if bt != nil && bt.TaskDistributorID.Valid && bt.TaskDistributorID.String != "" {
		selectedDistributorID = bt.TaskDistributorID.String
	}
	if selectedDistributorID == "" {
		return executionResourceCatalog{}, nil
	}

	resources, err := pe.repo.ListTaskDistributorResources(selectedDistributorID)
	if err != nil {
		return executionResourceCatalog{}, err
	}

	catalog := executionResourceCatalog{
		typeByID:         make(map[string]db.TaskDistributorResource),
		typeNameToID:     make(map[string]string),
		instanceByID:     make(map[string]db.TaskDistributorResource),
		instanceNameToID: make(map[string]string),
		instanceToType:   make(map[string]string),
		typeInstances:    make(map[string][]db.TaskDistributorResource),
	}

	for _, resource := range resources {
		if resource.Kind == "type" {
			catalog.typeByID[resource.ID] = resource
			catalog.typeNameToID[resource.Name] = resource.ID
			continue
		}
		catalog.instanceByID[resource.ID] = resource
		catalog.instanceNameToID[resource.Name] = resource.ID
		if resource.ParentResourceID != "" {
			catalog.instanceToType[resource.ID] = resource.ParentResourceID
			catalog.typeInstances[resource.ParentResourceID] = append(catalog.typeInstances[resource.ParentResourceID], resource)
		}
	}

	return catalog, nil
}

func (pe *PlanExecutor) resolveAcquireResourceToken(planID, token string, catalog executionResourceCatalog) (string, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return "", fmt.Errorf("empty resource token")
	}

	currentHolds := pe.stateManager.GetPlanResources(planID)
	heldNames := make(map[string]bool, len(currentHolds))
	for _, hold := range currentHolds {
		heldNames[hold.ResourceID] = true
	}

	if strings.HasPrefix(trimmed, "instance:") {
		instanceID := strings.TrimSpace(strings.TrimPrefix(trimmed, "instance:"))
		if instance, ok := catalog.instanceByID[instanceID]; ok {
			return instance.Name, nil
		}
		return "", fmt.Errorf("unknown instance id %q", instanceID)
	}

	typeID := ""
	if strings.HasPrefix(trimmed, "type:") {
		typeID = strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
	} else if resolvedTypeID, ok := catalog.typeNameToID[trimmed]; ok {
		typeID = resolvedTypeID
	}
	if typeID != "" {
		for _, instance := range catalog.typeInstances[typeID] {
			if !heldNames[instance.Name] {
				return instance.Name, nil
			}
		}
		return "", fmt.Errorf("no free instance for type %q", trimmed)
	}

	if instanceID, ok := catalog.instanceNameToID[trimmed]; ok {
		return catalog.instanceByID[instanceID].Name, nil
	}
	return trimmed, nil
}

func (pe *PlanExecutor) resolveReleaseResourceToken(planID, agentID, token string, catalog executionResourceCatalog) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "instance:") {
		instanceID := strings.TrimSpace(strings.TrimPrefix(trimmed, "instance:"))
		if instance, ok := catalog.instanceByID[instanceID]; ok {
			return instance.Name
		}
		return ""
	}

	typeID := ""
	if strings.HasPrefix(trimmed, "type:") {
		typeID = strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
	} else if resolvedTypeID, ok := catalog.typeNameToID[trimmed]; ok {
		typeID = resolvedTypeID
	}
	if typeID == "" {
		if instanceID, ok := catalog.instanceNameToID[trimmed]; ok {
			return catalog.instanceByID[instanceID].Name
		}
		return trimmed
	}

	currentHolds := pe.stateManager.GetPlanResources(planID)
	for _, hold := range currentHolds {
		instanceID, ok := catalog.instanceNameToID[hold.ResourceID]
		if !ok {
			continue
		}
		if catalog.instanceToType[instanceID] == typeID && hold.AgentID == agentID {
			return hold.ResourceID
		}
	}
	for _, hold := range currentHolds {
		instanceID, ok := catalog.instanceNameToID[hold.ResourceID]
		if !ok {
			continue
		}
		if catalog.instanceToType[instanceID] == typeID {
			return hold.ResourceID
		}
	}

	return ""
}

// waitForTask polls for task completion
func (pe *PlanExecutor) waitForTask(ctx context.Context, taskID string) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		case <-ticker.C:
			task, exists := pe.scheduler.GetTask(taskID)
			if !exists {
				// Task no longer active — check DB for final status
				dbTask, err := pe.repo.GetTask(taskID)
				if err != nil {
					return fmt.Errorf("failed to check task status: %v", err)
				}
				if dbTask == nil {
					return fmt.Errorf("task %s not found", taskID)
				}
				switch dbTask.Status {
				case string(TaskCompleted):
					return nil
				case string(TaskFailed):
					if dbTask.ErrorMessage.Valid {
						return fmt.Errorf("task failed: %s", dbTask.ErrorMessage.String)
					}
					return fmt.Errorf("task failed")
				case string(TaskCancelled):
					return fmt.Errorf("task cancelled")
				default:
					return nil // Assume completed if removed from active but not in DB as failed
				}
			}
			switch task.Status {
			case TaskCompleted:
				return nil
			case TaskFailed:
				return fmt.Errorf("task failed")
			case TaskCancelled:
				return fmt.Errorf("task cancelled")
			}
		}
	}
}

func (pe *PlanExecutor) failExecution(exec *PlanExecution, errMsg string) {
	exec.mu.Lock()
	exec.Status = PlanExecFailed
	exec.CompletedAt = time.Now()
	exec.Error = errMsg
	exec.mu.Unlock()

	pe.repo.UpdatePlanningProblemStatus(exec.ProblemID, "failed", nil, errMsg)
	pe.broadcastPlanUpdate(exec)
	pe.stateManager.ReleaseAllPlanResources(exec.ProblemID)

	log.Printf("[PlanExecutor] Plan execution %s failed: %s", exec.ID, errMsg)
}

func (pe *PlanExecutor) cancelExecution(exec *PlanExecution) {
	exec.mu.Lock()
	exec.Status = PlanExecCancelled
	exec.CompletedAt = time.Now()
	exec.Error = "cancelled by user"
	exec.mu.Unlock()

	pe.repo.UpdatePlanningProblemStatus(exec.ProblemID, "cancelled", nil, "cancelled by user")
	pe.broadcastPlanUpdate(exec)
	pe.stateManager.ReleaseAllPlanResources(exec.ProblemID)

	log.Printf("[PlanExecutor] Plan execution %s cancelled", exec.ID)
}

// PlanExecutionSnapshot is a thread-safe copy of PlanExecution state
type PlanExecutionSnapshot struct {
	ID             string
	ProblemID      string
	BehaviorTreeID string
	BehaviorTreeIDs []string
	Status         string
	CurrentOrder   int
	TotalOrders    int
	StartedAt      time.Time
	CompletedAt    *time.Time
	Error          string
	Steps          []StepStatusSnapshot
}

// StepStatusSnapshot is a thread-safe copy of step status
type StepStatusSnapshot struct {
	StepID        string
	StepName      string
	BehaviorTreeID string
	AgentID       string
	AgentName     string
	Order         int
	Status        string
	TaskID        string
	TaskName      string
	RuntimeTaskID string
	Error         string
}

// Snapshot returns a thread-safe copy of the execution state
func (exec *PlanExecution) Snapshot() PlanExecutionSnapshot {
	exec.mu.RLock()
	defer exec.mu.RUnlock()

	snap := PlanExecutionSnapshot{
		ID:              exec.ID,
		ProblemID:       exec.ProblemID,
		BehaviorTreeID:  exec.BehaviorTreeID,
		BehaviorTreeIDs: append([]string{}, exec.BehaviorTreeIDs...),
		Status:          string(exec.Status),
		CurrentOrder:    exec.CurrentOrder,
		TotalOrders:     exec.TotalOrders,
		StartedAt:       exec.StartedAt,
		Error:           exec.Error,
	}

	if !exec.CompletedAt.IsZero() {
		t := exec.CompletedAt
		snap.CompletedAt = &t
	}

	snap.Steps = make([]StepStatusSnapshot, 0, len(exec.StepStatuses))
	for _, ss := range exec.StepStatuses {
		snap.Steps = append(snap.Steps, StepStatusSnapshot{
			StepID:         ss.StepID,
			StepName:       ss.StepName,
			BehaviorTreeID: ss.BehaviorTreeID,
			AgentID:        ss.AgentID,
			AgentName:      ss.AgentName,
			Order:          ss.Order,
			Status:         string(ss.Status),
			TaskID:         ss.TaskID,
			TaskName:       ss.TaskName,
			RuntimeTaskID:  ss.RuntimeTaskID,
			Error:          ss.Error,
		})
	}

	return snap
}

// GetExecution returns an execution by ID
func (pe *PlanExecutor) GetExecution(id string) (*PlanExecution, bool) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	exec, ok := pe.executions[id]
	return exec, ok
}

// ListExecutions returns all active plan executions
func (pe *PlanExecutor) ListExecutions() []*PlanExecution {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	result := make([]*PlanExecution, 0, len(pe.executions))
	for _, exec := range pe.executions {
		result = append(result, exec)
	}
	return result
}

// CancelExecution cancels a running execution
func (pe *PlanExecutor) CancelExecution(id string) error {
	pe.mu.RLock()
	exec, ok := pe.executions[id]
	pe.mu.RUnlock()
	if !ok {
		return fmt.Errorf("execution %s not found", id)
	}

	exec.mu.RLock()
	status := exec.Status
	exec.mu.RUnlock()

	if status != PlanExecRunning && status != PlanExecPending {
		return fmt.Errorf("execution is not running (status: %s)", status)
	}

	exec.cancelFunc()
	return nil
}

// GetResourceAllocations returns current resource allocations across all plans
func (pe *PlanExecutor) GetResourceAllocations() []ResourceAllocationInfo {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	var allocations []ResourceAllocationInfo
	for _, exec := range pe.executions {
		exec.mu.RLock()
		status := exec.Status
		execID := exec.ID
		problemID := exec.ProblemID
		agentNames := make(map[string]string, len(exec.StepStatuses))
		for _, stepStatus := range exec.StepStatuses {
			agentNames[stepStatus.AgentID] = stepStatus.AgentName
		}
		exec.mu.RUnlock()

		if status != PlanExecRunning && status != PlanExecPending {
			continue
		}

		for _, hold := range pe.stateManager.GetPlanResources(problemID) {
			holderName := agentNames[hold.AgentID]
			if holderName == "" {
				holderName = hold.AgentID
			}
			allocations = append(allocations, ResourceAllocationInfo{
				Resource:        hold.ResourceID,
				HolderAgent:     holderName,
				HolderAgentID:   hold.AgentID,
				HolderAgentName: holderName,
				PlanID:          problemID,
				ProblemID:       problemID,
				PlanExecutionID: execID,
				TaskID:          hold.TaskID,
				StepID:          hold.StepID,
				AcquiredAt:      hold.AcquiredAt,
			})
		}
	}
	return allocations
}

// ResourceAllocationInfo represents a resource allocation
type ResourceAllocationInfo struct {
	Resource        string    `json:"resource"`
	HolderAgent     string    `json:"holder_agent"`
	HolderAgentID   string    `json:"holder_agent_id,omitempty"`
	HolderAgentName string    `json:"holder_agent_name,omitempty"`
	PlanID          string    `json:"plan_id,omitempty"`
	ProblemID       string    `json:"problem_id,omitempty"`
	PlanExecutionID string    `json:"plan_execution_id,omitempty"`
	TaskID          string    `json:"task_id,omitempty"`
	StepID          string    `json:"step_id,omitempty"`
	AcquiredAt      time.Time `json:"acquired_at"`
}

// broadcastPlanUpdate sends a plan execution update via WebSocket
func (pe *PlanExecutor) broadcastPlanUpdate(exec *PlanExecution) {
	if pe.broadcastFn == nil {
		return
	}

	exec.mu.RLock()
	msg := PlanExecutionUpdateMessage{
		Type:         "plan_execution_update",
		Timestamp:    time.Now().UnixMilli(),
		ExecutionID:  exec.ID,
		ProblemID:    exec.ProblemID,
		Status:       string(exec.Status),
		CurrentOrder: exec.CurrentOrder,
		TotalOrders:  exec.TotalOrders,
		Error:        exec.Error,
	}

	// Build step updates
	stepUpdates := make([]StepStatusWS, 0, len(exec.StepStatuses))
	for _, ss := range exec.StepStatuses {
		stepUpdates = append(stepUpdates, StepStatusWS{
			TaskID:    ss.TaskID,
			TaskName:  ss.TaskName,
			StepID:    ss.StepID,
			StepName:  ss.StepName,
			AgentID:   ss.AgentID,
			AgentName: ss.AgentName,
			Order:     ss.Order,
			Status:    string(ss.Status),
			Error:     ss.Error,
		})
	}
	msg.Steps = stepUpdates
	exec.mu.RUnlock()

	pe.broadcastFn(msg)
}

// PlanExecutionUpdateMessage is the WebSocket message for plan execution updates
type PlanExecutionUpdateMessage struct {
	Type         string         `json:"type"`
	Timestamp    int64          `json:"timestamp"`
	ExecutionID  string         `json:"execution_id"`
	ProblemID    string         `json:"problem_id"`
	Status       string         `json:"status"`
	CurrentOrder int            `json:"current_order"`
	TotalOrders  int            `json:"total_orders"`
	Steps        []StepStatusWS `json:"steps,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// StepStatusWS is a WebSocket representation of step status
type StepStatusWS struct {
	TaskID    string `json:"task_id,omitempty"`
	TaskName  string `json:"task_name,omitempty"`
	StepID    string `json:"step_id"`
	StepName  string `json:"step_name"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Order     int    `json:"order"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

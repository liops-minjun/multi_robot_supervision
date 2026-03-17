package pddl

import "central_server_go/internal/db"

type ResourceInfo struct {
	ID               string
	Name             string
	Kind             string
	ParentResourceID string
}

// PlanTask represents a single plannable task.
// In the current model a behavior tree is the task execution unit.
type PlanTask struct {
	TaskID              string
	TaskName            string
	BehaviorTreeID      string
	RequiredActionTypes []string
	Preconditions       []db.PlanningCondition
	RequiredResources   []string
	ResultStates        []db.PlanningEffect
	DuringState         []db.PlanningEffect
	RuntimeParams       map[string]string
	BoundAgentID        string
	BoundAgentName      string
}

// AgentInfo describes an agent available for task assignment.
type AgentInfo struct {
	ID           string
	Name         string
	Capabilities []string
	IsOnline     bool
}

// PlanProblem defines the full planning problem.
type PlanProblem struct {
	StateVars    []db.PlanningStateVar
	InitialState map[string]string
	GoalState    map[string]string
	Tasks        []PlanTask
	Agents       []AgentInfo
	Resources    []ResourceInfo
}

// TaskAssignment maps a task to an agent with execution order.
// step_* fields remain as compatibility aliases for older consumers.
type TaskAssignment struct {
	TaskID         string `json:"task_id"`
	TaskName       string `json:"task_name"`
	BehaviorTreeID string `json:"behavior_tree_id,omitempty"`
	StepID         string `json:"step_id,omitempty"`
	StepName       string `json:"step_name,omitempty"`
	AgentID        string `json:"agent_id"`
	AgentName      string `json:"agent_name"`
	Order          int    `json:"order"`
	Reason         string `json:"reason"`
	RuntimeParams  map[string]string `json:"runtime_params,omitempty"`
	ResultStates   []db.PlanningEffect `json:"result_states,omitempty"`
}

// StepAssignment is kept as an internal compatibility alias during the refactor.
type StepAssignment = TaskAssignment

// Plan is the result of the planner.
type Plan struct {
	Assignments    []TaskAssignment `json:"assignments"`
	IsValid        bool             `json:"is_valid"`
	ErrorMessage   string           `json:"error_message,omitempty"`
	TotalTasks     int              `json:"total_tasks"`
	TotalSteps     int              `json:"total_steps"`
	ParallelGroups int              `json:"parallel_groups"`
}

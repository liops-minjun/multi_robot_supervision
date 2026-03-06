package pddl

import "central_server_go/internal/db"

// PlanAction represents a single plannable action (maps to a BT step)
type PlanAction struct {
	StepID          string
	StepName        string
	ActionType      string // Required ROS2 capability (action_type)
	ResourceAcquire []string
	ResourceRelease []string
	Preconditions   []db.PlanningCondition
	Effects         []db.PlanningEffect
}

// AgentInfo describes an agent available for task assignment
type AgentInfo struct {
	ID           string
	Name         string
	Capabilities []string // action_type list this agent supports
	IsOnline     bool
}

// PlanProblem defines the full planning problem
type PlanProblem struct {
	StateVars    []db.PlanningStateVar
	InitialState map[string]string // variable -> value
	GoalState    map[string]string // variable -> value
	Actions      []PlanAction      // Available steps
	Agents       []AgentInfo       // Participating agents
}

// StepAssignment maps a step to an agent with execution order
type StepAssignment struct {
	StepID    string `json:"step_id"`
	StepName  string `json:"step_name"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Order     int    `json:"order"`  // Same order = parallel execution
	Reason    string `json:"reason"` // Why this agent was chosen
}

// Plan is the result of the planner
type Plan struct {
	Assignments    []StepAssignment `json:"assignments"`
	IsValid        bool             `json:"is_valid"`
	ErrorMessage   string           `json:"error_message,omitempty"`
	TotalSteps     int              `json:"total_steps"`
	ParallelGroups int              `json:"parallel_groups"`
}

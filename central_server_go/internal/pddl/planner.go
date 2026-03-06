package pddl

import (
	"central_server_go/internal/db"
	"fmt"
	"sort"
)

// Solve runs a forward-chaining planner with greedy agent assignment.
//
// Algorithm:
//  1. Build capability map (action_type -> capable agents)
//  2. Forward search from initial state: find actions whose preconditions
//     are satisfied, apply effects, repeat until goal is reached
//  3. Assign each step to the best capable agent (load balancing)
//  4. Compute parallel groups respecting resource and state dependencies
func Solve(problem *PlanProblem) *Plan {
	if err := ValidateProblem(problem); err != nil {
		return &Plan{IsValid: false, ErrorMessage: err.Error()}
	}

	// Build capability map
	capMap := buildCapabilityMap(problem.Agents)

	// Build agent name lookup
	agentNames := make(map[string]string)
	for _, a := range problem.Agents {
		agentNames[a.ID] = a.Name
	}

	// Current state starts from initial values (from state vars) merged with explicit initial state
	currentState := make(map[string]string)
	for _, sv := range problem.StateVars {
		if sv.InitialValue != "" {
			currentState[sv.Name] = sv.InitialValue
		}
	}
	for k, v := range problem.InitialState {
		currentState[k] = v
	}

	// Track which actions have been used
	used := make(map[string]bool)

	// Ordered list of actions to execute
	var orderedActions []PlanAction

	// Forward chaining: repeatedly find applicable actions until goal is met
	maxIterations := len(problem.Actions) * 2 // Prevent infinite loops
	for i := 0; i < maxIterations; i++ {
		if goalSatisfied(currentState, problem.GoalState) {
			break
		}

		// Find next applicable action
		found := false
		for _, action := range problem.Actions {
			if used[action.StepID] {
				continue
			}
			if preconditionsMet(currentState, action.Preconditions) {
				// Apply effects
				for _, eff := range action.Effects {
					currentState[eff.Variable] = eff.Value
				}
				used[action.StepID] = true
				orderedActions = append(orderedActions, action)
				found = true
				break // Re-evaluate from the start after applying effects
			}
		}

		if !found {
			// No applicable action found but goal not yet reached
			return &Plan{
				IsValid:      false,
				ErrorMessage: "no applicable actions found to reach goal state",
			}
		}
	}

	if !goalSatisfied(currentState, problem.GoalState) {
		return &Plan{
			IsValid:      false,
			ErrorMessage: "failed to reach goal state within iteration limit",
		}
	}

	// Assign agents to steps with load balancing
	agentLoad := make(map[string]int) // agentID -> number of assigned steps
	assignments := make([]StepAssignment, 0, len(orderedActions))

	for _, action := range orderedActions {
		agentID, agentName, reason := selectAgent(action, capMap, agentNames, agentLoad)
		assignments = append(assignments, StepAssignment{
			StepID:    action.StepID,
			StepName:  action.StepName,
			AgentID:   agentID,
			AgentName: agentName,
			Reason:    reason,
		})
		agentLoad[agentID]++
	}

	// Compute parallel execution groups
	computeParallelGroups(assignments, orderedActions)

	// Count parallel groups
	maxOrder := 0
	for _, a := range assignments {
		if a.Order > maxOrder {
			maxOrder = a.Order
		}
	}

	return &Plan{
		Assignments:    assignments,
		IsValid:        true,
		TotalSteps:     len(assignments),
		ParallelGroups: maxOrder + 1,
	}
}

// goalSatisfied checks if the current state satisfies the goal
func goalSatisfied(current, goal map[string]string) bool {
	for k, v := range goal {
		if current[k] != v {
			return false
		}
	}
	return true
}

// preconditionsMet checks if all preconditions are satisfied in the current state
func preconditionsMet(current map[string]string, conds []db.PlanningCondition) bool {
	for _, c := range conds {
		op := c.Operator
		if op == "" {
			op = "=="
		}
		currentVal := current[c.Variable]
		switch op {
		case "==":
			if currentVal != c.Value {
				return false
			}
		case "!=":
			if currentVal == c.Value {
				return false
			}
		}
	}
	return true
}

// selectAgent picks the best agent for an action using load balancing
func selectAgent(action PlanAction, capMap map[string][]string, agentNames map[string]string, agentLoad map[string]int) (string, string, string) {
	var candidates []string

	if action.ActionType != "" {
		candidates = capMap[action.ActionType]
	} else {
		// Steps without action_type can be assigned to any online agent
		for id := range agentNames {
			candidates = append(candidates, id)
		}
	}

	if len(candidates) == 0 {
		return "", "", "no capable agent"
	}

	// Sort by load (ascending), then by ID for stability
	sort.Slice(candidates, func(i, j int) bool {
		li, lj := agentLoad[candidates[i]], agentLoad[candidates[j]]
		if li != lj {
			return li < lj
		}
		return candidates[i] < candidates[j]
	})

	best := candidates[0]
	reason := fmt.Sprintf("has capability %q, lowest load (%d tasks)", action.ActionType, agentLoad[best])
	if action.ActionType == "" {
		reason = fmt.Sprintf("no specific capability required, lowest load (%d tasks)", agentLoad[best])
	}

	return best, agentNames[best], reason
}

// computeParallelGroups assigns execution order to assignments.
// Steps that share resources or have state dependencies are sequenced.
// Independent steps get the same order (can run in parallel).
func computeParallelGroups(assignments []StepAssignment, actions []PlanAction) {
	if len(assignments) == 0 {
		return
	}

	// Build action lookup
	actionMap := make(map[string]PlanAction)
	for _, a := range actions {
		actionMap[a.StepID] = a
	}

	// Track which variables each step reads/writes
	// A step must come after any step that writes a variable it reads (precondition)
	// and after any step that reads/writes a variable it writes (effect)

	// Also track resource conflicts
	order := 0
	// Track which resources are currently held (resource -> step index that holds it)
	heldResources := make(map[string]int) // resource -> order when it will be released
	// Track the latest order that wrote each variable
	varWriteOrder := make(map[string]int)

	for i := range assignments {
		action := actionMap[assignments[i].StepID]
		minOrder := 0

		// Check precondition dependencies: must come after the step that set the variable
		for _, cond := range action.Preconditions {
			if wo, ok := varWriteOrder[cond.Variable]; ok {
				if wo+1 > minOrder {
					minOrder = wo + 1
				}
			}
		}

		// Check resource conflicts: can't acquire a resource held by a concurrent step
		for _, res := range action.ResourceAcquire {
			if releaseOrder, ok := heldResources[res]; ok {
				if releaseOrder+1 > minOrder {
					minOrder = releaseOrder + 1
				}
			}
		}

		assignments[i].Order = minOrder

		// Update variable write tracking
		for _, eff := range action.Effects {
			varWriteOrder[eff.Variable] = minOrder
		}

		// Update resource tracking
		for _, res := range action.ResourceAcquire {
			heldResources[res] = minOrder
		}
		for _, res := range action.ResourceRelease {
			delete(heldResources, res)
		}

		if minOrder > order {
			order = minOrder
		}
	}
}

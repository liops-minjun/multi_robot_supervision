package pddl

import (
	"central_server_go/internal/db"
	"fmt"
	"sort"
)

// Solve runs a forward-chaining planner with greedy agent assignment.
//
// Algorithm:
//  1. Validate problem
//  2. Delete-relaxation reachability pre-check
//  3. Forward search from initial state: find actions whose preconditions
//     are satisfied, apply effects, repeat until goal is reached
//  4. Assign each step to the best capable agent (load balancing)
//  5. Compute parallel groups respecting resource, state, During-invariant,
//     and same-agent constraints
func Solve(problem *PlanProblem) *Plan {
	if err := ValidateProblem(problem); err != nil {
		return &Plan{IsValid: false, ErrorMessage: err.Error()}
	}

	// Reachability pre-check (delete-relaxation)
	if err := checkReachability(problem); err != nil {
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
	computeParallelGroups(assignments, orderedActions, problem.Resources)

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

// checkReachability performs delete-relaxation analysis.
// It applies all effect values without removing preconditions,
// computing the maximum reachable state space.
// Returns error if goal is provably unreachable.
func checkReachability(problem *PlanProblem) error {
	reachable := make(map[string]map[string]bool) // var -> set of possible values

	// Seed with initial state values
	for _, sv := range problem.StateVars {
		reachable[sv.Name] = map[string]bool{}
		if sv.InitialValue != "" {
			reachable[sv.Name][sv.InitialValue] = true
		}
	}
	for k, v := range problem.InitialState {
		if reachable[k] == nil {
			reachable[k] = map[string]bool{}
		}
		reachable[k][v] = true
	}

	// Fixed-point iteration: keep adding reachable values until no change
	changed := true
	for changed {
		changed = false
		for _, action := range problem.Actions {
			// Check if preconditions CAN be satisfied (any reachable value matches)
			if !relaxedPreconditionsMet(reachable, action.Preconditions) {
				continue
			}
			// Add effect values to reachable set
			for _, eff := range action.Effects {
				if reachable[eff.Variable] == nil {
					reachable[eff.Variable] = map[string]bool{}
				}
				if !reachable[eff.Variable][eff.Value] {
					reachable[eff.Variable][eff.Value] = true
					changed = true
				}
			}
		}
	}

	// Check if every goal value is reachable
	for varName, goalValue := range problem.GoalState {
		if reachable[varName] == nil || !reachable[varName][goalValue] {
			return fmt.Errorf("goal %s=%s is unreachable: no action chain can produce this value", varName, goalValue)
		}
	}
	return nil
}

// relaxedPreconditionsMet checks if preconditions can be satisfied
// under the relaxed (delete-free) model — any reachable value counts.
func relaxedPreconditionsMet(reachable map[string]map[string]bool, conds []db.PlanningCondition) bool {
	for _, c := range conds {
		vals := reachable[c.Variable]
		if vals == nil {
			return false
		}
		op := c.Operator
		if op == "" {
			op = "=="
		}
		switch op {
		case "==":
			if !vals[c.Value] {
				return false
			}
		case "!=":
			// Under relaxation, != is satisfiable if there's any value other than c.Value
			hasOther := false
			for v := range vals {
				if v != c.Value {
					hasOther = true
					break
				}
			}
			if !hasOther {
				return false
			}
		}
	}
	return true
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

// duringConflict returns true if actions a and b cannot run concurrently
// because one's effects violate the other's During invariants.
func duringConflict(a, b PlanAction) bool {
	// Check a.During vs b.Effects
	for _, dur := range a.During {
		for _, eff := range b.Effects {
			if dur.Variable == eff.Variable && dur.Value != eff.Value {
				return true
			}
		}
	}
	// Check b.During vs a.Effects
	for _, dur := range b.During {
		for _, eff := range a.Effects {
			if dur.Variable == eff.Variable && dur.Value != eff.Value {
				return true
			}
		}
	}
	return false
}

// computeParallelGroups assigns execution order to assignments.
// Steps are sequenced when they have:
//   - State variable dependencies (precondition reads vs effect writes)
//   - Resource conflicts (same resource acquired in same group)
//   - During-invariant conflicts (one's effects violate another's During)
//   - Same-agent collisions (agent can only run one step at a time)
//
// Independent steps get the same order (can run in parallel).
func computeParallelGroups(assignments []StepAssignment, actions []PlanAction, resources []ResourceInfo) {
	if len(assignments) == 0 {
		return
	}

	// Build action lookup
	actionMap := make(map[string]PlanAction)
	for _, a := range actions {
		actionMap[a.StepID] = a
	}
	resourceCatalog := buildResourceCatalog(resources)

	// Track which concrete instances are currently held.
	heldInstances := make(map[string]int)
	// Track the latest order that wrote each variable
	varWriteOrder := make(map[string]int)
	// Track concrete instances acquired per order group
	orderInstances := map[int]map[string]bool{}
	// Track agents used per order group
	orderAgents := map[int]map[string]bool{}
	// Track step indices per order group (for During conflict checks)
	orderSteps := map[int][]int{}

	for i := range assignments {
		action := actionMap[assignments[i].StepID]
		minOrder := 0

		// 1. Precondition dependencies: must come after the step that set the variable
		for _, cond := range action.Preconditions {
			if wo, ok := varWriteOrder[cond.Variable]; ok {
				if wo+1 > minOrder {
					minOrder = wo + 1
				}
			}
		}

		// 2. Explicit instance locks held by prior steps.
		for _, token := range action.ResourceAcquire {
			ref := resolveResourceToken(token, resourceCatalog)
			if ref.Kind == "instance" {
				if releaseOrder, ok := heldInstances[ref.Key]; ok && releaseOrder+1 > minOrder {
					minOrder = releaseOrder + 1
				}
			}
		}

		// 3. Resource conflict within order group
		for {
			conflict := false
			for _, token := range action.ResourceAcquire {
				ref := resolveResourceToken(token, resourceCatalog)
				if ref.Kind == "instance" && orderInstances[minOrder] != nil && orderInstances[minOrder][ref.Key] {
					conflict = true
					break
				}
				if ref.Kind == "type" && findFreeTypeInstance(ref.TypeKey, heldInstances, orderInstances[minOrder], resourceCatalog) == "" {
					conflict = true
					break
				}
			}
			if !conflict {
				break
			}
			minOrder++
		}

		// 4. Same-agent collision within order group
		agentID := assignments[i].AgentID
		for orderAgents[minOrder] != nil && orderAgents[minOrder][agentID] {
			minOrder++
		}

		// 5. During-invariant conflict with group members
		for {
			conflict := false
			for _, j := range orderSteps[minOrder] {
				if duringConflict(action, actionMap[assignments[j].StepID]) {
					conflict = true
					break
				}
			}
			if !conflict {
				break
			}
			minOrder++
		}

		// Assign order
		assignments[i].Order = minOrder

		// Update variable write tracking
		for _, eff := range action.Effects {
			varWriteOrder[eff.Variable] = minOrder
		}

		// Update resource tracking
		if orderInstances[minOrder] == nil {
			orderInstances[minOrder] = map[string]bool{}
		}
		for _, token := range action.ResourceAcquire {
			ref := resolveResourceToken(token, resourceCatalog)
			if ref.Kind == "instance" {
				heldInstances[ref.Key] = minOrder
				orderInstances[minOrder][ref.Key] = true
			}
			if ref.Kind == "type" {
				instanceID := findFreeTypeInstance(ref.TypeKey, heldInstances, orderInstances[minOrder], resourceCatalog)
				if instanceID != "" {
					heldInstances[instanceID] = minOrder
					orderInstances[minOrder][instanceID] = true
				}
			}
		}
		for _, token := range action.ResourceRelease {
			ref := resolveResourceToken(token, resourceCatalog)
			if ref.Kind == "instance" {
				delete(heldInstances, ref.Key)
			}
			if ref.Kind == "type" {
				releaseOneHeldTypeInstance(ref.TypeKey, heldInstances, resourceCatalog)
			}
		}

		// Track agent in this order group
		if orderAgents[minOrder] == nil {
			orderAgents[minOrder] = map[string]bool{}
		}
		orderAgents[minOrder][agentID] = true

		// Track step index in this order group
		orderSteps[minOrder] = append(orderSteps[minOrder], i)
	}
}

func findFreeTypeInstance(typeID string, heldInstances map[string]int, reservedInstances map[string]bool, catalog resourceCatalog) string {
	for _, instanceID := range catalog.typeInstances[typeID] {
		if _, ok := heldInstances[instanceID]; ok {
			continue
		}
		if reservedInstances != nil && reservedInstances[instanceID] {
			continue
		}
		return instanceID
	}
	return ""
}

func releaseOneHeldTypeInstance(typeID string, heldInstances map[string]int, catalog resourceCatalog) {
	for _, instanceID := range catalog.typeInstances[typeID] {
		if _, ok := heldInstances[instanceID]; ok {
			delete(heldInstances, instanceID)
			return
		}
	}
}

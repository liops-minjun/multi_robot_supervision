package pddl

import "fmt"

// ValidateProblem checks the planning problem for correctness
func ValidateProblem(problem *PlanProblem) error {
	if problem == nil {
		return fmt.Errorf("problem is nil")
	}

	// Build set of declared state variable names
	varNames := make(map[string]bool, len(problem.StateVars))
	for _, sv := range problem.StateVars {
		if sv.Name == "" {
			return fmt.Errorf("state variable has empty name")
		}
		varNames[sv.Name] = true
	}

	// Check goal variables exist in state vars
	for varName := range problem.GoalState {
		if !varNames[varName] {
			return fmt.Errorf("goal variable %q is not declared in planning_states", varName)
		}
	}

	// Check initial state variables exist
	for varName := range problem.InitialState {
		if !varNames[varName] {
			return fmt.Errorf("initial state variable %q is not declared in planning_states", varName)
		}
	}

	// Check each action's precondition/effect variables exist
	for _, action := range problem.Actions {
		for _, cond := range action.Preconditions {
			if !varNames[cond.Variable] {
				return fmt.Errorf("action %q precondition variable %q is not declared in planning_states", action.StepID, cond.Variable)
			}
		}
		for _, eff := range action.Effects {
			if !varNames[eff.Variable] {
				return fmt.Errorf("action %q effect variable %q is not declared in planning_states", action.StepID, eff.Variable)
			}
		}
	}

	// Check that every action has at least one capable agent
	capMap := buildCapabilityMap(problem.Agents)
	for _, action := range problem.Actions {
		if action.ActionType == "" {
			continue // Steps without action_type (e.g., wait steps) can be assigned to any agent
		}
		agents := capMap[action.ActionType]
		if len(agents) == 0 {
			return fmt.Errorf("no agent has capability %q required by step %q", action.ActionType, action.StepID)
		}
	}

	if len(problem.GoalState) == 0 {
		return fmt.Errorf("goal state is empty")
	}

	return nil
}

// buildCapabilityMap creates action_type -> []agentID mapping
func buildCapabilityMap(agents []AgentInfo) map[string][]string {
	capMap := make(map[string][]string)
	for _, agent := range agents {
		if !agent.IsOnline {
			continue
		}
		for _, cap := range agent.Capabilities {
			capMap[cap] = append(capMap[cap], agent.ID)
		}
	}
	return capMap
}

package pddl

import (
	"fmt"
	"strings"
)

// ValidateProblem checks the planning problem for correctness.
func ValidateProblem(problem *PlanProblem) error {
	if problem == nil {
		return fmt.Errorf("problem is nil")
	}
	if len(problem.GoalState) == 0 {
		return fmt.Errorf("goal state is empty")
	}

	resourceCatalog := buildResourceCatalog(problem.Resources)

	varNames := make(map[string]bool, len(problem.StateVars))
	for _, sv := range problem.StateVars {
		if sv.Name == "" {
			return fmt.Errorf("state variable has empty name")
		}
		varNames[sv.Name] = true
	}

	for varName := range problem.GoalState {
		if !varNames[varName] {
			return fmt.Errorf("goal variable %q is not declared in planning_states", varName)
		}
	}
	for varName := range problem.InitialState {
		if !varNames[varName] {
			return fmt.Errorf("initial state variable %q is not declared in planning_states", varName)
		}
	}

	if len(problem.Tasks) == 0 {
		return fmt.Errorf("no planning task is defined for the selected behavior tree")
	}

	for _, task := range problem.Tasks {
		if strings.TrimSpace(task.TaskID) == "" {
			return fmt.Errorf("planning task has empty id")
		}
		for _, eff := range task.ResultStates {
			if !varNames[eff.Variable] {
				return fmt.Errorf("task %q result variable %q is not declared in planning_states", task.TaskID, eff.Variable)
			}
		}
		for _, dur := range task.DuringState {
			if !varNames[dur.Variable] {
				return fmt.Errorf("task %q during variable %q is not declared in planning_states", task.TaskID, dur.Variable)
			}
		}
		for _, token := range task.RequiredResources {
			ref := resolveResourceToken(token, resourceCatalog)
			switch ref.Kind {
			case "type":
				if _, ok := resourceCatalog.typeByID[ref.Key]; !ok {
					return fmt.Errorf("task %q references unknown resource type %q", task.TaskID, token)
				}
				if resourceCatalog.typeCapacity[ref.Key] <= 0 {
					return fmt.Errorf("task %q references resource type %q but it has no instances", task.TaskID, token)
				}
			default:
				if strings.HasPrefix(token, "instance:") {
					if _, ok := resourceCatalog.instanceByID[ref.Key]; !ok {
						return fmt.Errorf("task %q references unknown resource instance %q", task.TaskID, token)
					}
				}
			}
		}

		if task.BoundAgentID != "" {
			var boundAgent *AgentInfo
			for idx := range problem.Agents {
				if problem.Agents[idx].ID == task.BoundAgentID {
					boundAgent = &problem.Agents[idx]
					break
				}
			}
			if boundAgent == nil {
				return fmt.Errorf("task %q is bound to unknown agent %q", task.TaskID, task.BoundAgentID)
			}
			if !agentCanRunTask(*boundAgent, task) {
				return fmt.Errorf("task %q is bound to agent %q but it is offline or missing required capabilities", task.TaskID, task.BoundAgentID)
			}
			continue
		}

		if len(capableAgentIDs(task, problem.Agents)) == 0 {
			return fmt.Errorf("no online agent satisfies capabilities required by task %q", task.TaskID)
		}
	}

	return nil
}

func capableAgentIDs(task PlanTask, agents []AgentInfo) []string {
	var candidates []string
	for _, agent := range agents {
		if !agent.IsOnline {
			continue
		}
		if agentCanRunTask(agent, task) {
			candidates = append(candidates, agent.ID)
		}
	}
	return candidates
}

func agentCanRunTask(agent AgentInfo, task PlanTask) bool {
	if !agent.IsOnline {
		return false
	}
	if len(task.RequiredActionTypes) == 0 {
		return true
	}

	capSet := make(map[string]bool, len(agent.Capabilities))
	for _, capability := range agent.Capabilities {
		capSet[capability] = true
	}
	for _, required := range task.RequiredActionTypes {
		if !capSet[required] {
			return false
		}
	}
	return true
}

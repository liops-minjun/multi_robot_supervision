package pddl

import (
	"central_server_go/internal/db"
	"fmt"
	"sort"
	"strings"
)

// Solve runs a task-level planner.
//
// The behavior tree itself is the execution unit; the planner only decides
// which task definitions are needed to satisfy the requested goal state.
func Solve(problem *PlanProblem) *Plan {
	if err := ValidateProblem(problem); err != nil {
		return &Plan{IsValid: false, ErrorMessage: err.Error()}
	}

	if err := checkReachability(problem); err != nil {
		return &Plan{IsValid: false, ErrorMessage: err.Error()}
	}

	agentNames := make(map[string]string, len(problem.Agents))
	for _, agent := range problem.Agents {
		agentNames[agent.ID] = agent.Name
	}

	currentState := make(map[string]string)
	for _, sv := range problem.StateVars {
		if sv.InitialValue != "" {
			currentState[sv.Name] = sv.InitialValue
		}
	}
	for key, value := range problem.InitialState {
		currentState[key] = value
	}

	if goalSatisfied(currentState, problem.GoalState) {
		return &Plan{
			Assignments:    []TaskAssignment{},
			IsValid:        true,
			TotalTasks:     0,
			TotalSteps:     0,
			ParallelGroups: 0,
		}
	}

	used := make(map[string]bool)
	var orderedTasks []PlanTask

	maxIterations := len(problem.Tasks) * 4
	for i := 0; i < maxIterations; i++ {
		if goalSatisfied(currentState, problem.GoalState) {
			break
		}

		unsatisfiedGoals := collectUnsatisfiedGoals(currentState, problem.GoalState)
		bestIndex := -1
		bestDirectScore := -1
		bestEnableScore := -1
		for idx, task := range problem.Tasks {
			if used[task.TaskID] {
				continue
			}
			if !preconditionsMet(currentState, task.Preconditions) {
				continue
			}
			directScore := taskGoalProgress(task, currentState, problem.GoalState)
			enableScore := taskEnableProgress(task, currentState, problem.Tasks, unsatisfiedGoals, used)
			if directScore <= 0 && enableScore <= 0 {
				continue
			}
			if directScore > bestDirectScore || (directScore == bestDirectScore && enableScore > bestEnableScore) {
				bestIndex = idx
				bestDirectScore = directScore
				bestEnableScore = enableScore
			}
		}

		if bestIndex < 0 {
			return &Plan{
				IsValid:      false,
				ErrorMessage: "no task can advance the requested goal state",
			}
		}

		task := problem.Tasks[bestIndex]
		for _, effect := range task.ResultStates {
			currentState[effect.Variable] = effect.Value
		}
		used[task.TaskID] = true
		orderedTasks = append(orderedTasks, task)
	}

	if !goalSatisfied(currentState, problem.GoalState) {
		return &Plan{
			IsValid:      false,
			ErrorMessage: "failed to reach goal state within iteration limit",
		}
	}

	assignments := make([]TaskAssignment, 0, len(orderedTasks))
	agentLoad := make(map[string]int)
	for _, task := range orderedTasks {
		agentID, agentName, reason := selectAgent(task, problem.Agents, agentNames, agentLoad)
		if agentID == "" {
			return &Plan{
				IsValid:      false,
				ErrorMessage: fmt.Sprintf("no capable agent available for task %q", task.TaskName),
			}
		}
		runtimeParams := cloneStringMap(task.RuntimeParams)
		if runtimeParams == nil {
			runtimeParams = make(map[string]string)
		}
		if strings.TrimSpace(agentName) == "" {
			agentName = agentID
		}
		runtimeParams["agent"] = agentName
		runtimeParams["agent_id"] = agentID
		runtimeParams["agent_name"] = agentName
		runtimeParams["agent.id"] = agentID
		runtimeParams["agent.name"] = agentName
		assignments = append(assignments, TaskAssignment{
			TaskID:         task.TaskID,
			TaskName:       task.TaskName,
			BehaviorTreeID: task.BehaviorTreeID,
			StepID:         task.TaskID,
			StepName:       task.TaskName,
			AgentID:        agentID,
			AgentName:      agentName,
			Reason:         reason,
			RuntimeParams:  runtimeParams,
			ResultStates:   cloneEffects(task.ResultStates),
		})
		agentLoad[agentID]++
	}

	computeParallelGroups(assignments, orderedTasks, problem.Resources)

	maxOrder := 0
	for _, assignment := range assignments {
		if assignment.Order > maxOrder {
			maxOrder = assignment.Order
		}
	}

	return &Plan{
		Assignments:    assignments,
		IsValid:        true,
		TotalTasks:     len(assignments),
		TotalSteps:     len(assignments),
		ParallelGroups: maxOrder + 1,
	}
}

func checkReachability(problem *PlanProblem) error {
	reachable := make(map[string]map[string]bool)
	for _, sv := range problem.StateVars {
		reachable[sv.Name] = map[string]bool{}
		if sv.InitialValue != "" {
			reachable[sv.Name][sv.InitialValue] = true
		}
	}
	for key, value := range problem.InitialState {
		if reachable[key] == nil {
			reachable[key] = map[string]bool{}
		}
		reachable[key][value] = true
	}

	changed := true
	for changed {
		changed = false
		for _, task := range problem.Tasks {
			if !relaxedPreconditionsMet(reachable, task.Preconditions) {
				continue
			}
			for _, effect := range task.ResultStates {
				if reachable[effect.Variable] == nil {
					reachable[effect.Variable] = map[string]bool{}
				}
				if !reachable[effect.Variable][effect.Value] {
					reachable[effect.Variable][effect.Value] = true
					changed = true
				}
			}
		}
	}

	for varName, goalValue := range problem.GoalState {
		if reachable[varName] == nil || !reachable[varName][goalValue] {
			return fmt.Errorf("goal %s=%s is unreachable: no task can produce this value", varName, goalValue)
		}
	}
	return nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func goalSatisfied(current, goal map[string]string) bool {
	for key, value := range goal {
		if current[key] != value {
			return false
		}
	}
	return true
}

func taskGoalProgress(task PlanTask, current, goal map[string]string) int {
	score := 0
	for _, effect := range task.ResultStates {
		goalValue, wanted := goal[effect.Variable]
		if !wanted {
			continue
		}
		if current[effect.Variable] == goalValue {
			continue
		}
		if effect.Value == goalValue {
			score++
		}
	}
	return score
}

func collectUnsatisfiedGoals(current, goal map[string]string) map[string]string {
	unsatisfied := make(map[string]string)
	for key, value := range goal {
		if current[key] != value {
			unsatisfied[key] = value
		}
	}
	return unsatisfied
}

func conditionMet(current map[string]string, cond db.PlanningCondition) bool {
	op := cond.Operator
	if op == "" {
		op = "=="
	}
	currentValue := current[cond.Variable]
	switch op {
	case "!=":
		return currentValue != cond.Value
	case "==":
		fallthrough
	default:
		return currentValue == cond.Value
	}
}

func effectSatisfiesCondition(effect db.PlanningEffect, cond db.PlanningCondition) bool {
	if effect.Variable != cond.Variable {
		return false
	}
	op := cond.Operator
	if op == "" {
		op = "=="
	}
	switch op {
	case "!=":
		return effect.Value != cond.Value
	case "==":
		fallthrough
	default:
		return effect.Value == cond.Value
	}
}

func taskCanProduceUnsatisfiedGoal(task PlanTask, unsatisfiedGoals map[string]string) bool {
	for _, effect := range task.ResultStates {
		goalValue, wanted := unsatisfiedGoals[effect.Variable]
		if !wanted {
			continue
		}
		if effect.Value == goalValue {
			return true
		}
	}
	return false
}

func taskEnableProgress(
	task PlanTask,
	current map[string]string,
	allTasks []PlanTask,
	unsatisfiedGoals map[string]string,
	used map[string]bool,
) int {
	score := 0
	for _, targetTask := range allTasks {
		if used[targetTask.TaskID] {
			continue
		}
		if !taskCanProduceUnsatisfiedGoal(targetTask, unsatisfiedGoals) {
			continue
		}
		for _, cond := range targetTask.Preconditions {
			if conditionMet(current, cond) {
				continue
			}
			for _, effect := range task.ResultStates {
				if effectSatisfiesCondition(effect, cond) {
					score++
					break
				}
			}
		}
	}
	return score
}

func selectAgent(task PlanTask, agents []AgentInfo, agentNames map[string]string, agentLoad map[string]int) (string, string, string) {
	if task.BoundAgentID != "" {
		for _, agent := range agents {
			if agent.ID != task.BoundAgentID {
				continue
			}
			if !agentCanRunTask(agent, task) {
				return "", "", "bound agent is offline or missing required capabilities"
			}
			name := strings.TrimSpace(agent.Name)
			if name == "" {
				name = agent.ID
			}
			return agent.ID, name, "task is bound by {{agent.*}} placeholder"
		}
		return "", "", "bound agent not found"
	}

	candidates := capableAgentIDs(task, agents)
	if len(candidates) == 0 {
		return "", "", "no capable agent"
	}

	sort.Slice(candidates, func(i, j int) bool {
		li, lj := agentLoad[candidates[i]], agentLoad[candidates[j]]
		if li != lj {
			return li < lj
		}
		return candidates[i] < candidates[j]
	})

	best := candidates[0]
	reason := fmt.Sprintf("satisfies %d required capabilities, lowest load (%d tasks)", len(task.RequiredActionTypes), agentLoad[best])
	if len(task.RequiredActionTypes) == 0 {
		reason = fmt.Sprintf("no specific capability required, lowest load (%d tasks)", agentLoad[best])
	}
	return best, agentNames[best], reason
}

func duringConflict(left, right PlanTask) bool {
	for _, dur := range left.DuringState {
		for _, eff := range right.ResultStates {
			if dur.Variable == eff.Variable && dur.Value != eff.Value {
				return true
			}
		}
	}
	for _, dur := range right.DuringState {
		for _, eff := range left.ResultStates {
			if dur.Variable == eff.Variable && dur.Value != eff.Value {
				return true
			}
		}
	}
	return false
}

func computeParallelGroups(assignments []TaskAssignment, tasks []PlanTask, resources []ResourceInfo) {
	if len(assignments) == 0 {
		return
	}

	taskMap := make(map[string]PlanTask, len(tasks))
	for _, task := range tasks {
		taskMap[task.TaskID] = task
	}
	resourceCatalog := buildResourceCatalog(resources)

	heldInstances := make(map[string]int)
	orderInstances := map[int]map[string]bool{}
	orderAgents := map[int]map[string]bool{}
	orderTasks := map[int][]int{}

	for i := range assignments {
		task := taskMap[assignments[i].TaskID]
		minOrder := 0

		for _, token := range task.RequiredResources {
			ref := resolveResourceToken(token, resourceCatalog)
			if ref.Kind == "instance" {
				if releaseOrder, ok := heldInstances[ref.Key]; ok && releaseOrder+1 > minOrder {
					minOrder = releaseOrder + 1
				}
			}
		}

		for {
			conflict := false
			for _, token := range task.RequiredResources {
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

		agentID := assignments[i].AgentID
		for orderAgents[minOrder] != nil && orderAgents[minOrder][agentID] {
			minOrder++
		}

		for {
			conflict := false
			for _, j := range orderTasks[minOrder] {
				if duringConflict(task, taskMap[assignments[j].TaskID]) {
					conflict = true
					break
				}
			}
			if !conflict {
				break
			}
			minOrder++
		}

		assignments[i].Order = minOrder

		if orderInstances[minOrder] == nil {
			orderInstances[minOrder] = map[string]bool{}
		}
		for _, token := range task.RequiredResources {
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

		if orderAgents[minOrder] == nil {
			orderAgents[minOrder] = map[string]bool{}
		}
		orderAgents[minOrder][agentID] = true
		orderTasks[minOrder] = append(orderTasks[minOrder], i)
	}
}

func findFreeTypeInstance(typeID string, heldInstances map[string]int, currentOrder map[string]bool, catalog resourceCatalog) string {
	for _, instanceID := range catalog.typeInstances[typeID] {
		if _, held := heldInstances[instanceID]; held {
			continue
		}
		if currentOrder != nil && currentOrder[instanceID] {
			continue
		}
		return instanceID
	}
	return ""
}

// Legacy compatibility for helper reuse.
func relaxedPreconditionsMet(reachable map[string]map[string]bool, conds []db.PlanningCondition) bool {
	for _, cond := range conds {
		if cond.Variable == "" {
			continue
		}
		values := reachable[cond.Variable]
		if len(values) == 0 {
			return false
		}

		op := cond.Operator
		if op == "" {
			op = "=="
		}

		switch op {
		case "==":
			if !values[cond.Value] {
				return false
			}
		case "!=":
			if len(values) == 1 && values[cond.Value] {
				return false
			}
		default:
			if !values[cond.Value] {
				return false
			}
		}
	}
	return true
}

func preconditionsMet(current map[string]string, conds []db.PlanningCondition) bool {
	for _, cond := range conds {
		if cond.Variable == "" {
			continue
		}
		op := cond.Operator
		if op == "" {
			op = "=="
		}

		currentValue := current[cond.Variable]
		switch op {
		case "!=":
			if currentValue == cond.Value {
				return false
			}
		case "==":
			fallthrough
		default:
			if currentValue != cond.Value {
				return false
			}
		}
	}
	return true
}

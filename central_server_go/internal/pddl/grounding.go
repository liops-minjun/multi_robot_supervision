package pddl

import (
	"central_server_go/internal/db"
	"fmt"
	"sort"
	"strings"
)

type groundedResource struct {
	Token    string
	Resource ResourceInfo
	TypeID   string
	TypeName string
}

// GroundTasks expands generic task templates into concrete resource-bound tasks.
// Currently this is primarily intended for one generic task template such as
// "go_to_cnc_and_park" that should be solved once per concrete CNC instance.
func GroundTasks(tasks []PlanTask, resources []ResourceInfo, agents []AgentInfo) ([]PlanTask, error) {
	catalog := buildResourceCatalog(resources)
	grounded := make([]PlanTask, 0, len(tasks))

	for _, task := range tasks {
		taskGrounded, err := groundTask(task, catalog, agents)
		if err != nil {
			return nil, err
		}
		grounded = append(grounded, taskGrounded...)
	}

	return grounded, nil
}

func groundTask(task PlanTask, catalog resourceCatalog, agents []AgentInfo) ([]PlanTask, error) {
	type candidateSet struct {
		token     string
		instances []string
	}

	fixed := make(map[string]groundedResource)
	var dynamic []candidateSet

	for _, token := range task.RequiredResources {
		ref := resolveResourceToken(token, catalog)
		switch ref.Kind {
		case "type":
			instanceIDs := append([]string{}, catalog.typeInstances[ref.Key]...)
			if len(instanceIDs) == 0 {
				return nil, fmt.Errorf("task %q references resource type %q but it has no instances", task.TaskID, token)
			}
			dynamic = append(dynamic, candidateSet{token: token, instances: instanceIDs})
		case "instance":
			resource, ok := catalog.instanceByID[ref.Key]
			if !ok {
				return nil, fmt.Errorf("task %q references unknown resource instance %q", task.TaskID, token)
			}
			typeID := catalog.instanceToType[resource.ID]
			fixed[token] = groundedResource{
				Token:    token,
				Resource: resource,
				TypeID:   typeID,
				TypeName: catalog.typeByID[typeID].Name,
			}
		default:
			fixed[token] = groundedResource{
				Token: token,
				Resource: ResourceInfo{
					ID:   strings.TrimSpace(token),
					Name: strings.TrimSpace(token),
					Kind: "instance",
				},
			}
		}
	}

	if len(dynamic) == 0 {
		groundedTask, err := instantiateGroundTask(task, fixed)
		if err != nil {
			return nil, err
		}
		return groundTaskByAgent([]PlanTask{groundedTask}, agents)
	}

	sort.Slice(dynamic, func(i, j int) bool {
		return dynamic[i].token < dynamic[j].token
	})

	results := make([]PlanTask, 0)
	used := make(map[string]bool)

	var expand func(index int, bindings map[string]groundedResource) error
	expand = func(index int, bindings map[string]groundedResource) error {
		if index >= len(dynamic) {
			merged := make(map[string]groundedResource, len(fixed)+len(bindings))
			for token, binding := range fixed {
				merged[token] = binding
			}
			for token, binding := range bindings {
				merged[token] = binding
			}
			groundedTask, err := instantiateGroundTask(task, merged)
			if err != nil {
				return err
			}
			results = append(results, groundedTask)
			return nil
		}

		set := dynamic[index]
		for _, instanceID := range set.instances {
			if used[instanceID] {
				continue
			}
			resource := catalog.instanceByID[instanceID]
			typeID := catalog.instanceToType[instanceID]
			bindings[set.token] = groundedResource{
				Token:    set.token,
				Resource: resource,
				TypeID:   typeID,
				TypeName: catalog.typeByID[typeID].Name,
			}
			used[instanceID] = true
			if err := expand(index+1, bindings); err != nil {
				return err
			}
			delete(bindings, set.token)
			delete(used, instanceID)
		}
		return nil
	}

	if err := expand(0, make(map[string]groundedResource)); err != nil {
		return nil, err
	}

	return groundTaskByAgent(results, agents)
}

func groundTaskByAgent(tasks []PlanTask, agents []AgentInfo) ([]PlanTask, error) {
	result := make([]PlanTask, 0, len(tasks))
	for _, task := range tasks {
		if !usesGenericAgentPlaceholderTask(task) {
			result = append(result, task)
			continue
		}
		if len(agents) == 0 {
			return nil, fmt.Errorf("task %q uses {{agent.*}} placeholders but no agent was provided", task.TaskID)
		}
		candidateAgents := make([]AgentInfo, 0, len(agents))
		for _, agent := range agents {
			if !agentCanRunTask(agent, task) {
				continue
			}
			candidateAgents = append(candidateAgents, agent)
		}
		if len(candidateAgents) == 0 {
			return nil, fmt.Errorf("task %q uses {{agent.*}} placeholders but no capable online agent is available", task.TaskID)
		}
		for _, agent := range candidateAgents {
			bound := instantiateAgentTask(task, agent)
			result = append(result, bound)
		}
	}
	return result, nil
}

func instantiateGroundTask(task PlanTask, bindings map[string]groundedResource) (PlanTask, error) {
	orderedBindings := make([]groundedResource, 0, len(bindings))
	for _, binding := range bindings {
		orderedBindings = append(orderedBindings, binding)
	}
	sort.Slice(orderedBindings, func(i, j int) bool {
		return orderedBindings[i].Token < orderedBindings[j].Token
	})

	var primary *groundedResource
	if len(orderedBindings) > 0 {
		primary = &orderedBindings[0]
	}

	if primary == nil && usesGenericResourcePlaceholderTask(task) {
		return PlanTask{}, fmt.Errorf("task %q uses {{resource.*}} placeholders but has no bound resource", task.TaskID)
	}

	grounded := task
	grounded.Preconditions = cloneConditions(task.Preconditions)
	grounded.RequiredResources = cloneStringSlice(task.RequiredResources)
	grounded.ResultStates = cloneEffects(task.ResultStates)
	grounded.WarningResultStates = cloneEffects(task.WarningResultStates)
	grounded.ErrorResultStates = cloneEffects(task.ErrorResultStates)
	grounded.DuringState = cloneEffects(task.DuringState)
	grounded.WarningMessageVariable = strings.TrimSpace(task.WarningMessageVariable)
	grounded.ErrorMessageVariable = strings.TrimSpace(task.ErrorMessageVariable)
	grounded.RuntimeParams = buildRuntimeParams(primary)

	for i, token := range grounded.RequiredResources {
		if binding, ok := bindings[token]; ok {
			grounded.RequiredResources[i] = "instance:" + binding.Resource.ID
		}
	}

	for i := range grounded.Preconditions {
		grounded.Preconditions[i].Variable = substituteResourcePlaceholders(grounded.Preconditions[i].Variable, primary)
		grounded.Preconditions[i].Value = substituteResourcePlaceholders(grounded.Preconditions[i].Value, primary)
	}
	for i := range grounded.ResultStates {
		grounded.ResultStates[i].Variable = substituteResourcePlaceholders(grounded.ResultStates[i].Variable, primary)
		grounded.ResultStates[i].Value = substituteResourcePlaceholders(grounded.ResultStates[i].Value, primary)
	}
	for i := range grounded.WarningResultStates {
		grounded.WarningResultStates[i].Variable = substituteResourcePlaceholders(grounded.WarningResultStates[i].Variable, primary)
		grounded.WarningResultStates[i].Value = substituteResourcePlaceholders(grounded.WarningResultStates[i].Value, primary)
	}
	for i := range grounded.ErrorResultStates {
		grounded.ErrorResultStates[i].Variable = substituteResourcePlaceholders(grounded.ErrorResultStates[i].Variable, primary)
		grounded.ErrorResultStates[i].Value = substituteResourcePlaceholders(grounded.ErrorResultStates[i].Value, primary)
	}
	for i := range grounded.DuringState {
		grounded.DuringState[i].Variable = substituteResourcePlaceholders(grounded.DuringState[i].Variable, primary)
		grounded.DuringState[i].Value = substituteResourcePlaceholders(grounded.DuringState[i].Value, primary)
	}
	grounded.WarningMessageVariable = substituteResourcePlaceholders(grounded.WarningMessageVariable, primary)
	grounded.ErrorMessageVariable = substituteResourcePlaceholders(grounded.ErrorMessageVariable, primary)

	if primary != nil {
		grounded.TaskID = grounded.TaskID + "::" + primary.Resource.ID
		grounded.TaskName = strings.TrimSpace(fmt.Sprintf("%s [%s]", grounded.TaskName, primary.Resource.Name))
	}

	return grounded, nil
}

func usesGenericResourcePlaceholderTask(task PlanTask) bool {
	contains := func(value string) bool {
		return strings.Contains(value, "{{resource.")
	}
	for _, cond := range task.Preconditions {
		if contains(cond.Variable) || contains(cond.Value) {
			return true
		}
	}
	for _, effect := range task.ResultStates {
		if contains(effect.Variable) || contains(effect.Value) {
			return true
		}
	}
	for _, effect := range task.WarningResultStates {
		if contains(effect.Variable) || contains(effect.Value) {
			return true
		}
	}
	for _, effect := range task.ErrorResultStates {
		if contains(effect.Variable) || contains(effect.Value) {
			return true
		}
	}
	for _, effect := range task.DuringState {
		if contains(effect.Variable) || contains(effect.Value) {
			return true
		}
	}
	return contains(task.WarningMessageVariable) || contains(task.ErrorMessageVariable)
}

func usesGenericAgentPlaceholderTask(task PlanTask) bool {
	contains := func(value string) bool {
		return strings.Contains(value, "{{agent.") || strings.Contains(value, "{{agent}}")
	}
	for _, cond := range task.Preconditions {
		if contains(cond.Variable) || contains(cond.Value) {
			return true
		}
	}
	for _, effect := range task.ResultStates {
		if contains(effect.Variable) || contains(effect.Value) {
			return true
		}
	}
	for _, effect := range task.WarningResultStates {
		if contains(effect.Variable) || contains(effect.Value) {
			return true
		}
	}
	for _, effect := range task.ErrorResultStates {
		if contains(effect.Variable) || contains(effect.Value) {
			return true
		}
	}
	for _, effect := range task.DuringState {
		if contains(effect.Variable) || contains(effect.Value) {
			return true
		}
	}
	return contains(task.WarningMessageVariable) || contains(task.ErrorMessageVariable)
}

func substituteResourcePlaceholders(value string, primary *groundedResource) string {
	if value == "" || primary == nil {
		return value
	}

	replacements := map[string]string{
		"{{resource}}":           primary.Resource.Name,
		"{{resource.id}}":        primary.Resource.ID,
		"{{resource.name}}":      primary.Resource.Name,
		"{{resource.kind}}":      primary.Resource.Kind,
		"{{resource.type_id}}":   primary.TypeID,
		"{{resource.type_name}}": primary.TypeName,
	}

	result := value
	for placeholder, replacement := range replacements {
		result = strings.ReplaceAll(result, placeholder, replacement)
	}
	return result
}

func substituteAgentPlaceholders(value string, agent AgentInfo) string {
	if value == "" {
		return value
	}
	agentName := strings.TrimSpace(agent.Name)
	if agentName == "" {
		agentName = agent.ID
	}
	replacements := map[string]string{
		"{{agent}}":      agentName,
		"{{agent.id}}":   agent.ID,
		"{{agent.name}}": agentName,
	}

	result := value
	for placeholder, replacement := range replacements {
		result = strings.ReplaceAll(result, placeholder, replacement)
	}
	return result
}

func instantiateAgentTask(task PlanTask, agent AgentInfo) PlanTask {
	bound := task
	bound.Preconditions = cloneConditions(task.Preconditions)
	bound.ResultStates = cloneEffects(task.ResultStates)
	bound.WarningResultStates = cloneEffects(task.WarningResultStates)
	bound.ErrorResultStates = cloneEffects(task.ErrorResultStates)
	bound.DuringState = cloneEffects(task.DuringState)
	bound.WarningMessageVariable = strings.TrimSpace(task.WarningMessageVariable)
	bound.ErrorMessageVariable = strings.TrimSpace(task.ErrorMessageVariable)
	bound.RuntimeParams = cloneStringMap(task.RuntimeParams)

	for i := range bound.Preconditions {
		bound.Preconditions[i].Variable = substituteAgentPlaceholders(bound.Preconditions[i].Variable, agent)
		bound.Preconditions[i].Value = substituteAgentPlaceholders(bound.Preconditions[i].Value, agent)
	}
	for i := range bound.ResultStates {
		bound.ResultStates[i].Variable = substituteAgentPlaceholders(bound.ResultStates[i].Variable, agent)
		bound.ResultStates[i].Value = substituteAgentPlaceholders(bound.ResultStates[i].Value, agent)
	}
	for i := range bound.WarningResultStates {
		bound.WarningResultStates[i].Variable = substituteAgentPlaceholders(bound.WarningResultStates[i].Variable, agent)
		bound.WarningResultStates[i].Value = substituteAgentPlaceholders(bound.WarningResultStates[i].Value, agent)
	}
	for i := range bound.ErrorResultStates {
		bound.ErrorResultStates[i].Variable = substituteAgentPlaceholders(bound.ErrorResultStates[i].Variable, agent)
		bound.ErrorResultStates[i].Value = substituteAgentPlaceholders(bound.ErrorResultStates[i].Value, agent)
	}
	for i := range bound.DuringState {
		bound.DuringState[i].Variable = substituteAgentPlaceholders(bound.DuringState[i].Variable, agent)
		bound.DuringState[i].Value = substituteAgentPlaceholders(bound.DuringState[i].Value, agent)
	}
	bound.WarningMessageVariable = substituteAgentPlaceholders(bound.WarningMessageVariable, agent)
	bound.ErrorMessageVariable = substituteAgentPlaceholders(bound.ErrorMessageVariable, agent)

	agentName := strings.TrimSpace(agent.Name)
	if agentName == "" {
		agentName = agent.ID
	}
	if bound.RuntimeParams == nil {
		bound.RuntimeParams = make(map[string]string)
	}
	bound.RuntimeParams["agent"] = agentName
	bound.RuntimeParams["agent_id"] = agent.ID
	bound.RuntimeParams["agent_name"] = agentName
	bound.RuntimeParams["agent.id"] = agent.ID
	bound.RuntimeParams["agent.name"] = agentName

	bound.BoundAgentID = agent.ID
	bound.BoundAgentName = agentName
	bound.TaskID = bound.TaskID + "::" + agent.ID
	bound.TaskName = strings.TrimSpace(fmt.Sprintf("%s [%s]", bound.TaskName, agentName))
	return bound
}

func buildRuntimeParams(primary *groundedResource) map[string]string {
	if primary == nil {
		return nil
	}

	params := map[string]string{
		"resource":           primary.Resource.Name,
		"resource_id":        primary.Resource.ID,
		"resource_name":      primary.Resource.Name,
		"resource_kind":      primary.Resource.Kind,
		"resource.id":        primary.Resource.ID,
		"resource.name":      primary.Resource.Name,
		"resource.kind":      primary.Resource.Kind,
		"resource.type_id":   primary.TypeID,
		"resource.type_name": primary.TypeName,
	}
	if primary.TypeID != "" {
		params["resource_type_id"] = primary.TypeID
	}
	if primary.TypeName != "" {
		params["resource_type_name"] = primary.TypeName
	}
	return params
}

func cloneConditions(values []db.PlanningCondition) []db.PlanningCondition {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]db.PlanningCondition, len(values))
	copy(cloned, values)
	return cloned
}

func cloneEffects(values []db.PlanningEffect) []db.PlanningEffect {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]db.PlanningEffect, len(values))
	copy(cloned, values)
	return cloned
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

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
func GroundTasks(tasks []PlanTask, resources []ResourceInfo) ([]PlanTask, error) {
	catalog := buildResourceCatalog(resources)
	grounded := make([]PlanTask, 0, len(tasks))

	for _, task := range tasks {
		taskGrounded, err := groundTask(task, catalog)
		if err != nil {
			return nil, err
		}
		grounded = append(grounded, taskGrounded...)
	}

	return grounded, nil
}

func groundTask(task PlanTask, catalog resourceCatalog) ([]PlanTask, error) {
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
		return []PlanTask{groundedTask}, nil
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

	return results, nil
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
	grounded.DuringState = cloneEffects(task.DuringState)
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
	for i := range grounded.DuringState {
		grounded.DuringState[i].Variable = substituteResourcePlaceholders(grounded.DuringState[i].Variable, primary)
		grounded.DuringState[i].Value = substituteResourcePlaceholders(grounded.DuringState[i].Value, primary)
	}

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
	for _, effect := range task.DuringState {
		if contains(effect.Variable) || contains(effect.Value) {
			return true
		}
	}
	return false
}

func substituteResourcePlaceholders(value string, primary *groundedResource) string {
	if value == "" || primary == nil {
		return value
	}

	replacements := map[string]string{
		"{{resource}}":          primary.Resource.Name,
		"{{resource.id}}":       primary.Resource.ID,
		"{{resource.name}}":     primary.Resource.Name,
		"{{resource.kind}}":     primary.Resource.Kind,
		"{{resource.type_id}}":  primary.TypeID,
		"{{resource.type_name}}": primary.TypeName,
	}

	result := value
	for placeholder, replacement := range replacements {
		result = strings.ReplaceAll(result, placeholder, replacement)
	}
	return result
}

func buildRuntimeParams(primary *groundedResource) map[string]string {
	if primary == nil {
		return nil
	}

	params := map[string]string{
		"resource":       primary.Resource.Name,
		"resource_id":    primary.Resource.ID,
		"resource_name":  primary.Resource.Name,
		"resource_kind":  primary.Resource.Kind,
		"resource.id":    primary.Resource.ID,
		"resource.name":  primary.Resource.Name,
		"resource.kind":  primary.Resource.Kind,
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

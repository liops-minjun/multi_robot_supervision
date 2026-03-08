package executor

import (
	"fmt"
	"strings"

	"central_server_go/internal/db"
	"central_server_go/internal/state"
)

type runtimeResourceCatalog struct {
	typeByID         map[string]db.TaskDistributorResource
	typeNameToID     map[string]string
	instanceByID     map[string]db.TaskDistributorResource
	instanceNameToID map[string]string
	instanceToType   map[string]string
	typeInstances    map[string][]db.TaskDistributorResource
}

func loadRuntimeResourceCatalog(repo *db.Repository, distributorID string) (runtimeResourceCatalog, error) {
	distributorID = strings.TrimSpace(distributorID)
	if distributorID == "" {
		return runtimeResourceCatalog{}, nil
	}

	resources, err := repo.ListTaskDistributorResources(distributorID)
	if err != nil {
		return runtimeResourceCatalog{}, err
	}

	catalog := runtimeResourceCatalog{
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

func resolveRuntimeBindings(requiredTokens []string, currentHolds []state.PlanResourceHold, alreadyReserved map[string]bool, catalog runtimeResourceCatalog) (map[string]string, error) {
	if len(requiredTokens) == 0 {
		return nil, nil
	}

	heldNames := make(map[string]bool, len(currentHolds)+len(alreadyReserved))
	for _, hold := range currentHolds {
		heldNames[hold.ResourceID] = true
	}
	for resourceID := range alreadyReserved {
		heldNames[resourceID] = true
	}

	bindings := make(map[string]string, len(requiredTokens))
	for _, token := range requiredTokens {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		resourceID, err := resolveRuntimeBindingToken(trimmed, catalog, heldNames)
		if err != nil {
			return nil, err
		}
		bindings[trimmed] = resourceID
		heldNames[resourceID] = true
	}
	return bindings, nil
}

func resolveRuntimeBindingToken(token string, catalog runtimeResourceCatalog, heldNames map[string]bool) (string, error) {
	if strings.HasPrefix(token, "instance:") {
		instanceID := strings.TrimSpace(strings.TrimPrefix(token, "instance:"))
		if instance, ok := catalog.instanceByID[instanceID]; ok {
			return instance.Name, nil
		}
		return "", fmt.Errorf("unknown instance id %q", instanceID)
	}

	typeID := ""
	if strings.HasPrefix(token, "type:") {
		typeID = strings.TrimSpace(strings.TrimPrefix(token, "type:"))
	} else if resolvedTypeID, ok := catalog.typeNameToID[token]; ok {
		typeID = resolvedTypeID
	}
	if typeID != "" {
		for _, instance := range catalog.typeInstances[typeID] {
			if !heldNames[instance.Name] {
				return instance.Name, nil
			}
		}
		return "", fmt.Errorf("no free instance for type %q", token)
	}

	if instanceID, ok := catalog.instanceNameToID[token]; ok {
		return catalog.instanceByID[instanceID].Name, nil
	}
	return token, nil
}

package pddl

import "strings"

type resourceCatalog struct {
	typeByID         map[string]ResourceInfo
	typeNameToID     map[string]string
	instanceByID     map[string]ResourceInfo
	instanceNameToID map[string]string
	instanceToType   map[string]string
	typeCapacity     map[string]int
	typeInstances    map[string][]string
}

type normalizedResourceRef struct {
	Kind    string
	Key     string
	TypeKey string
}

func buildResourceCatalog(resources []ResourceInfo) resourceCatalog {
	catalog := resourceCatalog{
		typeByID:         make(map[string]ResourceInfo),
		typeNameToID:     make(map[string]string),
		instanceByID:     make(map[string]ResourceInfo),
		instanceNameToID: make(map[string]string),
		instanceToType:   make(map[string]string),
		typeCapacity:     make(map[string]int),
		typeInstances:    make(map[string][]string),
	}

	for _, resource := range resources {
		switch strings.ToLower(strings.TrimSpace(resource.Kind)) {
		case "type":
			catalog.typeByID[resource.ID] = resource
			if resource.Name != "" {
				catalog.typeNameToID[resource.Name] = resource.ID
			}
		default:
			catalog.instanceByID[resource.ID] = resource
			if resource.Name != "" {
				catalog.instanceNameToID[resource.Name] = resource.ID
			}
			if resource.ParentResourceID != "" {
				catalog.instanceToType[resource.ID] = resource.ParentResourceID
				catalog.typeCapacity[resource.ParentResourceID]++
				catalog.typeInstances[resource.ParentResourceID] = append(catalog.typeInstances[resource.ParentResourceID], resource.ID)
			}
		}
	}

	return catalog
}

func resolveResourceToken(token string, catalog resourceCatalog) normalizedResourceRef {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return normalizedResourceRef{}
	}

	if strings.HasPrefix(trimmed, "type:") {
		typeID := strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
		return normalizedResourceRef{
			Kind:    "type",
			Key:     typeID,
			TypeKey: typeID,
		}
	}

	if strings.HasPrefix(trimmed, "instance:") {
		instanceID := strings.TrimSpace(strings.TrimPrefix(trimmed, "instance:"))
		return normalizedResourceRef{
			Kind:    "instance",
			Key:     instanceID,
			TypeKey: catalog.instanceToType[instanceID],
		}
	}

	if typeID, ok := catalog.typeNameToID[trimmed]; ok {
		return normalizedResourceRef{
			Kind:    "type",
			Key:     typeID,
			TypeKey: typeID,
		}
	}

	if instanceID, ok := catalog.instanceNameToID[trimmed]; ok {
		return normalizedResourceRef{
			Kind:    "instance",
			Key:     instanceID,
			TypeKey: catalog.instanceToType[instanceID],
		}
	}

	return normalizedResourceRef{
		Kind: "instance",
		Key:  trimmed,
	}
}

package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"central_server_go/internal/db"
	"central_server_go/internal/graph"
)

func (s *Scheduler) prepareExecutionGraph(ctx context.Context, agentID, taskID string, baseGraph *db.BehaviorTree, runtimeParams map[string]string) (string, error) {
	if baseGraph == nil {
		return "", fmt.Errorf("behavior tree is nil")
	}

	concreteGraph, changed, err := buildConcreteRuntimeGraph(baseGraph, agentID, taskID, runtimeParams)
	if err != nil {
		return "", err
	}

	if !changed {
		if err := s.ensureGraphDeployed(ctx, agentID, baseGraph); err != nil {
			return "", err
		}
		return baseGraph.ID, nil
	}

	if err := s.deployConcreteExecutionGraph(ctx, agentID, concreteGraph); err != nil {
		return "", err
	}

	return concreteGraph.BehaviorTree.ID, nil
}

func (s *Scheduler) deployConcreteExecutionGraph(ctx context.Context, agentID string, concreteGraph *graph.CanonicalGraph) error {
	if concreteGraph == nil {
		return fmt.Errorf("concrete graph is nil")
	}
	if s.quicHandler == nil {
		return fmt.Errorf("raw QUIC handler is not configured")
	}

	concreteGraph.BehaviorTree.AgentID = agentID
	concreteGraph.SubstituteServerPatterns("")

	graphJSON, err := json.Marshal(concreteGraph)
	if err != nil {
		return fmt.Errorf("serialize concrete graph: %w", err)
	}

	deployCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	result, err := s.quicHandler.DeployCanonicalGraph(deployCtx, agentID, graphJSON)
	if err != nil {
		return fmt.Errorf("deploy concrete graph %s: %w", concreteGraph.BehaviorTree.ID, err)
	}
	if !result.Success {
		deployErr := strings.TrimSpace(result.Error)
		if deployErr == "" {
			deployErr = "unknown deploy error"
		}
		return fmt.Errorf("deploy concrete graph %s failed: %s", concreteGraph.BehaviorTree.ID, deployErr)
	}

	s.stateManager.GraphCache().SetDeployed(agentID, concreteGraph.BehaviorTree.ID, concreteGraph)
	log.Printf("[Scheduler] Deployed concrete runtime graph %s to agent %s", concreteGraph.BehaviorTree.ID, agentID)
	return nil
}

func buildConcreteRuntimeGraph(baseGraph *db.BehaviorTree, agentID, taskID string, runtimeParams map[string]string) (*graph.CanonicalGraph, bool, error) {
	canonicalGraph, err := graph.FromDBModel(baseGraph)
	if err != nil {
		return nil, false, fmt.Errorf("convert behavior tree %s to canonical: %w", baseGraph.ID, err)
	}

	if canonicalGraph.BehaviorTree.AgentID == "" {
		canonicalGraph.BehaviorTree.AgentID = agentID
	}

	changed := materializeRuntimeParamsIntoGraph(canonicalGraph, runtimeParams)
	if !changed {
		return canonicalGraph, false, nil
	}

	shortTaskID := strings.TrimSpace(taskID)
	if len(shortTaskID) > 8 {
		shortTaskID = shortTaskID[:8]
	}

	canonicalGraph.BehaviorTree.ID = fmt.Sprintf("%s__exec__%s", baseGraph.ID, shortTaskID)
	canonicalGraph.BehaviorTree.Name = strings.TrimSpace(fmt.Sprintf("%s [%s]", baseGraph.Name, shortTaskID))
	canonicalGraph.BehaviorTree.UpdatedAt = time.Now().UTC()
	canonicalGraph.Checksum = canonicalGraph.ComputeChecksum()

	if err := canonicalGraph.Validate(); err != nil {
		return nil, false, fmt.Errorf("validate concrete runtime graph: %w", err)
	}

	return canonicalGraph, true, nil
}

func materializeRuntimeParamsIntoGraph(cg *graph.CanonicalGraph, runtimeParams map[string]string) bool {
	if cg == nil || len(runtimeParams) == 0 {
		return false
	}

	changed := false
	for vertexIdx := range cg.Vertices {
		step := cg.Vertices[vertexIdx].Step
		if step == nil || step.Action == nil || step.Action.Params == nil {
			continue
		}
		if materializeActionParams(step.Action.Params, runtimeParams) {
			changed = true
		}
	}

	return changed
}

func materializeActionParams(params *graph.ActionParams, runtimeParams map[string]string) bool {
	if params == nil || len(runtimeParams) == 0 {
		return false
	}

	changed := false

	if len(params.Data) > 0 {
		for key, value := range params.Data {
			resolved, valueChanged := substituteRuntimeInterface(value, runtimeParams)
			if valueChanged {
				params.Data[key] = resolved
				changed = true
			}
		}
	}

	for fieldPath, source := range params.FieldSources {
		switch source.Source {
		case graph.ParameterSourceExpression:
			resolvedExpr, replaced, fullyResolved := substituteRuntimeString(source.Expression, runtimeParams)
			if !replaced {
				continue
			}
			if fullyResolved {
				source.Source = graph.ParameterSourceConstant
				source.Value = coerceRuntimeValue(resolvedExpr)
				source.Expression = ""
			} else {
				source.Expression = resolvedExpr
			}
			params.FieldSources[fieldPath] = source
			changed = true
		case graph.ParameterSourceConstant:
			resolved, valueChanged := substituteRuntimeInterface(source.Value, runtimeParams)
			if valueChanged {
				source.Value = resolved
				params.FieldSources[fieldPath] = source
				changed = true
			}
		}
	}

	return changed
}

func substituteRuntimeInterface(value interface{}, runtimeParams map[string]string) (interface{}, bool) {
	switch typed := value.(type) {
	case string:
		resolved, changed, fullyResolved := substituteRuntimeString(typed, runtimeParams)
		if !changed {
			return value, false
		}
		if fullyResolved {
			return coerceRuntimeValue(resolved), true
		}
		return resolved, true
	case []interface{}:
		changed := false
		resolved := make([]interface{}, len(typed))
		for idx, item := range typed {
			next, itemChanged := substituteRuntimeInterface(item, runtimeParams)
			resolved[idx] = next
			changed = changed || itemChanged
		}
		if !changed {
			return value, false
		}
		return resolved, true
	case map[string]interface{}:
		changed := false
		resolved := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			next, itemChanged := substituteRuntimeInterface(item, runtimeParams)
			resolved[key] = next
			changed = changed || itemChanged
		}
		if !changed {
			return value, false
		}
		return resolved, true
	default:
		return value, false
	}
}

func substituteRuntimeString(value string, runtimeParams map[string]string) (string, bool, bool) {
	if value == "" || len(runtimeParams) == 0 {
		return value, false, false
	}

	var builder strings.Builder
	replacedAny := false
	fullyResolved := true
	cursor := 0

	for cursor < len(value) {
		start := strings.Index(value[cursor:], "${")
		if start < 0 {
			builder.WriteString(value[cursor:])
			break
		}
		start += cursor
		builder.WriteString(value[cursor:start])

		end := strings.Index(value[start+2:], "}")
		if end < 0 {
			builder.WriteString(value[start:])
			fullyResolved = false
			break
		}
		end += start + 2

		key := strings.TrimSpace(value[start+2 : end])
		replacement, ok := runtimeParams[key]
		if !ok {
			builder.WriteString(value[start : end+1])
			fullyResolved = false
		} else {
			builder.WriteString(replacement)
			replacedAny = true
		}
		cursor = end + 1
	}

	if !replacedAny {
		return value, false, false
	}

	resolved := builder.String()
	return resolved, true, fullyResolved && !strings.Contains(resolved, "${")
}

func coerceRuntimeValue(value string) interface{} {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	if strings.EqualFold(trimmed, "true") {
		return true
	}
	if strings.EqualFold(trimmed, "false") {
		return false
	}

	if i, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return f
	}

	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "\"") {
		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
	}

	return value
}

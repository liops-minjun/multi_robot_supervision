package api

import "strings"

func normalizeActionGraphSteps(raw []map[string]interface{}) []map[string]interface{} {
	if raw == nil {
		return nil
	}
	normalized := make([]map[string]interface{}, 0, len(raw))
	for _, step := range raw {
		if step == nil {
			continue
		}
		cloned := make(map[string]interface{}, len(step))
		for k, v := range step {
			cloned[k] = v
		}
		normalizeActionGraphStep(cloned)
		normalized = append(normalized, cloned)
	}
	return normalized
}

func normalizeActionGraphStep(step map[string]interface{}) {
	startConditions := normalizeStartConditions(step["start_conditions"])
	if len(startConditions) == 0 {
		startConditions = normalizeStartConditions(step["startConditions"])
	}
	if len(startConditions) == 0 {
		startConditions = startConditionsFromStartStates(step["startStates"])
	}
	if len(startConditions) > 0 {
		step["start_conditions"] = startConditions
	}
	delete(step, "startStates")
	delete(step, "startConditions")

	preStates := extractStateList(step["pre_states"])
	if len(preStates) == 0 {
		preStates = extractStateList(step["preStates"])
	}
	if len(preStates) > 0 {
		step["pre_states"] = preStates
	}
	delete(step, "preStates")

	duringStates := extractStateList(step["during_states"])
	if len(duringStates) == 0 {
		duringStates = extractStateList(step["duringStates"])
	}
	if len(duringStates) > 0 {
		step["during_states"] = duringStates
	}
	delete(step, "duringStates")

	successStates := extractStateList(step["success_states"])
	if len(successStates) == 0 {
		successStates = extractStateList(step["successStates"])
	}
	failureStates := extractStateList(step["failure_states"])
	if len(failureStates) == 0 {
		failureStates = extractStateList(step["failureStates"])
	}
	endStates := normalizeEndStates(step["end_states"])
	if len(endStates) == 0 {
		endStates = normalizeEndStates(step["endStates"])
	}
	if len(endStates) > 0 {
		step["end_states"] = endStates
	}
	if len(successStates) == 0 && len(failureStates) == 0 {
		derivedSuccess, derivedFailure := classifyEndStates(step["end_states"])
		successStates = derivedSuccess
		failureStates = derivedFailure
	}
	if len(successStates) > 0 {
		step["success_states"] = successStates
	}
	if len(failureStates) > 0 {
		step["failure_states"] = failureStates
	}
	delete(step, "successStates")
	delete(step, "failureStates")
	delete(step, "endStates")

	normalizeTransition(step)
}

func normalizeStartConditions(raw interface{}) []map[string]interface{} {
	conditions := extractMapList(raw)
	if len(conditions) == 0 {
		return nil
	}
	normalized := make([]map[string]interface{}, 0, len(conditions))
	for _, cond := range conditions {
		if cond == nil {
			continue
		}
		out := make(map[string]interface{}, len(cond))
		for k, v := range cond {
			out[k] = v
		}
		normalizeStartConditionKeys(out)
		normalized = append(normalized, out)
	}
	return normalized
}

func normalizeStartConditionKeys(cond map[string]interface{}) {
	if v, ok := cond["stateOperator"]; ok {
		cond["state_operator"] = v
		delete(cond, "stateOperator")
	}
	if v, ok := cond["allowedStates"]; ok {
		cond["allowed_states"] = v
		delete(cond, "allowedStates")
	}
	if v, ok := cond["maxStalenessSec"]; ok {
		cond["max_staleness_sec"] = v
		delete(cond, "maxStalenessSec")
	}
	if v, ok := cond["requireOnline"]; ok {
		cond["require_online"] = v
		delete(cond, "requireOnline")
	}
	if v, ok := cond["targetType"]; ok {
		cond["target_type"] = v
		delete(cond, "targetType")
	}
	if v, ok := cond["robotId"]; ok {
		cond["robot_id"] = v
		delete(cond, "robotId")
	}
	if v, ok := cond["agentId"]; ok {
		cond["agent_id"] = v
		delete(cond, "agentId")
	}
	if v, ok := cond["agentType"]; ok {
		cond["agent_id"] = v
		delete(cond, "agentType")
	}
	if _, ok := cond["require_online"]; !ok {
		cond["require_online"] = true
	}
	if op, ok := cond["operator"].(string); ok {
		cond["operator"] = strings.ToLower(op)
	}
	if q, ok := cond["quantifier"].(string); ok {
		q = strings.ToLower(q)
		if q == "every" {
			q = "all"
		}
		cond["quantifier"] = q
	}
}

func normalizeTransition(step map[string]interface{}) {
	raw, ok := step["transition"].(map[string]interface{})
	if !ok || raw == nil {
		return
	}

	if v, ok := raw["onOutcomes"]; ok {
		raw["on_outcomes"] = normalizeOutcomeTransitions(v)
		delete(raw, "onOutcomes")
	}
	if v, ok := raw["on_outcomes"]; ok {
		raw["on_outcomes"] = normalizeOutcomeTransitions(v)
	}
}

func startConditionsFromStartStates(raw interface{}) []map[string]interface{} {
	startStates := extractMapList(raw)
	if len(startStates) == 0 {
		return nil
	}

	conditions := make([]map[string]interface{}, 0, len(startStates))
	for _, state := range startStates {
		if state == nil {
			continue
		}
		quantifier := strings.ToLower(extractString(state, "quantifier"))
		if quantifier == "every" {
			quantifier = "all"
		}
		if quantifier == "" {
			quantifier = "self"
		}
		operator := strings.ToLower(extractString(state, "operator"))
		agentID := extractString(state, "agentId")
		if agentID == "" {
			agentID = extractString(state, "agentType")
		}
		robotID := extractString(state, "robotId")
		stateValue := extractString(state, "state")
		if stateValue == "" {
			continue
		}

		cond := map[string]interface{}{
			"id":             extractString(state, "id"),
			"quantifier":     quantifier,
			"state":          stateValue,
			"state_operator": "==",
			"require_online": true,
		}
		if operator != "" {
			cond["operator"] = operator
		}
		switch quantifier {
		case "self":
			cond["target_type"] = "self"
		case "specific":
			if agentID != "" {
				cond["target_type"] = "agent"
				cond["agent_id"] = agentID
			} else if robotID != "" {
				cond["target_type"] = "robot"
				cond["robot_id"] = robotID
			} else {
				cond["target_type"] = "agent"
			}
		default:
			if agentID != "" {
				cond["target_type"] = "agent"
				cond["agent_id"] = agentID
			} else {
				cond["target_type"] = "all"
			}
		}
		conditions = append(conditions, cond)
	}

	return conditions
}

func extractStateList(raw interface{}) []string {
	values := extractStringList(raw)
	if len(values) == 0 {
		return nil
	}
	return dedupeStrings(values)
}

func extractStringList(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return append([]string{}, v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			switch typed := item.(type) {
			case string:
				if typed != "" {
					out = append(out, typed)
				}
			case map[string]interface{}:
				if s := extractString(typed, "state"); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func classifyEndStates(raw interface{}) ([]string, []string) {
	endStates := extractMapList(raw)
	if len(endStates) == 0 {
		return nil, nil
	}

	var success []string
	var failure []string
	for idx, endState := range endStates {
		state := extractString(endState, "state")
		if state == "" {
			continue
		}
		outcome := normalizeOutcome(extractString(endState, "outcome"))
		if outcome != "" {
			if outcome == "success" {
				success = append(success, state)
			} else {
				failure = append(failure, state)
			}
			continue
		}
		label := strings.ToLower(extractString(endState, "label"))
		stateLower := strings.ToLower(state)
		isFailure := containsAny(label, []string{"fail", "error", "abort", "timeout", "cancel"}) ||
			containsAny(stateLower, []string{"error", "fail", "abort", "timeout", "cancel"})
		isSuccess := containsAny(label, []string{"success", "complete"}) ||
			containsAny(stateLower, []string{"idle", "ready", "complete", "success"})

		switch {
		case isFailure && !isSuccess:
			failure = append(failure, state)
		case isSuccess && !isFailure:
			success = append(success, state)
		default:
			if idx == 0 && len(success) == 0 {
				success = append(success, state)
			} else {
				failure = append(failure, state)
			}
		}
	}

	return dedupeStrings(success), dedupeStrings(failure)
}

func normalizeEndStates(raw interface{}) []map[string]interface{} {
	endStates := extractMapList(raw)
	if len(endStates) == 0 {
		return nil
	}
	normalized := make([]map[string]interface{}, 0, len(endStates))
	for idx, endState := range endStates {
		if endState == nil {
			continue
		}
		out := make(map[string]interface{}, len(endState))
		for k, v := range endState {
			out[k] = v
		}
		outcome := normalizeOutcome(extractString(out, "outcome"))
		if outcome == "" {
			outcome = inferOutcomeFromEndState(out, idx)
		}
		if outcome != "" {
			out["outcome"] = outcome
		}
		if v, ok := out["condition"]; ok {
			out["condition"] = v
		}
		normalized = append(normalized, out)
	}
	return normalized
}

func normalizeOutcomeTransitions(raw interface{}) []map[string]interface{} {
	outcomes := extractMapList(raw)
	if len(outcomes) == 0 {
		return nil
	}
	normalized := make([]map[string]interface{}, 0, len(outcomes))
	for _, transition := range outcomes {
		if transition == nil {
			continue
		}
		out := make(map[string]interface{}, len(transition))
		for k, v := range transition {
			out[k] = v
		}
		outcome := normalizeOutcome(extractString(out, "outcome"))
		if outcome != "" {
			out["outcome"] = outcome
		}
		normalized = append(normalized, out)
	}
	return normalized
}

func inferOutcomeFromEndState(endState map[string]interface{}, index int) string {
	label := strings.ToLower(extractString(endState, "label"))
	state := strings.ToLower(extractString(endState, "state"))

	switch {
	case containsAny(label, []string{"timeout"}) || containsAny(state, []string{"timeout"}):
		return "timeout"
	case containsAny(label, []string{"cancel"}) || containsAny(state, []string{"cancel"}):
		return "cancelled"
	case containsAny(label, []string{"abort"}) || containsAny(state, []string{"abort"}):
		return "aborted"
	case containsAny(label, []string{"fail", "error"}) || containsAny(state, []string{"fail", "error"}):
		return "failed"
	case containsAny(label, []string{"success", "complete"}) || containsAny(state, []string{"idle", "ready", "complete", "success"}):
		return "success"
	default:
		if index == 0 {
			return "success"
		}
		return "failed"
	}
}

func normalizeOutcome(value string) string {
	if value == "" {
		return ""
	}
	switch strings.ToLower(value) {
	case "success", "succeeded":
		return "success"
	case "failed", "failure", "error":
		return "failed"
	case "aborted", "abort":
		return "aborted"
	case "cancelled", "canceled", "cancel":
		return "cancelled"
	case "timeout", "timed_out":
		return "timeout"
	case "rejected":
		return "rejected"
	default:
		return strings.ToLower(value)
	}
}

func extractMapList(raw interface{}) []map[string]interface{} {
	switch v := raw.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if mapped, ok := item.(map[string]interface{}); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func extractString(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	if v, ok := values[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsAny(value string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

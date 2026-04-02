package api

import "strings"

func effectiveAgentCurrentState(currentState string, isOnline, isExecuting bool) string {
	state := strings.TrimSpace(currentState)
	lower := strings.ToLower(state)

	if !isOnline {
		return "offline"
	}

	if isExecuting {
		switch lower {
		case "", "idle", "ready", "starting":
			return "running"
		}
	}

	if state == "" {
		return "idle"
	}
	return state
}

package graph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"central_server_go/internal/db"
)

// =============================================================================
// Converter: DB Model <-> Canonical Graph
// =============================================================================

// FromDBModel converts a database BehaviorTree to CanonicalGraph
func FromDBModel(bt *db.BehaviorTree) (*CanonicalGraph, error) {
	if bt == nil {
		return nil, fmt.Errorf("behavior tree is nil")
	}

	// Parse steps from JSONB
	var steps []db.BehaviorTreeStep
	if bt.Steps != nil {
		if err := json.Unmarshal(bt.Steps, &steps); err != nil {
			return nil, fmt.Errorf("failed to parse steps: %w", err)
		}
	}

	// Parse preconditions
	var preconditions []db.Precondition
	if bt.Preconditions != nil {
		json.Unmarshal(bt.Preconditions, &preconditions)
	}

	// Convert to canonical format
	graph := &CanonicalGraph{
		SchemaVersion: SchemaVersion,
		BehaviorTree: BehaviorTreeMeta{
			ID:          bt.ID,
			Name:        bt.Name,
			Version:     bt.Version,
			Description: bt.Description.String,
			CreatedAt:   bt.CreatedAt,
			UpdatedAt:   bt.UpdatedAt,
		},
		Vertices: make([]Vertex, 0, len(steps)),
		Edges:    make([]Edge, 0),
	}

	if bt.AgentID.Valid {
		graph.BehaviorTree.AgentID = bt.AgentID.String
	}
	entryPoint := ""
	if bt.EntryPoint.Valid {
		entryPoint = bt.EntryPoint.String
	}

	// Build vertices and edges from steps
	for _, step := range steps {
		vertex := stepToVertex(&step)
		graph.Vertices = append(graph.Vertices, vertex)

		// Extract edges from transitions
		edges := extractEdges(&step)
		graph.Edges = append(graph.Edges, edges...)
	}

	if entryPoint != "" {
		graph.EntryPoint = entryPoint
	} else if len(steps) > 0 {
		graph.EntryPoint = steps[0].ID
	}
	if graph.EntryPoint != "" && graph.GetVertex(graph.EntryPoint) == nil && len(steps) > 0 {
		graph.EntryPoint = steps[0].ID
	}

	// Compute checksum
	graph.Checksum = graph.ComputeChecksum()

	return graph, nil
}

// ToDBModel converts a CanonicalGraph to database BehaviorTree
func ToDBModel(cg *CanonicalGraph) (*db.BehaviorTree, error) {
	if cg == nil {
		return nil, fmt.Errorf("canonical graph is nil")
	}

	// Convert vertices to steps
	steps := make([]db.BehaviorTreeStep, 0, len(cg.Vertices))
	for _, v := range cg.Vertices {
		step := vertexToStep(&v, cg)
		steps = append(steps, step)
	}

	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal steps: %w", err)
	}

	bt := &db.BehaviorTree{
		ID:        cg.BehaviorTree.ID,
		Name:      cg.BehaviorTree.Name,
		Version:   cg.BehaviorTree.Version,
		Steps:     stepsJSON,
		CreatedAt: cg.BehaviorTree.CreatedAt,
		UpdatedAt: time.Now(),
	}

	if cg.BehaviorTree.Description != "" {
		bt.Description.String = cg.BehaviorTree.Description
		bt.Description.Valid = true
	}

	if cg.BehaviorTree.AgentID != "" {
		bt.AgentID.String = cg.BehaviorTree.AgentID
		bt.AgentID.Valid = true
	}
	if cg.EntryPoint != "" {
		bt.EntryPoint = sql.NullString{String: cg.EntryPoint, Valid: true}
	}

	return bt, nil
}

// stepToVertex converts a DB step to a canonical vertex
func stepToVertex(step *db.BehaviorTreeStep) Vertex {
	vertex := Vertex{
		ID:   step.ID,
		Name: step.Name,
	}

	// Determine vertex type
	if step.Type == "terminal" {
		vertex.Type = VertexTypeTerminal
		vertex.Terminal = &TerminalData{
			TerminalType: TerminalType(step.TerminalType),
			Alert:        step.Alert,
			Message:      step.Message,
		}
	} else {
		vertex.Type = VertexTypeStep

		stepData := &StepData{
			JobName:            step.JobName,
			AutoGenerateStates: step.AutoGenerateStates,
			States: &StateConfig{
				Pre:     step.PreStates,
				During:  selectPrimaryDuringStates(step),
				Success: step.SuccessStates,
				Failure: step.FailureStates,
			},
			StartConditions: toGraphStartConditions(step.StartConditions),
			EndStates:       toGraphEndStates(step.EndStates),
		}

		// Determine step type
		if step.WaitFor != nil {
			stepData.StepType = StepTypeWait
			stepData.Wait = &WaitConfig{
				Type:       WaitType(step.WaitFor.Type),
				Message:    step.WaitFor.Message,
				TimeoutSec: step.WaitFor.TimeoutSec,
			}
		} else if step.Action != nil {
			stepData.StepType = StepTypeAction
			stepData.Action = &ActionConfig{
				Type:       step.Action.Type,
				Server:     step.Action.Server,
				TimeoutSec: step.Action.TimeoutSec,
			}

			if step.Action.Params != nil {
				stepData.Action.Params = &ActionParams{
					Source:       step.Action.Params.Source,
					WaypointID:   step.Action.Params.WaypointID,
					Data:         step.Action.Params.Data,
					Fields:       step.Action.Params.Fields,
					FieldSources: convertDBFieldSourcesToGraph(step.Action.Params.FieldSources),
				}
			}
		}

		vertex.Step = stepData
	}

	return vertex
}

func selectPrimaryDuringStates(step *db.BehaviorTreeStep) []string {
	if step == nil {
		return nil
	}
	for _, target := range step.DuringStateTargets {
		targetType := strings.ToLower(target.TargetType)
		if targetType == "" || targetType == "self" || targetType == "all" {
			if target.State != "" {
				return []string{target.State}
			}
		}
	}
	for _, state := range step.DuringStates {
		if state != "" {
			return []string{state}
		}
	}
	return nil
}

// extractEdges extracts edges from step transitions
func extractEdges(step *db.BehaviorTreeStep) []Edge {
	var edges []Edge

	if step.Transition == nil {
		return edges
	}

	hasOutcomes := len(step.Transition.OnOutcomes) > 0

	if hasOutcomes {
		for _, transition := range step.Transition.OnOutcomes {
			if transition.Next == "" {
				continue
			}
			condition := encodeOutcomeCondition(transition.Outcome, transition.State)
			edges = append(edges, Edge{
				From: step.ID,
				To:   transition.Next,
				Type: EdgeTypeConditional,
				Config: &EdgeConfig{
					Condition: condition,
				},
			})
		}
	}

	// On success
	if !hasOutcomes && step.Transition.OnSuccess != nil {
		switch v := step.Transition.OnSuccess.(type) {
		case string:
			edges = append(edges, Edge{
				From: step.ID,
				To:   v,
				Type: EdgeTypeOnSuccess,
			})
		case map[string]interface{}:
			if next, ok := v["next"].(string); ok {
				edges = append(edges, Edge{
					From: step.ID,
					To:   next,
					Type: EdgeTypeOnSuccess,
				})
			}
		}
	}

	// On failure
	if !hasOutcomes && step.Transition.OnFailure != nil {
		switch v := step.Transition.OnFailure.(type) {
		case string:
			edges = append(edges, Edge{
				From: step.ID,
				To:   v,
				Type: EdgeTypeOnFailure,
			})
		case map[string]interface{}:
			cfg := &EdgeConfig{}
			if retry, ok := v["retry"].(float64); ok {
				cfg.Retry = int(retry)
			}
			if fallback, ok := v["fallback"].(string); ok {
				cfg.Fallback = fallback
				edges = append(edges, Edge{
					From:   step.ID,
					To:     fallback,
					Type:   EdgeTypeOnFailure,
					Config: cfg,
				})
			} else if next, ok := v["next"].(string); ok {
				edges = append(edges, Edge{
					From:   step.ID,
					To:     next,
					Type:   EdgeTypeOnFailure,
					Config: cfg,
				})
			}
		}
	}

	// On timeout
	if step.Transition.OnTimeout != "" {
		edges = append(edges, Edge{
			From: step.ID,
			To:   step.Transition.OnTimeout,
			Type: EdgeTypeOnTimeout,
		})
	}

	// On confirm (for wait steps)
	if step.Transition.OnConfirm != "" {
		edges = append(edges, Edge{
			From: step.ID,
			To:   step.Transition.OnConfirm,
			Type: EdgeTypeOnConfirm,
		})
	}

	// On cancel
	if step.Transition.OnCancel != "" {
		edges = append(edges, Edge{
			From: step.ID,
			To:   step.Transition.OnCancel,
			Type: EdgeTypeOnCancel,
		})
	}

	return edges
}

// vertexToStep converts a canonical vertex to a DB step
func vertexToStep(v *Vertex, cg *CanonicalGraph) db.BehaviorTreeStep {
	step := db.BehaviorTreeStep{
		ID:   v.ID,
		Name: v.Name,
	}

	if v.Type == VertexTypeTerminal && v.Terminal != nil {
		step.Type = "terminal"
		step.TerminalType = string(v.Terminal.TerminalType)
		step.Alert = v.Terminal.Alert
		step.Message = v.Terminal.Message
	} else if v.Step != nil {
		// Job name and auto-generate states
		step.JobName = v.Step.JobName
		step.AutoGenerateStates = v.Step.AutoGenerateStates

		// States
		if v.Step.States != nil {
			step.PreStates = v.Step.States.Pre
			step.DuringStates = v.Step.States.During
			step.SuccessStates = v.Step.States.Success
			step.FailureStates = v.Step.States.Failure
		}

		// Start conditions
		if len(v.Step.StartConditions) > 0 {
			step.StartConditions = toDBStartConditions(v.Step.StartConditions)
		}
		if len(v.Step.EndStates) > 0 {
			step.EndStates = toDBEndStates(v.Step.EndStates)
		}

		// Action
		if v.Step.Action != nil {
			step.Action = &db.StepAction{
				Type:       v.Step.Action.Type,
				Server:     v.Step.Action.Server,
				TimeoutSec: v.Step.Action.TimeoutSec,
			}
			if v.Step.Action.Params != nil {
				step.Action.Params = &db.ActionParams{
					Source:       v.Step.Action.Params.Source,
					WaypointID:   v.Step.Action.Params.WaypointID,
					Data:         v.Step.Action.Params.Data,
					Fields:       v.Step.Action.Params.Fields,
					FieldSources: convertGraphFieldSourcesToDB(v.Step.Action.Params.FieldSources),
				}
			}
		}

		// Wait
		if v.Step.Wait != nil {
			step.WaitFor = &db.WaitFor{
				Type:       string(v.Step.Wait.Type),
				Message:    v.Step.Wait.Message,
				TimeoutSec: v.Step.Wait.TimeoutSec,
			}
		}

		// Build transitions from edges
		step.Transition = buildTransitionFromEdges(v.ID, cg)
	}

	return step
}

func toGraphStartConditions(conds []db.StartCondition) []StartCondition {
	if len(conds) == 0 {
		return nil
	}
	out := make([]StartCondition, 0, len(conds))
	for _, c := range conds {
		out = append(out, StartCondition{
			ID:              c.ID,
			Operator:        c.Operator,
			Quantifier:      c.Quantifier,
			TargetType:      c.TargetType,
			AgentID:         c.AgentID,
			State:           c.State,
			StateOperator:   c.StateOperator,
			AllowedStates:   c.AllowedStates,
			MaxStalenessSec: c.MaxStalenessSec,
			RequireOnline:   c.RequireOnline,
			Message:         c.Message,
		})
	}
	return out
}

func toDBStartConditions(conds []StartCondition) []db.StartCondition {
	if len(conds) == 0 {
		return nil
	}
	out := make([]db.StartCondition, 0, len(conds))
	for _, c := range conds {
		out = append(out, db.StartCondition{
			ID:              c.ID,
			Operator:        c.Operator,
			Quantifier:      c.Quantifier,
			TargetType:      c.TargetType,
			AgentID:         c.AgentID,
			State:           c.State,
			StateOperator:   c.StateOperator,
			AllowedStates:   c.AllowedStates,
			MaxStalenessSec: c.MaxStalenessSec,
			RequireOnline:   c.RequireOnline,
			Message:         c.Message,
		})
	}
	return out
}

func toGraphEndStates(states []db.EndState) []EndState {
	if len(states) == 0 {
		return nil
	}
	out := make([]EndState, 0, len(states))
	for _, state := range states {
		out = append(out, EndState{
			ID:      state.ID,
			State:   state.State,
			Label:   state.Label,
			Color:   state.Color,
			Outcome: state.Outcome,
		})
	}
	return out
}

func toDBEndStates(states []EndState) []db.EndState {
	if len(states) == 0 {
		return nil
	}
	out := make([]db.EndState, 0, len(states))
	for _, state := range states {
		out = append(out, db.EndState{
			ID:      state.ID,
			State:   state.State,
			Label:   state.Label,
			Color:   state.Color,
			Outcome: state.Outcome,
		})
	}
	return out
}

// buildTransitionFromEdges reconstructs transitions from graph edges
func buildTransitionFromEdges(vertexID string, cg *CanonicalGraph) *db.StepTransition {
	edges := cg.GetOutgoingEdges(vertexID)
	if len(edges) == 0 {
		return nil
	}

	transition := &db.StepTransition{}

	for _, e := range edges {
		switch e.Type {
		case EdgeTypeOnSuccess:
			transition.OnSuccess = e.To
		case EdgeTypeOnFailure:
			if e.Config != nil && e.Config.Retry > 0 {
				transition.OnFailure = map[string]interface{}{
					"retry":    e.Config.Retry,
					"fallback": e.To,
				}
			} else {
				transition.OnFailure = e.To
			}
		case EdgeTypeOnTimeout:
			transition.OnTimeout = e.To
		case EdgeTypeOnConfirm:
			transition.OnConfirm = e.To
		case EdgeTypeOnCancel:
			transition.OnCancel = e.To
		case EdgeTypeConditional:
			cond := ""
			if e.Config != nil {
				cond = e.Config.Condition
			}
			outcome, state, _ := decodeOutcomeCondition(cond)
			transition.OnOutcomes = append(transition.OnOutcomes, db.OutcomeTransition{
				Outcome: outcome,
				Next:    e.To,
				State:   state,
			})
		}
	}

	return transition
}

func encodeOutcomeCondition(outcome, state string) string {
	payload := map[string]string{}
	if strings.TrimSpace(outcome) != "" {
		payload["outcome"] = strings.TrimSpace(outcome)
	}
	if strings.TrimSpace(state) != "" {
		payload["state"] = strings.TrimSpace(state)
	}
	if len(payload) == 0 {
		return ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}

func decodeOutcomeCondition(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return "", "", false
	}
	payload := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", "", false
	}
	outcome := strings.TrimSpace(payload["outcome"])
	state := strings.TrimSpace(payload["state"])
	return outcome, state, outcome != "" || state != ""
}

// =============================================================================
// Field Sources Conversion
// =============================================================================

// convertDBFieldSourcesToGraph converts DB field sources to graph schema format
func convertDBFieldSourcesToGraph(dbSources map[string]db.ParameterFieldSource) map[string]ParameterFieldSource {
	if len(dbSources) == 0 {
		return nil
	}
	result := make(map[string]ParameterFieldSource, len(dbSources))
	for fieldName, dbSource := range dbSources {
		result[fieldName] = ParameterFieldSource{
			Source:      ParameterSourceType(dbSource.Source),
			Value:       dbSource.Value,
			StepID:      dbSource.StepID,
			ResultField: dbSource.ResultField,
			Expression:  dbSource.Expression,
		}
	}
	return result
}

// convertGraphFieldSourcesToDB converts graph schema field sources to DB format
func convertGraphFieldSourcesToDB(graphSources map[string]ParameterFieldSource) map[string]db.ParameterFieldSource {
	if len(graphSources) == 0 {
		return nil
	}
	result := make(map[string]db.ParameterFieldSource, len(graphSources))
	for fieldName, graphSource := range graphSources {
		result[fieldName] = db.ParameterFieldSource{
			Source:      string(graphSource.Source),
			Value:       graphSource.Value,
			StepID:      graphSource.StepID,
			ResultField: graphSource.ResultField,
			Expression:  graphSource.Expression,
		}
	}
	return result
}

// =============================================================================
// Deploy Message Format (for QUIC transport)
// =============================================================================

// DeployMessage is the message format for deploying behavior trees
type DeployMessage struct {
	CorrelationID string          `json:"correlation_id"`
	Action        string          `json:"action"` // "deploy"
	BehaviorTree  *CanonicalGraph `json:"behavior_tree"`
}

// DeployedMessage is the response after successful deployment
type DeployedMessage struct {
	CorrelationID  string `json:"correlation_id"`
	BehaviorTreeID string `json:"behavior_tree_id"`
	Version        int    `json:"version"`
	Success        bool   `json:"success"`
	Error          string `json:"error,omitempty"`
	Checksum       string `json:"checksum,omitempty"`
}

// ExecuteMessage is the message format for executing behavior trees
type ExecuteMessage struct {
	CorrelationID  string                 `json:"correlation_id"`
	Action         string                 `json:"action"` // "execute"
	BehaviorTreeID string                 `json:"behavior_tree_id"`
	AgentID        string                 `json:"agent_id"`
	Params         map[string]interface{} `json:"params,omitempty"`
}

// StatusMessage is the message for execution status updates
type StatusMessage struct {
	BehaviorTreeID string                 `json:"behavior_tree_id"`
	AgentID        string                 `json:"agent_id"`
	ExecutionID    string                 `json:"execution_id"`
	Status         string                 `json:"status"` // running, completed, failed, cancelled
	CurrentStepID  string                 `json:"current_step_id,omitempty"`
	Error          string                 `json:"error,omitempty"`
	Result         map[string]interface{} `json:"result,omitempty"`
}

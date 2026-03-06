package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"gorm.io/datatypes"
)

// Repository provides Neo4j access methods.
type Repository struct {
	db *Database
}

// NewRepository creates a new repository.
func NewRepository(db *Database) *Repository {
	return &Repository{db: db}
}

// =============================================================================
// Helpers
// =============================================================================

func (r *Repository) withSession(ctx context.Context, mode neo4j.AccessMode, fn func(neo4j.ManagedTransaction) (any, error)) (any, error) {
	session := r.db.Driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   mode,
		DatabaseName: r.db.Database,
	})
	defer session.Close(ctx)

	switch mode {
	case neo4j.AccessModeWrite:
		return session.ExecuteWrite(ctx, fn)
	default:
		return session.ExecuteRead(ctx, fn)
	}
}

func getString(props map[string]any, key string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getStringWithFallback tries to get a string value from props using the primary key,
// falling back to the fallback key if the primary key doesn't exist.
// This is used for backward compatibility during database migrations.
func getStringWithFallback(props map[string]any, key string, fallbackKey string) string {
	if v := getString(props, key); v != "" {
		return v
	}
	return getString(props, fallbackKey)
}

func getBool(props map[string]any, key string) bool {
	if v, ok := props[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getInt64(props map[string]any, key string) int64 {
	if v, ok := props[key]; ok {
		switch t := v.(type) {
		case int64:
			return t
		case int:
			return int64(t)
		case float64:
			return int64(t)
		}
	}
	return 0
}

func getFloat64(props map[string]any, key string) float64 {
	if v, ok := props[key]; ok {
		switch t := v.(type) {
		case float64:
			return t
		case int64:
			return float64(t)
		case int:
			return float64(t)
		}
	}
	return 0
}

func getStringSlice(props map[string]any, key string) []string {
	if v, ok := props[key]; ok {
		switch t := v.(type) {
		case []string:
			return t
		case []any:
			out := make([]string, 0, len(t))
			for _, item := range t {
				if s, ok := item.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
	}
	return nil
}

// isTemplateFromProps returns true for explicit templates and legacy template records.
// Legacy data may not have is_template set, so we treat empty agent_id as template.
func isTemplateFromProps(props map[string]any) bool {
	if getBool(props, "is_template") {
		return true
	}
	return strings.TrimSpace(getString(props, "agent_id")) == ""
}

func toNullString(val string) sql.NullString {
	if val == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: val, Valid: true}
}

func toNullTimeMillis(ms int64) sql.NullTime {
	if ms == 0 {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: time.UnixMilli(ms).UTC(), Valid: true}
}

func timeToMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

func jsonString(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func jsonBytesToMap(data datatypes.JSON) map[string]interface{} {
	if len(data) == 0 {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func toInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

type graphEdge struct {
	From      string
	To        string
	EdgeType  string
	Retry     int
	Fallback  string
	Condition string
}

func parseBehaviorTreeSteps(graph *BehaviorTree) ([]BehaviorTreeStep, error) {
	if graph == nil || len(graph.Steps) == 0 {
		return nil, nil
	}
	var steps []BehaviorTreeStep
	if err := json.Unmarshal(graph.Steps, &steps); err != nil {
		return nil, fmt.Errorf("failed to parse steps: %w", err)
	}
	return steps, nil
}

func isSelfOnlyCondition(cond StartCondition) bool {
	if cond.Quantifier != "" && strings.ToLower(cond.Quantifier) != "self" {
		return false
	}
	if cond.TargetType != "" && strings.ToLower(cond.TargetType) != "self" {
		return false
	}
	if cond.AgentID != "" {
		return false
	}
	return true
}

func executionModeFromSteps(steps []BehaviorTreeStep) string {
	for _, step := range steps {
		for _, cond := range step.StartConditions {
			if !isSelfOnlyCondition(cond) {
				return "server"
			}
		}
	}
	return "agent"
}

func checksumForJSON(data string) string {
	if data == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("sha256:%x", hash)
}

func extractEdgesFromSteps(steps []BehaviorTreeStep) []graphEdge {
	var edges []graphEdge
	for i := range steps {
		edges = append(edges, extractEdgesFromStep(&steps[i])...)
	}
	return edges
}

func extractEdgesFromStep(step *BehaviorTreeStep) []graphEdge {
	if step == nil || step.Transition == nil {
		return nil
	}

	var edges []graphEdge

	if step.Transition.OnSuccess != nil {
		switch v := step.Transition.OnSuccess.(type) {
		case string:
			if v != "" {
				edges = append(edges, graphEdge{From: step.ID, To: v, EdgeType: "on_success"})
			}
		case map[string]any:
			if next, ok := v["next"].(string); ok && next != "" {
				edges = append(edges, graphEdge{From: step.ID, To: next, EdgeType: "on_success"})
			}
		}
	}

	if step.Transition.OnFailure != nil {
		switch v := step.Transition.OnFailure.(type) {
		case string:
			if v != "" {
				edges = append(edges, graphEdge{From: step.ID, To: v, EdgeType: "on_failure"})
			}
		case map[string]any:
			retry := 0
			if rawRetry, ok := v["retry"]; ok {
				retry = toInt(rawRetry)
			}
			fallback := ""
			if fb, ok := v["fallback"].(string); ok {
				fallback = fb
			}
			next := ""
			if n, ok := v["next"].(string); ok {
				next = n
			}
			target := fallback
			if target == "" {
				target = next
			}
			if target != "" {
				edges = append(edges, graphEdge{
					From:     step.ID,
					To:       target,
					EdgeType: "on_failure",
					Retry:    retry,
					Fallback: fallback,
				})
			}
		}
	}

	if step.Transition.OnTimeout != "" {
		edges = append(edges, graphEdge{From: step.ID, To: step.Transition.OnTimeout, EdgeType: "on_timeout"})
	}

	if step.Transition.OnConfirm != "" {
		edges = append(edges, graphEdge{From: step.ID, To: step.Transition.OnConfirm, EdgeType: "on_confirm"})
	}

	if step.Transition.OnCancel != "" {
		edges = append(edges, graphEdge{From: step.ID, To: step.Transition.OnCancel, EdgeType: "on_cancel"})
	}

	return edges
}

func relTypeForEdge(edgeType string) string {
	switch strings.ToLower(edgeType) {
	case "on_success":
		return "ON_SUCCESS"
	case "on_failure":
		return "ON_FAILURE"
	case "on_timeout":
		return "ON_TIMEOUT"
	case "on_confirm":
		return "ON_CONFIRM"
	case "on_cancel":
		return "ON_CANCEL"
	default:
		return ""
	}
}

func decodeAgent(node neo4j.Node) Agent {
	props := node.Props
	return Agent{
		ID:                  getString(props, "id"),
		Name:                getString(props, "name"),
		Namespace:           getString(props, "namespace"),
		IPAddress:           toNullString(getString(props, "ip_address")),
		Tags:                datatypes.JSON([]byte(getString(props, "tags_json"))),
		LastSeen:            toNullTimeMillis(getInt64(props, "last_seen_ms")),
		CurrentState:        getString(props, "current_state"),
		Status:              getString(props, "status"),
		HardwareFingerprint: toNullString(getString(props, "hardware_fingerprint")),
		AssignedByServer:    getBool(props, "assigned_by_server"),
		CapabilityTemplateSavedAt: toNullTimeMillis(getInt64(props, "capability_template_saved_at_ms")),
		CapabilityTemplateCapabilityCount: int(getInt64(props, "capability_template_capability_count")),
		CreatedAt:           time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
		UpdatedAt:           time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
	}
}

func decodeStateDefinition(node neo4j.Node) StateDefinition {
	props := node.Props
	def := StateDefinition{
		ID:           getString(props, "id"),
		Name:         getString(props, "name"),
		Description:  toNullString(getString(props, "description")),
		DefaultState: getString(props, "default_state"),
		Version:      int(getInt64(props, "version")),
		CreatedAt:    time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
		UpdatedAt:    time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
	}

	if statesJSON := getString(props, "states_json"); statesJSON != "" {
		_ = json.Unmarshal([]byte(statesJSON), &def.States)
	}
	if mappingsJSON := getString(props, "action_mappings_json"); mappingsJSON != "" {
		_ = json.Unmarshal([]byte(mappingsJSON), &def.ActionMappings)
	}
	if waypointsJSON := getString(props, "teachable_waypoints_json"); waypointsJSON != "" {
		_ = json.Unmarshal([]byte(waypointsJSON), &def.TeachableWaypoints)
	}

	return def
}

// =============================================================================
// Agent Operations
// =============================================================================

// CountAgents returns the total number of agents in the database
func (r *Repository) CountAgents() (int64, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `MATCH (a:Agent) RETURN count(a) AS cnt`, nil)
		if err != nil {
			return int64(0), err
		}
		if res.Next(ctx) {
			if cnt, ok := res.Record().Get("cnt"); ok {
				return cnt.(int64), nil
			}
		}
		return int64(0), nil
	})
	if err != nil {
		return 0, err
	}
	return result.(int64), nil
}

// GetNextAgentNumber returns the next sequential number for auto-assigned agent naming.
// It checks both legacy "Agent-NNN" and current "Task Manager-NNN" names,
// then returns max(N)+1.
func (r *Repository) GetNextAgentNumber() (int, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		// Keep a single sequence across legacy and current prefixes.
		res, err := tx.Run(ctx, `
			MATCH (a:Agent)
			WITH CASE
				WHEN a.name =~ 'Task Manager-[0-9]+' THEN toInteger(substring(a.name, 13))
				WHEN a.name =~ 'Agent-[0-9]+' THEN toInteger(substring(a.name, 6))
				ELSE null
			END AS num
			WHERE num IS NOT NULL
			RETURN max(num) AS maxNum
		`, nil)
		if err != nil {
			return 1, err
		}
		if res.Next(ctx) {
			if maxNum, ok := res.Record().Get("maxNum"); ok && maxNum != nil {
				return int(maxNum.(int64)) + 1, nil
			}
		}
		return 1, nil
	})
	if err != nil {
		return 1, err
	}
	return result.(int), nil
}

func (r *Repository) GetAgent(id string) (*Agent, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `MATCH (a:Agent {id: $id}) RETURN a`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("a")
			if agentNode, ok := node.(neo4j.Node); ok {
				agent := decodeAgent(agentNode)
				return &agent, nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*Agent), nil
}

func (r *Repository) GetAllAgents() ([]Agent, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `MATCH (a:Agent) RETURN a`, nil)
		if err != nil {
			return nil, err
		}
		var agents []Agent
		for res.Next(ctx) {
			node, _ := res.Record().Get("a")
			if agentNode, ok := node.(neo4j.Node); ok {
				agents = append(agents, decodeAgent(agentNode))
			}
		}
		return agents, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]Agent), nil
}

func (r *Repository) UpdateAgentStatus(id, status string, ipAddress string) error {
	ctx := context.Background()
	props := map[string]any{
		"id":      id,
		"status":  status,
		"now_ms":  time.Now().UTC().UnixMilli(),
		"has_ip":  ipAddress != "",
		"ip_addr": ipAddress,
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (a:Agent {id: $id})
			SET a.status = $status,
			    a.last_seen_ms = $now_ms
			WITH a
			WHERE $has_ip
			SET a.ip_address = $ip_addr
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) UpdateAgentLastSeen(id string) error {
	ctx := context.Background()
	props := map[string]any{
		"id":     id,
		"now_ms": time.Now().UTC().UnixMilli(),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (a:Agent {id: $id})
			SET a.last_seen_ms = $now_ms
		`, props)
		return nil, err
	})
	return err
}

// SaveAgentCapabilityTemplate marks current capabilities as a persisted RTM template snapshot.
func (r *Repository) SaveAgentCapabilityTemplate(agentID string) (int, error) {
	caps, err := r.GetAgentCapabilities(agentID)
	if err != nil {
		return 0, err
	}
	if len(caps) == 0 {
		return 0, fmt.Errorf("no capabilities found for agent %s", agentID)
	}

	ctx := context.Background()
	nowMs := time.Now().UTC().UnixMilli()
	props := map[string]any{
		"id":                agentID,
		"saved_at_ms":       nowMs,
		"capability_count":  len(caps),
		"updated_at_ms":     nowMs,
	}

	_, err = r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
			MATCH (a:Agent {id: $id})
			SET a.capability_template_saved_at_ms = $saved_at_ms,
			    a.capability_template_capability_count = $capability_count,
			    a.updated_at_ms = $updated_at_ms
			RETURN a.id AS id
		`, props)
		if err != nil {
			return nil, err
		}
		if !result.Next(ctx) {
			return nil, fmt.Errorf("agent not found: %s", agentID)
		}
		return nil, nil
	})
	if err != nil {
		return 0, err
	}

	return len(caps), nil
}

func (r *Repository) CreateOrUpdateAgent(agent *Agent) error {
	if agent == nil {
		return fmt.Errorf("agent is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":                   agent.ID,
		"name":                 agent.Name,
		"ip_address":           agent.IPAddress.String,
		"status":               agent.Status,
		"hardware_fingerprint": agent.HardwareFingerprint.String,
		"assigned_by_server":   agent.AssignedByServer,
		"created_at_ms":        timeToMillis(agent.CreatedAt),
		"last_seen_ms":         timeToMillis(agent.LastSeen.Time),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MERGE (a:Agent {id: $id})
			SET a.name = $name,
			    a.ip_address = $ip_address,
			    a.status = $status,
			    a.hardware_fingerprint = $hardware_fingerprint,
			    a.assigned_by_server = $assigned_by_server,
			    a.created_at_ms = coalesce(a.created_at_ms, $created_at_ms),
			    a.last_seen_ms = $last_seen_ms
		`, props)
		return nil, err
	})
	return err
}

// UpdateAgent updates an existing agent's mutable fields (like name)
func (r *Repository) UpdateAgent(agent *Agent) error {
	if agent == nil {
		return fmt.Errorf("agent is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":   agent.ID,
		"name": agent.Name,
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
			MATCH (a:Agent {id: $id})
			SET a.name = $name
			RETURN a.id
		`, props)
		if err != nil {
			return nil, err
		}
		if !result.Next(ctx) {
			return nil, fmt.Errorf("agent not found: %s", agent.ID)
		}
		return nil, nil
	})
	return err
}

// GetAgentByHardwareFingerprint retrieves an agent by its hardware fingerprint
func (r *Repository) GetAgentByHardwareFingerprint(fingerprint string) (*Agent, error) {
	if fingerprint == "" {
		return nil, nil
	}
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
			MATCH (a:Agent {hardware_fingerprint: $fingerprint})
			RETURN a.id AS id, a.name AS name, a.status AS status,
			       a.hardware_fingerprint AS hardware_fingerprint,
			       a.assigned_by_server AS assigned_by_server
		`, map[string]any{"fingerprint": fingerprint})
		if err != nil {
			return nil, err
		}
		if result.Next(ctx) {
			rec := result.Record()
			agent := &Agent{}
			if v, ok := rec.Get("id"); ok && v != nil {
				agent.ID = v.(string)
			}
			if v, ok := rec.Get("name"); ok && v != nil {
				agent.Name = v.(string)
			}
			if v, ok := rec.Get("status"); ok && v != nil {
				agent.Status = v.(string)
			}
			if v, ok := rec.Get("hardware_fingerprint"); ok && v != nil {
				agent.HardwareFingerprint = sql.NullString{String: v.(string), Valid: true}
			}
			if v, ok := rec.Get("assigned_by_server"); ok && v != nil {
				agent.AssignedByServer = v.(bool)
			}
			return agent, nil
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*Agent), nil
}

func (r *Repository) CreateAgent(agent *Agent) error {
	return r.CreateOrUpdateAgent(agent)
}

func (r *Repository) DeleteAgent(id string) error {
	ctx := context.Background()
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (a:Agent {id: $id})
			DETACH DELETE a
		`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}

		// AgentCapability nodes are stored as separate entities (property-linked by agent_id),
		// so removing an Agent node alone leaves orphan capabilities behind.
		_, err = tx.Run(ctx, `
			MATCH (c:AgentCapability {agent_id: $id})
			DETACH DELETE c
		`, map[string]any{"id": id})
		return nil, err
	})
	return err
}

// UpdateAgentEnhancedState updates an agent's enhanced state (state code, semantic tags, graph ID)
func (r *Repository) UpdateAgentEnhancedState(id, stateCode string, semanticTags []string, graphID string) error {
	ctx := context.Background()

	tagsJSON := "[]"
	if len(semanticTags) > 0 {
		if b, err := json.Marshal(semanticTags); err == nil {
			tagsJSON = string(b)
		}
	}

	props := map[string]any{
		"id":            id,
		"state_code":    stateCode,
		"semantic_tags": tagsJSON,
		"graph_id":      graphID,
		"updated_at_ms": time.Now().UTC().UnixMilli(),
	}

	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (a:Agent {id: $id})
			SET a.current_state_code = $state_code,
			    a.semantic_tags = $semantic_tags,
			    a.current_graph_id = $graph_id,
			    a.updated_at_ms = $updated_at_ms
		`, props)
		return nil, err
	})
	return err
}

// GetAgentEnhancedState retrieves an agent's enhanced state
func (r *Repository) GetAgentEnhancedState(id string) (stateCode string, semanticTags []string, graphID string, err error) {
	ctx := context.Background()

	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
			MATCH (a:Agent {id: $id})
			RETURN a.current_state_code AS state_code,
			       a.semantic_tags AS semantic_tags,
			       a.current_graph_id AS graph_id
		`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}

		if result.Next(ctx) {
			record := result.Record()
			return map[string]any{
				"state_code":    getString(record.AsMap(), "state_code"),
				"semantic_tags": getString(record.AsMap(), "semantic_tags"),
				"graph_id":      getString(record.AsMap(), "graph_id"),
			}, nil
		}
		return nil, nil
	})

	if err != nil {
		return "", nil, "", err
	}
	if result == nil {
		return "", nil, "", fmt.Errorf("agent %s not found", id)
	}

	m := result.(map[string]any)
	stateCode = m["state_code"].(string)
	graphID = m["graph_id"].(string)

	tagsJSON := m["semantic_tags"].(string)
	if tagsJSON != "" && tagsJSON != "[]" {
		json.Unmarshal([]byte(tagsJSON), &semanticTags)
	}

	return stateCode, semanticTags, graphID, nil
}

// GetAllAgentStates retrieves enhanced state info for all agents
func (r *Repository) GetAllAgentStates() (map[string]struct {
	StateCode    string
	SemanticTags []string
	GraphID      string
	IsOnline     bool
}, error) {
	ctx := context.Background()

	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
			MATCH (a:Agent)
			RETURN a.id AS id,
			       a.current_state_code AS state_code,
			       a.semantic_tags AS semantic_tags,
			       a.current_graph_id AS graph_id,
			       a.status AS status
		`, nil)
		if err != nil {
			return nil, err
		}

		states := make(map[string]struct {
			StateCode    string
			SemanticTags []string
			GraphID      string
			IsOnline     bool
		})

		for result.Next(ctx) {
			record := result.Record()
			m := record.AsMap()
			id := getString(m, "id")
			stateCode := getString(m, "state_code")
			graphID := getString(m, "graph_id")
			status := getString(m, "status")

			var tags []string
			tagsJSON := getString(m, "semantic_tags")
			if tagsJSON != "" && tagsJSON != "[]" {
				json.Unmarshal([]byte(tagsJSON), &tags)
			}

			states[id] = struct {
				StateCode    string
				SemanticTags []string
				GraphID      string
				IsOnline     bool
			}{
				StateCode:    stateCode,
				SemanticTags: tags,
				GraphID:      graphID,
				IsOnline:     status == "online",
			}
		}

		return states, nil
	})

	if err != nil {
		return nil, err
	}
	return result.(map[string]struct {
		StateCode    string
		SemanticTags []string
		GraphID      string
		IsOnline     bool
	}), nil
}

// =============================================================================
// BehaviorTree Operations
// =============================================================================

func (r *Repository) GetBehaviorTree(id string) (*BehaviorTree, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (g:ActionGraph {id: $id})
			RETURN g
			ORDER BY g.version DESC
			LIMIT 1
		`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("g")
			if gNode, ok := node.(neo4j.Node); ok {
				props := gNode.Props
				stepsJSON := getString(props, "steps_json")
				preconditionsJSON := getString(props, "preconditions_json")
				statesJSON := getString(props, "states_json")
				entryPoint := getString(props, "entry_point")
				bt := BehaviorTree{
					ID:                 getString(props, "id"),
					Name:               getString(props, "name"),
					Description:        toNullString(getString(props, "description")),
					AgentID:            toNullString(getString(props, "agent_id")),
					Version:            int(getInt64(props, "version")),
					IsTemplate:         isTemplateFromProps(props),
					TemplateCategory:   toNullString(getString(props, "template_category")),
					AutoGenerateStates: getBool(props, "auto_generate_states"),
					CreatedAt:          time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:          time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
					// Lock fields
					LockedBy:      toNullString(getString(props, "locked_by")),
					LockSessionID: toNullString(getString(props, "lock_session_id")),
				}
				if entryPoint != "" {
					bt.EntryPoint = toNullString(entryPoint)
				}
				if stepsJSON != "" {
					bt.Steps = datatypes.JSON([]byte(stepsJSON))
				}
				if preconditionsJSON != "" {
					bt.Preconditions = datatypes.JSON([]byte(preconditionsJSON))
				}
				if statesJSON != "" {
					bt.States = datatypes.JSON([]byte(statesJSON))
				}
				if planningStatesJSON := getString(props, "planning_states_json"); planningStatesJSON != "" {
					bt.PlanningStates = datatypes.JSON([]byte(planningStatesJSON))
				}
				// Parse lock timestamps
				if lockedAtMs := getInt64(props, "locked_at_ms"); lockedAtMs > 0 {
					bt.LockedAt = sql.NullTime{Time: time.UnixMilli(lockedAtMs).UTC(), Valid: true}
				}
				if lockExpiresAtMs := getInt64(props, "lock_expires_at_ms"); lockExpiresAtMs > 0 {
					bt.LockExpiresAt = sql.NullTime{Time: time.UnixMilli(lockExpiresAtMs).UTC(), Valid: true}
				}
				return &bt, nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*BehaviorTree), nil
}

func (r *Repository) GetBehaviorTreesByAgent(agentID string) ([]BehaviorTree, error) {
	return r.GetBehaviorTrees(agentID, true)
}

// GetBehaviorTreesWithExpiredLocks returns all behavior trees with expired locks
func (r *Repository) GetBehaviorTreesWithExpiredLocks() ([]BehaviorTree, error) {
	ctx := context.Background()
	nowMs := time.Now().UTC().UnixMilli()

	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		// Find all graphs with lock_expires_at_ms set and expired
		res, err := tx.Run(ctx, `
			MATCH (g:ActionGraph)
			WHERE g.lock_expires_at_ms > 0 AND g.lock_expires_at_ms < $now_ms
			RETURN g
		`, map[string]any{"now_ms": nowMs})
		if err != nil {
			return nil, err
		}

		var graphs []BehaviorTree
		for res.Next(ctx) {
			node, _ := res.Record().Get("g")
			if gNode, ok := node.(neo4j.Node); ok {
				props := gNode.Props
				bt := BehaviorTree{
					ID:            getString(props, "id"),
					Name:          getString(props, "name"),
					LockedBy:      toNullString(getString(props, "locked_by")),
					LockSessionID: toNullString(getString(props, "lock_session_id")),
				}
				if lockedAtMs := getInt64(props, "locked_at_ms"); lockedAtMs > 0 {
					bt.LockedAt = sql.NullTime{Time: time.UnixMilli(lockedAtMs).UTC(), Valid: true}
				}
				if lockExpiresAtMs := getInt64(props, "lock_expires_at_ms"); lockExpiresAtMs > 0 {
					bt.LockExpiresAt = sql.NullTime{Time: time.UnixMilli(lockExpiresAtMs).UTC(), Valid: true}
				}
				graphs = append(graphs, bt)
			}
		}
		return graphs, res.Err()
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return []BehaviorTree{}, nil
	}
	return result.([]BehaviorTree), nil
}

func (r *Repository) GetBehaviorTrees(agentID string, includeTemplates bool) ([]BehaviorTree, error) {
	ctx := context.Background()
	query := "MATCH (g:ActionGraph) "
	params := map[string]any{}
	if agentID != "" {
		if includeTemplates {
			query += "WHERE g.agent_id = $agent_id OR g.is_template = true OR coalesce(g.agent_id, '') = '' "
		} else {
			query += "WHERE g.agent_id = $agent_id "
		}
		params["agent_id"] = agentID
	}
	query += "RETURN g"

	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		var graphs []BehaviorTree
		for res.Next(ctx) {
			node, _ := res.Record().Get("g")
			if gNode, ok := node.(neo4j.Node); ok {
				props := gNode.Props
				entryPoint := getString(props, "entry_point")
				var entryPointValue sql.NullString
				if entryPoint != "" {
					entryPointValue = toNullString(entryPoint)
				}
				stepsJSON := getString(props, "steps_json")
				preconditionsJSON := getString(props, "preconditions_json")
				statesJSON := getString(props, "states_json")
				bt := BehaviorTree{
					ID:                 getString(props, "id"),
					Name:               getString(props, "name"),
					Description:        toNullString(getString(props, "description")),
					AgentID:            toNullString(getString(props, "agent_id")),
					EntryPoint:         entryPointValue,
					Version:            int(getInt64(props, "version")),
					IsTemplate:         isTemplateFromProps(props),
					TemplateCategory:   toNullString(getString(props, "template_category")),
					AutoGenerateStates: getBool(props, "auto_generate_states"),
					CreatedAt:          time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:          time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
				}
				if stepsJSON != "" {
					bt.Steps = datatypes.JSON([]byte(stepsJSON))
				}
				if preconditionsJSON != "" {
					bt.Preconditions = datatypes.JSON([]byte(preconditionsJSON))
				}
				if statesJSON != "" {
					bt.States = datatypes.JSON([]byte(statesJSON))
				}
				if planningStatesJSON := getString(props, "planning_states_json"); planningStatesJSON != "" {
					bt.PlanningStates = datatypes.JSON([]byte(planningStatesJSON))
				}
				graphs = append(graphs, bt)
			}
		}
		return graphs, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]BehaviorTree), nil
}

func (r *Repository) CreateBehaviorTree(graph *BehaviorTree) error {
	if graph == nil {
		return fmt.Errorf("behavior tree is nil")
	}
	steps, err := parseBehaviorTreeSteps(graph)
	if err != nil {
		return err
	}
	requiredTypes := ExtractActionTypesFromSteps(steps)
	executionMode := executionModeFromSteps(steps)
	stepsJSON := string(graph.Steps)
	entryPoint := graph.EntryPoint.String
	if entryPoint == "" && len(steps) > 0 {
		entryPoint = steps[0].ID
		graph.EntryPoint = toNullString(entryPoint)
	}

	// Auto-generate states if enabled (default: true)
	statesJSON := string(graph.States)
	if graph.AutoGenerateStates || len(graph.States) == 0 {
		var existingStates []GraphState
		if len(graph.States) > 0 {
			json.Unmarshal(graph.States, &existingStates)
		}
		generatedStates := GenerateStatesFromSteps(steps, existingStates)
		if b, err := json.Marshal(generatedStates); err == nil {
			statesJSON = string(b)
			graph.States = datatypes.JSON(b)
		}
	}

	ctx := context.Background()
	planningStatesJSON := string(graph.PlanningStates)
	props := map[string]any{
		"id":                    graph.ID,
		"name":                  graph.Name,
		"description":           graph.Description.String,
		"agent_id":              graph.AgentID.String,
		"entry_point":           entryPoint,
		"version":               graph.Version,
		"is_template":           graph.IsTemplate,
		"template_category":     graph.TemplateCategory.String,
		"steps_json":            stepsJSON,
		"preconditions_json":    string(graph.Preconditions),
		"required_action_types": requiredTypes,
		"execution_mode":        executionMode,
		"checksum":              checksumForJSON(stepsJSON),
		"schema_version":        "1.0.0",
		"states_json":           statesJSON,
		"auto_generate_states":  graph.AutoGenerateStates,
		"planning_states_json":  planningStatesJSON,
		"created_at_ms":         timeToMillis(graph.CreatedAt),
		"updated_at_ms":         timeToMillis(graph.UpdatedAt),
	}
	_, err = r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			CREATE (g:ActionGraph {
				id: $id,
				name: $name,
				description: $description,
				agent_id: $agent_id,
				entry_point: $entry_point,
				version: $version,
				is_template: $is_template,
				template_category: $template_category,
				steps_json: $steps_json,
				preconditions_json: $preconditions_json,
				required_action_types: $required_action_types,
				execution_mode: $execution_mode,
				checksum: $checksum,
				schema_version: $schema_version,
				states_json: $states_json,
				auto_generate_states: $auto_generate_states,
				planning_states_json: $planning_states_json,
				created_at_ms: $created_at_ms,
				updated_at_ms: $updated_at_ms
			})
		`, props)
		if err != nil {
			return nil, err
		}
		if err := r.storeBehaviorTreeStructure(ctx, tx, graph, steps); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func (r *Repository) UpdateBehaviorTree(graph *BehaviorTree) error {
	if graph == nil {
		return fmt.Errorf("behavior tree is nil")
	}
	steps, err := parseBehaviorTreeSteps(graph)
	if err != nil {
		return err
	}
	requiredTypes := ExtractActionTypesFromSteps(steps)
	executionMode := executionModeFromSteps(steps)
	stepsJSON := string(graph.Steps)
	entryPoint := graph.EntryPoint.String
	if entryPoint == "" && len(steps) > 0 {
		entryPoint = steps[0].ID
		graph.EntryPoint = toNullString(entryPoint)
	}

	// Auto-generate states if enabled
	statesJSON := string(graph.States)
	if graph.AutoGenerateStates {
		var existingStates []GraphState
		if len(graph.States) > 0 {
			json.Unmarshal(graph.States, &existingStates)
		}
		generatedStates := GenerateStatesFromSteps(steps, existingStates)
		if b, err := json.Marshal(generatedStates); err == nil {
			statesJSON = string(b)
			graph.States = datatypes.JSON(b)
		}
	}

	ctx := context.Background()

	// Handle lock fields - convert to milliseconds for Neo4j storage
	var lockedAtMs, lockExpiresAtMs int64
	if graph.LockedAt.Valid {
		lockedAtMs = graph.LockedAt.Time.UTC().UnixMilli()
	}
	if graph.LockExpiresAt.Valid {
		lockExpiresAtMs = graph.LockExpiresAt.Time.UTC().UnixMilli()
	}

	planningStatesJSON := string(graph.PlanningStates)
	props := map[string]any{
		"id":                    graph.ID,
		"name":                  graph.Name,
		"description":           graph.Description.String,
		"agent_id":              graph.AgentID.String,
		"entry_point":           entryPoint,
		"version":               graph.Version,
		"is_template":           graph.IsTemplate,
		"template_category":     graph.TemplateCategory.String,
		"steps_json":            stepsJSON,
		"preconditions_json":    string(graph.Preconditions),
		"required_action_types": requiredTypes,
		"execution_mode":        executionMode,
		"checksum":              checksumForJSON(stepsJSON),
		"schema_version":        "1.0.0",
		"states_json":           statesJSON,
		"auto_generate_states":  graph.AutoGenerateStates,
		"planning_states_json":  planningStatesJSON,
		"updated_at_ms":         time.Now().UTC().UnixMilli(),
		// Lock fields
		"locked_by":          graph.LockedBy.String,
		"locked_at_ms":       lockedAtMs,
		"lock_expires_at_ms": lockExpiresAtMs,
		"lock_session_id":    graph.LockSessionID.String,
	}
	_, err = r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (g:ActionGraph {id: $id})
			SET g.name = $name,
				g.description = $description,
				g.agent_id = $agent_id,
				g.entry_point = $entry_point,
				g.version = $version,
				g.is_template = $is_template,
				g.template_category = $template_category,
				g.steps_json = $steps_json,
			    g.preconditions_json = $preconditions_json,
			    g.required_action_types = $required_action_types,
			    g.execution_mode = $execution_mode,
			    g.checksum = $checksum,
			    g.schema_version = $schema_version,
			    g.states_json = $states_json,
			    g.auto_generate_states = $auto_generate_states,
			    g.planning_states_json = $planning_states_json,
			    g.updated_at_ms = $updated_at_ms,
			    g.locked_by = $locked_by,
			    g.locked_at_ms = $locked_at_ms,
			    g.lock_expires_at_ms = $lock_expires_at_ms,
			    g.lock_session_id = $lock_session_id
		`, props)
		if err != nil {
			return nil, err
		}
		if err := r.storeBehaviorTreeStructure(ctx, tx, graph, steps); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func (r *Repository) DeleteBehaviorTree(id string) error {
	ctx := context.Background()
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (g:ActionGraph {id: $id})
			DETACH DELETE g
		`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		_, err = tx.Run(ctx, `
			MATCH (n {graph_id: $id})
			DETACH DELETE n
		`, map[string]any{"id": id})
		return nil, err
	})
	return err
}

func (r *Repository) storeBehaviorTreeStructure(ctx context.Context, tx neo4j.ManagedTransaction, graph *BehaviorTree, steps []BehaviorTreeStep) error {
	if graph == nil {
		return fmt.Errorf("behavior tree is nil")
	}

	_, err := tx.Run(ctx, `
		MATCH (n {graph_id: $graph_id})
		DETACH DELETE n
	`, map[string]any{"graph_id": graph.ID})
	if err != nil {
		return err
	}

	if len(steps) == 0 {
		return nil
	}

	entryPoint := graph.EntryPoint.String
	if entryPoint == "" {
		entryPoint = steps[0].ID
	}

	for _, step := range steps {
		isTerminal := step.Type == "terminal" || step.TerminalType != ""
		if isTerminal {
			props := map[string]any{
				"id":            step.ID,
				"graph_id":      graph.ID,
				"graph_version": graph.Version,
				"terminal_type": step.TerminalType,
				"message":       step.Message,
				"alert":         step.Alert,
			}
			_, err = tx.Run(ctx, `
				MATCH (g:ActionGraph {id:$graph_id})
				CREATE (t:Terminal {
					id: $id,
					graph_id: $graph_id,
					graph_version: $graph_version,
					terminal_type: $terminal_type,
					message: $message,
					alert: $alert
				})
				CREATE (g)-[:CONTAINS]->(t)
			`, props)
			if err != nil {
				return err
			}
			continue
		}

		stepType := ""
		if step.Action != nil {
			stepType = "action"
		} else if step.WaitFor != nil {
			stepType = "wait"
		} else if step.Type != "" {
			stepType = step.Type
		}

		actionJSON, err := jsonString(step.Action)
		if err != nil {
			return err
		}
		waitJSON, err := jsonString(step.WaitFor)
		if err != nil {
			return err
		}

		props := map[string]any{
			"id":             step.ID,
			"graph_id":       graph.ID,
			"graph_version":  graph.Version,
			"name":           step.Name,
			"step_type":      stepType,
			"action_type":    "",
			"action_server":  "",
			"action_json":    actionJSON,
			"wait_json":      waitJSON,
			"condition_json": "",
			"pre_states":     step.PreStates,
			"during_states":  step.DuringStates,
			"success_states": step.SuccessStates,
			"failure_states": step.FailureStates,
		}

		if step.Action != nil {
			props["action_type"] = step.Action.Type
			props["action_server"] = step.Action.Server
		}

		_, err = tx.Run(ctx, `
			MATCH (g:ActionGraph {id:$graph_id})
			CREATE (s:Step {
				id: $id,
				graph_id: $graph_id,
				graph_version: $graph_version,
				name: $name,
				step_type: $step_type,
				action_type: $action_type,
				action_server: $action_server,
				action_json: $action_json,
				wait_json: $wait_json,
				condition_json: $condition_json,
				pre_states: $pre_states,
				during_states: $during_states,
				success_states: $success_states,
				failure_states: $failure_states
			})
			CREATE (g)-[:CONTAINS]->(s)
		`, props)
		if err != nil {
			return err
		}

		// Start conditions
		for idx, cond := range step.StartConditions {
			condID := cond.ID
			if condID == "" {
				condID = fmt.Sprintf("%s:%s:%d", graph.ID, step.ID, idx+1)
			}
			operator := strings.ToLower(cond.Operator)
			if operator == "" {
				operator = "and"
			}
			condProps := map[string]any{
				"id":                condID,
				"graph_id":          graph.ID,
				"graph_version":     graph.Version,
				"operator":          operator,
				"quantifier":        cond.Quantifier,
				"target_type":       cond.TargetType,
				"agent_id":          cond.AgentID,
				"state_operator":    cond.StateOperator,
				"state":             cond.State,
				"allowed_states":    cond.AllowedStates,
				"max_staleness_sec": cond.MaxStalenessSec,
				"require_online":    cond.RequireOnline,
				"message":           cond.Message,
				"step_id":           step.ID,
				"order":             idx + 1,
			}
			_, err = tx.Run(ctx, `
				MATCH (g:ActionGraph {id:$graph_id})
				MATCH (s:Step {id:$step_id, graph_id:$graph_id})
				CREATE (c:Condition {
					id: $id,
					graph_id: $graph_id,
					graph_version: $graph_version,
					operator: $operator,
					quantifier: $quantifier,
					target_type: $target_type,
					agent_id: $agent_id,
					state_operator: $state_operator,
					state: $state,
					allowed_states: $allowed_states,
					max_staleness_sec: $max_staleness_sec,
					require_online: $require_online,
					message: $message
				})
				CREATE (g)-[:CONTAINS]->(c)
				CREATE (s)-[:GATED_BY {order:$order, operator:$operator}]->(c)
			`, condProps)
			if err != nil {
				return err
			}
		}

		// State relationships
		for _, stateName := range step.DuringStates {
			if stateName == "" {
				continue
			}
			_, err = tx.Run(ctx, `
				MATCH (s:Step {id:$step_id, graph_id:$graph_id})
				MERGE (st:State {name:$state})
				CREATE (s)-[:SETS_DURING]->(st)
			`, map[string]any{"step_id": step.ID, "graph_id": graph.ID, "state": stateName})
			if err != nil {
				return err
			}
		}

		for _, stateName := range step.SuccessStates {
			if stateName == "" {
				continue
			}
			_, err = tx.Run(ctx, `
				MATCH (s:Step {id:$step_id, graph_id:$graph_id})
				MERGE (st:State {name:$state})
				CREATE (s)-[:SETS_SUCCESS]->(st)
			`, map[string]any{"step_id": step.ID, "graph_id": graph.ID, "state": stateName})
			if err != nil {
				return err
			}
		}

		for _, stateName := range step.FailureStates {
			if stateName == "" {
				continue
			}
			_, err = tx.Run(ctx, `
				MATCH (s:Step {id:$step_id, graph_id:$graph_id})
				MERGE (st:State {name:$state})
				CREATE (s)-[:SETS_FAILURE]->(st)
			`, map[string]any{"step_id": step.ID, "graph_id": graph.ID, "state": stateName})
			if err != nil {
				return err
			}
		}
	}

	edges := extractEdgesFromSteps(steps)
	for _, edge := range edges {
		relType := relTypeForEdge(edge.EdgeType)
		if relType == "" {
			continue
		}
		query := fmt.Sprintf(`
			MATCH (from {id:$from, graph_id:$graph_id})
			MATCH (to {id:$to, graph_id:$graph_id})
			CREATE (from)-[:%s {retry:$retry, fallback:$fallback, condition:$condition}]->(to)
		`, relType)
		_, err = tx.Run(ctx, query, map[string]any{
			"from":      edge.From,
			"to":        edge.To,
			"graph_id":  graph.ID,
			"retry":     edge.Retry,
			"fallback":  edge.Fallback,
			"condition": edge.Condition,
		})
		if err != nil {
			return err
		}
	}

	if entryPoint != "" {
		_, err = tx.Run(ctx, `
			MATCH (g:ActionGraph {id:$graph_id})
			MATCH (s:Step {id:$entry_id, graph_id:$graph_id})
			MERGE (g)-[:ENTRY_POINT]->(s)
		`, map[string]any{"graph_id": graph.ID, "entry_id": entryPoint})
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Repository) GetBehaviorTreeSteps(id string) ([]BehaviorTreeStep, error) {
	graph, err := r.GetBehaviorTree(id)
	if err != nil || graph == nil {
		return nil, err
	}
	var steps []BehaviorTreeStep
	if len(graph.Steps) == 0 {
		return steps, nil
	}
	if err := json.Unmarshal(graph.Steps, &steps); err != nil {
		return nil, fmt.Errorf("failed to unmarshal steps: %w", err)
	}
	return steps, nil
}

// =============================================================================
// Agent Behavior Tree Operations
// =============================================================================

func (r *Repository) GetAgentBehaviorTree(agentID, graphID string) (*AgentBehaviorTree, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (aag:AgentActionGraph {agent_id:$agent_id, behavior_tree_id:$graph_id})
			RETURN aag
		`, map[string]any{"agent_id": agentID, "graph_id": graphID})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("aag")
			if aagNode, ok := node.(neo4j.Node); ok {
				props := aagNode.Props
				aag := AgentBehaviorTree{
					ID:               getString(props, "id"),
					AgentID:          getString(props, "agent_id"),
					BehaviorTreeID:   getStringWithFallback(props, "behavior_tree_id", "action_graph_id"),
					ServerVersion:    int(getInt64(props, "server_version")),
					DeployedVersion:  int(getInt64(props, "deployed_version")),
					DeploymentStatus: getString(props, "deployment_status"),
					DeploymentError:  toNullString(getString(props, "deployment_error")),
					DeployedAt:       toNullTimeMillis(getInt64(props, "deployed_at_ms")),
					Enabled:          getBool(props, "enabled"),
					Priority:         int(getInt64(props, "priority")),
					CreatedAt:        time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:        time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
				}
				return &aag, nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*AgentBehaviorTree), nil
}

func (r *Repository) GetAgentBehaviorTrees(agentID string) ([]AgentBehaviorTree, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (aag:AgentActionGraph {agent_id:$agent_id})
			RETURN aag
		`, map[string]any{"agent_id": agentID})
		if err != nil {
			return nil, err
		}
		var list []AgentBehaviorTree
		for res.Next(ctx) {
			node, _ := res.Record().Get("aag")
			if aagNode, ok := node.(neo4j.Node); ok {
				props := aagNode.Props
				list = append(list, AgentBehaviorTree{
					ID:               getString(props, "id"),
					AgentID:          getString(props, "agent_id"),
					BehaviorTreeID:   getStringWithFallback(props, "behavior_tree_id", "action_graph_id"),
					ServerVersion:    int(getInt64(props, "server_version")),
					DeployedVersion:  int(getInt64(props, "deployed_version")),
					DeploymentStatus: getString(props, "deployment_status"),
					DeploymentError:  toNullString(getString(props, "deployment_error")),
					DeployedAt:       toNullTimeMillis(getInt64(props, "deployed_at_ms")),
					Enabled:          getBool(props, "enabled"),
					Priority:         int(getInt64(props, "priority")),
					CreatedAt:        time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:        time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
				})
			}
		}
		return list, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]AgentBehaviorTree), nil
}

func (r *Repository) CreateAgentBehaviorTree(aag *AgentBehaviorTree) error {
	if aag == nil {
		return fmt.Errorf("agent behavior tree is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":                aag.ID,
		"agent_id":          aag.AgentID,
		"behavior_tree_id":  aag.BehaviorTreeID,
		"server_version":    aag.ServerVersion,
		"deployed_version":  aag.DeployedVersion,
		"deployment_status": aag.DeploymentStatus,
		"deployment_error":  aag.DeploymentError.String,
		"deployed_at_ms":    timeToMillis(aag.DeployedAt.Time),
		"enabled":           aag.Enabled,
		"priority":          aag.Priority,
		"created_at_ms":     timeToMillis(aag.CreatedAt),
		"updated_at_ms":     timeToMillis(aag.UpdatedAt),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			CREATE (aag:AgentActionGraph {
				id: $id,
				agent_id: $agent_id,
				behavior_tree_id: $behavior_tree_id,
				server_version: $server_version,
				deployed_version: $deployed_version,
				deployment_status: $deployment_status,
				deployment_error: $deployment_error,
				deployed_at_ms: $deployed_at_ms,
				enabled: $enabled,
				priority: $priority,
				created_at_ms: $created_at_ms,
				updated_at_ms: $updated_at_ms
			})
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) UpdateAgentBehaviorTree(aag *AgentBehaviorTree) error {
	if aag == nil {
		return fmt.Errorf("agent behavior tree is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":                aag.ID,
		"server_version":    aag.ServerVersion,
		"deployed_version":  aag.DeployedVersion,
		"deployment_status": aag.DeploymentStatus,
		"deployment_error":  aag.DeploymentError.String,
		"deployed_at_ms":    timeToMillis(aag.DeployedAt.Time),
		"enabled":           aag.Enabled,
		"priority":          aag.Priority,
		"updated_at_ms":     time.Now().UTC().UnixMilli(),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (aag:AgentActionGraph {id: $id})
			SET aag.server_version = $server_version,
			    aag.deployed_version = $deployed_version,
			    aag.deployment_status = $deployment_status,
			    aag.deployment_error = $deployment_error,
			    aag.deployed_at_ms = $deployed_at_ms,
			    aag.enabled = $enabled,
			    aag.priority = $priority,
			    aag.updated_at_ms = $updated_at_ms
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) DeleteAgentBehaviorTree(agentID, graphID string) error {
	ctx := context.Background()
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (aag:AgentActionGraph {agent_id:$agent_id, behavior_tree_id:$graph_id})
			DETACH DELETE aag
		`, map[string]any{"agent_id": agentID, "graph_id": graphID})
		return nil, err
	})
	return err
}

func (r *Repository) DeleteAgentBehaviorTreesByAgent(agentID string) error {
	ctx := context.Background()
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (aag:AgentActionGraph {agent_id:$agent_id})
			DETACH DELETE aag
		`, map[string]any{"agent_id": agentID})
		return nil, err
	})
	return err
}

func (r *Repository) GetAgentBehaviorTreesByGraphID(graphID string) ([]AgentBehaviorTree, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (aag:AgentActionGraph {behavior_tree_id:$graph_id})
			RETURN aag
		`, map[string]any{"graph_id": graphID})
		if err != nil {
			return nil, err
		}
		var list []AgentBehaviorTree
		for res.Next(ctx) {
			node, _ := res.Record().Get("aag")
			if aagNode, ok := node.(neo4j.Node); ok {
				props := aagNode.Props
				list = append(list, AgentBehaviorTree{
					ID:               getString(props, "id"),
					AgentID:          getString(props, "agent_id"),
					BehaviorTreeID:   getStringWithFallback(props, "behavior_tree_id", "action_graph_id"),
					ServerVersion:    int(getInt64(props, "server_version")),
					DeployedVersion:  int(getInt64(props, "deployed_version")),
					DeploymentStatus: getString(props, "deployment_status"),
					DeploymentError:  toNullString(getString(props, "deployment_error")),
					DeployedAt:       toNullTimeMillis(getInt64(props, "deployed_at_ms")),
					Enabled:          getBool(props, "enabled"),
					Priority:         int(getInt64(props, "priority")),
					CreatedAt:        time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:        time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
				})
			}
		}
		return list, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]AgentBehaviorTree), nil
}

func (r *Repository) GetAgentBehaviorTreeByID(id string) (*AgentBehaviorTree, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (aag:AgentActionGraph {id:$id})
			RETURN aag
		`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("aag")
			if aagNode, ok := node.(neo4j.Node); ok {
				props := aagNode.Props
				aag := AgentBehaviorTree{
					ID:               getString(props, "id"),
					AgentID:          getString(props, "agent_id"),
					BehaviorTreeID:   getStringWithFallback(props, "behavior_tree_id", "action_graph_id"),
					ServerVersion:    int(getInt64(props, "server_version")),
					DeployedVersion:  int(getInt64(props, "deployed_version")),
					DeploymentStatus: getString(props, "deployment_status"),
					DeploymentError:  toNullString(getString(props, "deployment_error")),
					DeployedAt:       toNullTimeMillis(getInt64(props, "deployed_at_ms")),
					Enabled:          getBool(props, "enabled"),
					Priority:         int(getInt64(props, "priority")),
					CreatedAt:        time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:        time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
				}
				return &aag, nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*AgentBehaviorTree), nil
}

func (r *Repository) UpdateDeploymentStatus(id, status string, version int, errorMsg string) error {
	ctx := context.Background()
	props := map[string]any{
		"id":        id,
		"status":    status,
		"version":   version,
		"error_msg": errorMsg,
		"now_ms":    time.Now().UTC().UnixMilli(),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (aag:AgentActionGraph {id:$id})
			SET aag.deployment_status = $status,
			    aag.deployed_version = $version,
			    aag.deployment_error = $error_msg,
			    aag.updated_at_ms = $now_ms
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) GetAssignedTemplateIDs(agentID string) map[string]bool {
	assigned := map[string]bool{}
	aags, err := r.GetAgentBehaviorTrees(agentID)
	if err != nil {
		return assigned
	}
	for _, aag := range aags {
		assigned[aag.BehaviorTreeID] = true
	}
	return assigned
}

// =============================================================================
// Deployment Logs
// =============================================================================

func (r *Repository) CreateDeploymentLog(logEntry *BehaviorTreeDeploymentLog) error {
	if logEntry == nil {
		return fmt.Errorf("deployment log is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":                     logEntry.ID,
		"agent_behavior_tree_id": logEntry.AgentBehaviorTreeID,
		"action":                 logEntry.Action,
		"version":                logEntry.Version,
		"status":                 logEntry.Status,
		"error_message":          logEntry.ErrorMessage.String,
		"initiated_at_ms":        timeToMillis(logEntry.InitiatedAt),
		"completed_at_ms":        timeToMillis(logEntry.CompletedAt.Time),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			CREATE (l:DeploymentLog {
				id: $id,
				agent_behavior_tree_id: $agent_behavior_tree_id,
				action: $action,
				version: $version,
				status: $status,
				error_message: $error_message,
				initiated_at_ms: $initiated_at_ms,
				completed_at_ms: $completed_at_ms
			})
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) UpdateDeploymentLog(logEntry *BehaviorTreeDeploymentLog) error {
	if logEntry == nil {
		return fmt.Errorf("deployment log is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":              logEntry.ID,
		"status":          logEntry.Status,
		"error_message":   logEntry.ErrorMessage.String,
		"completed_at_ms": timeToMillis(logEntry.CompletedAt.Time),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (l:DeploymentLog {id:$id})
			SET l.status = $status,
			    l.error_message = $error_message,
			    l.completed_at_ms = $completed_at_ms
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) GetDeploymentLogs(agentBehaviorTreeID string) ([]BehaviorTreeDeploymentLog, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (l:DeploymentLog {agent_behavior_tree_id:$id})
			RETURN l
			ORDER BY l.initiated_at_ms DESC
		`, map[string]any{"id": agentBehaviorTreeID})
		if err != nil {
			return nil, err
		}
		var logs []BehaviorTreeDeploymentLog
		for res.Next(ctx) {
			node, _ := res.Record().Get("l")
			if lNode, ok := node.(neo4j.Node); ok {
				props := lNode.Props
				logs = append(logs, BehaviorTreeDeploymentLog{
					ID:                  getString(props, "id"),
					AgentBehaviorTreeID: getString(props, "agent_behavior_tree_id"),
					Action:              getString(props, "action"),
					Version:             int(getInt64(props, "version")),
					Status:              getString(props, "status"),
					ErrorMessage:        toNullString(getString(props, "error_message")),
					InitiatedAt:         time.UnixMilli(getInt64(props, "initiated_at_ms")).UTC(),
					CompletedAt:         toNullTimeMillis(getInt64(props, "completed_at_ms")),
				})
			}
		}
		return logs, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]BehaviorTreeDeploymentLog), nil
}

func (r *Repository) DeleteDeploymentLogsForAssignment(assignmentID string) {
	ctx := context.Background()
	_, _ = r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (l:DeploymentLog {agent_behavior_tree_id:$id})
			DETACH DELETE l
		`, map[string]any{"id": assignmentID})
		return nil, err
	})
}

// =============================================================================
// Task Operations
// =============================================================================

func (r *Repository) GetTask(id string) (*Task, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `MATCH (t:Task {id:$id}) RETURN t`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("t")
			if tNode, ok := node.(neo4j.Node); ok {
				props := tNode.Props
				task := Task{
					ID:               getString(props, "id"),
					BehaviorTreeID:   toNullString(getStringWithFallback(props, "behavior_tree_id", "action_graph_id")),
					AgentID:          toNullString(getString(props, "agent_id")),
					Status:           getString(props, "status"),
					CurrentStepID:    toNullString(getString(props, "current_step_id")),
					CurrentStepIndex: int(getInt64(props, "current_step_index")),
					StepResults:      datatypes.JSON([]byte(getString(props, "step_results_json"))),
					RetryCount:       datatypes.JSON([]byte(getString(props, "retry_count_json"))),
					ErrorMessage:     toNullString(getString(props, "error_message")),
					CreatedAt:        time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					StartedAt:        toNullTimeMillis(getInt64(props, "started_at_ms")),
					CompletedAt:      toNullTimeMillis(getInt64(props, "completed_at_ms")),
				}
				return &task, nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*Task), nil
}

func (r *Repository) GetActiveTasks() ([]Task, error) {
	return r.GetTasks("", "running")
}

func (r *Repository) GetTasksByAgent(agentID string) ([]Task, error) {
	return r.GetTasks(agentID, "")
}

func (r *Repository) GetTasks(agentID, status string) ([]Task, error) {
	ctx := context.Background()
	query := "MATCH (t:Task) "
	params := map[string]any{}
	if agentID != "" {
		query += "WHERE t.agent_id = $agent_id "
		params["agent_id"] = agentID
	}
	if status != "" {
		if agentID != "" {
			query += "AND t.status = $status "
		} else {
			query += "WHERE t.status = $status "
		}
		params["status"] = status
	}
	query += "RETURN t ORDER BY t.created_at_ms DESC"

	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		var tasks []Task
		for res.Next(ctx) {
			node, _ := res.Record().Get("t")
			if tNode, ok := node.(neo4j.Node); ok {
				props := tNode.Props
				tasks = append(tasks, Task{
					ID:               getString(props, "id"),
					BehaviorTreeID:   toNullString(getStringWithFallback(props, "behavior_tree_id", "action_graph_id")),
					AgentID:          toNullString(getString(props, "agent_id")),
					Status:           getString(props, "status"),
					CurrentStepID:    toNullString(getString(props, "current_step_id")),
					CurrentStepIndex: int(getInt64(props, "current_step_index")),
					StepResults:      datatypes.JSON([]byte(getString(props, "step_results_json"))),
					RetryCount:       datatypes.JSON([]byte(getString(props, "retry_count_json"))),
					ErrorMessage:     toNullString(getString(props, "error_message")),
					CreatedAt:        time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					StartedAt:        toNullTimeMillis(getInt64(props, "started_at_ms")),
					CompletedAt:      toNullTimeMillis(getInt64(props, "completed_at_ms")),
				})
			}
		}
		return tasks, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]Task), nil
}

func (r *Repository) CreateTask(task *Task) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":                 task.ID,
		"behavior_tree_id":   task.BehaviorTreeID.String,
		"agent_id":           task.AgentID.String,
		"status":             task.Status,
		"current_step_id":    task.CurrentStepID.String,
		"current_step_index": task.CurrentStepIndex,
		"step_results_json":  string(task.StepResults),
		"retry_count_json":   string(task.RetryCount),
		"error_message":      task.ErrorMessage.String,
		"created_at_ms":      timeToMillis(task.CreatedAt),
		"started_at_ms":      timeToMillis(task.StartedAt.Time),
		"completed_at_ms":    timeToMillis(task.CompletedAt.Time),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			CREATE (t:Task {
				id: $id,
				behavior_tree_id: $behavior_tree_id,
				agent_id: $agent_id,
				status: $status,
				current_step_id: $current_step_id,
				current_step_index: $current_step_index,
				step_results_json: $step_results_json,
				retry_count_json: $retry_count_json,
				error_message: $error_message,
				created_at_ms: $created_at_ms,
				started_at_ms: $started_at_ms,
				completed_at_ms: $completed_at_ms
			})
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) UpdateTask(task *Task) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":                 task.ID,
		"status":             task.Status,
		"current_step_id":    task.CurrentStepID.String,
		"current_step_index": task.CurrentStepIndex,
		"step_results_json":  string(task.StepResults),
		"retry_count_json":   string(task.RetryCount),
		"error_message":      task.ErrorMessage.String,
		"started_at_ms":      timeToMillis(task.StartedAt.Time),
		"completed_at_ms":    timeToMillis(task.CompletedAt.Time),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (t:Task {id:$id})
			SET t.status = $status,
			    t.current_step_id = $current_step_id,
			    t.current_step_index = $current_step_index,
			    t.step_results_json = $step_results_json,
			    t.retry_count_json = $retry_count_json,
			    t.error_message = $error_message,
			    t.started_at_ms = $started_at_ms,
			    t.completed_at_ms = $completed_at_ms
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) UpdateTaskStatus(id, status string, stepID string, stepIndex int, errorMsg string) error {
	ctx := context.Background()
	props := map[string]any{
		"id":                 id,
		"status":             status,
		"current_step_id":    stepID,
		"current_step_index": stepIndex,
		"error_message":      errorMsg,
		"now_ms":             time.Now().UTC().UnixMilli(),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (t:Task {id:$id})
			SET t.status = $status,
			    t.current_step_id = $current_step_id,
			    t.current_step_index = $current_step_index,
			    t.error_message = $error_message,
			    t.updated_at_ms = $now_ms
		`, props)
		return nil, err
	})
	return err
}

// UpdateTaskPreconditionStatus updates the precondition waiting status of a task
func (r *Repository) UpdateTaskPreconditionStatus(taskID string, waiting bool, blockingConditions []BlockingConditionInfo) error {
	ctx := context.Background()

	var blockingJSON []byte
	var err error
	if waiting && len(blockingConditions) > 0 {
		blockingJSON, err = json.Marshal(blockingConditions)
		if err != nil {
			return fmt.Errorf("failed to marshal blocking conditions: %w", err)
		}
	}

	var waitingSinceMs int64
	if waiting {
		waitingSinceMs = time.Now().UTC().UnixMilli()
	}

	props := map[string]any{
		"id":                             taskID,
		"waiting_for_precondition_since": waitingSinceMs,
		"blocking_conditions_json":       string(blockingJSON),
		"now_ms":                         time.Now().UTC().UnixMilli(),
	}

	_, err = r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (t:Task {id:$id})
			SET t.waiting_for_precondition_since = CASE WHEN $waiting_for_precondition_since > 0 THEN $waiting_for_precondition_since ELSE null END,
			    t.blocking_conditions_json = CASE WHEN $blocking_conditions_json <> '' THEN $blocking_conditions_json ELSE null END,
			    t.updated_at_ms = $now_ms
		`, props)
		return nil, err
	})
	return err
}

// =============================================================================
// Command Queue
// =============================================================================

func (r *Repository) CreateCommand(cmd *CommandQueue) error {
	if cmd == nil {
		return fmt.Errorf("command is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":              cmd.ID,
		"agent_id":        cmd.AgentID.String,
		"command_type":    cmd.CommandType,
		"payload_json":    string(cmd.Payload),
		"status":          cmd.Status,
		"result_json":     string(cmd.Result),
		"created_at_ms":   timeToMillis(cmd.CreatedAt),
		"processed_at_ms": timeToMillis(cmd.ProcessedAt.Time),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			CREATE (c:CommandQueue {
				id: $id,
				agent_id: $agent_id,
				command_type: $command_type,
				payload_json: $payload_json,
				status: $status,
				result_json: $result_json,
				created_at_ms: $created_at_ms,
				processed_at_ms: $processed_at_ms
			})
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) GetPendingCommands(agentID string) ([]CommandQueue, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (c:CommandQueue {agent_id:$agent_id, status:'pending'})
			RETURN c
			ORDER BY c.created_at_ms ASC
		`, map[string]any{"agent_id": agentID})
		if err != nil {
			return nil, err
		}
		var cmds []CommandQueue
		for res.Next(ctx) {
			node, _ := res.Record().Get("c")
			if cNode, ok := node.(neo4j.Node); ok {
				props := cNode.Props
				cmds = append(cmds, CommandQueue{
					ID:          getString(props, "id"),
					AgentID:     toNullString(getString(props, "agent_id")),
					CommandType: getString(props, "command_type"),
					Payload:     datatypes.JSON([]byte(getString(props, "payload_json"))),
					Status:      getString(props, "status"),
					Result:      datatypes.JSON([]byte(getString(props, "result_json"))),
					CreatedAt:   time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					ProcessedAt: toNullTimeMillis(getInt64(props, "processed_at_ms")),
				})
			}
		}
		return cmds, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]CommandQueue), nil
}

func (r *Repository) UpdateCommandStatus(id, status string, result map[string]interface{}) error {
	ctx := context.Background()
	resultJSON, err := jsonString(result)
	if err != nil {
		return fmt.Errorf("failed to marshal command result: %w", err)
	}
	props := map[string]any{
		"id":              id,
		"status":          status,
		"result_json":     resultJSON,
		"processed_at_ms": time.Now().UTC().UnixMilli(),
	}
	_, err = r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (c:CommandQueue {id:$id})
			SET c.status = $status,
			    c.result_json = $result_json,
			    c.processed_at_ms = $processed_at_ms
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) GetCommand(id string) (*CommandQueue, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `MATCH (c:CommandQueue {id:$id}) RETURN c`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("c")
			if cNode, ok := node.(neo4j.Node); ok {
				props := cNode.Props
				cmd := CommandQueue{
					ID:          getString(props, "id"),
					AgentID:     toNullString(getString(props, "agent_id")),
					CommandType: getString(props, "command_type"),
					Payload:     datatypes.JSON([]byte(getString(props, "payload_json"))),
					Status:      getString(props, "status"),
					Result:      datatypes.JSON([]byte(getString(props, "result_json"))),
					CreatedAt:   time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					ProcessedAt: toNullTimeMillis(getInt64(props, "processed_at_ms")),
				}
				return &cmd, nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*CommandQueue), nil
}

// =============================================================================
// Waypoints
// =============================================================================

func (r *Repository) GetWaypoint(id string) (*Waypoint, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `MATCH (w:Waypoint {id:$id}) RETURN w`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("w")
			if wNode, ok := node.(neo4j.Node); ok {
				props := wNode.Props
				wp := Waypoint{
					ID:           getString(props, "id"),
					Name:         getString(props, "name"),
					WaypointType: getString(props, "waypoint_type"),
					Data:         datatypes.JSON([]byte(getString(props, "data_json"))),
					CreatedBy:    toNullString(getString(props, "created_by")),
					Description:  toNullString(getString(props, "description")),
					Tags:         datatypes.JSON([]byte(getString(props, "tags_json"))),
					CreatedAt:    time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:    time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
				}
				return &wp, nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*Waypoint), nil
}

func (r *Repository) GetAllWaypoints() ([]Waypoint, error) {
	return r.GetWaypoints("")
}

func (r *Repository) GetWaypoints(waypointType string) ([]Waypoint, error) {
	ctx := context.Background()
	query := "MATCH (w:Waypoint) "
	params := map[string]any{}
	if waypointType != "" {
		query += "WHERE w.waypoint_type = $type "
		params["type"] = waypointType
	}
	query += "RETURN w ORDER BY w.created_at_ms DESC"
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		var waypoints []Waypoint
		for res.Next(ctx) {
			node, _ := res.Record().Get("w")
			if wNode, ok := node.(neo4j.Node); ok {
				props := wNode.Props
				waypoints = append(waypoints, Waypoint{
					ID:           getString(props, "id"),
					Name:         getString(props, "name"),
					WaypointType: getString(props, "waypoint_type"),
					Data:         datatypes.JSON([]byte(getString(props, "data_json"))),
					CreatedBy:    toNullString(getString(props, "created_by")),
					Description:  toNullString(getString(props, "description")),
					Tags:         datatypes.JSON([]byte(getString(props, "tags_json"))),
					CreatedAt:    time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:    time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
				})
			}
		}
		return waypoints, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]Waypoint), nil
}

func (r *Repository) CreateWaypoint(wp *Waypoint) error {
	if wp == nil {
		return fmt.Errorf("waypoint is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":            wp.ID,
		"name":          wp.Name,
		"waypoint_type": wp.WaypointType,
		"data_json":     string(wp.Data),
		"created_by":    wp.CreatedBy.String,
		"description":   wp.Description.String,
		"tags_json":     string(wp.Tags),
		"created_at_ms": timeToMillis(wp.CreatedAt),
		"updated_at_ms": timeToMillis(wp.UpdatedAt),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			CREATE (w:Waypoint {
				id: $id,
				name: $name,
				waypoint_type: $waypoint_type,
				data_json: $data_json,
				created_by: $created_by,
				description: $description,
				tags_json: $tags_json,
				created_at_ms: $created_at_ms,
				updated_at_ms: $updated_at_ms
			})
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) UpdateWaypoint(wp *Waypoint) error {
	if wp == nil {
		return fmt.Errorf("waypoint is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":            wp.ID,
		"name":          wp.Name,
		"data_json":     string(wp.Data),
		"description":   wp.Description.String,
		"tags_json":     string(wp.Tags),
		"updated_at_ms": time.Now().UTC().UnixMilli(),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (w:Waypoint {id:$id})
			SET w.name = $name,
			    w.data_json = $data_json,
			    w.description = $description,
			    w.tags_json = $tags_json,
			    w.updated_at_ms = $updated_at_ms
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) DeleteWaypoint(id string) error {
	ctx := context.Background()
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `MATCH (w:Waypoint {id:$id}) DETACH DELETE w`, map[string]any{"id": id})
		return nil, err
	})
	return err
}

// =============================================================================
// State Definitions
// =============================================================================

func (r *Repository) GetStateDefinition(id string) (*StateDefinition, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `MATCH (d:StateDefinition {id:$id}) RETURN d`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("d")
			if dNode, ok := node.(neo4j.Node); ok {
				def := decodeStateDefinition(dNode)
				return &def, nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*StateDefinition), nil
}

func (r *Repository) GetStateDefinitions() ([]StateDefinition, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `MATCH (d:StateDefinition) RETURN d ORDER BY d.created_at_ms DESC`, nil)
		if err != nil {
			return nil, err
		}
		var defs []StateDefinition
		for res.Next(ctx) {
			node, _ := res.Record().Get("d")
			if dNode, ok := node.(neo4j.Node); ok {
				defs = append(defs, decodeStateDefinition(dNode))
			}
		}
		return defs, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]StateDefinition), nil
}

func (r *Repository) CreateStateDefinition(def *StateDefinition) error {
	if def == nil {
		return fmt.Errorf("state definition is nil")
	}
	ctx := context.Background()
	statesJSON, err := jsonString(def.States)
	if err != nil {
		return fmt.Errorf("failed to marshal states: %w", err)
	}
	actionMappingsJSON, err := jsonString(def.ActionMappings)
	if err != nil {
		return fmt.Errorf("failed to marshal action mappings: %w", err)
	}
	teachableWaypointsJSON, err := jsonString(def.TeachableWaypoints)
	if err != nil {
		return fmt.Errorf("failed to marshal teachable waypoints: %w", err)
	}
	props := map[string]any{
		"id":                       def.ID,
		"name":                     def.Name,
		"description":              def.Description.String,
		"default_state":            def.DefaultState,
		"states_json":              statesJSON,
		"action_mappings_json":     actionMappingsJSON,
		"teachable_waypoints_json": teachableWaypointsJSON,
		"version":                  def.Version,
		"created_at_ms":            timeToMillis(def.CreatedAt),
		"updated_at_ms":            timeToMillis(def.UpdatedAt),
	}
	_, err = r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			CREATE (d:StateDefinition {
				id: $id,
				name: $name,
				description: $description,
				default_state: $default_state,
				states_json: $states_json,
				action_mappings_json: $action_mappings_json,
				teachable_waypoints_json: $teachable_waypoints_json,
				version: $version,
				created_at_ms: $created_at_ms,
				updated_at_ms: $updated_at_ms
			})
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) UpdateStateDefinition(def *StateDefinition) error {
	if def == nil {
		return fmt.Errorf("state definition is nil")
	}
	ctx := context.Background()
	statesJSON, err := jsonString(def.States)
	if err != nil {
		return fmt.Errorf("failed to marshal states: %w", err)
	}
	actionMappingsJSON, err := jsonString(def.ActionMappings)
	if err != nil {
		return fmt.Errorf("failed to marshal action mappings: %w", err)
	}
	teachableWaypointsJSON, err := jsonString(def.TeachableWaypoints)
	if err != nil {
		return fmt.Errorf("failed to marshal teachable waypoints: %w", err)
	}
	props := map[string]any{
		"id":                       def.ID,
		"name":                     def.Name,
		"description":              def.Description.String,
		"default_state":            def.DefaultState,
		"states_json":              statesJSON,
		"action_mappings_json":     actionMappingsJSON,
		"teachable_waypoints_json": teachableWaypointsJSON,
		"version":                  def.Version,
		"updated_at_ms":            timeToMillis(def.UpdatedAt),
	}
	_, err = r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (d:StateDefinition {id:$id})
			SET d.name = $name,
			    d.description = $description,
			    d.default_state = $default_state,
			    d.states_json = $states_json,
			    d.action_mappings_json = $action_mappings_json,
			    d.teachable_waypoints_json = $teachable_waypoints_json,
			    d.version = $version,
			    d.updated_at_ms = $updated_at_ms
		`, props)
		return nil, err
	})
	return err
}

func (r *Repository) DeleteStateDefinition(id string) error {
	ctx := context.Background()
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `MATCH (d:StateDefinition {id:$id}) DETACH DELETE d`, map[string]any{"id": id})
		return nil, err
	})
	return err
}

// =============================================================================
// Templates
// =============================================================================

func (r *Repository) GetTemplates() ([]BehaviorTree, error) {
	graphs, err := r.GetBehaviorTrees("", true)
	if err != nil {
		return nil, err
	}

	templates := make([]BehaviorTree, 0, len(graphs))
	for _, g := range graphs {
		if g.IsTemplate {
			templates = append(templates, g)
		}
	}
	return templates, nil
}

func (r *Repository) GetTemplate(id string) (*BehaviorTree, error) {
	graph, err := r.GetBehaviorTree(id)
	if err != nil || graph == nil {
		return graph, err
	}
	// Backward compatibility: treat graphs without agent ownership as templates.
	if !graph.IsTemplate && graph.AgentID.Valid {
		return nil, nil
	}
	return graph, nil
}

func (r *Repository) CountTemplateAssignments(templateID string) int {
	aags, err := r.GetAgentBehaviorTreesByGraphID(templateID)
	if err != nil {
		return 0
	}
	return len(aags)
}

func (r *Repository) MarkTemplateAssignmentsOutdated(templateID string, newVersion int) {
	ctx := context.Background()
	_, _ = r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		// Update server_version for all assignments
		// Only mark as "outdated" if it was previously "deployed" (deployed_version > 0)
		// Keep "pending" status for assignments that were never deployed
		_, err := tx.Run(ctx, `
			MATCH (aag:AgentActionGraph {behavior_tree_id:$id})
			SET aag.server_version = $newVersion,
			    aag.deployment_status = CASE
			        WHEN aag.deployed_version > 0 THEN "outdated"
			        ELSE aag.deployment_status
			    END
		`, map[string]any{"id": templateID, "newVersion": newVersion})
		return nil, err
	})
}

func (r *Repository) DeleteTemplateAssignments(templateID string) {
	_ = r.DeleteAgentBehaviorTree("", templateID)
}

// =============================================================================
// Capability Operations (Agent-based)
// =============================================================================

func (r *Repository) SyncAgentCapabilities(agentID string, capabilities []AgentCapability) error {
	ctx := context.Background()

	// First, get existing capabilities to preserve user-editable metadata
	existingCaps, _ := r.GetAgentCapabilities(agentID)
	existingMetadata := make(map[string]AgentCapability)
	for _, cap := range existingCaps {
		existingMetadata[cap.ID] = cap
	}

	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		// Mark existing capabilities as deleted (soft delete for sync tracking)
		_, err := tx.Run(ctx, `
			MATCH (c:AgentCapability {agent_id:$agent_id})
			WHERE c.deleted_at_ms IS NULL OR c.deleted_at_ms = 0
			SET c.deleted_at_ms = $deleted_at_ms
		`, map[string]any{"agent_id": agentID, "deleted_at_ms": time.Now().UnixMilli()})
		if err != nil {
			return nil, err
		}

		for _, cap := range capabilities {
			// Preserve user-editable metadata from existing capability
			description := ""
			category := ""
			defaultTimeout := 30.0
			schemaVersion := 1
			if existing, ok := existingMetadata[cap.ID]; ok {
				if existing.Description.Valid {
					description = existing.Description.String
				}
				if existing.Category.Valid {
					category = existing.Category.String
				}
				if existing.DefaultTimeout > 0 {
					defaultTimeout = existing.DefaultTimeout
				}
				if existing.SchemaVersion > 0 {
					schemaVersion = existing.SchemaVersion
				}
			}

			// Upsert: merge existing or create new
			_, err := tx.Run(ctx, `
				MERGE (c:AgentCapability {id: $id})
				SET c.agent_id = $agent_id,
					c.capability_kind = $capability_kind,
					c.action_type = $action_type,
					c.action_server = $action_server,
					c.node_name = $node_name,
					c.is_lifecycle_node = $is_lifecycle_node,
					c.lifecycle_state = $lifecycle_state,
					c.goal_schema_json = $goal_schema_json,
					c.result_schema_json = $result_schema_json,
					c.feedback_schema_json = $feedback_schema_json,
					c.success_criteria_json = $success_criteria_json,
					c.description = $description,
					c.category = $category,
					c.default_timeout = $default_timeout,
					c.schema_version = $schema_version,
					c.status = $status,
					c.is_available = $is_available,
					c.last_used_at_ms = $last_used_at_ms,
					c.discovered_at_ms = $discovered_at_ms,
					c.updated_at_ms = $updated_at_ms,
					c.deleted_at_ms = 0
			`, map[string]any{
				"id":                    cap.ID,
				"agent_id":              agentID,
				"capability_kind":       cap.CapabilityKind,
				"action_type":           cap.ActionType,
				"action_server":         cap.ActionServer,
				"node_name":             cap.NodeName,
				"is_lifecycle_node":     cap.IsLifecycleNode,
				"lifecycle_state":       cap.LifecycleState,
				"goal_schema_json":      string(cap.GoalSchema),
				"result_schema_json":    string(cap.ResultSchema),
				"feedback_schema_json":  string(cap.FeedbackSchema),
				"success_criteria_json": string(cap.SuccessCriteria),
				"description":           description,
				"category":              category,
				"default_timeout":       defaultTimeout,
				"schema_version":        schemaVersion,
				"status":                cap.Status,
				"is_available":          cap.IsAvailable,
				"last_used_at_ms":       timeToMillis(cap.LastUsedAt.Time),
				"discovered_at_ms":      timeToMillis(cap.DiscoveredAt),
				"updated_at_ms":         timeToMillis(cap.UpdatedAt),
			})
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return err
}

func (r *Repository) GetAgentCapabilities(agentID string) ([]AgentCapability, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (c:AgentCapability {agent_id:$agent_id})
			WHERE c.deleted_at_ms IS NULL OR c.deleted_at_ms = 0
			RETURN c
		`, map[string]any{"agent_id": agentID})
		if err != nil {
			return nil, err
		}
		var caps []AgentCapability
		for res.Next(ctx) {
			node, _ := res.Record().Get("c")
			if cNode, ok := node.(neo4j.Node); ok {
				caps = append(caps, capabilityFromNeo4jNode(cNode.Props))
			}
		}
		return caps, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]AgentCapability), nil
}

// capabilityFromNeo4jNode converts Neo4j node properties to AgentCapability
func capabilityFromNeo4jNode(props map[string]any) AgentCapability {
	cap := AgentCapability{
		ID:              getString(props, "id"),
		AgentID:         getString(props, "agent_id"),
		CapabilityKind:  getString(props, "capability_kind"),
		ActionType:      getString(props, "action_type"),
		ActionServer:    getString(props, "action_server"),
		NodeName:        getString(props, "node_name"),
		IsLifecycleNode: getBool(props, "is_lifecycle_node"),
		GoalSchema:      datatypes.JSON([]byte(getString(props, "goal_schema_json"))),
		ResultSchema:    datatypes.JSON([]byte(getString(props, "result_schema_json"))),
		FeedbackSchema:  datatypes.JSON([]byte(getString(props, "feedback_schema_json"))),
		SuccessCriteria: datatypes.JSON([]byte(getString(props, "success_criteria_json"))),
		DefaultTimeout:  getFloat64(props, "default_timeout"),
		SchemaVersion:   int(getInt64(props, "schema_version")),
		Status:          getString(props, "status"),
		IsAvailable:     getBool(props, "is_available"),
		LifecycleState:  getString(props, "lifecycle_state"),
		LastUsedAt:      toNullTimeMillis(getInt64(props, "last_used_at_ms")),
		DiscoveredAt:    time.UnixMilli(getInt64(props, "discovered_at_ms")).UTC(),
		UpdatedAt:       time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
		DeletedAt:       toNullTimeMillis(getInt64(props, "deleted_at_ms")),
	}
	if desc := getString(props, "description"); desc != "" {
		cap.Description = toNullString(desc)
	}
	if cat := getString(props, "category"); cat != "" {
		cap.Category = toNullString(cat)
	}
	if cap.DefaultTimeout == 0 {
		cap.DefaultTimeout = 30.0
	}
	if cap.SchemaVersion == 0 {
		cap.SchemaVersion = 1
	}
	if strings.TrimSpace(cap.LifecycleState) == "" {
		cap.LifecycleState = "unknown"
	}
	cap.CapabilityKind = normalizeCapabilityKind(cap.CapabilityKind, cap.ActionType)
	return cap
}

func normalizeCapabilityKind(kind, actionType string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "service":
		return "service"
	case "action":
		return "action"
	default:
		if strings.Contains(strings.ToLower(actionType), "/srv/") {
			return "service"
		}
		return "action"
	}
}

func (r *Repository) GetAllAgentCapabilities() ([]AgentCapability, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (c:AgentCapability)
			WHERE c.deleted_at_ms IS NULL OR c.deleted_at_ms = 0
			RETURN c
		`, nil)
		if err != nil {
			return nil, err
		}
		var caps []AgentCapability
		for res.Next(ctx) {
			node, _ := res.Record().Get("c")
			if cNode, ok := node.(neo4j.Node); ok {
				caps = append(caps, capabilityFromNeo4jNode(cNode.Props))
			}
		}
		return caps, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]AgentCapability), nil
}

// UpdateAgentCapabilityStatus updates the status, availability, and lifecycle state of an agent capability
func (r *Repository) UpdateAgentCapabilityStatus(agentID, actionType, status string, available bool, lifecycleState string) error {
	ctx := context.Background()
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		// Build dynamic SET clause based on provided values
		query := `
			MATCH (c:AgentCapability {agent_id: $agent_id, action_type: $action_type})
			SET c.status = $status, c.is_available = $available, c.updated_at_ms = $updated_at_ms`

		params := map[string]any{
			"agent_id":      agentID,
			"action_type":   actionType,
			"status":        status,
			"available":     available,
			"updated_at_ms": time.Now().UnixMilli(),
		}

		// Add lifecycle_state if provided
		if lifecycleState != "" {
			query += `, c.lifecycle_state = $lifecycle_state`
			params["lifecycle_state"] = lifecycleState
		}

		_, err := tx.Run(ctx, query, params)
		return nil, err
	})
	return err
}

// GetCapabilityByID retrieves a single capability by its ID
func (r *Repository) GetCapabilityByID(id string) (*AgentCapability, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (c:AgentCapability {id: $id})
			RETURN c
		`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("c")
			if cNode, ok := node.(neo4j.Node); ok {
				cap := capabilityFromNeo4jNode(cNode.Props)
				return &cap, nil
			}
		}
		return nil, res.Err()
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*AgentCapability), nil
}

// CapabilityMetadataUpdate contains fields that can be updated via API
type CapabilityMetadataUpdate struct {
	Description    *string  `json:"description,omitempty"`
	Category       *string  `json:"category,omitempty"`
	DefaultTimeout *float64 `json:"default_timeout,omitempty"`
	SchemaVersion  *int     `json:"schema_version,omitempty"`
}

// UpdateCapabilityMetadata updates user-editable metadata for a capability
func (r *Repository) UpdateCapabilityMetadata(id string, update CapabilityMetadataUpdate) error {
	ctx := context.Background()
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		// Build dynamic SET clause
		setClauses := []string{"c.updated_at_ms = $updated_at_ms"}
		params := map[string]any{
			"id":            id,
			"updated_at_ms": time.Now().UnixMilli(),
		}

		if update.Description != nil {
			setClauses = append(setClauses, "c.description = $description")
			params["description"] = *update.Description
		}
		if update.Category != nil {
			setClauses = append(setClauses, "c.category = $category")
			params["category"] = *update.Category
		}
		if update.DefaultTimeout != nil {
			setClauses = append(setClauses, "c.default_timeout = $default_timeout")
			params["default_timeout"] = *update.DefaultTimeout
		}
		if update.SchemaVersion != nil {
			setClauses = append(setClauses, "c.schema_version = $schema_version")
			params["schema_version"] = *update.SchemaVersion
		}

		query := fmt.Sprintf(`
			MATCH (c:AgentCapability {id: $id})
			SET %s
			RETURN c
		`, strings.Join(setClauses, ", "))

		res, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		if !res.Next(ctx) {
			return nil, fmt.Errorf("capability not found: %s", id)
		}
		return nil, res.Err()
	})
	return err
}

// GetCapabilitiesChangedSince returns capabilities changed since the given timestamp
// Includes both updated and soft-deleted capabilities for incremental sync
func (r *Repository) GetCapabilitiesChangedSince(since time.Time) ([]AgentCapability, error) {
	ctx := context.Background()
	sinceMs := since.UnixMilli()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (c:AgentCapability)
			WHERE c.updated_at_ms > $since_ms
			   OR (c.deleted_at_ms IS NOT NULL AND c.deleted_at_ms > 0 AND c.deleted_at_ms > $since_ms)
			RETURN c
			ORDER BY c.updated_at_ms ASC
		`, map[string]any{"since_ms": sinceMs})
		if err != nil {
			return nil, err
		}
		var caps []AgentCapability
		for res.Next(ctx) {
			node, _ := res.Record().Get("c")
			if cNode, ok := node.(neo4j.Node); ok {
				caps = append(caps, capabilityFromNeo4jNode(cNode.Props))
			}
		}
		return caps, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]AgentCapability), nil
}

// GetCapabilitiesByCategory returns all capabilities in a specific category
func (r *Repository) GetCapabilitiesByCategory(category string) ([]AgentCapability, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (c:AgentCapability {category: $category})
			WHERE c.deleted_at_ms IS NULL OR c.deleted_at_ms = 0
			RETURN c
		`, map[string]any{"category": category})
		if err != nil {
			return nil, err
		}
		var caps []AgentCapability
		for res.Next(ctx) {
			node, _ := res.Record().Get("c")
			if cNode, ok := node.(neo4j.Node); ok {
				caps = append(caps, capabilityFromNeo4jNode(cNode.Props))
			}
		}
		return caps, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]AgentCapability), nil
}

func (r *Repository) GetAgentActionTypes(agentID string) ([]string, error) {
	caps, err := r.GetAgentCapabilities(agentID)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, c := range caps {
		set[c.ActionType] = true
	}
	var types []string
	for t := range set {
		types = append(types, t)
	}
	return types, nil
}

// FindCompatibleAgents returns agents that have all required action types
// Optimized: Uses batch queries instead of N+1 pattern
func (r *Repository) FindCompatibleAgents(requiredActionTypes []string) ([]Agent, error) {
	agents, err := r.GetAllAgents()
	if err != nil {
		return nil, err
	}

	// Get all capabilities in one query
	allCaps, err := r.GetAllAgentCapabilities()
	if err != nil {
		return nil, err
	}

	// Build action types set per agent
	actionTypesByAgent := make(map[string]map[string]bool)
	for _, cap := range allCaps {
		if actionTypesByAgent[cap.AgentID] == nil {
			actionTypesByAgent[cap.AgentID] = make(map[string]bool)
		}
		actionTypesByAgent[cap.AgentID][cap.ActionType] = true
	}

	var result []Agent
	for _, agent := range agents {
		available := actionTypesByAgent[agent.ID]
		if available == nil {
			available = make(map[string]bool)
		}
		ok := true
		for _, req := range requiredActionTypes {
			if !available[req] {
				ok = false
				break
			}
		}
		if ok {
			result = append(result, agent)
		}
	}
	return result, nil
}

// FindAgentsWithCompatibility returns all agents with compatibility info for required action types
// Optimized: Uses batch queries instead of N+1 pattern
func (r *Repository) FindAgentsWithCompatibility(requiredActionTypes []string) ([]CompatibleAgentInfo, error) {
	agents, err := r.GetAllAgents()
	if err != nil {
		return nil, err
	}

	// Get all capabilities in one query
	allCaps, err := r.GetAllAgentCapabilities()
	if err != nil {
		return nil, err
	}

	// Build action types set per agent
	actionTypesByAgent := make(map[string]map[string]bool)
	for _, cap := range allCaps {
		if actionTypesByAgent[cap.AgentID] == nil {
			actionTypesByAgent[cap.AgentID] = make(map[string]bool)
		}
		actionTypesByAgent[cap.AgentID][cap.ActionType] = true
	}

	var result []CompatibleAgentInfo
	for _, agent := range agents {
		typeSet := actionTypesByAgent[agent.ID]
		if typeSet == nil {
			typeSet = make(map[string]bool)
		}
		var missing []string
		for _, req := range requiredActionTypes {
			if !typeSet[req] {
				missing = append(missing, req)
			}
		}
		result = append(result, CompatibleAgentInfo{
			Agent:               agent,
			MissingCapabilities: missing,
			HasAllCapabilities:  len(missing) == 0,
		})
	}
	return result, nil
}

func (r *Repository) GetAllActionTypesWithAgentCount() ([]ActionTypeWithCount, error) {
	caps, err := r.GetAllAgentCapabilities()
	if err != nil {
		return nil, err
	}
	counts := map[string]map[string]bool{}
	for _, c := range caps {
		if strings.EqualFold(c.CapabilityKind, "service") {
			continue
		}
		if counts[c.ActionType] == nil {
			counts[c.ActionType] = map[string]bool{}
		}
		counts[c.ActionType][c.AgentID] = true
	}
	var result []ActionTypeWithCount
	for actionType, agents := range counts {
		result = append(result, ActionTypeWithCount{
			ActionType: actionType,
			AgentCount: len(agents),
		})
	}
	return result, nil
}

func (r *Repository) FindTemplatesCompatibleWithAgent(agentID string) ([]TemplateCompatibilityInfo, error) {
	templates, err := r.GetTemplates()
	if err != nil {
		return nil, err
	}
	agentTypes, _ := r.GetAgentActionTypes(agentID)
	agentSet := map[string]bool{}
	for _, t := range agentTypes {
		agentSet[t] = true
	}
	assigned := r.GetAssignedTemplateIDs(agentID)
	var result []TemplateCompatibilityInfo
	for _, tpl := range templates {
		var steps []BehaviorTreeStep
		_ = json.Unmarshal(tpl.Steps, &steps)
		required := ExtractActionTypesFromSteps(steps)
		var missing []string
		for _, req := range required {
			if !agentSet[req] {
				missing = append(missing, req)
			}
		}
		result = append(result, TemplateCompatibilityInfo{
			Template:            tpl,
			RequiredActionTypes: required,
			MissingCapabilities: missing,
			IsFullyCompatible:   len(missing) == 0,
			AlreadyAssigned:     assigned[tpl.ID],
		})
	}
	return result, nil
}

// =============================================================================
// Maintenance
// =============================================================================

// CleanupReport summarizes retention cleanup results.
type CleanupReport struct {
	DeploymentLogs int
	Tasks          int
	Commands       int
}

// Total returns the total number of deleted nodes.
func (r CleanupReport) Total() int {
	return r.DeploymentLogs + r.Tasks + r.Commands
}

func (r *Repository) deleteInBatches(ctx context.Context, query string, params map[string]any, batchSize int) (int, error) {
	total := 0
	for {
		batchParams := make(map[string]any, len(params)+1)
		for key, value := range params {
			batchParams[key] = value
		}
		batchParams["limit"] = batchSize

		result, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
			res, err := tx.Run(ctx, query, batchParams)
			if err != nil {
				return 0, err
			}
			if res.Next(ctx) {
				val, _ := res.Record().Get("deleted")
				return toInt(val), res.Err()
			}
			if err := res.Err(); err != nil {
				return 0, err
			}
			return 0, nil
		})
		if err != nil {
			return total, err
		}
		deleted := result.(int)
		total += deleted
		if deleted == 0 {
			break
		}
	}
	return total, nil
}

// CleanupOldData deletes logs and completed executions older than retention.
func (r *Repository) CleanupOldData(retention time.Duration) (CleanupReport, error) {
	report := CleanupReport{}
	if retention <= 0 {
		return report, nil
	}
	cutoff := time.Now().UTC().Add(-retention).UnixMilli()
	ctx := context.Background()

	const batchSize = 1000

	deploymentLogQuery := `
		MATCH (l:DeploymentLog)
		WHERE (CASE
			WHEN l.completed_at_ms IS NULL OR l.completed_at_ms = 0 THEN l.initiated_at_ms
			ELSE l.completed_at_ms
		END) < $cutoff
		WITH l LIMIT $limit
		WITH collect(l) AS nodes
		FOREACH (n IN nodes | DETACH DELETE n)
		RETURN size(nodes) AS deleted
	`
	taskQuery := `
		MATCH (t:Task)
		WHERE t.status IN $statuses
		  AND t.completed_at_ms > 0
		  AND t.completed_at_ms < $cutoff
		WITH t LIMIT $limit
		WITH collect(t) AS nodes
		FOREACH (n IN nodes | DETACH DELETE n)
		RETURN size(nodes) AS deleted
	`
	commandQuery := `
		MATCH (c:CommandQueue)
		WHERE c.status <> 'pending'
		  AND c.processed_at_ms > 0
		  AND c.processed_at_ms < $cutoff
		WITH c LIMIT $limit
		WITH collect(c) AS nodes
		FOREACH (n IN nodes | DETACH DELETE n)
		RETURN size(nodes) AS deleted
	`

	var err error
	report.DeploymentLogs, err = r.deleteInBatches(ctx, deploymentLogQuery, map[string]any{"cutoff": cutoff}, batchSize)
	if err != nil {
		return report, err
	}
	report.Tasks, err = r.deleteInBatches(ctx, taskQuery, map[string]any{
		"cutoff":   cutoff,
		"statuses": []string{"completed", "failed", "cancelled"},
	}, batchSize)
	if err != nil {
		return report, err
	}
	report.Commands, err = r.deleteInBatches(ctx, commandQuery, map[string]any{"cutoff": cutoff}, batchSize)
	if err != nil {
		return report, err
	}

	return report, nil
}

// =============================================================================
// Templates / Assignments Helpers
// =============================================================================

func (r *Repository) CountTemplates() (int, error) {
	templates, err := r.GetTemplates()
	if err != nil {
		return 0, err
	}
	return len(templates), nil
}

// =============================================================================
// Batch Query Methods (for N+1 query optimization)
// =============================================================================

// GetAllAgentBehaviorTrees returns all agent behavior tree assignments in a single query
func (r *Repository) GetAllAgentBehaviorTrees() ([]AgentBehaviorTree, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `MATCH (aag:AgentActionGraph) RETURN aag`, nil)
		if err != nil {
			return nil, err
		}
		var list []AgentBehaviorTree
		for res.Next(ctx) {
			node, _ := res.Record().Get("aag")
			if aagNode, ok := node.(neo4j.Node); ok {
				props := aagNode.Props
				list = append(list, AgentBehaviorTree{
					ID:               getString(props, "id"),
					AgentID:          getString(props, "agent_id"),
					BehaviorTreeID:   getStringWithFallback(props, "behavior_tree_id", "action_graph_id"),
					ServerVersion:    int(getInt64(props, "server_version")),
					DeployedVersion:  int(getInt64(props, "deployed_version")),
					DeploymentStatus: getString(props, "deployment_status"),
					DeploymentError:  toNullString(getString(props, "deployment_error")),
					DeployedAt:       toNullTimeMillis(getInt64(props, "deployed_at_ms")),
					Enabled:          getBool(props, "enabled"),
					Priority:         int(getInt64(props, "priority")),
					CreatedAt:        time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:        time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
				})
			}
		}
		return list, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.([]AgentBehaviorTree), nil
}

// GetBehaviorTreesByIDs retrieves multiple behavior trees by their IDs in a single query
func (r *Repository) GetBehaviorTreesByIDs(ids []string) (map[string]*BehaviorTree, error) {
	if len(ids) == 0 {
		return make(map[string]*BehaviorTree), nil
	}
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (g:ActionGraph)
			WHERE g.id IN $ids
			RETURN g
		`, map[string]any{"ids": ids})
		if err != nil {
			return nil, err
		}
		graphs := make(map[string]*BehaviorTree)
		for res.Next(ctx) {
			node, _ := res.Record().Get("g")
			if gNode, ok := node.(neo4j.Node); ok {
				props := gNode.Props
				stepsJSON := getString(props, "steps_json")
				preconditionsJSON := getString(props, "preconditions_json")
				statesJSON := getString(props, "states_json")
				entryPoint := getString(props, "entry_point")
				bt := BehaviorTree{
					ID:                 getString(props, "id"),
					Name:               getString(props, "name"),
					Description:        toNullString(getString(props, "description")),
					AgentID:            toNullString(getString(props, "agent_id")),
					Version:            int(getInt64(props, "version")),
					IsTemplate:         isTemplateFromProps(props),
					TemplateCategory:   toNullString(getString(props, "template_category")),
					AutoGenerateStates: getBool(props, "auto_generate_states"),
					CreatedAt:          time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
					UpdatedAt:          time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
				}
				if entryPoint != "" {
					bt.EntryPoint = toNullString(entryPoint)
				}
				if stepsJSON != "" {
					bt.Steps = datatypes.JSON([]byte(stepsJSON))
				}
				if preconditionsJSON != "" {
					bt.Preconditions = datatypes.JSON([]byte(preconditionsJSON))
				}
				if statesJSON != "" {
					bt.States = datatypes.JSON([]byte(statesJSON))
				}
				if planningStatesJSON := getString(props, "planning_states_json"); planningStatesJSON != "" {
					bt.PlanningStates = datatypes.JSON([]byte(planningStatesJSON))
				}
				graphs[bt.ID] = &bt
			}
		}
		return graphs, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string]*BehaviorTree), nil
}

// =============================================================================
// PDDL Planning Problems
// =============================================================================

// CreatePlanningProblem creates a new planning problem node in Neo4j
func (r *Repository) CreatePlanningProblem(pp *PlanningProblem) error {
	if pp == nil {
		return fmt.Errorf("planning problem is nil")
	}
	ctx := context.Background()
	props := map[string]any{
		"id":               pp.ID,
		"name":             pp.Name,
		"behavior_tree_id": pp.BehaviorTreeID,
		"initial_state":    string(pp.InitialState),
		"goal_state":       string(pp.GoalState),
		"agent_ids":        string(pp.AgentIDs),
		"status":           pp.Status,
		"plan_result":      string(pp.PlanResult),
		"error_message":    pp.ErrorMessage.String,
		"created_at_ms":    timeToMillis(pp.CreatedAt),
		"updated_at_ms":    timeToMillis(pp.UpdatedAt),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			CREATE (p:PlanningProblem {
				id: $id,
				name: $name,
				behavior_tree_id: $behavior_tree_id,
				initial_state: $initial_state,
				goal_state: $goal_state,
				agent_ids: $agent_ids,
				status: $status,
				plan_result: $plan_result,
				error_message: $error_message,
				created_at_ms: $created_at_ms,
				updated_at_ms: $updated_at_ms
			})
		`, props)
		return nil, err
	})
	return err
}

// GetPlanningProblem retrieves a planning problem by ID
func (r *Repository) GetPlanningProblem(id string) (*PlanningProblem, error) {
	ctx := context.Background()
	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (p:PlanningProblem {id: $id})
			RETURN p
		`, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			node, _ := res.Record().Get("p")
			if pNode, ok := node.(neo4j.Node); ok {
				return parsePlanningProblemNode(pNode.Props), nil
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*PlanningProblem), nil
}

// ListPlanningProblems lists planning problems, optionally filtered by BT ID
func (r *Repository) ListPlanningProblems(behaviorTreeID string) ([]PlanningProblem, error) {
	ctx := context.Background()
	query := "MATCH (p:PlanningProblem) "
	params := map[string]any{}
	if behaviorTreeID != "" {
		query += "WHERE p.behavior_tree_id = $bt_id "
		params["bt_id"] = behaviorTreeID
	}
	query += "RETURN p ORDER BY p.created_at_ms DESC"

	result, err := r.withSession(ctx, neo4j.AccessModeRead, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		var problems []PlanningProblem
		for res.Next(ctx) {
			node, _ := res.Record().Get("p")
			if pNode, ok := node.(neo4j.Node); ok {
				problems = append(problems, *parsePlanningProblemNode(pNode.Props))
			}
		}
		return problems, res.Err()
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return []PlanningProblem{}, nil
	}
	return result.([]PlanningProblem), nil
}

// UpdatePlanningProblemStatus updates the status and optionally the plan result
func (r *Repository) UpdatePlanningProblemStatus(id, status string, planResult []byte, errMsg string) error {
	ctx := context.Background()
	props := map[string]any{
		"id":            id,
		"status":        status,
		"plan_result":   string(planResult),
		"error_message": errMsg,
		"updated_at_ms": time.Now().UTC().UnixMilli(),
	}
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (p:PlanningProblem {id: $id})
			SET p.status = $status,
			    p.plan_result = $plan_result,
			    p.error_message = $error_message,
			    p.updated_at_ms = $updated_at_ms
		`, props)
		return nil, err
	})
	return err
}

// DeletePlanningProblem deletes a planning problem
func (r *Repository) DeletePlanningProblem(id string) error {
	ctx := context.Background()
	_, err := r.withSession(ctx, neo4j.AccessModeWrite, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (p:PlanningProblem {id: $id})
			DETACH DELETE p
		`, map[string]any{"id": id})
		return nil, err
	})
	return err
}

func parsePlanningProblemNode(props map[string]any) *PlanningProblem {
	pp := &PlanningProblem{
		ID:             getString(props, "id"),
		Name:           getString(props, "name"),
		BehaviorTreeID: getString(props, "behavior_tree_id"),
		Status:         getString(props, "status"),
		ErrorMessage:   toNullString(getString(props, "error_message")),
		CreatedAt:      time.UnixMilli(getInt64(props, "created_at_ms")).UTC(),
		UpdatedAt:      time.UnixMilli(getInt64(props, "updated_at_ms")).UTC(),
	}
	if is := getString(props, "initial_state"); is != "" {
		pp.InitialState = datatypes.JSON([]byte(is))
	}
	if gs := getString(props, "goal_state"); gs != "" {
		pp.GoalState = datatypes.JSON([]byte(gs))
	}
	if ai := getString(props, "agent_ids"); ai != "" {
		pp.AgentIDs = datatypes.JSON([]byte(ai))
	}
	if pr := getString(props, "plan_result"); pr != "" {
		pp.PlanResult = datatypes.JSON([]byte(pr))
	}
	return pp
}

// =============================================================================
// Transactions
// =============================================================================

func (r *Repository) WithTransaction(fn func(*Repository) error) error {
	// Neo4j transaction boundaries are handled per query in this repository.
	// For now, execute the function without wrapping in a shared transaction.
	return fn(r)
}

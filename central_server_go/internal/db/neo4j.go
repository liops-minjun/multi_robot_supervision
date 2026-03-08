package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"central_server_go/internal/config"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Database wraps the Neo4j driver.
type Database struct {
	Driver   neo4j.DriverWithContext
	Database string
}

// New creates a new Neo4j connection.
func New(cfg *config.Neo4jConfig) (*Database, error) {
	if cfg == nil {
		return nil, fmt.Errorf("neo4j config is nil")
	}

	driver, err := neo4j.NewDriverWithContext(
		cfg.URI,
		neo4j.BasicAuth(cfg.Username, cfg.Password, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to neo4j: %w", err)
	}

	db := &Database{
		Driver:   driver,
		Database: cfg.Database,
	}

	log.Printf("Connected to Neo4j at %s (db=%s)", cfg.URI, cfg.Database)
	return db, nil
}

// Close closes the Neo4j driver.
func (d *Database) Close() error {
	if d == nil || d.Driver == nil {
		return nil
	}
	return d.Driver.Close(context.Background())
}

// HealthCheck verifies the database connection.
func (d *Database) HealthCheck() error {
	if d == nil || d.Driver == nil {
		return fmt.Errorf("neo4j driver not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := d.Driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: d.Database,
	})
	defer session.Close(ctx)

	_, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, "RETURN 1", nil)
		return nil, err
	})
	return err
}

// EnsureIndexes creates indexes for better query performance.
// This should be called during application startup.
func (d *Database) EnsureIndexes() error {
	if d == nil || d.Driver == nil {
		return fmt.Errorf("neo4j driver not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session := d.Driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: d.Database,
	})
	defer session.Close(ctx)

	// List of indexes to create (using CREATE INDEX IF NOT EXISTS for idempotency)
	indexes := []string{
		// Agent indexes
		"CREATE INDEX agent_id IF NOT EXISTS FOR (a:Agent) ON (a.id)",
		"CREATE INDEX agent_status IF NOT EXISTS FOR (a:Agent) ON (a.status)",
		"CREATE INDEX agent_hardware_fingerprint IF NOT EXISTS FOR (a:Agent) ON (a.hardware_fingerprint)",

		// AgentCapability indexes for N+1 query optimization
		"CREATE INDEX capability_agent_id IF NOT EXISTS FOR (c:AgentCapability) ON (c.agent_id)",
		"CREATE INDEX capability_action_type IF NOT EXISTS FOR (c:AgentCapability) ON (c.action_type)",
		"CREATE INDEX capability_category IF NOT EXISTS FOR (c:AgentCapability) ON (c.category)",
		"CREATE INDEX capability_updated_at IF NOT EXISTS FOR (c:AgentCapability) ON (c.updated_at_ms)",
		"CREATE INDEX capability_deleted_at IF NOT EXISTS FOR (c:AgentCapability) ON (c.deleted_at_ms)",

		// AgentActionGraph indexes for assignment lookups
		"CREATE INDEX aag_agent_id IF NOT EXISTS FOR (aag:AgentActionGraph) ON (aag.agent_id)",
		"CREATE INDEX aag_action_graph_id IF NOT EXISTS FOR (aag:AgentActionGraph) ON (aag.action_graph_id)",

		// ActionGraph indexes
		"CREATE INDEX action_graph_id IF NOT EXISTS FOR (g:ActionGraph) ON (g.id)",
		"CREATE INDEX action_graph_template IF NOT EXISTS FOR (g:ActionGraph) ON (g.is_template)",
		"CREATE INDEX action_graph_agent_id IF NOT EXISTS FOR (g:ActionGraph) ON (g.agent_id)",

		// Task indexes for task queries
		"CREATE INDEX task_id IF NOT EXISTS FOR (t:Task) ON (t.id)",
		"CREATE INDEX task_agent_id IF NOT EXISTS FOR (t:Task) ON (t.agent_id)",
		"CREATE INDEX task_status IF NOT EXISTS FOR (t:Task) ON (t.status)",

		// Waypoint indexes
		"CREATE INDEX waypoint_id IF NOT EXISTS FOR (w:Waypoint) ON (w.id)",

		// TaskDistributor indexes
		"CREATE INDEX task_distributor_id IF NOT EXISTS FOR (td:TaskDistributor) ON (td.id)",
		"CREATE INDEX task_distributor_state_id IF NOT EXISTS FOR (tds:TaskDistributorState) ON (tds.id)",
		"CREATE INDEX task_distributor_state_td_id IF NOT EXISTS FOR (tds:TaskDistributorState) ON (tds.task_distributor_id)",
		"CREATE INDEX task_distributor_resource_id IF NOT EXISTS FOR (tdr:TaskDistributorResource) ON (tdr.id)",
		"CREATE INDEX task_distributor_resource_td_id IF NOT EXISTS FOR (tdr:TaskDistributorResource) ON (tdr.task_distributor_id)",
	}

	for _, indexQuery := range indexes {
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			_, err := tx.Run(ctx, indexQuery, nil)
			return nil, err
		})
		if err != nil {
			log.Printf("Warning: Failed to create index: %s - %v", indexQuery, err)
			// Continue with other indexes even if one fails
		}
	}

	log.Println("Neo4j indexes ensured")
	return nil
}

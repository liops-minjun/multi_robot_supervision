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

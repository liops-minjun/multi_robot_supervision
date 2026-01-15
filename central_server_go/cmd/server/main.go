package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"central_server_go/internal/api"
	"central_server_go/internal/config"
	"central_server_go/internal/db"
	"central_server_go/internal/executor"
	fleetgrpc "central_server_go/internal/grpc"
	"central_server_go/internal/state"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Fleet Server (Go)...")

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to Neo4j
	database, err := db.New(&cfg.Neo4j)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Health check database
	if err := database.HealthCheck(); err != nil {
		log.Fatalf("Database health check failed: %v", err)
	}
	log.Println("Database connection established")

	// Ensure indexes for better query performance
	if err := database.EnsureIndexes(); err != nil {
		log.Printf("Warning: Failed to ensure indexes: %v", err)
	}

	// Create repository
	repo := db.NewRepository(database)

	// Create global state manager
	stateManager := state.NewGlobalStateManager()
	log.Println("State manager initialized")

	// Load existing agents and robots into state manager
	if err := loadExistingState(repo, stateManager); err != nil {
		log.Printf("Warning: Failed to load existing state: %v", err)
	}

	// Create gRPC server
	grpcServer, err := fleetgrpc.NewServer(cfg, stateManager)
	if err != nil {
		log.Fatalf("Failed to create gRPC server: %v", err)
	}

	// Create fleet control handler
	handler := fleetgrpc.NewFleetControlHandler(stateManager, repo, grpcServer)

	// Definitions path for action metadata
	definitionsPath := cfg.Server.DefinitionsPath
	if definitionsPath == "" {
		definitionsPath = "definitions"
	}

	// Create raw QUIC handler for C++ agents (if TLS enabled)
	var rawQUICHandler *fleetgrpc.RawQUICHandler
	if cfg.TLS.Enabled {
		rawQUICHandler = fleetgrpc.NewRawQUICHandler(stateManager, repo, nil)
	}

	// Create scheduler
	scheduler := executor.NewScheduler(stateManager, repo, handler, rawQUICHandler)
	scheduler.Start()
	defer scheduler.Stop()

	// Create REST API server (with rawQUICHandler for QUIC-based deployments)
	apiServer := api.NewServer(repo, stateManager, scheduler, rawQUICHandler, definitionsPath)

	// Set WebSocket hub on rawQUICHandler after apiServer is created
	if rawQUICHandler != nil {
		rawQUICHandler.SetWebSocketHub(apiServer.GetWebSocketHub())
	}

	// Start HTTP server in background
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.HTTPPort),
		Handler:      apiServer.Router(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("Starting HTTP server on port %d", cfg.Server.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
			cancel()
		}
	}()

	// Start gRPC server in background
	go func() {
		if err := grpcServer.Start(ctx); err != nil {
			log.Printf("gRPC server error: %v", err)
			cancel()
		}
	}()

	// Start raw QUIC handler for C++ agents
	if rawQUICHandler != nil {
		// Load TLS config
		tlsConfig, err := loadTLSConfig(cfg)
		if err != nil {
			log.Printf("Warning: Failed to load TLS config for raw QUIC: %v", err)
		} else {
			go func() {
				addr := fmt.Sprintf(":%d", cfg.Server.RawQUICPort)
				if err := rawQUICHandler.Start(addr, tlsConfig); err != nil {
					log.Printf("Raw QUIC handler error: %v", err)
				}
			}()
		}

		// Start fleet state broadcasting for cross-agent coordination
		go rawQUICHandler.StartFleetStateBroadcast(time.Second)
		log.Println("Fleet state broadcast started (1s interval)")
	}

	// Print server info
	log.Printf("Fleet Server started successfully")
	log.Printf("  HTTP port: %d", cfg.Server.HTTPPort)
	log.Printf("  gRPC port: %d", cfg.Server.GRPCPort)
	if cfg.TLS.Enabled {
		log.Printf("  Raw QUIC port: %d (for C++ agents)", cfg.Server.RawQUICPort)
	}
	log.Printf("  TLS enabled: %v", cfg.TLS.Enabled)
	log.Printf("  mTLS (client auth): %v", cfg.TLS.RequireClientCert)
	log.Printf("  Neo4j URI: %s", cfg.Neo4j.URI)
	log.Printf("  Neo4j DB: %s", cfg.Neo4j.Database)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutdown signal received...")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop components
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	grpcServer.Stop()
	if rawQUICHandler != nil {
		rawQUICHandler.Stop()
	}

	// Wait for graceful shutdown or timeout
	select {
	case <-shutdownCtx.Done():
		log.Println("Shutdown timeout exceeded")
	default:
		log.Println("Server stopped gracefully")
	}
}

// loadExistingState loads existing agents and robots into the state manager
func loadExistingState(repo *db.Repository, stateManager *state.GlobalStateManager) error {
	// Load all agents
	agents, err := repo.GetAllAgents()
	if err != nil {
		return err
	}

	for _, agent := range agents {
		// Register robot (agent_id = robot_id in 1:1 model, initially offline)
		stateManager.RegisterRobot(
			agent.ID,
			agent.Name,
			agent.ID,
			agent.CurrentState,
		)
		stateManager.SetRobotOnline(agent.ID, false)

		// Register agent (will be set online when connected)
		stateManager.RegisterAgent(agent.ID, agent.Name, "")
	}

	log.Printf("Loaded %d agents from database", len(agents))

	return nil
}

// loadTLSConfig loads TLS configuration for QUIC
func loadTLSConfig(cfg *config.Config) (*tls.Config, error) {
	if !cfg.TLS.Enabled {
		return nil, fmt.Errorf("TLS not enabled")
	}

	// Load server certificate
	serverCert, err := tls.LoadX509KeyPair(cfg.TLS.ServerCert, cfg.TLS.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"fleet-agent-raw", "h3"},
	}

	// Load CA certificate for client verification (mTLS)
	if cfg.TLS.RequireClientCert && cfg.TLS.CACert != "" {
		caCert, err := os.ReadFile(cfg.TLS.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.ClientCAs = caCertPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsConfig, nil
}
